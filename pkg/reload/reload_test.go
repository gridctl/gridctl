package reload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// mockWorkloadRuntime implements runtime.WorkloadRuntime for testing.
type mockWorkloadRuntime struct {
	startFn  func(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error)
	existsFn func(ctx context.Context, name string) (bool, runtime.WorkloadID, error)
}

func newMockWorkloadRuntime() *mockWorkloadRuntime {
	return &mockWorkloadRuntime{
		startFn: func(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
			return &runtime.WorkloadStatus{
				ID:       runtime.WorkloadID("mock-" + cfg.Name),
				Name:     cfg.Name,
				State:    runtime.WorkloadStateRunning,
				HostPort: cfg.HostPort,
			}, nil
		},
		existsFn: func(ctx context.Context, name string) (bool, runtime.WorkloadID, error) {
			return false, "", nil
		},
	}
}

func (m *mockWorkloadRuntime) Start(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
	return m.startFn(ctx, cfg)
}
func (m *mockWorkloadRuntime) Stop(ctx context.Context, id runtime.WorkloadID) error   { return nil }
func (m *mockWorkloadRuntime) Remove(ctx context.Context, id runtime.WorkloadID) error { return nil }
func (m *mockWorkloadRuntime) Status(ctx context.Context, id runtime.WorkloadID) (*runtime.WorkloadStatus, error) {
	return &runtime.WorkloadStatus{ID: id, State: runtime.WorkloadStateRunning}, nil
}
func (m *mockWorkloadRuntime) Exists(ctx context.Context, name string) (bool, runtime.WorkloadID, error) {
	return m.existsFn(ctx, name)
}
func (m *mockWorkloadRuntime) List(ctx context.Context, filter runtime.WorkloadFilter) ([]runtime.WorkloadStatus, error) {
	return nil, nil
}
func (m *mockWorkloadRuntime) GetHostPort(ctx context.Context, id runtime.WorkloadID, exposedPort int) (int, error) {
	return 0, nil
}
func (m *mockWorkloadRuntime) EnsureNetwork(ctx context.Context, name string, opts runtime.NetworkOptions) error {
	return nil
}
func (m *mockWorkloadRuntime) ListNetworks(ctx context.Context, stack string) ([]string, error) {
	return nil, nil
}
func (m *mockWorkloadRuntime) RemoveNetwork(ctx context.Context, name string) error { return nil }
func (m *mockWorkloadRuntime) EnsureImage(ctx context.Context, imageName string) error {
	return nil
}
func (m *mockWorkloadRuntime) Ping(ctx context.Context) error { return nil }
func (m *mockWorkloadRuntime) Close() error                   { return nil }

// mockBuilder implements runtime.Builder for testing.
type mockBuilder struct{}

func (m *mockBuilder) Build(ctx context.Context, opts runtime.BuildOptions) (*runtime.BuildResult, error) {
	return &runtime.BuildResult{ImageTag: opts.Tag}, nil
}

func writeStackFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}

func setupHandler(t *testing.T, stackPath string, cfg *config.Stack) (*Handler, *mockWorkloadRuntime) {
	t.Helper()

	mockRT := newMockWorkloadRuntime()
	orch := runtime.NewOrchestrator(mockRT, &mockBuilder{})
	gw := mcp.NewGateway()

	h := NewHandler(stackPath, cfg, gw, orch, 8180, 9000, nil, nil)
	return h, mockRT
}

func TestNewHandler(t *testing.T) {
	cfg := &config.Stack{Name: "test"}
	gw := mcp.NewGateway()
	mockRT := newMockWorkloadRuntime()
	orch := runtime.NewOrchestrator(mockRT, &mockBuilder{})

	h := NewHandler("/path/to/stack.yaml", cfg, gw, orch, 8180, 9000, nil, nil)
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
	if h.stackPath != "/path/to/stack.yaml" {
		t.Errorf("expected stack path '/path/to/stack.yaml', got %q", h.stackPath)
	}
	if h.port != 8180 {
		t.Errorf("expected port 8180, got %d", h.port)
	}
}

