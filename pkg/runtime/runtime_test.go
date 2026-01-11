package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"agentlab/pkg/config"
	"agentlab/pkg/logging"
)

// testLogger returns a discard logger for tests
func testLogger() *slog.Logger {
	return logging.NewDiscardLogger()
}

// MockWorkloadRuntime is a mock implementation of WorkloadRuntime for testing.
type MockWorkloadRuntime struct {
	// Error injection
	PingError        error
	StartError       error
	StopError        error
	RemoveError      error
	ExistsError      error
	ListError        error
	GetHostPortError error
	EnsureNetworkErr error
	ListNetworksErr  error
	RemoveNetworkErr error
	EnsureImageError error

	// Call tracking
	StartedWorkloads []WorkloadConfig
	StoppedWorkloads []WorkloadID
	RemovedWorkloads []WorkloadID
	CreatedNetworks  []string
	RemovedNetworks  []string
	EnsuredImages    []string

	// State
	ExistingWorkloads map[string]WorkloadID
	ListedWorkloads   []WorkloadStatus
	HostPorts         map[WorkloadID]int
}

func NewMockWorkloadRuntime() *MockWorkloadRuntime {
	return &MockWorkloadRuntime{
		ExistingWorkloads: make(map[string]WorkloadID),
		HostPorts:         make(map[WorkloadID]int),
	}
}

func (m *MockWorkloadRuntime) Start(ctx context.Context, cfg WorkloadConfig) (*WorkloadStatus, error) {
	if m.StartError != nil {
		return nil, m.StartError
	}
	m.StartedWorkloads = append(m.StartedWorkloads, cfg)
	id := WorkloadID("mock-" + cfg.Name)
	return &WorkloadStatus{
		ID:       id,
		Name:     cfg.Name,
		Topology: cfg.Topology,
		Type:     cfg.Type,
		State:    WorkloadStateRunning,
		HostPort: cfg.HostPort,
		Image:    cfg.Image,
		Labels:   cfg.Labels,
	}, nil
}

func (m *MockWorkloadRuntime) Stop(ctx context.Context, id WorkloadID) error {
	if m.StopError != nil {
		return m.StopError
	}
	m.StoppedWorkloads = append(m.StoppedWorkloads, id)
	return nil
}

func (m *MockWorkloadRuntime) Remove(ctx context.Context, id WorkloadID) error {
	if m.RemoveError != nil {
		return m.RemoveError
	}
	m.RemovedWorkloads = append(m.RemovedWorkloads, id)
	return nil
}

func (m *MockWorkloadRuntime) Status(ctx context.Context, id WorkloadID) (*WorkloadStatus, error) {
	return &WorkloadStatus{
		ID:    id,
		State: WorkloadStateRunning,
	}, nil
}

func (m *MockWorkloadRuntime) Exists(ctx context.Context, name string) (bool, WorkloadID, error) {
	if m.ExistsError != nil {
		return false, "", m.ExistsError
	}
	if id, ok := m.ExistingWorkloads[name]; ok {
		return true, id, nil
	}
	return false, "", nil
}

func (m *MockWorkloadRuntime) List(ctx context.Context, filter WorkloadFilter) ([]WorkloadStatus, error) {
	if m.ListError != nil {
		return nil, m.ListError
	}
	return m.ListedWorkloads, nil
}

func (m *MockWorkloadRuntime) GetHostPort(ctx context.Context, id WorkloadID, exposedPort int) (int, error) {
	if m.GetHostPortError != nil {
		return 0, m.GetHostPortError
	}
	if port, ok := m.HostPorts[id]; ok {
		return port, nil
	}
	return 0, nil
}

func (m *MockWorkloadRuntime) EnsureNetwork(ctx context.Context, name string, opts NetworkOptions) error {
	if m.EnsureNetworkErr != nil {
		return m.EnsureNetworkErr
	}
	m.CreatedNetworks = append(m.CreatedNetworks, name)
	return nil
}

func (m *MockWorkloadRuntime) ListNetworks(ctx context.Context, topology string) ([]string, error) {
	if m.ListNetworksErr != nil {
		return nil, m.ListNetworksErr
	}
	return m.CreatedNetworks, nil
}

func (m *MockWorkloadRuntime) RemoveNetwork(ctx context.Context, name string) error {
	if m.RemoveNetworkErr != nil {
		return m.RemoveNetworkErr
	}
	m.RemovedNetworks = append(m.RemovedNetworks, name)
	return nil
}

func (m *MockWorkloadRuntime) EnsureImage(ctx context.Context, imageName string) error {
	if m.EnsureImageError != nil {
		return m.EnsureImageError
	}
	m.EnsuredImages = append(m.EnsuredImages, imageName)
	return nil
}

func (m *MockWorkloadRuntime) Ping(ctx context.Context) error {
	return m.PingError
}

func (m *MockWorkloadRuntime) Close() error {
	return nil
}

// Ensure MockWorkloadRuntime implements WorkloadRuntime
var _ WorkloadRuntime = (*MockWorkloadRuntime)(nil)

// MockBuilder is a mock implementation of Builder for testing.
type MockBuilder struct {
	BuildError  error
	BuildResult *BuildResult
}

func (m *MockBuilder) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	if m.BuildError != nil {
		return nil, m.BuildError
	}
	if m.BuildResult != nil {
		return m.BuildResult, nil
	}
	return &BuildResult{
		ImageTag: opts.Tag,
		Cached:   false,
	}, nil
}

// Ensure MockBuilder implements Builder
var _ Builder = (*MockBuilder)(nil)

