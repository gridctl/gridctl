package controller

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestGatewayBuilder_BuildLogging_Fresh(t *testing.T) {
	cfg := Config{Verbose: true}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	logBuffer, handler := builder.buildLogging(true)
	if logBuffer == nil {
		t.Fatal("expected logBuffer to be non-nil")
	}
	if handler == nil {
		t.Fatal("expected handler to be non-nil")
	}
}

func TestGatewayBuilder_BuildLogging_Existing(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	existingBuffer := logging.NewLogBuffer(100)
	existingHandler := logging.NewRedactingHandler(logging.NewBufferHandler(existingBuffer, nil))
	builder.SetExistingLogInfra(existingBuffer, existingHandler)

	logBuffer, handler := builder.buildLogging(false)
	if logBuffer != existingBuffer {
		t.Error("expected existing buffer to be returned")
	}
	if handler != existingHandler {
		t.Error("expected existing handler to be returned")
	}
}

func TestGatewayBuilder_BuildA2AGateway_NoA2A(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name:    "test",
		Agents:  []config.Agent{{Name: "agent1"}},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	gw := builder.buildA2AGateway(handler)
	if gw != nil {
		t.Error("expected nil A2A gateway when no A2A agents")
	}
}

func TestGatewayBuilder_BuildA2AGateway_WithLocalA2A(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "agent1",
				A2A: &config.A2AConfig{
					Enabled: true,
					Skills:  []config.A2ASkill{{ID: "s1", Name: "Skill One"}},
				},
			},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	gw := builder.buildA2AGateway(handler)
	if gw == nil {
		t.Error("expected A2A gateway when agents have A2A config")
	}
}

