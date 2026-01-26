package integration

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// MockWorkloadRuntime is a test implementation of WorkloadRuntime for unit testing.
// It tracks all calls and allows injecting errors for testing error handling.
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
	StartedWorkloads []runtime.WorkloadConfig
	StoppedWorkloads []runtime.WorkloadID
	RemovedWorkloads []runtime.WorkloadID
	CreatedNetworks  []string
	RemovedNetworks  []string
	EnsuredImages    []string

	// State
	ExistingWorkloads map[string]runtime.WorkloadID
	ListedWorkloads   []runtime.WorkloadStatus
	HostPorts         map[runtime.WorkloadID]int
}

func NewMockWorkloadRuntime() *MockWorkloadRuntime {
	return &MockWorkloadRuntime{
		ExistingWorkloads: make(map[string]runtime.WorkloadID),
		HostPorts:         make(map[runtime.WorkloadID]int),
	}
}

func (m *MockWorkloadRuntime) Start(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
	if m.StartError != nil {
		return nil, m.StartError
	}
	m.StartedWorkloads = append(m.StartedWorkloads, cfg)
	id := runtime.WorkloadID("mock-" + cfg.Name)
	return &runtime.WorkloadStatus{
		ID:       id,
		Name:     cfg.Name,
		Topology: cfg.Topology,
		Type:     cfg.Type,
		State:    runtime.WorkloadStateRunning,
		HostPort: cfg.HostPort,
		Image:    cfg.Image,
		Labels:   cfg.Labels,
	}, nil
}

func (m *MockWorkloadRuntime) Stop(ctx context.Context, id runtime.WorkloadID) error {
	if m.StopError != nil {
		return m.StopError
	}
	m.StoppedWorkloads = append(m.StoppedWorkloads, id)
	return nil
}

func (m *MockWorkloadRuntime) Remove(ctx context.Context, id runtime.WorkloadID) error {
	if m.RemoveError != nil {
		return m.RemoveError
	}
	m.RemovedWorkloads = append(m.RemovedWorkloads, id)
	return nil
}

func (m *MockWorkloadRuntime) Status(ctx context.Context, id runtime.WorkloadID) (*runtime.WorkloadStatus, error) {
	return &runtime.WorkloadStatus{
		ID:    id,
		State: runtime.WorkloadStateRunning,
	}, nil
}

func (m *MockWorkloadRuntime) Exists(ctx context.Context, name string) (bool, runtime.WorkloadID, error) {
	if m.ExistsError != nil {
		return false, "", m.ExistsError
	}
	if id, ok := m.ExistingWorkloads[name]; ok {
		return true, id, nil
	}
	return false, "", nil
}

func (m *MockWorkloadRuntime) List(ctx context.Context, filter runtime.WorkloadFilter) ([]runtime.WorkloadStatus, error) {
	if m.ListError != nil {
		return nil, m.ListError
	}
	return m.ListedWorkloads, nil
}

func (m *MockWorkloadRuntime) GetHostPort(ctx context.Context, id runtime.WorkloadID, exposedPort int) (int, error) {
	if m.GetHostPortError != nil {
		return 0, m.GetHostPortError
	}
	if port, ok := m.HostPorts[id]; ok {
		return port, nil
	}
	return 0, nil
}

func (m *MockWorkloadRuntime) EnsureNetwork(ctx context.Context, name string, opts runtime.NetworkOptions) error {
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
var _ runtime.WorkloadRuntime = (*MockWorkloadRuntime)(nil)

// MockBuilder is a mock implementation of Builder for testing.
type MockBuilder struct {
	BuildError  error
	BuildResult *runtime.BuildResult
}

func (m *MockBuilder) Build(ctx context.Context, opts runtime.BuildOptions) (*runtime.BuildResult, error) {
	if m.BuildError != nil {
		return nil, m.BuildError
	}
	if m.BuildResult != nil {
		return m.BuildResult, nil
	}
	return &runtime.BuildResult{
		ImageTag: opts.Tag,
		Cached:   false,
	}, nil
}

// Ensure MockBuilder implements Builder
var _ runtime.Builder = (*MockBuilder)(nil)

// TestOrchestrator_Up_ExternalServer tests that external MCP servers are handled correctly.
// External servers should not create containers but should be registered in the result.
func TestOrchestrator_Up_ExternalServer(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	topo := &config.Topology{
		Version: "1",
		Name:    "test-external",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name: "external-api",
				URL:  "https://api.example.com/mcp",
			},
			{
				Name:  "container-server",
				Image: "mcp-server:latest",
				Port:  3000,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 MCP servers in result
	if len(result.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(result.MCPServers))
	}

	// Find external server
	var externalServer *runtime.MCPServerResult
	var containerServer *runtime.MCPServerResult
	for i := range result.MCPServers {
		if result.MCPServers[i].Name == "external-api" {
			externalServer = &result.MCPServers[i]
		} else if result.MCPServers[i].Name == "container-server" {
			containerServer = &result.MCPServers[i]
		}
	}

	// Verify external server
	if externalServer == nil {
		t.Fatal("external-api server not found in result")
	}
	if !externalServer.External {
		t.Error("expected External flag to be true for external-api")
	}
	if externalServer.URL != "https://api.example.com/mcp" {
		t.Errorf("expected URL 'https://api.example.com/mcp', got %q", externalServer.URL)
	}
	if externalServer.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID for external server, got %q", externalServer.WorkloadID)
	}

	// Verify container server
	if containerServer == nil {
		t.Fatal("container-server not found in result")
	}
	if containerServer.External {
		t.Error("expected External flag to be false for container-server")
	}
	if containerServer.WorkloadID == "" {
		t.Error("expected non-empty WorkloadID for container server")
	}

	// Verify only 1 container was started (not 2)
	if len(mockRT.StartedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(mockRT.StartedWorkloads))
	}
	if mockRT.StartedWorkloads[0].Name != "container-server" {
		t.Errorf("expected started workload 'container-server', got %q", mockRT.StartedWorkloads[0].Name)
	}
}

