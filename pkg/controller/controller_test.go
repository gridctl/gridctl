package controller

import (
	"io/fs"
	"os"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestNew(t *testing.T) {
	cfg := Config{
		StackPath: "/path/to/stack.yaml",
		Port:      8180,
		BasePort:  9000,
	}
	ctrl := New(cfg)
	if ctrl == nil {
		t.Fatal("New returned nil")
	}
}

func TestStackController_SetVersion(t *testing.T) {
	ctrl := New(Config{})
	ctrl.SetVersion("v1.0.0")
	if ctrl.version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", ctrl.version)
	}
}

func TestStackController_SetWebFS(t *testing.T) {
	ctrl := New(Config{})
	ctrl.SetWebFS(func() (fs.FS, error) {
		return nil, nil
	})
	if ctrl.webFS == nil {
		t.Error("expected webFS to be set")
	}
}

func TestBuildWorkloadSummaries_Empty(t *testing.T) {
	stack := &config.Stack{}
	result := &runtime.UpResult{}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestBuildWorkloadSummaries_MCPServers(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{Name: "http-server", Transport: "http"},
			{Name: "stdio-server", Transport: "stdio"},
			{Name: "ext-server", URL: "https://example.com"},
			{Name: "local-server", Command: []string{"./server"}},
			{Name: "ssh-server", Command: []string{"/opt/server"}, SSH: &config.SSHConfig{Host: "10.0.0.1", User: "user"}},
			{Name: "api-server", OpenAPI: &config.OpenAPIConfig{Spec: "/spec.json"}},
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "http-server"},
			{Name: "stdio-server"},
			{Name: "ext-server"},
			{Name: "local-server"},
			{Name: "ssh-server"},
			{Name: "api-server"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)

	if len(summaries) != 6 {
		t.Fatalf("expected 6 summaries, got %d", len(summaries))
	}

	expectedTransports := map[string]string{
		"http-server":  "http",
		"stdio-server": "stdio",
		"ext-server":   "external",
		"local-server": "local",
		"ssh-server":   "ssh",
		"api-server":   "openapi",
	}

	for _, s := range summaries {
		if s.Type != "mcp-server" {
			t.Errorf("expected type 'mcp-server', got '%s' for %s", s.Type, s.Name)
		}
		if s.State != "running" {
			t.Errorf("expected state 'running', got '%s' for %s", s.State, s.Name)
		}
		expected, ok := expectedTransports[s.Name]
		if !ok {
			t.Errorf("unexpected server: %s", s.Name)
			continue
		}
		if s.Transport != expected {
			t.Errorf("expected transport '%s' for %s, got '%s'", expected, s.Name, s.Transport)
		}
	}
}

func TestBuildWorkloadSummaries_DefaultTransport(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{Name: "default-server", Transport: ""}, // Empty transport defaults to "http"
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "default-server"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Transport != "http" {
		t.Errorf("expected transport 'http' for default, got '%s'", summaries[0].Transport)
	}
}

