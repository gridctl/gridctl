package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

func TestCreateContainer_MCPServer(t *testing.T) {
	mock := &MockDockerClient{}

	cfg := ContainerConfig{
		Name:        "agentlab-test-server",
		Image:       "mcp-server:latest",
		Port:        3000,
		HostPort:    9000,
		NetworkName: "test-net",
		Labels: map[string]string{
			LabelManaged:   "true",
			LabelMCPServer: "test-server",
		},
		Transport: "http",
	}

	ctx := context.Background()
	id, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id != "mock-container-agentlab-test-server" {
		t.Errorf("got ID %q, want %q", id, "mock-container-agentlab-test-server")
	}

	if len(mock.CreatedContainers) != 1 || mock.CreatedContainers[0] != "agentlab-test-server" {
		t.Errorf("expected container to be created, got %v", mock.CreatedContainers)
	}
}

func TestCreateContainer_Resource(t *testing.T) {
	mock := &MockDockerClient{}

	cfg := ContainerConfig{
		Name:        "agentlab-test-postgres",
		Image:       "postgres:16",
		Port:        0, // Resources don't expose ports
		NetworkName: "test-net",
		Labels: map[string]string{
			LabelManaged:  "true",
			LabelResource: "postgres",
		},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "secret",
		},
	}

	ctx := context.Background()
	id, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty container ID")
	}
}

func TestCreateContainer_WithCommand(t *testing.T) {
	mock := &MockDockerClient{}

	cfg := ContainerConfig{
		Name:        "agentlab-test-cmd",
		Image:       "alpine:latest",
		Command:     []string{"sh", "-c", "echo hello"},
		NetworkName: "test-net",
		Labels:      ManagedLabels("test", "cmd", false),
	}

	ctx := context.Background()
	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateContainer_StdioTransport(t *testing.T) {
	mock := &MockDockerClient{}

	cfg := ContainerConfig{
		Name:        "agentlab-test-stdio",
		Image:       "stdio-mcp:latest",
		NetworkName: "test-net",
		Transport:   "stdio",
		Labels:      ManagedLabels("test", "stdio", true),
	}

	ctx := context.Background()
	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateContainer_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerCreateError: errors.New("create failed"),
	}

	cfg := ContainerConfig{
		Name:        "test-container",
		Image:       "nginx:latest",
		NetworkName: "test-net",
	}

	ctx := context.Background()
	_, err := CreateContainer(ctx, mock, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestStartContainer_Success(t *testing.T) {
	mock := &MockDockerClient{}

	ctx := context.Background()
	err := StartContainer(ctx, mock, "container-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.StartedContainers) != 1 || mock.StartedContainers[0] != "container-123" {
		t.Errorf("expected container to be started, got %v", mock.StartedContainers)
	}
}

func TestStartContainer_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerStartError: errors.New("start failed"),
	}

	ctx := context.Background()
	err := StartContainer(ctx, mock, "container-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestStopContainer_Success(t *testing.T) {
	mock := &MockDockerClient{}

	ctx := context.Background()
	err := StopContainer(ctx, mock, "container-123", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.StoppedContainers) != 1 || mock.StoppedContainers[0] != "container-123" {
		t.Errorf("expected container to be stopped, got %v", mock.StoppedContainers)
	}
}

func TestStopContainer_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerStopError: errors.New("stop failed"),
	}

	ctx := context.Background()
	err := StopContainer(ctx, mock, "container-123", 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRemoveContainer_Success(t *testing.T) {
	mock := &MockDockerClient{}

	ctx := context.Background()
	err := RemoveContainer(ctx, mock, "container-123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedContainers) != 1 || mock.RemovedContainers[0] != "container-123" {
		t.Errorf("expected container to be removed, got %v", mock.RemovedContainers)
	}
}

func TestRemoveContainer_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerRemoveError: errors.New("remove failed"),
	}

	ctx := context.Background()
	err := RemoveContainer(ctx, mock, "container-123", true)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContainerExists_Found(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{ID: "abc123", Names: []string{"/agentlab-test-server"}},
		},
	}

	ctx := context.Background()
	exists, id, err := ContainerExists(ctx, mock, "agentlab-test-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !exists {
		t.Error("expected container to exist")
	}
	if id != "abc123" {
		t.Errorf("got ID %q, want %q", id, "abc123")
	}
}

func TestContainerExists_NotFound(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{ID: "abc123", Names: []string{"/other-container"}},
		},
	}

	ctx := context.Background()
	exists, id, err := ContainerExists(ctx, mock, "agentlab-test-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exists {
		t.Error("expected container to not exist")
	}
	if id != "" {
		t.Errorf("expected empty ID, got %q", id)
	}
}