// TestOrchestrator_Up_LocalProcessServer tests that local process MCP servers are handled correctly.
// Local process servers should not create containers but should be registered in the result.
func TestOrchestrator_Up_LocalProcessServer(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	topo := &config.Topology{
		Version: "1",
		Name:    "test-local",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:    "local-server",
				Command: []string{"npx", "-y", "@modelcontextprotocol/server-filesystem"},
			},
			{
				Name:  "container-server",
				Image: "mcp-server:latest",
				Port:  3000,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 MCP servers in result
	if len(result.MCPServers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(result.MCPServers))
	}

	// Find local process server
	var localServer *runtime.MCPServerResult
	for i := range result.MCPServers {
		if result.MCPServers[i].Name == "local-server" {
			localServer = &result.MCPServers[i]
			break
		}
	}

	// Verify local process server
	if localServer == nil {
		t.Fatal("local-server not found in result")
	}
	if !localServer.LocalProcess {
		t.Error("expected LocalProcess flag to be true")
	}
	if len(localServer.Command) != 3 {
		t.Errorf("expected 3 command args, got %d", len(localServer.Command))
	}
	if localServer.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID for local process server, got %q", localServer.WorkloadID)
	}

	// Verify only 1 container was started (not 2)
	if len(mockRT.StartedWorkloads) != 1 {
		t.Errorf("expected 1 workload started, got %d", len(mockRT.StartedWorkloads))
	}
}

// TestOrchestrator_Up_SSHServer tests that SSH-based MCP servers are handled correctly.
func TestOrchestrator_Up_SSHServer(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	topo := &config.Topology{
		Version: "1",
		Name:    "test-ssh",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name:    "ssh-server",
				Command: []string{"python", "-m", "mcp_server"},
				SSH: &config.SSHConfig{
					Host:         "remote.example.com",
					User:         "deploy",
					Port:         22,
					IdentityFile: "~/.ssh/id_rsa",
				},
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 MCP server in result
	if len(result.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(result.MCPServers))
	}

	sshServer := result.MCPServers[0]
	if !sshServer.SSH {
		t.Error("expected SSH flag to be true")
	}
	if sshServer.SSHHost != "remote.example.com" {
		t.Errorf("expected SSHHost 'remote.example.com', got %q", sshServer.SSHHost)
	}
	if sshServer.SSHUser != "deploy" {
		t.Errorf("expected SSHUser 'deploy', got %q", sshServer.SSHUser)
	}
	if sshServer.SSHPort != 22 {
		t.Errorf("expected SSHPort 22, got %d", sshServer.SSHPort)
	}
	if sshServer.WorkloadID != "" {
		t.Errorf("expected empty WorkloadID for SSH server, got %q", sshServer.WorkloadID)
	}

	// Verify no containers were started
	if len(mockRT.StartedWorkloads) != 0 {
		t.Errorf("expected 0 workloads started, got %d", len(mockRT.StartedWorkloads))
	}
}