func TestBuildWorkloadSummaries_Agents(t *testing.T) {
	stack := &config.Stack{}
	result := &runtime.UpResult{
		Agents: []runtime.AgentResult{
			{Name: "agent-1"},
			{Name: "agent-2"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	for _, s := range summaries {
		if s.Type != "agent" {
			t.Errorf("expected type 'agent', got '%s'", s.Type)
		}
		if s.Transport != "container" {
			t.Errorf("expected transport 'container', got '%s'", s.Transport)
		}
	}
}

func TestBuildWorkloadSummaries_Resources(t *testing.T) {
	stack := &config.Stack{
		Resources: []config.Resource{
			{Name: "postgres"},
			{Name: "redis"},
		},
	}
	result := &runtime.UpResult{}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	for _, s := range summaries {
		if s.Type != "resource" {
			t.Errorf("expected type 'resource', got '%s'", s.Type)
		}
		if s.Transport != "container" {
			t.Errorf("expected transport 'container', got '%s'", s.Transport)
		}
	}
}

func TestBuildWorkloadSummaries_Mixed(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{Name: "server1", Transport: "http"},
		},
		Resources: []config.Resource{
			{Name: "db"},
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "server1"},
		},
		Agents: []runtime.AgentResult{
			{Name: "agent1"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}

	types := make(map[string]int)
	for _, s := range summaries {
		types[s.Type]++
	}
	if types["mcp-server"] != 1 {
		t.Errorf("expected 1 mcp-server, got %d", types["mcp-server"])
	}
	if types["agent"] != 1 {
		t.Errorf("expected 1 agent, got %d", types["agent"])
	}
	if types["resource"] != 1 {
		t.Errorf("expected 1 resource, got %d", types["resource"])
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}
	if cfg.Port != 0 {
		t.Errorf("expected default port 0 (zero value), got %d", cfg.Port)
	}
	if cfg.Verbose {
		t.Error("expected Verbose to default to false")
	}
	if cfg.DaemonChild {
		t.Error("expected DaemonChild to default to false")
	}
}

func TestCreatePrinter_Quiet(t *testing.T) {
	sc := New(Config{Quiet: true})
	stack := &config.Stack{Name: "test"}
	printer := sc.createPrinter(stack)
	if printer != nil {
		t.Error("expected nil printer when Quiet=true")
	}
}

func TestCreatePrinter_NotQuiet(t *testing.T) {
	sc := New(Config{StackPath: "/path/to/stack.yaml"})
	sc.SetVersion("v0.1.0")
	stack := &config.Stack{Name: "test"}
	printer := sc.createPrinter(stack)
	if printer == nil {
		t.Error("expected non-nil printer when Quiet=false")
	}
}

func TestCreatePrinter_Verbose(t *testing.T) {
	sc := New(Config{StackPath: "/path/to/stack.yaml", Verbose: true})
	stack := &config.Stack{Name: "test"}
	// Should not panic with verbose mode
	printer := sc.createPrinter(stack)
	if printer == nil {
		t.Error("expected non-nil printer when Verbose=true")
	}
}

func TestSetupOrchestratorLogging_ForegroundVerbose(t *testing.T) {
	sc := New(Config{Foreground: true, Verbose: true})
	rt := runtime.NewOrchestrator(nil, nil)

	logBuffer, handler := sc.setupOrchestratorLogging(rt)
	if logBuffer == nil {
		t.Error("expected non-nil logBuffer in foreground+non-quiet mode")
	}
	if handler == nil {
		t.Error("expected non-nil handler in foreground+non-quiet mode")
	}
}

func TestSetupOrchestratorLogging_ForegroundNonQuiet(t *testing.T) {
	sc := New(Config{Foreground: true, Quiet: false})
	rt := runtime.NewOrchestrator(nil, nil)

	logBuffer, handler := sc.setupOrchestratorLogging(rt)
	if logBuffer == nil {
		t.Error("expected non-nil logBuffer in foreground+non-quiet mode")
	}
	if handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestSetupOrchestratorLogging_NonQuiet(t *testing.T) {
	sc := New(Config{Foreground: false, Quiet: false})
	rt := runtime.NewOrchestrator(nil, nil)

	logBuffer, handler := sc.setupOrchestratorLogging(rt)
	// Non-foreground, non-quiet returns nil buffer but still sets logger on rt
	if logBuffer != nil {
		t.Error("expected nil logBuffer in non-foreground mode")
	}
	if handler != nil {
		t.Error("expected nil handler in non-foreground mode")
	}
}

func TestSetupOrchestratorLogging_Quiet(t *testing.T) {
	sc := New(Config{Quiet: true})
	rt := runtime.NewOrchestrator(nil, nil)

	logBuffer, handler := sc.setupOrchestratorLogging(rt)
	if logBuffer != nil {
		t.Error("expected nil logBuffer in quiet mode")
	}
	if handler != nil {
		t.Error("expected nil handler in quiet mode")
	}
}

func TestSetupOrchestratorLogging_WithVault(t *testing.T) {
	sc := New(Config{Foreground: true})
	sc.vaultStore = vault.NewStore(t.TempDir())
	rt := runtime.NewOrchestrator(nil, nil)

	logBuffer, handler := sc.setupOrchestratorLogging(rt)
	if logBuffer == nil {
		t.Error("expected non-nil logBuffer")
	}
	if handler == nil {
		t.Error("expected non-nil handler")
	}
}

func TestNewGatewayBuilder(t *testing.T) {
	cfg := Config{
		Port:     8180,
		CodeMode: true,
		NoExpand: true,
	}
	sc := &StackController{
		config:  cfg,
		version: "v1.0.0",
	}
	sc.SetWebFS(func() (fs.FS, error) { return nil, nil })
	sc.vaultStore = vault.NewStore(t.TempDir())

	stack := &config.Stack{Name: "test"}
	rt := runtime.NewOrchestrator(nil, nil)
	result := &runtime.UpResult{}

	builder := sc.newGatewayBuilder(stack, rt, result)
	if builder == nil {
		t.Fatal("expected non-nil builder")
	}
	if builder.version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", builder.version)
	}
	if builder.webFS == nil {
		t.Error("expected webFS to be set")
	}
	if builder.vaultStore == nil {
		t.Error("expected vaultStore to be set")
	}
	if builder.config.Port != 8180 {
		t.Errorf("expected port 8180, got %d", builder.config.Port)
	}
}

func TestNewVaultSetAdapter(t *testing.T) {
	store := vault.NewStore(t.TempDir())
	adapter := newVaultSetAdapter(store)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.store != store {
		t.Error("expected adapter to wrap the given store")
	}
}

func TestVaultSetAdapter_Get_NotFound(t *testing.T) {
	store := vault.NewStore(t.TempDir())
	adapter := newVaultSetAdapter(store)

	_, found := adapter.Get("nonexistent")
	if found {
		t.Error("expected found=false for nonexistent key")
	}
}

func TestVaultSetAdapter_GetSetSecrets_Empty(t *testing.T) {
	store := vault.NewStore(t.TempDir())
	adapter := newVaultSetAdapter(store)

	secrets := adapter.GetSetSecrets("nonexistent-set")
	if len(secrets) != 0 {
		t.Errorf("expected 0 secrets, got %d", len(secrets))
	}
}

func TestVaultSetAdapter_GetSetSecrets_WithSecrets(t *testing.T) {
	dir := t.TempDir()
	// Write a secrets.json file that the vault store will load
	secretsJSON := `[
		{"key":"DB_PASSWORD","value":"secret123","set":"database"},
		{"key":"DB_HOST","value":"localhost","set":"database"},
		{"key":"API_KEY","value":"key456","set":"api"}
	]`
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(dir+"/secrets.json", []byte(secretsJSON), 0644); err != nil {
		t.Fatalf("writing secrets.json: %v", err)
	}

	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("loading vault: %v", err)
	}

	adapter := newVaultSetAdapter(store)

	// Get secrets for the "database" set
	secrets := adapter.GetSetSecrets("database")
	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}

	// Verify the secrets are config.VaultSecret type with correct values
	secretMap := make(map[string]string)
	for _, s := range secrets {
		secretMap[s.Key] = s.Value
	}
	if secretMap["DB_PASSWORD"] != "secret123" {
		t.Errorf("expected DB_PASSWORD=secret123, got %s", secretMap["DB_PASSWORD"])
	}
	if secretMap["DB_HOST"] != "localhost" {
		t.Errorf("expected DB_HOST=localhost, got %s", secretMap["DB_HOST"])
	}
}