func TestOrchestrator_Up_SimpleNetwork(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:  "server1",
				Image: "mcp-server:latest",
				Port:  3000,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify network was created
	if len(mockRT.CreatedNetworks) != 1 || mockRT.CreatedNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' to be created, got %v", mockRT.CreatedNetworks)
	}

	// Verify workload was started
	if len(mockRT.StartedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(mockRT.StartedWorkloads))
	}

	// Verify result
	if len(result.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server in result, got %d", len(result.MCPServers))
	}
	if result.MCPServers[0].Name != "server1" {
		t.Errorf("expected server name 'server1', got %q", result.MCPServers[0].Name)
	}
}

func TestOrchestrator_Up_MultipleServers(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "mcp-server:latest", Port: 3000},
			{Name: "server2", Image: "another-server:latest", Port: 3001},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 2 {
		t.Errorf("expected 2 MCP servers, got %d", len(result.MCPServers))
	}

	// Verify host ports are assigned sequentially
	if result.MCPServers[0].HostPort != 9000 {
		t.Errorf("expected first server on port 9000, got %d", result.MCPServers[0].HostPort)
	}
	if result.MCPServers[1].HostPort != 9001 {
		t.Errorf("expected second server on port 9001, got %d", result.MCPServers[1].HostPort)
	}
}

func TestOrchestrator_Up_WithResources(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "mcp-server:latest", Port: 3000},
		},
		Resources: []config.Resource{
			{Name: "postgres", Image: "postgres:16", Env: map[string]string{"POSTGRES_PASSWORD": "secret"}},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 2 workloads started (1 MCP server + 1 resource)
	if len(mockRT.StartedWorkloads) != 2 {
		t.Errorf("expected 2 workloads started, got %d", len(mockRT.StartedWorkloads))
	}
}

func TestOrchestrator_Up_AdvancedNetworkMode(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Networks: []config.Network{
			{Name: "public-net", Driver: "bridge"},
			{Name: "private-net", Driver: "bridge"},
		},
		MCPServers: []config.MCPServer{
			{Name: "frontend", Image: "frontend:latest", Port: 3000, Network: "public-net"},
			{Name: "backend", Image: "backend:latest", Port: 3001, Network: "private-net"},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both networks were created
	if len(mockRT.CreatedNetworks) != 2 {
		t.Errorf("expected 2 networks created, got %d", len(mockRT.CreatedNetworks))
	}
}

func TestOrchestrator_Up_ImageEnsured(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "nginx:latest", Port: 80},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify image was ensured
	if len(mockRT.EnsuredImages) != 1 || mockRT.EnsuredImages[0] != "nginx:latest" {
		t.Errorf("expected 'nginx:latest' to be ensured, got %v", mockRT.EnsuredImages)
	}
}

func TestOrchestrator_Up_PingError(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.PingError = errors.New("runtime unavailable")
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOrchestrator_Up_NetworkCreateError(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.EnsureNetworkErr = errors.New("network create failed")
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "nginx:latest", Port: 80},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOrchestrator_Up_WorkloadStartError(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.StartError = errors.New("workload start failed")
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "nginx:latest", Port: 80},
		},
	}

	ctx := context.Background()
	_, err := orch.Up(ctx, topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOrchestrator_Down_Success(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.ListedWorkloads = []WorkloadStatus{
		{ID: "workload-1", Name: "agentlab-test-server1", Type: WorkloadTypeMCPServer},
		{ID: "workload-2", Name: "agentlab-test-postgres", Type: WorkloadTypeResource},
	}
	mockRT.CreatedNetworks = []string{"test-net"}
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	err := orch.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify workloads were stopped
	if len(mockRT.StoppedWorkloads) != 2 {
		t.Errorf("expected 2 workloads stopped, got %d", len(mockRT.StoppedWorkloads))
	}

	// Verify workloads were removed
	if len(mockRT.RemovedWorkloads) != 2 {
		t.Errorf("expected 2 workloads removed, got %d", len(mockRT.RemovedWorkloads))
	}

	// Verify networks were removed
	if len(mockRT.RemovedNetworks) != 1 {
		t.Errorf("expected 1 network removed, got %d", len(mockRT.RemovedNetworks))
	}
}

func TestOrchestrator_Down_NoWorkloads(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.ListedWorkloads = []WorkloadStatus{}
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	err := orch.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should complete without stopping/removing anything
	if len(mockRT.StoppedWorkloads) != 0 {
		t.Errorf("expected no workloads stopped, got %d", len(mockRT.StoppedWorkloads))
	}
}

func TestOrchestrator_Down_PingError(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.PingError = errors.New("runtime unavailable")
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	err := orch.Down(ctx, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOrchestrator_Down_StopError_Continues(t *testing.T) {
	// Down should continue even if stopping a workload fails
	mockRT := NewMockWorkloadRuntime()
	mockRT.ListedWorkloads = []WorkloadStatus{
		{ID: "workload-1", Name: "agentlab-test-server1"},
	}
	mockRT.StopError = errors.New("stop failed")
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	// Should not return error, just log warning
	err := orch.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOrchestrator_Status(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockRT.ListedWorkloads = []WorkloadStatus{
		{
			ID:       "1234567890123456",
			Name:     "agentlab-test-server1",
			Type:     WorkloadTypeMCPServer,
			Topology: "test",
			State:    WorkloadStateRunning,
			Message:  "Up 1 minute",
			Labels: map[string]string{
				"agentlab.mcp-server": "server1",
			},
		},
	}
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)

	ctx := context.Background()
	statuses, err := orch.Status(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Errorf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Type != WorkloadTypeMCPServer {
		t.Errorf("expected type 'mcp-server', got '%s'", statuses[0].Type)
	}
}