func TestGatewayBuilder_BuildA2AGateway_WithExternalA2A(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		A2AAgents: []config.A2AAgent{
			{Name: "remote-agent", URL: "https://example.com/agent"},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	gw := builder.buildA2AGateway(handler)
	if gw == nil {
		t.Error("expected A2A gateway when external A2A agents exist")
	}
}

func TestGatewayBuilder_SetVersion(t *testing.T) {
	builder := NewGatewayBuilder(Config{}, &config.Stack{}, "", nil, &runtime.UpResult{})
	builder.SetVersion("v0.1.0")
	if builder.version != "v0.1.0" {
		t.Errorf("expected version 'v0.1.0', got '%s'", builder.version)
	}
}

func TestGatewayBuilder_Build_WithEmptyRegistry(t *testing.T) {
	regDir := t.TempDir() // Empty directory — no prompts or skills

	cfg := Config{Port: 8180}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	if inst.RegistryServer == nil {
		t.Fatal("expected RegistryServer to be non-nil")
	}
	if inst.RegistryServer.HasContent() {
		t.Error("expected empty registry to have no content")
	}

	// Registry should NOT be in the router (progressive disclosure)
	client := inst.Gateway.Router().GetClient("registry")
	if client != nil {
		t.Error("expected registry to NOT be registered in router when empty")
	}

	// API server should have the registry server
	if inst.APIServer.RegistryServer() == nil {
		t.Error("expected API server to have registry server set")
	}
}

func TestGatewayBuilder_Build_WithPopulatedRegistry(t *testing.T) {
	regDir := t.TempDir()

	// Create a SKILL.md file in directory-based layout
	skillDir := filepath.Join(regDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}
	skillMD := `---
name: test-skill
description: A test skill
state: active
---

# Test Skill

Execute some-server__some-tool with key=value.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}

	cfg := Config{Port: 8180}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	if inst.RegistryServer == nil {
		t.Fatal("expected RegistryServer to be non-nil")
	}
	if !inst.RegistryServer.HasContent() {
		t.Error("expected populated registry to have content")
	}

	// Registry SHOULD be in the router (progressive disclosure — content present)
	client := inst.Gateway.Router().GetClient("registry")
	if client == nil {
		t.Fatal("expected registry to be registered in router when it has content")
	}

	// Registry should NOT expose tools — skills are served as prompts/resources
	tools := inst.Gateway.Router().AggregatedTools()
	for _, tool := range tools {
		if tool.Name == mcp.PrefixTool("registry", "test-skill") {
			t.Error("registry should not expose skills as tools")
		}
	}

	// Skills should be available as prompts
	prompts := inst.RegistryServer.ListPromptData()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "test-skill" {
		t.Errorf("prompt name = %q, want %q", prompts[0].Name, "test-skill")
	}

	// API server should have the registry server
	if inst.APIServer.RegistryServer() == nil {
		t.Error("expected API server to have registry server set")
	}
}

func TestGatewayBuilder_BuildLogging_DaemonChild(t *testing.T) {
	cfg := Config{DaemonChild: true}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	logBuffer, handler := builder.buildLogging(false)
	if logBuffer == nil {
		t.Fatal("expected non-nil logBuffer")
	}
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestGatewayBuilder_BuildLogging_NeitherVerboseNorDaemon(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	logBuffer, handler := builder.buildLogging(false)
	if logBuffer == nil {
		t.Fatal("expected non-nil logBuffer")
	}
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestGatewayBuilder_BuildLogging_WithVaultStore(t *testing.T) {
	cfg := Config{Verbose: true}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	builder.SetVaultStore(vault.NewStore(t.TempDir()))

	logBuffer, handler := builder.buildLogging(true)
	if logBuffer == nil {
		t.Fatal("expected non-nil logBuffer")
	}
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestGatewayBuilder_SetWebFS(t *testing.T) {
	builder := NewGatewayBuilder(Config{}, &config.Stack{}, "", nil, &runtime.UpResult{})
	builder.SetWebFS(func() (fs.FS, error) { return nil, nil })
	if builder.webFS == nil {
		t.Error("expected webFS to be set")
	}
}

func TestGatewayBuilder_SetVaultStore(t *testing.T) {
	builder := NewGatewayBuilder(Config{}, &config.Stack{}, "", nil, &runtime.UpResult{})
	store := vault.NewStore(t.TempDir())
	builder.SetVaultStore(store)
	if builder.vaultStore != store {
		t.Error("expected vaultStore to be set")
	}
}

func TestGatewayBuilder_Build_CodeModeFromCLI(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180, CodeMode: true}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.Gateway.CodeModeStatus() != "on" {
		t.Errorf("expected code mode 'on', got '%s'", inst.Gateway.CodeModeStatus())
	}
}

func TestGatewayBuilder_Build_CodeModeFromStack(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Gateway: &config.GatewayConfig{
			CodeMode:        "on",
			CodeModeTimeout: 60,
		},
	}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.Gateway.CodeModeStatus() != "on" {
		t.Errorf("expected code mode 'on', got '%s'", inst.Gateway.CodeModeStatus())
	}
}

func TestGatewayBuilder_Build_NoCodeMode(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.Gateway.CodeModeStatus() != "off" {
		t.Errorf("expected code mode 'off', got '%s'", inst.Gateway.CodeModeStatus())
	}
}

func TestGatewayBuilder_Build_WithAllowedOrigins(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Gateway: &config.GatewayConfig{
			AllowedOrigins: []string{"https://example.com"},
		},
	}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.APIServer == nil {
		t.Fatal("expected non-nil APIServer")
	}
}

func TestGatewayBuilder_Build_WithAuth(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Gateway: &config.GatewayConfig{
			Auth: &config.AuthConfig{
				Type:  "bearer",
				Token: "secret",
			},
		},
	}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.APIServer == nil {
		t.Fatal("expected non-nil APIServer")
	}
}

func TestGatewayBuilder_Build_HTTPServer(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 9999}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.HTTPServer == nil {
		t.Fatal("expected non-nil HTTPServer")
	}
	if inst.HTTPServer.Addr != ":9999" {
		t.Errorf("expected addr ':9999', got '%s'", inst.HTTPServer.Addr)
	}
}

func TestGatewayBuilder_Build_WithVault(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir
	builder.SetVaultStore(vault.NewStore(t.TempDir()))

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.APIServer == nil {
		t.Fatal("expected non-nil APIServer")
	}
}

func TestGatewayBuilder_Build_WebFSError_Verbose(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir
	builder.SetWebFS(func() (fs.FS, error) {
		return nil, fmt.Errorf("no embedded web UI")
	})

	// Build with verbose=true to trigger the warning branch
	inst, err := builder.Build(true)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.Gateway == nil {
		t.Fatal("expected non-nil Gateway")
	}
}

func TestGatewayBuilder_Build_WebFSSuccess(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir
	builder.SetWebFS(func() (fs.FS, error) {
		return os.DirFS(t.TempDir()), nil
	})

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.APIServer == nil {
		t.Fatal("expected non-nil APIServer")
	}
}

func TestGatewayBuilder_Build_WithA2AGateway(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "a2a-agent",
				A2A: &config.A2AConfig{
					Enabled: true,
					Skills: []config.A2ASkill{
						{ID: "s1", Name: "Skill One"},
					},
				},
			},
		},
	}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}
	if inst.A2AGateway == nil {
		t.Fatal("expected non-nil A2AGateway for A2A-enabled agents")
	}
}

func TestNewGatewayBuilder_Fields(t *testing.T) {
	cfg := Config{Port: 8080, NoExpand: true}
	stack := &config.Stack{Name: "mystack"}
	rt := runtime.NewOrchestrator(nil, nil)
	result := &runtime.UpResult{}

	b := NewGatewayBuilder(cfg, stack, "/path/to/stack.yaml", rt, result)
	if b.config.Port != 8080 {
		t.Errorf("expected port 8080, got %d", b.config.Port)
	}
	if b.stackPath != "/path/to/stack.yaml" {
		t.Errorf("expected stackPath '/path/to/stack.yaml', got '%s'", b.stackPath)
	}
	if b.stack.Name != "mystack" {
		t.Errorf("expected stack name 'mystack', got '%s'", b.stack.Name)
	}
}

func TestGatewayBuilder_RegisterAgents_Empty(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{Name: "test"}
	result := &runtime.UpResult{} // No agents
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, result)

	gw := mcp.NewGateway()
	// Should not panic or add any agents
	builder.registerAgents(gw, false)
	builder.registerAgents(gw, true)
}

func TestGatewayBuilder_RegisterAgents_WithAgents(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{Name: "test"}
	result := &runtime.UpResult{
		Agents: []runtime.AgentResult{
			{
				Name: "agent1",
				Uses: []config.ToolSelector{
					{Server: "server1"},
					{Server: "server2", Tools: []string{"read"}},
				},
			},
			{Name: "agent2"},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, result)

	gw := mcp.NewGateway()
	builder.registerAgents(gw, true)

	if !gw.HasAgent("agent1") {
		t.Error("expected agent1 to be registered")
	}
	if !gw.HasAgent("agent2") {
		t.Error("expected agent2 to be registered")
	}
}

func TestGatewayBuilder_PrintEndpoints(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8888}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	// Should not panic
	builder.printEndpoints(inst)
}

func TestGatewayBuilder_PrintEndpoints_WithA2A(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8888}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "a2a-agent",
				A2A:  &config.A2AConfig{Enabled: true},
			},
		},
	}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	// Should not panic, and should print A2A endpoints
	builder.printEndpoints(inst)
}

func TestGatewayBuilder_RegisterA2AAgents_NilGateway(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	// Should return early without panicking
	builder.registerA2AAgents(context.Background(), nil, handler, false)
}

func TestGatewayBuilder_RegisterA2AAgents_WithAgents(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "local-a2a",
				A2A: &config.A2AConfig{
					Enabled: true,
					Version: "2.0.0",
					Skills: []config.A2ASkill{
						{ID: "s1", Name: "Skill One", Description: "First skill", Tags: []string{"test"}},
					},
				},
			},
			{Name: "non-a2a-agent"}, // Should be skipped
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	// Create a real A2A gateway
	a2aGW := builder.buildA2AGateway(handler)
	if a2aGW == nil {
		t.Fatal("expected non-nil A2A gateway")
	}

	// Should register agents (verbose for coverage)
	builder.registerA2AAgents(context.Background(), a2aGW, handler, true)
}

func TestGatewayBuilder_RegisterA2AAgents_DefaultVersion(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "agent-no-version",
				A2A: &config.A2AConfig{
					Enabled: true,
					// Version not set - should default to "1.0.0"
				},
			},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	a2aGW := builder.buildA2AGateway(handler)
	builder.registerA2AAgents(context.Background(), a2aGW, handler, false)
}

func TestGatewayBuilder_RegisterAgentAdapters_NoA2AAgents(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{
		Name:   "test",
		Agents: []config.Agent{{Name: "simple-agent"}},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	gw := mcp.NewGateway()
	err := builder.registerAgentAdapters(context.Background(), gw, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayBuilder_RegisterAgentAdapters_WithUsedAgents(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "provider-agent",
				A2A: &config.A2AConfig{
					Enabled: true,
					Version: "1.0.0",
					Skills: []config.A2ASkill{
						{ID: "s1", Name: "Skill", Description: "A skill"},
					},
				},
			},
			{
				Name: "consumer-agent",
				Uses: []config.ToolSelector{
					{Server: "provider-agent"}, // Using the A2A agent
				},
			},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	gw := mcp.NewGateway()
	err := builder.registerAgentAdapters(context.Background(), gw, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayBuilder_SetupHotReload_NoWatch(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180, Watch: false}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))
	registrar := NewServerRegistrar(inst.Gateway, false)

	// Should set up reload handler but not start watcher
	builder.setupHotReload(context.Background(), inst, registrar, handler, false)
}

func TestGatewayBuilder_SetupHotReload_NoWatch_Verbose(t *testing.T) {
	regDir := t.TempDir()
	cfg := Config{Port: 8180, Watch: false, NoExpand: true}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))
	registrar := NewServerRegistrar(inst.Gateway, false)

	// Setup with verbose=true for additional print coverage
	builder.setupHotReload(context.Background(), inst, registrar, handler, true)
}

func TestGatewayBuilder_SetupHotReload_WithWatch(t *testing.T) {
	regDir := t.TempDir()
	// Create a temporary stack file for the watcher
	stackFile := filepath.Join(regDir, "stack.yaml")
	if err := os.WriteFile(stackFile, []byte("name: test\n"), 0644); err != nil {
		t.Fatalf("writing stack file: %v", err)
	}

	cfg := Config{Port: 8180, Watch: true}
	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, stackFile, rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))
	registrar := NewServerRegistrar(inst.Gateway, false)

	ctx, cancel := context.WithCancel(context.Background())
	// Should set up reload handler and start watcher
	builder.setupHotReload(ctx, inst, registrar, handler, true)
	cancel() // Stop the watcher
}

func TestGatewayBuilder_RegisterA2AAgents_ExternalAgents(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		A2AAgents: []config.A2AAgent{
			{
				Name: "external-a2a",
				URL:  "http://127.0.0.1:1/nonexistent", // Will fail to connect
			},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	// Need an A2A-enabled agent to trigger buildA2AGateway
	stack.Agents = append(stack.Agents, config.Agent{
		Name: "trigger-a2a",
		A2A:  &config.A2AConfig{Enabled: true},
	})
	a2aGW := builder.buildA2AGateway(handler)
	if a2aGW == nil {
		t.Fatal("expected non-nil A2A gateway")
	}

	// Should handle registration failure gracefully (verbose for warning coverage)
	builder.registerA2AAgents(context.Background(), a2aGW, handler, true)
}

func TestGatewayBuilder_RegisterA2AAgents_ExternalWithAuth(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		A2AAgents: []config.A2AAgent{
			{
				Name: "auth-a2a",
				URL:  "http://127.0.0.1:1/nonexistent",
				Auth: &config.A2AAuth{
					Type:       "bearer",
					TokenEnv:   "TEST_A2A_TOKEN_NONEXISTENT",
					HeaderName: "Authorization",
				},
			},
		},
		Agents: []config.Agent{
			{Name: "trigger", A2A: &config.A2AConfig{Enabled: true}},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	a2aGW := builder.buildA2AGateway(handler)
	if a2aGW == nil {
		t.Fatal("expected non-nil A2A gateway")
	}

	// Should handle auth and registration failure gracefully
	builder.registerA2AAgents(context.Background(), a2aGW, handler, false)
}

func TestGatewayBuilder_RegisterAgentAdapters_NoUsedAgents(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "a2a-agent",
				A2A: &config.A2AConfig{
					Enabled: true,
				},
			},
			{
				Name: "other-agent",
				Uses: []config.ToolSelector{
					{Server: "external-server"}, // Not using the A2A agent
				},
			},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	gw := mcp.NewGateway()
	err := builder.registerAgentAdapters(context.Background(), gw, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
