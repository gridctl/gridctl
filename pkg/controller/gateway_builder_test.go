package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestGatewayBuilder_BuildLogging_Fresh(t *testing.T) {
	cfg := Config{Verbose: true}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	logBuffer, handler, _ := builder.buildLogging(true)
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

	logBuffer, handler, _ := builder.buildLogging(false)
	if logBuffer != existingBuffer {
		t.Error("expected existing buffer to be returned")
	}
	if handler != existingHandler {
		t.Error("expected existing handler to be returned")
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

// TestGatewayBuilder_Build_WiresTSDispatcherForExternalCallers is the
// regression test for the wiring gap that hid TypeScript skills from
// MCP clients. Before the fix, the registry server's TSDispatcher was
// never installed by the builder, so a skill.ts dropped on disk loaded
// into the registry's store but produced "not a registered tool" when
// any external client called it. The test drops the minimal
// SKILL.md + skill.ts pair the registry walker recognises, builds the
// gateway, and asserts:
//
//  1. Gateway.Router().AggregatedTools() lists the skill (proves
//     SetTSDispatcher ran before the router refreshed its tool list).
//  2. Calling RegistryServer.CallTool dispatches into the sandbox and
//     returns the skill's resolved value (proves the dispatcher's
//     bindings provider produces a usable Bindings struct).
//
// The skill itself does not call llm() / tool(), so the dispatcher's
// nil-when-unwired ChatModel is fine.
func TestGatewayBuilder_Build_WiresTSDispatcherForExternalCallers(t *testing.T) {
	regDir := t.TempDir()
	skillDir := filepath.Join(regDir, "skills", "echo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}
	skillMD := `---
name: echo
description: ts echo skill
state: active
---

Echo input back.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
	skillTS := `export default async function (i: any) { return { echoed: i }; }`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.ts"), []byte(skillTS), 0o644); err != nil {
		t.Fatalf("writing skill.ts: %v", err)
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

	// Surface check: the TS skill must appear in the aggregated tool
	// list under the gateway's "registry__" prefix. Before the fix, the
	// registry's Tools() filtered TS skills out when the dispatcher was
	// nil — so the absence of this entry was the load-bearing user-
	// visible bug.
	wantPrefixed := mcp.PrefixTool("registry", "echo")
	found := false
	for _, tl := range inst.Gateway.Router().AggregatedTools() {
		if tl.Name == wantPrefixed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("aggregated tools missing %q (TS dispatcher not wired)", wantPrefixed)
	}

	// Execution check: dispatch through the registry server's CallTool
	// — this is the path Gateway.HandleToolCall hits for an external
	// MCP client. A successful echo proves the sandbox+dispatcher chain
	// is reachable end-to-end.
	res, err := inst.RegistryServer.CallTool(context.Background(), "echo", map[string]any{"v": 7})
	if err != nil {
		t.Fatalf("RegistryServer.CallTool(echo): %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("CallTool returned no content")
	}
	got := res.Content[0].Text
	if !strings.Contains(got, `"echoed":`) || !strings.Contains(got, `"v":7`) {
		t.Errorf("CallTool result = %q, want JSON containing echoed.v=7", got)
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

	logBuffer, handler, _ := builder.buildLogging(false)
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

	logBuffer, handler, _ := builder.buildLogging(false)
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

	logBuffer, handler, _ := builder.buildLogging(true)
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


// TestGatewayBuilder_PersistedLogsArriveOnDisk drives a record through the
// canonical pkg/mcp/gateway pattern (clientLogger := g.logger.With("server", name))
// and asserts the per-server logs.jsonl receives the entry. Locks in the
// router-side fix that recognizes "server" as a routing key.
func TestGatewayBuilder_PersistedLogsArriveOnDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	regDir := t.TempDir()
	stack := &config.Stack{
		Name: "teststack",
		Telemetry: &config.TelemetryConfig{
			Persist: config.TelemetryPersistence{Logs: true},
		},
		MCPServers: []config.MCPServer{
			{Name: "github"},
		},
	}
	cfg := Config{Port: 8181}
	rt := runtime.NewOrchestrator(nil, nil)
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", rt, &runtime.UpResult{})
	builder.registryDir = regDir

	inst, err := builder.Build(false)
	if err != nil {
		t.Fatalf("Build() returned error: %v", err)
	}

	// Mirror gateway.go:900: clientLogger := logger.With("server", name).
	clientLogger := slog.New(inst.Handler).With("server", "github")
	clientLogger.Info("server registered", "transport", "stdio")

	// Lumberjack writes through synchronously inside slog handler, so the
	// file should be non-empty by the time Handle returns. Read it and
	// verify the message round-trips through JSON.
	path := state.TelemetryServerPath(stack.Name, "github", "logs")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var entries []map[string]any
	for scanner.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("malformed json line %q: %v", scanner.Text(), err)
		}
		entries = append(entries, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("logs.jsonl is empty — record was not routed to disk via server attr")
	}
	if got := entries[0]["msg"]; got != "server registered" {
		t.Errorf("msg = %v, want %q", got, "server registered")
	}
	if got := entries[0]["server"]; got != "github" {
		t.Errorf("server attr lost on disk: %v", entries[0])
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

