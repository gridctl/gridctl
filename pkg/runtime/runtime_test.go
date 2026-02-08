package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"go.uber.org/mock/gomock"
)

// testLogger returns a discard logger for tests
func testLogger() *slog.Logger {
	return logging.NewDiscardLogger()
}

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

// setupOrchestratorMock configures a MockWorkloadRuntime with default behaviors
// that match the old hand-rolled mock for orchestrator tests.
// Returns the mock and tracking slices for verification.
type runtimeTracker struct {
	startedWorkloads []WorkloadConfig
	stoppedWorkloads []WorkloadID
	removedWorkloads []WorkloadID
	createdNetworks  []string
	removedNetworks  []string
	ensuredImages    []string
}

func setupDefaultRuntime(ctrl *gomock.Controller) (*MockWorkloadRuntime, *runtimeTracker) {
	mockRT := NewMockWorkloadRuntime(ctrl)
	tracker := &runtimeTracker{}

	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().Close().Return(nil).AnyTimes()

	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, opts NetworkOptions) error {
			tracker.createdNetworks = append(tracker.createdNetworks, name)
			return nil
		},
	).AnyTimes()

	mockRT.EXPECT().EnsureImage(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, imageName string) error {
			tracker.ensuredImages = append(tracker.ensuredImages, imageName)
			return nil
		},
	).AnyTimes()

	mockRT.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(false, WorkloadID(""), nil).AnyTimes()

	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, cfg WorkloadConfig) (*WorkloadStatus, error) {
			tracker.startedWorkloads = append(tracker.startedWorkloads, cfg)
			id := WorkloadID("mock-" + cfg.Name)
			return &WorkloadStatus{
				ID:       id,
				Name:     cfg.Name,
				Stack:    cfg.Stack,
				Type:     cfg.Type,
				State:    WorkloadStateRunning,
				HostPort: cfg.HostPort,
				Image:    cfg.Image,
				Labels:   cfg.Labels,
			}, nil
		},
	).AnyTimes()

	mockRT.EXPECT().Stop(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id WorkloadID) error {
			tracker.stoppedWorkloads = append(tracker.stoppedWorkloads, id)
			return nil
		},
	).AnyTimes()

	mockRT.EXPECT().Remove(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id WorkloadID) error {
			tracker.removedWorkloads = append(tracker.removedWorkloads, id)
			return nil
		},
	).AnyTimes()

	mockRT.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string) error {
			tracker.removedNetworks = append(tracker.removedNetworks, name)
			return nil
		},
	).AnyTimes()

	mockRT.EXPECT().ListNetworks(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, stack string) ([]string, error) {
			return tracker.createdNetworks, nil
		},
	).AnyTimes()

	mockRT.EXPECT().List(gomock.Any(), gomock.Any()).Return([]WorkloadStatus{}, nil).AnyTimes()
	mockRT.EXPECT().GetHostPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(0, nil).AnyTimes()
	mockRT.EXPECT().Status(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id WorkloadID) (*WorkloadStatus, error) {
			return &WorkloadStatus{ID: id, State: WorkloadStateRunning}, nil
		},
	).AnyTimes()

	return mockRT, tracker
}

func TestOrchestrator_Up_SimpleNetwork(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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

	if len(tracker.createdNetworks) != 1 || tracker.createdNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' to be created, got %v", tracker.createdNetworks)
	}
	if len(tracker.startedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(tracker.startedWorkloads))
	}
	if len(result.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server in result, got %d", len(result.MCPServers))
	}
	if result.MCPServers[0].Name != "server1" {
		t.Errorf("expected server name 'server1', got %q", result.MCPServers[0].Name)
	}
}

func TestOrchestrator_Up_MultipleServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, _ := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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
	if result.MCPServers[0].HostPort != 9000 {
		t.Errorf("expected first server on port 9000, got %d", result.MCPServers[0].HostPort)
	}
	if result.MCPServers[1].HostPort != 9001 {
		t.Errorf("expected second server on port 9001, got %d", result.MCPServers[1].HostPort)
	}
}

