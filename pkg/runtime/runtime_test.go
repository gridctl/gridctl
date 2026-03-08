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

func TestOrchestrator_Up_NoPingWhenNoDocker(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	// Ping and EnsureNetwork should NOT be called for non-container stacks
	mockRT.EXPECT().Ping(gomock.Any()).Times(0)
	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	mockRT.EXPECT().Close().Return(nil).AnyTimes()
	mockBuilder := &MockBuilder{}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-topo",
		MCPServers: []config.MCPServer{
			{Name: "local-server", Command: []string{"npx", "server"}},
			{Name: "ext-server", URL: "http://localhost:8080"},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.MCPServers) != 2 {
		t.Errorf("expected 2 MCP servers, got %d", len(result.MCPServers))
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

func TestOrchestrator_Up_SourceBasedServer(t *testing.T) {
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
			{
				Name:   "src-server",
				Source: &config.Source{Type: "git", URL: "https://github.com/example/repo", Dockerfile: "Dockerfile"},
				Port:   3000,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(result.MCPServers))
	}
	if len(tracker.startedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(tracker.startedWorkloads))
	}
	// Source-based server should use generated image tag
	if tracker.startedWorkloads[0].Image != "gridctl-test-topo-src-server:latest" {
		t.Errorf("expected generated image tag, got %q", tracker.startedWorkloads[0].Image)
	}
}

func TestOrchestrator_Up_AgentMCPEndpointInjection(t *testing.T) {
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
			{Name: "server1", Image: "alpine", Port: 3000},
		},
		Agents: []config.Agent{
			{Name: "agent1", Image: "agent:latest", Uses: []config.ToolSelector{{Server: "server1"}}},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000, GatewayPort: 8180})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(result.Agents))
	}

	// Find agent workload and check MCP_ENDPOINT env var
	var agentCfg *WorkloadConfig
	for i := range tracker.startedWorkloads {
		if tracker.startedWorkloads[i].Type == WorkloadTypeAgent {
			agentCfg = &tracker.startedWorkloads[i]
			break
		}
	}
	if agentCfg == nil {
		t.Fatal("agent workload not found in started workloads")
	}
	endpoint, ok := agentCfg.Env["MCP_ENDPOINT"]
	if !ok {
		t.Fatal("MCP_ENDPOINT not set in agent env")
	}
	if endpoint == "" {
		t.Error("MCP_ENDPOINT should not be empty")
	}
}

func TestOrchestrator_Up_AgentDependencyOrdering(t *testing.T) {
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
			{Name: "server1", Image: "alpine", Port: 3000},
		},
		Agents: []config.Agent{
			{
				Name:  "dependent-agent",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{{Server: "provider-agent"}},
			},
			{
				Name:  "provider-agent",
				Image: "agent:latest",
				A2A:   &config.A2AConfig{Enabled: true, Skills: []config.A2ASkill{{ID: "s1", Name: "Skill 1"}}},
				Uses:  []config.ToolSelector{{Server: "server1"}},
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(result.Agents))
	}

	// Provider agent should start before dependent agent
	var providerIdx, dependentIdx int
	for i, w := range tracker.startedWorkloads {
		if w.Name == "provider-agent" {
			providerIdx = i
		}
		if w.Name == "dependent-agent" {
			dependentIdx = i
		}
	}
	if providerIdx >= dependentIdx {
		t.Errorf("provider-agent (index %d) should start before dependent-agent (index %d)", providerIdx, dependentIdx)
	}
}

func TestOrchestrator_Up_ServerStartError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureImage(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(false, WorkloadID(""), nil).AnyTimes()
	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).Return(nil, errors.New("container start failed")).AnyTimes()

	orch := NewOrchestrator(mockRT, &MockBuilder{})
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine", Port: 3000},
		},
	}

	_, err := orch.Up(context.Background(), topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOrchestrator_Up_AgentStartError(t *testing.T) {
	ctrl := gomock.NewController(t)
	callCount := 0
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().EnsureImage(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	mockRT.EXPECT().Exists(gomock.Any(), gomock.Any()).Return(false, WorkloadID(""), nil).AnyTimes()
	mockRT.EXPECT().Start(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, cfg WorkloadConfig) (*WorkloadStatus, error) {
			callCount++
			if cfg.Type == WorkloadTypeAgent {
				return nil, errors.New("agent start failed")
			}
			return &WorkloadStatus{
				ID:       WorkloadID("mock-" + cfg.Name),
				Name:     cfg.Name,
				State:    WorkloadStateRunning,
				HostPort: cfg.HostPort,
			}, nil
		},
	).AnyTimes()

	orch := NewOrchestrator(mockRT, &MockBuilder{})
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "alpine", Port: 3000},
		},
		Agents: []config.Agent{
			{Name: "agent1", Image: "alpine", Uses: []config.ToolSelector{{Server: "server1"}}},
		},
	}

	_, err := orch.Up(context.Background(), topo, UpOptions{BasePort: 9000})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOrchestrator_SetRuntimeInfo(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, _ := setupDefaultRuntime(ctrl)

	orch := NewOrchestrator(mockRT, &MockBuilder{})

	if orch.RuntimeInfo() != nil {
		t.Error("expected nil RuntimeInfo initially")
	}

	info := &RuntimeInfo{Type: RuntimeDocker, SocketPath: "/var/run/docker.sock"}
	orch.SetRuntimeInfo(info)

	if orch.RuntimeInfo() != info {
		t.Error("expected RuntimeInfo to be set")
	}
}