func TestContainerExists_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerListError: errors.New("list failed"),
	}

	ctx := context.Background()
	_, _, err := ContainerExists(ctx, mock, "any")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListManagedContainers_FilterByTopology(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "1",
				Names: []string{"/agentlab-topo1-server1"},
				Labels: map[string]string{
					LabelManaged:  "true",
					LabelTopology: "topo1",
				},
			},
			{
				ID:    "2",
				Names: []string{"/agentlab-topo1-server2"},
				Labels: map[string]string{
					LabelManaged:  "true",
					LabelTopology: "topo1",
				},
			},
		},
	}

	ctx := context.Background()
	containers, err := ListManagedContainers(ctx, mock, "topo1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(containers))
	}
}

func TestListManagedContainers_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerListError: errors.New("list failed"),
	}

	ctx := context.Background()
	_, err := ListManagedContainers(ctx, mock, "any")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerIP_Success(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"container-123": {
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {IPAddress: "172.18.0.5"},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ip, err := GetContainerIP(ctx, mock, "container-123", "test-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ip != "172.18.0.5" {
		t.Errorf("got IP %q, want %q", ip, "172.18.0.5")
	}
}

func TestGetContainerIP_NetworkNotFound(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"container-123": {
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"other-net": {IPAddress: "172.18.0.5"},
					},
				},
			},
		},
	}

	ctx := context.Background()
	_, err := GetContainerIP(ctx, mock, "container-123", "test-net")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerIP_InspectError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerInspectError: errors.New("inspect failed"),
	}

	ctx := context.Background()
	_, err := GetContainerIP(ctx, mock, "container-123", "test-net")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerHostPort_Success(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"container-123": {
				NetworkSettings: &types.NetworkSettings{
					NetworkSettingsBase: types.NetworkSettingsBase{
						Ports: nat.PortMap{
							"3000/tcp": []nat.PortBinding{
								{HostIP: "0.0.0.0", HostPort: "9000"},
							},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	port, err := GetContainerHostPort(ctx, mock, "container-123", 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if port != 9000 {
		t.Errorf("got port %d, want %d", port, 9000)
	}
}

func TestGetContainerHostPort_NotBound(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"container-123": {
				NetworkSettings: &types.NetworkSettings{
					NetworkSettingsBase: types.NetworkSettingsBase{
						Ports: nat.PortMap{}, // No port bindings
					},
				},
			},
		},
	}

	ctx := context.Background()
	_, err := GetContainerHostPort(ctx, mock, "container-123", 3000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerHostPort_InspectError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerInspectError: errors.New("inspect failed"),
	}

	ctx := context.Background()
	_, err := GetContainerHostPort(ctx, mock, "container-123", 3000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateContainer_WithVolumes(t *testing.T) {
	mock := &MockDockerClient{}

	cfg := ContainerConfig{
		Name:        "agentlab-test-postgres",
		Image:       "postgres:16",
		Port:        0,
		NetworkName: "test-net",
		Labels: map[string]string{
			LabelManaged:  "true",
			LabelResource: "postgres",
		},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "secret",
		},
		Volumes: []string{
			"/host/data:/var/lib/postgresql/data",
			"/host/config:/etc/postgresql:ro",
		},
	}

	ctx := context.Background()
	id, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty container ID")
	}

	// Verify that volumes were passed to the host config
	if mock.LastHostConfig == nil {
		t.Fatal("expected host config to be set")
	}

	if len(mock.LastHostConfig.Binds) != 2 {
		t.Errorf("expected 2 volume binds, got %d", len(mock.LastHostConfig.Binds))
	}

	expectedBinds := []string{
		"/host/data:/var/lib/postgresql/data",
		"/host/config:/etc/postgresql:ro",
	}
	for i, expected := range expectedBinds {
		if mock.LastHostConfig.Binds[i] != expected {
			t.Errorf("bind %d: got %q, want %q", i, mock.LastHostConfig.Binds[i], expected)
		}
	}
}

func TestCreateContainer_SetsExtraHosts(t *testing.T) {
	mock := &MockDockerClient{}

	cfg := ContainerConfig{
		Name:        "test-container",
		Image:       "alpine:latest",
		NetworkName: "test-net",
	}

	ctx := context.Background()
	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.LastHostConfig == nil {
		t.Fatal("expected host config to be set")
	}

	if len(mock.LastHostConfig.ExtraHosts) != 1 {
		t.Fatalf("expected 1 extra host, got %d", len(mock.LastHostConfig.ExtraHosts))
	}

	expected := "host.docker.internal:host-gateway"
	if mock.LastHostConfig.ExtraHosts[0] != expected {
		t.Errorf("got ExtraHosts[0]=%q, want %q", mock.LastHostConfig.ExtraHosts[0], expected)
	}
}