func TestOrchestrator_Up_WithResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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

	if len(tracker.startedWorkloads) != 2 {
		t.Errorf("expected 2 workloads started, got %d", len(tracker.startedWorkloads))
	}
}

func TestOrchestrator_Up_AdvancedNetworkMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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

	if len(tracker.createdNetworks) != 2 {
		t.Errorf("expected 2 networks created, got %d", len(tracker.createdNetworks))
	}
}

func TestOrchestrator_Up_ImageEnsured(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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

	if len(tracker.ensuredImages) != 1 || tracker.ensuredImages[0] != "nginx:latest" {
		t.Errorf("expected 'nginx:latest' to be ensured, got %v", tracker.ensuredImages)
	}
}

func TestOrchestrator_Up_PingError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(errors.New("runtime unavailable")).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("network create failed")).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureImage(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(false, WorkloadID(""), nil).AnyTimes()
	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return(nil, errors.New("workload start failed")).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
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
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	tracker := &runtimeTracker{}

	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().List(gomock.Any(), gomock.Any()).Return([]WorkloadStatus{
		{ID: "workload-1", Name: "gridctl-test-server1", Type: WorkloadTypeMCPServer},
		{ID: "workload-2", Name: "gridctl-test-postgres", Type: WorkloadTypeResource},
	}, nil).AnyTimes()
	mockRT.EXPECT().Stop(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, id WorkloadID) error {
		tracker.stoppedWorkloads = append(tracker.stoppedWorkloads, id)
		return nil
	}).AnyTimes()
	mockRT.EXPECT().Remove(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, id WorkloadID) error {
		tracker.removedWorkloads = append(tracker.removedWorkloads, id)
		return nil
	}).AnyTimes()
	mockRT.EXPECT().ListNetworks(gomock.Any(), gomock.Any()).Return([]string{"test-net"}, nil).AnyTimes()
	mockRT.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) error {
		tracker.removedNetworks = append(tracker.removedNetworks, name)
		return nil
	}).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	err := orch.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tracker.stoppedWorkloads) != 2 {
		t.Errorf("expected 2 workloads stopped, got %d", len(tracker.stoppedWorkloads))
	}
	if len(tracker.removedWorkloads) != 2 {
		t.Errorf("expected 2 workloads removed, got %d", len(tracker.removedWorkloads))
	}
	if len(tracker.removedNetworks) != 1 {
		t.Errorf("expected 1 network removed, got %d", len(tracker.removedNetworks))
	}
}

func TestOrchestrator_Down_NoWorkloads(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, tracker := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	err := orch.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tracker.stoppedWorkloads) != 0 {
		t.Errorf("expected no workloads stopped, got %d", len(tracker.stoppedWorkloads))
	}
}

func TestOrchestrator_Down_PingError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(errors.New("runtime unavailable")).AnyTimes()
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
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().List(gomock.Any(), gomock.Any()).Return([]WorkloadStatus{
		{ID: "workload-1", Name: "gridctl-test-server1"},
	}, nil).AnyTimes()
	mockRT.EXPECT().Stop(gomock.Any(), gomock.Any()).Return(errors.New("stop failed")).AnyTimes()
	mockRT.EXPECT().Remove(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().ListNetworks(gomock.Any(), gomock.Any()).Return([]string{}, nil).AnyTimes()
	mockRT.EXPECT().RemoveNetwork(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	ctx := context.Background()
	err := orch.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOrchestrator_Status(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().List(gomock.Any(), gomock.Any()).Return([]WorkloadStatus{
		{
			ID:      "1234567890123456",
			Name:    "gridctl-test-server1",
			Type:    WorkloadTypeMCPServer,
			Stack:   "test",
			State:   WorkloadStateRunning,
			Message: "Up 1 minute",
			Labels: map[string]string{
				"gridctl.mcp-server": "server1",
			},
		},
	}, nil).AnyTimes()
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