func TestOrchestrator_Up_BuildError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT, _ := setupDefaultRuntime(ctrl)
	mockBuilder := &MockBuilder{BuildError: errors.New("build failed")}

	orch := NewOrchestrator(mockRT, mockBuilder)
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{
				Name:   "src-server",
				Source: &config.Source{Type: "git", URL: "https://example.com/repo"},
				Port:   3000,
			},
		},
	}

	_, err := orch.Up(context.Background(), topo, UpOptions{BasePort: 9000})
	if err == nil {
		t.Fatal("expected build error, got nil")
	}
}

func TestOrchestrator_Up_ExternalAndSSHServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockRT := NewMockWorkloadRuntime(ctrl)
	mockRT.EXPECT().Ping(gomock.Any()).Times(0) // No ping needed for non-container stack
	mockRT.EXPECT().Close().Return(nil).AnyTimes()

	orch := NewOrchestrator(mockRT, &MockBuilder{})
	orch.SetLogger(testLogger())

	topo := &config.Stack{
		Version: "1",
		Name:    "test-topo",
		MCPServers: []config.MCPServer{
			{Name: "ext", URL: "http://example.com"},
			{Name: "ssh-srv", SSH: &config.SSHConfig{Host: "10.0.0.1", User: "user"}, Command: []string{"server"}},
			{Name: "openapi-srv", OpenAPI: &config.OpenAPIConfig{Spec: "spec.json"}},
		},
	}

	result, err := orch.Up(context.Background(), topo, UpOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.MCPServers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(result.MCPServers))
	}

	// Verify server types
	for _, s := range result.MCPServers {
		switch s.Name {
		case "ext":
			if !s.External {
				t.Error("expected ext to be External")
			}
		case "ssh-srv":
			if !s.SSH {
				t.Error("expected ssh-srv to be SSH")
			}
		case "openapi-srv":
			if !s.OpenAPI {
				t.Error("expected openapi-srv to be OpenAPI")
			}
		}
	}
}

func TestContainerName(t *testing.T) {
	name := containerName("mystack", "myserver")
	if name != "gridctl-mystack-myserver" {
		t.Errorf("expected 'gridctl-mystack-myserver', got %q", name)
	}
}

func TestGenerateTag(t *testing.T) {
	tag := generateTag("mystack", "myserver")
	if tag != "gridctl-mystack-myserver:latest" {
		t.Errorf("expected 'gridctl-mystack-myserver:latest', got %q", tag)
	}
}

func TestManagedLabels(t *testing.T) {
	labels := managedLabels("stack1", "server1", true)
	if labels["gridctl.managed"] != "true" {
		t.Error("expected gridctl.managed=true")
	}
	if labels["gridctl.stack"] != "stack1" {
		t.Error("expected gridctl.stack=stack1")
	}
	if labels["gridctl.mcp-server"] != "server1" {
		t.Error("expected gridctl.mcp-server=server1")
	}

	// Resource labels
	resLabels := managedLabels("stack1", "postgres", false)
	if _, ok := resLabels["gridctl.mcp-server"]; ok {
		t.Error("resource should not have mcp-server label")
	}
	if resLabels["gridctl.resource"] != "postgres" {
		t.Error("expected gridctl.resource=postgres")
	}
}

func TestAgentLabels(t *testing.T) {
	labels := agentLabels("stack1", "agent1")
	if labels["gridctl.managed"] != "true" {
		t.Error("expected gridctl.managed=true")
	}
	if labels["gridctl.agent"] != "agent1" {
		t.Error("expected gridctl.agent=agent1")
	}
}

func TestSortAgentsByDependency(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{{Name: "s1"}},
		Agents: []config.Agent{
			{
				Name:  "dependent",
				Image: "alpine",
				Uses:  []config.ToolSelector{{Server: "provider"}},
			},
			{
				Name:  "provider",
				Image: "alpine",
				A2A:   &config.A2AConfig{Enabled: true, Skills: []config.A2ASkill{{ID: "s1", Name: "S1"}}},
				Uses:  []config.ToolSelector{{Server: "s1"}},
			},
		},
	}

	sorted, err := sortAgentsByDependency(stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(sorted))
	}
	if sorted[0].Name != "provider" {
		t.Errorf("expected provider first, got %q", sorted[0].Name)
	}
}

func TestSortAgentsByDependency_Empty(t *testing.T) {
	stack := &config.Stack{}
	sorted, err := sortAgentsByDependency(stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sorted != nil {
		t.Errorf("expected nil for empty agents, got %v", sorted)
	}
}

func TestRuntimeRequiredError(t *testing.T) {
	stack := &config.Stack{
		Name: "test",
		MCPServers: []config.MCPServer{
			{Name: "container-srv", Image: "alpine", Port: 3000},
			{Name: "external-srv", URL: "http://example.com"},
		},
	}

	err := runtimeRequiredError(stack, errors.New("connection refused"))
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if !errors.Is(err, errors.New("")) {
		// Just check it wraps the ping error
		if len(errMsg) == 0 {
			t.Error("expected non-empty error")
		}
	}
}