func TestHandler_SettersAndGetters(t *testing.T) {
	cfg := &config.Stack{Name: "test"}
	gw := mcp.NewGateway()
	mockRT := newMockWorkloadRuntime()
	orch := runtime.NewOrchestrator(mockRT, &mockBuilder{})

	h := NewHandler("/path", cfg, gw, orch, 8180, 9000, nil, nil)

	// SetNoExpand
	h.SetNoExpand(true)
	if !h.noExpand {
		t.Error("expected noExpand to be true")
	}

	// SetRegisterServerFunc
	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		return nil
	})
	if h.registerServer == nil {
		t.Error("expected registerServer to be set")
	}

	// SetLogger with nil should not panic
	h.SetLogger(nil)

	// CurrentConfig
	if h.CurrentConfig() != cfg {
		t.Error("CurrentConfig should return the initial config")
	}
}

func TestHandler_Reload_NoChanges(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
`
	stackPath := writeStackFile(t, content)

	initialCfg, err := config.LoadStack(stackPath)
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	if result.Message != "no changes detected" {
		t.Errorf("expected 'no changes detected', got %q", result.Message)
	}
}

func TestHandler_Reload_ConfigLoadFailure(t *testing.T) {
	stackPath := writeStackFile(t, "invalid: yaml: content: [")

	cfg := &config.Stack{Name: "test", Network: config.Network{Name: "net"}}
	h, _ := setupHandler(t, stackPath, cfg)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected Go error (should return result error): %v", err)
	}
	if result.Success {
		t.Error("expected failure for invalid config")
	}
	if result.Message == "" {
		t.Error("expected error message")
	}
}

func TestHandler_Reload_NetworkChanged(t *testing.T) {
	content := `
name: test
network:
  name: new-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "old-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "server1", Image: "alpine:latest", Port: 3000}},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure for network change")
	}
	if result.Message == "" {
		t.Error("expected error message about network change")
	}
}

func TestHandler_Reload_MCPServerAdded(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
  - name: server2
    image: nginx:latest
    port: 3001
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "server1", Image: "alpine:latest", Port: 3000}},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	registerCalled := false
	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		registerCalled = true
		return nil
	})

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	if len(result.Added) != 1 || result.Added[0] != "mcp-server:server2" {
		t.Errorf("expected [mcp-server:server2], got %v", result.Added)
	}
	if !registerCalled {
		t.Error("expected registerServer callback to be called")
	}
}

func TestHandler_Reload_MCPServerRemoved(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:    "test",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine:latest", Port: 3000},
			{Name: "server2", Image: "nginx:latest", Port: 3001},
		},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "mcp-server:server2" {
		t.Errorf("expected [mcp-server:server2] removed, got %v", result.Removed)
	}
}

func TestHandler_Reload_MCPServerModified(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: nginx:latest
    port: 3000
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "server1", Image: "alpine:latest", Port: 3000}},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	registerCalled := false
	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		registerCalled = true
		return nil
	})

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	if len(result.Modified) != 1 || result.Modified[0] != "mcp-server:server1" {
		t.Errorf("expected [mcp-server:server1] modified, got %v", result.Modified)
	}
	if !registerCalled {
		t.Error("expected registerServer to be called for modified server")
	}
}

func TestHandler_Reload_ResourceAddedAndRemoved(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
resources:
  - name: redis
    image: redis:7
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "server1", Image: "alpine:latest", Port: 3000}},
		Resources:  []config.Resource{{Name: "postgres", Image: "postgres:16"}},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}

	assertContains(t, result.Removed, "resource:postgres")
	assertContains(t, result.Added, "resource:redis")
}