func TestVaultSetAdapter_Get_WithValue(t *testing.T) {
	dir := t.TempDir()
	secretsJSON := `[{"key":"MY_SECRET","value":"hello"}]`
	if err := os.WriteFile(dir+"/secrets.json", []byte(secretsJSON), 0644); err != nil {
		t.Fatalf("writing secrets.json: %v", err)
	}

	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("loading vault: %v", err)
	}

	adapter := newVaultSetAdapter(store)
	val, found := adapter.Get("MY_SECRET")
	if !found {
		t.Error("expected found=true")
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got '%s'", val)
	}
}

func TestBuildWorkloadSummaries_OnlyResources(t *testing.T) {
	stack := &config.Stack{
		Resources: []config.Resource{
			{Name: "pg"},
			{Name: "redis"},
			{Name: "minio"},
		},
	}
	result := &runtime.UpResult{}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}
	for _, s := range summaries {
		if s.Type != "resource" {
			t.Errorf("expected type 'resource', got '%s' for %s", s.Type, s.Name)
		}
	}
}

func TestBuildWorkloadSummaries_ServerNotInConfig(t *testing.T) {
	// MCPServer result has a name that doesn't match any config entry
	stack := &config.Stack{
		MCPServers: []config.MCPServer{},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "orphan-server"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	// Transport defaults to "http" via the fallback for empty transport
	// But since the config entry is missing, serverTransports won't have this key,
	// so the transport stays empty string
	if summaries[0].Transport != "" {
		t.Errorf("expected empty transport for orphan server, got '%s'", summaries[0].Transport)
	}
}