// TestOrchestrator_Up_AgentDependencyOrder tests that agents are started in correct dependency order.
// Agents that depend on other agents should be started after their dependencies.
func TestOrchestrator_Up_AgentDependencyOrder(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	// Create topology with agents that have dependencies
	// agent-c depends on agent-b, agent-b depends on agent-a
	topo := &config.Topology{
		Version: "1",
		Name:    "test-deps",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		Agents: []config.Agent{
			{
				Name:  "agent-c",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{{Server: "agent-b"}}, // Depends on agent-b
				A2A:   &config.A2AConfig{Enabled: true},
			},
			{
				Name:  "agent-a",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{}, // No dependencies
				A2A:   &config.A2AConfig{Enabled: true},
			},
			{
				Name:  "agent-b",
				Image: "agent:latest",
				Uses:  []config.ToolSelector{{Server: "agent-a"}}, // Depends on agent-a
				A2A:   &config.A2AConfig{Enabled: true},
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 agents
	if len(result.Agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(result.Agents))
	}

	// Verify start order: agent-a should be before agent-b, agent-b before agent-c
	startOrder := make(map[string]int)
	for i, w := range mockRT.StartedWorkloads {
		startOrder[w.Name] = i
	}

	if startOrder["agent-a"] >= startOrder["agent-b"] {
		t.Errorf("agent-a (pos %d) should start before agent-b (pos %d)", startOrder["agent-a"], startOrder["agent-b"])
	}
	if startOrder["agent-b"] >= startOrder["agent-c"] {
		t.Errorf("agent-b (pos %d) should start before agent-c (pos %d)", startOrder["agent-b"], startOrder["agent-c"])
	}
}

// TestOrchestrator_Up_MixedServerTypes tests a topology with all server types.
func TestOrchestrator_Up_MixedServerTypes(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	topo := &config.Topology{
		Version: "1",
		Name:    "test-mixed",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{
				Name: "external-api",
				URL:  "https://api.example.com/mcp",
			},
			{
				Name:    "local-server",
				Command: []string{"node", "server.js"},
			},
			{
				Name:    "ssh-server",
				Command: []string{"python", "server.py"},
				SSH: &config.SSHConfig{
					Host: "remote.example.com",
					User: "deploy",
				},
			},
			{
				Name:  "container-1",
				Image: "mcp-server:v1",
				Port:  3000,
			},
			{
				Name:  "container-2",
				Image: "mcp-server:v2",
				Port:  3001,
			},
		},
	}

	ctx := context.Background()
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 5 MCP servers in result
	if len(result.MCPServers) != 5 {
		t.Fatalf("expected 5 MCP servers, got %d", len(result.MCPServers))
	}

	// Count server types
	var external, local, ssh, container int
	for _, s := range result.MCPServers {
		if s.External {
			external++
		} else if s.LocalProcess {
			local++
		} else if s.SSH {
			ssh++
		} else {
			container++
		}
	}

	if external != 1 {
		t.Errorf("expected 1 external server, got %d", external)
	}
	if local != 1 {
		t.Errorf("expected 1 local server, got %d", local)
	}
	if ssh != 1 {
		t.Errorf("expected 1 SSH server, got %d", ssh)
	}
	if container != 2 {
		t.Errorf("expected 2 container servers, got %d", container)
	}

	// Verify only 2 containers were started
	if len(mockRT.StartedWorkloads) != 2 {
		t.Errorf("expected 2 workloads started, got %d", len(mockRT.StartedWorkloads))
	}

	// Verify container ports are sequential
	if result.MCPServers[3].HostPort != 9000 {
		t.Errorf("expected container-1 on port 9000, got %d", result.MCPServers[3].HostPort)
	}
	if result.MCPServers[4].HostPort != 9001 {
		t.Errorf("expected container-2 on port 9001, got %d", result.MCPServers[4].HostPort)
	}
}

// TestOrchestrator_Up_BasePortConfiguration tests that BasePort is correctly used.
func TestOrchestrator_Up_BasePortConfiguration(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	topo := &config.Topology{
		Version: "1",
		Name:    "test-ports",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "mcp:v1", Port: 3000},
			{Name: "server2", Image: "mcp:v2", Port: 3001},
			{Name: "server3", Image: "mcp:v3", Port: 3002},
		},
	}

	ctx := context.Background()

	// Test with custom base port
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 10000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify ports start from 10000
	expectedPorts := []int{10000, 10001, 10002}
	for i, server := range result.MCPServers {
		if server.HostPort != expectedPorts[i] {
			t.Errorf("server %d: expected port %d, got %d", i, expectedPorts[i], server.HostPort)
		}
	}
}

// TestOrchestrator_Up_DefaultBasePort tests that default BasePort (9000) is used when not specified.
func TestOrchestrator_Up_DefaultBasePort(t *testing.T) {
	mockRT := NewMockWorkloadRuntime()
	mockBuilder := &MockBuilder{}

	orch := runtime.NewOrchestrator(mockRT, mockBuilder)

	topo := &config.Topology{
		Version: "1",
		Name:    "test-default-port",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "mcp:v1", Port: 3000},
		},
	}

	ctx := context.Background()

	// Test with BasePort=0 (should default to 9000)
	result, err := orch.Up(ctx, topo, runtime.UpOptions{BasePort: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify port defaults to 9000
	if result.MCPServers[0].HostPort != 9000 {
		t.Errorf("expected default port 9000, got %d", result.MCPServers[0].HostPort)
	}
}