func TestHandler_Reload_PartialFailure(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
  - name: server2
    image: nginx:latest
    port: 3001
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "server1", Image: "alpine:latest", Port: 3000}},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		return fmt.Errorf("registration failed")
	})

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Error("expected partial failure errors")
	}
}

func TestHandler_StopAndRemoveContainer_NonExistent(t *testing.T) {
	mockRT := newMockWorkloadRuntime()
	orch := runtime.NewOrchestrator(mockRT, &mockBuilder{})
	gw := mcp.NewGateway()
	cfg := &config.Stack{Name: "test"}

	h := NewHandler("/path", cfg, gw, orch, 8180, 9000, nil, nil)

	err := h.stopAndRemoveContainer(context.Background(), "gridctl-test-nonexistent")
	if err != nil {
		t.Errorf("expected nil for non-existent container, got: %v", err)
	}
}

func TestHandler_AllocatePort(t *testing.T) {
	mockRT := newMockWorkloadRuntime()
	orch := runtime.NewOrchestrator(mockRT, &mockBuilder{})
	gw := mcp.NewGateway()
	cfg := &config.Stack{Name: "test"}

	h := NewHandler("/path", cfg, gw, orch, 8180, 9000, nil, nil)

	port := h.allocatePort(context.Background())
	if port != 9000 {
		t.Errorf("expected port 9000, got %d", port)
	}
}

func TestContainerName(t *testing.T) {
	name := containerName("mystack", "myserver")
	if name != "gridctl-mystack-myserver" {
		t.Errorf("expected 'gridctl-mystack-myserver', got %q", name)
	}
}

func TestHandler_Reload_ExternalServerNoContainer(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    url: http://example.com
    transport: http
  - name: server2
    url: http://new-example.com
    transport: http
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:    "test",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", URL: "http://example.com", Transport: "http"},
		},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	registerCalls := 0
	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, hostPort int) error {
		registerCalls++
		return nil
	})

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	if registerCalls != 1 {
		t.Errorf("expected registerServer called once for new external server, got %d", registerCalls)
	}
}

func TestHandler_Reload_ResourceModified(t *testing.T) {
	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
resources:
  - name: postgres
    image: postgres:17
`
	stackPath := writeStackFile(t, content)

	initialCfg := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "server1", Image: "alpine:latest", Port: 3000}},
		Resources:  []config.Resource{{Name: "postgres", Image: "postgres:16"}},
	}

	h, _ := setupHandler(t, stackPath, initialCfg)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}
	assertContains(t, result.Modified, "resource:postgres")
}


// mockVault implements config.VaultLookup for testing.
type mockVault struct {
	secrets map[string]string
}

func (m *mockVault) Get(key string) (string, bool) {
	v, ok := m.secrets[key]
	return v, ok
}

func TestHandler_Reload_WithVault(t *testing.T) {
	vault := &mockVault{secrets: map[string]string{
		"DB_PASSWORD": "secret-from-vault",
	}}

	content := `
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      DB_PASS: "${vault:DB_PASSWORD}"
`
	stackPath := writeStackFile(t, content)

	initialCfg, err := config.LoadStack(stackPath, config.WithVault(vault))
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}

	mockRT := newMockWorkloadRuntime()
	orch := runtime.NewOrchestrator(mockRT, &mockBuilder{})
	gw := mcp.NewGateway()

	h := NewHandler(stackPath, initialCfg, gw, orch, 8180, 9000, vault, nil)

	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got: %s", result.Message)
	}

	// Verify the reloaded config has vault secrets resolved
	reloaded := h.CurrentConfig()
	if reloaded.MCPServers[0].Env["DB_PASS"] != "secret-from-vault" {
		t.Errorf("expected vault secret resolved, got %q", reloaded.MCPServers[0].Env["DB_PASS"])
	}
}

func assertContains(t *testing.T, items []string, expected string) {
	t.Helper()
	for _, item := range items {
		if item == expected {
			return
		}
	}
	t.Errorf("expected %q in %v", expected, items)
}
