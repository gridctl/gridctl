package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"agentlab/pkg/builder"
	"agentlab/pkg/config"
	"agentlab/pkg/logging"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
)

// testLogger returns a discard logger for tests
func testLogger() *slog.Logger {
	return logging.NewDiscardLogger()
}

func TestRuntime_Up_SimpleNetwork(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"mcp-server:latest"}},
		},
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

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
	result, err := rt.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify network was created
	if len(mock.CreatedNetworks) != 1 || mock.CreatedNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' to be created, got %v", mock.CreatedNetworks)
	}

	// Verify container was created and started
	if len(mock.CreatedContainers) != 1 {
		t.Errorf("expected 1 container created, got %d", len(mock.CreatedContainers))
	}
	if len(mock.StartedContainers) != 1 {
		t.Errorf("expected 1 container started, got %d", len(mock.StartedContainers))
	}

	// Verify result
	if len(result.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server in result, got %d", len(result.MCPServers))
	}
	if result.MCPServers[0].Name != "server1" {
		t.Errorf("expected server name 'server1', got %q", result.MCPServers[0].Name)
	}
}

func TestRuntime_Up_MultipleServers(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"mcp-server:latest", "another-server:latest"}},
		},
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

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
	result, err := rt.Up(ctx, topo, UpOptions{BasePort: 9000})
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

func TestRuntime_Up_WithResources(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"mcp-server:latest", "postgres:16"}},
		},
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

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
	_, err := rt.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify 2 containers created (1 MCP server + 1 resource)
	if len(mock.CreatedContainers) != 2 {
		t.Errorf("expected 2 containers created, got %d", len(mock.CreatedContainers))
	}
}

func TestRuntime_Up_AdvancedNetworkMode(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"frontend:latest", "backend:latest"}},
		},
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

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
	_, err := rt.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both networks were created
	if len(mock.CreatedNetworks) != 2 {
		t.Errorf("expected 2 networks created, got %d", len(mock.CreatedNetworks))
	}
}

func TestRuntime_Up_ImagePull(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{}, // No local images, will pull
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "nginx:latest", Port: 80},
		},
	}

	ctx := context.Background()
	_, err := rt.Up(ctx, topo, UpOptions{BasePort: 9000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify image was pulled
	if len(mock.PulledImages) != 1 || mock.PulledImages[0] != "nginx:latest" {
		t.Errorf("expected 'nginx:latest' to be pulled, got %v", mock.PulledImages)
	}
}

func TestRuntime_Up_PingError(t *testing.T) {
	mock := &MockDockerClient{
		PingError: errors.New("Docker daemon unavailable"),
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
	}

	ctx := context.Background()
	_, err := rt.Up(ctx, topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRuntime_Up_NetworkCreateError(t *testing.T) {
	mock := &MockDockerClient{
		NetworkCreateError: errors.New("network create failed"),
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "nginx:latest", Port: 80},
		},
	}

	ctx := context.Background()
	_, err := rt.Up(ctx, topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRuntime_Up_ContainerCreateError(t *testing.T) {
	mock := &MockDockerClient{
		Images:               []image.Summary{{RepoTags: []string{"nginx:latest"}}},
		ContainerCreateError: errors.New("container create failed"),
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	topo := &config.Topology{
		Version: "1",
		Name:    "test-topo",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "nginx:latest", Port: 80},
		},
	}

	ctx := context.Background()
	_, err := rt.Up(ctx, topo, UpOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRuntime_Down_Success(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "container-1",
				Names: []string{"/agentlab-test-server1"},
				Labels: map[string]string{
					LabelManaged:   "true",
					LabelTopology:  "test",
					LabelMCPServer: "server1",
				},
			},
			{
				ID:    "container-2",
				Names: []string{"/agentlab-test-postgres"},
				Labels: map[string]string{
					LabelManaged:  "true",
					LabelTopology: "test",
					LabelResource: "postgres",
				},
			},
		},
		Networks: []network.Summary{
			{Name: "test-net"},
		},
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	ctx := context.Background()
	err := rt.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify containers were stopped
	if len(mock.StoppedContainers) != 2 {
		t.Errorf("expected 2 containers stopped, got %d", len(mock.StoppedContainers))
	}

	// Verify containers were removed
	if len(mock.RemovedContainers) != 2 {
		t.Errorf("expected 2 containers removed, got %d", len(mock.RemovedContainers))
	}

	// Verify networks were removed
	if len(mock.RemovedNetworks) != 1 {
		t.Errorf("expected 1 network removed, got %d", len(mock.RemovedNetworks))
	}
}

func TestRuntime_Down_NoContainers(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{},
		Networks:   []network.Summary{},
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	ctx := context.Background()
	err := rt.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should complete without stopping/removing anything
	if len(mock.StoppedContainers) != 0 {
		t.Errorf("expected no containers stopped, got %d", len(mock.StoppedContainers))
	}
}

func TestRuntime_Down_PingError(t *testing.T) {
	mock := &MockDockerClient{
		PingError: errors.New("Docker daemon unavailable"),
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	ctx := context.Background()
	err := rt.Down(ctx, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRuntime_Down_StopError_Continues(t *testing.T) {
	// Down should continue even if stopping a container fails
	mock := &MockDockerClient{
		Containers: []types.Container{
			{ID: "container-1", Names: []string{"/agentlab-test-server1"}},
		},
		ContainerStopError: errors.New("stop failed"),
	}

	rt := &Runtime{
		cli:     mock,
		builder: builder.New(mock),
		logger:  testLogger(),
	}

	ctx := context.Background()
	// Should not return error, just print warning
	err := rt.Down(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntime_Status(t *testing.T) {
	mockCli := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "1234567890123456", // Must be > 12 chars
				Names: []string{"/agentlab-test-server1"},
				Labels: map[string]string{
					LabelManaged:   "true",
					LabelTopology:  "test",
					LabelMCPServer: "server1",
				},
				State:  "running",
				Status: "Up 1 minute",
			},
		},
	}

	rt := &Runtime{
		cli:     mockCli,
		builder: builder.New(mockCli),
	}

	ctx := context.Background()
	statuses, err := rt.Status(ctx, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Errorf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].MCPServerName != "server1" {
		t.Errorf("expected server name 'server1', got '%s'", statuses[0].MCPServerName)
	}
}

func TestRuntime_Ping_Error(t *testing.T) {
	mockCli := &MockDockerClient{
		PingError: context.DeadlineExceeded,
	}

	// We can test the package-level Ping function directly
	err := Ping(context.Background(), mockCli)
	if err == nil {
		t.Fatal("expected error from Ping")
	}
}
