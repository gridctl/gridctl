package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
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

func TestNewDaemonManager(t *testing.T) {
	cfg := Config{Port: 8180, BasePort: 9000}
	dm := NewDaemonManager(cfg)
	if dm == nil {
		t.Fatal("expected non-nil DaemonManager")
	}
	if dm.config.Port != 8180 {
		t.Errorf("expected port 8180, got %d", dm.config.Port)
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

	// Create a skill YAML file
	skillsDir := filepath.Join(regDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("creating skills dir: %v", err)
	}
	skillYAML := `name: test-skill
description: A test skill
state: active
steps:
  - tool: some-server__some-tool
    arguments:
      key: value
`
	if err := os.WriteFile(filepath.Join(skillsDir, "test-skill.yaml"), []byte(skillYAML), 0644); err != nil {
		t.Fatalf("writing skill YAML: %v", err)
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

	// Tools should appear in aggregated list
	tools := inst.Gateway.Router().AggregatedTools()
	found := false
	for _, tool := range tools {
		if tool.Name == mcp.PrefixTool("registry", "test-skill") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tool 'registry__test-skill' in aggregated tools, got: %v", toolNames(tools))
	}

	// API server should have the registry server
	if inst.APIServer.RegistryServer() == nil {
		t.Error("expected API server to have registry server set")
	}
}

func toolNames(tools []mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
