package docker

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

func TestCreateContainer(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "test-server",
		Image:       "test:latest",
		Env:         map[string]string{"KEY": "value"},
		Port:        3000,
		HostPort:    9000,
		NetworkName: "test-net",
		Labels:      map[string]string{LabelManaged: "true"},
	}

	containerID, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if containerID != "mock-container-test-server" {
		t.Errorf("expected container ID 'mock-container-test-server', got %q", containerID)
	}

	if len(mock.CreatedContainers) != 1 || mock.CreatedContainers[0] != "test-server" {
		t.Errorf("expected container 'test-server' to be created, got %v", mock.CreatedContainers)
	}

	// Verify port binding was configured
	if mock.LastHostConfig == nil {
		t.Fatal("expected host config to be set")
	}
	portKey := nat.Port("3000/tcp")
	bindings, ok := mock.LastHostConfig.PortBindings[portKey]
	if !ok {
		t.Fatal("expected port binding for 3000/tcp")
	}
	if len(bindings) != 1 || bindings[0].HostPort != "9000" {
		t.Errorf("expected host port 9000, got %v", bindings)
	}
}

func TestCreateContainer_AutoAssignPort(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "test-server",
		Image:       "test:latest",
		Port:        3000,
		HostPort:    0, // Auto-assign
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify host port is empty string (Docker auto-assigns)
	portKey := nat.Port("3000/tcp")
	bindings := mock.LastHostConfig.PortBindings[portKey]
	if len(bindings) != 1 || bindings[0].HostPort != "" {
		t.Errorf("expected empty host port for auto-assign, got %v", bindings)
	}
}

func TestCreateContainer_NoPort(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "test-server",
		Image:       "test:latest",
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no port bindings
	if len(mock.LastHostConfig.PortBindings) != 0 {
		t.Errorf("expected no port bindings, got %v", mock.LastHostConfig.PortBindings)
	}
}

func TestCreateContainer_StdioTransport(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "stdio-server",
		Image:       "stdio:latest",
		Transport:   "stdio",
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify call was made (stdio config is set on container.Config, not host config)
	if len(mock.CreatedContainers) != 1 {
		t.Errorf("expected 1 container created, got %d", len(mock.CreatedContainers))
	}
}

func TestCreateContainer_WithCommand(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "cmd-server",
		Image:       "test:latest",
		Command:     []string{"python", "server.py"},
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.CreatedContainers) != 1 {
		t.Errorf("expected 1 container created, got %d", len(mock.CreatedContainers))
	}
}

func TestCreateContainer_WithVolumes(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "vol-server",
		Image:       "test:latest",
		Volumes:     []string{"/host/data:/container/data:ro"},
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.LastHostConfig.Binds) != 1 || mock.LastHostConfig.Binds[0] != "/host/data:/container/data:ro" {
		t.Errorf("expected volume mount, got %v", mock.LastHostConfig.Binds)
	}
}

func TestCreateContainer_Error(t *testing.T) {
	mock := &MockDockerClient{}
	mock.ContainerCreateError = fmt.Errorf("create failed")
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "test-server",
		Image:       "test:latest",
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateContainer_HostDockerInternal(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	cfg := ContainerConfig{
		Name:        "test-server",
		Image:       "test:latest",
		NetworkName: "test-net",
	}

	_, err := CreateContainer(ctx, mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify host.docker.internal is set
	found := false
	for _, host := range mock.LastHostConfig.ExtraHosts {
		if host == "host.docker.internal:host-gateway" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected host.docker.internal:host-gateway in ExtraHosts")
	}
}

func TestStartContainer(t *testing.T) {
	mock := &MockDockerClient{}
	err := StartContainer(context.Background(), mock, "container-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.StartedContainers) != 1 || mock.StartedContainers[0] != "container-123" {
		t.Errorf("expected container 'container-123' to be started, got %v", mock.StartedContainers)
	}
}

func TestStartContainer_Error(t *testing.T) {
	mock := &MockDockerClient{}
	mock.ContainerStartError = fmt.Errorf("start failed")

	err := StartContainer(context.Background(), mock, "container-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestStopContainer(t *testing.T) {
	mock := &MockDockerClient{}
	err := StopContainer(context.Background(), mock, "container-123", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.StoppedContainers) != 1 || mock.StoppedContainers[0] != "container-123" {
		t.Errorf("expected container 'container-123' to be stopped, got %v", mock.StoppedContainers)
	}
}

func TestStopContainer_Error(t *testing.T) {
	mock := &MockDockerClient{}
	mock.ContainerStopError = fmt.Errorf("stop failed")

	err := StopContainer(context.Background(), mock, "container-123", 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRemoveContainer(t *testing.T) {
	mock := &MockDockerClient{}
	err := RemoveContainer(context.Background(), mock, "container-123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedContainers) != 1 || mock.RemovedContainers[0] != "container-123" {
		t.Errorf("expected container 'container-123' to be removed, got %v", mock.RemovedContainers)
	}
}

func TestRemoveContainer_Error(t *testing.T) {
	mock := &MockDockerClient{}
	mock.ContainerRemoveError = fmt.Errorf("remove failed")

	err := RemoveContainer(context.Background(), mock, "container-123", true)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestContainerExists_Found(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123",
				Names: []string{"/my-container"},
			},
		},
	}

	exists, id, err := ContainerExists(context.Background(), mock, "my-container")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected container to exist")
	}
	if id != "abc123" {
		t.Errorf("expected ID 'abc123', got %q", id)
	}
}

func TestContainerExists_NotFound(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{
				ID:    "abc123",
				Names: []string{"/other-container"},
			},
		},
	}

	exists, id, err := ContainerExists(context.Background(), mock, "my-container")
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

func TestContainerExists_Empty(t *testing.T) {
	mock := &MockDockerClient{}

	exists, _, err := ContainerExists(context.Background(), mock, "my-container")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected container to not exist")
	}
}

func TestContainerExists_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerListError: fmt.Errorf("list failed"),
	}

	_, _, err := ContainerExists(context.Background(), mock, "my-container")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListManagedContainers(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{ID: "c1", Names: []string{"/gridctl-test-server1"}},
			{ID: "c2", Names: []string{"/gridctl-test-server2"}},
		},
	}

	containers, err := ListManagedContainers(context.Background(), mock, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(containers))
	}
}

func TestListManagedContainers_NoStack(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []types.Container{
			{ID: "c1", Names: []string{"/gridctl-a-server1"}},
		},
	}

	containers, err := ListManagedContainers(context.Background(), mock, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(containers))
	}
}

func TestListManagedContainers_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerListError: fmt.Errorf("list failed"),
	}

	_, err := ListManagedContainers(context.Background(), mock, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerIP(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"c1": {
				ContainerJSONBase: &types.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &types.ContainerState{Status: "running"},
				},
				Config: &container.Config{Labels: map[string]string{}},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"test-net": {IPAddress: "172.18.0.2"},
					},
					NetworkSettingsBase: types.NetworkSettingsBase{
						Ports: nat.PortMap{},
					},
				},
			},
		},
	}

	ip, err := GetContainerIP(context.Background(), mock, "c1", "test-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "172.18.0.2" {
		t.Errorf("expected IP '172.18.0.2', got %q", ip)
	}
}

func TestGetContainerIP_NetworkNotFound(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"c1": {
				ContainerJSONBase: &types.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &types.ContainerState{Status: "running"},
				},
				Config: &container.Config{Labels: map[string]string{}},
				NetworkSettings: &types.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: types.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}

	_, err := GetContainerIP(context.Background(), mock, "c1", "missing-net")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerIP_InspectError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerInspectError: fmt.Errorf("inspect failed"),
	}

	_, err := GetContainerIP(context.Background(), mock, "c1", "test-net")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerHostPort(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"c1": {
				ContainerJSONBase: &types.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &types.ContainerState{Status: "running"},
				},
				Config: &container.Config{Labels: map[string]string{}},
				NetworkSettings: &types.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
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

	port, err := GetContainerHostPort(context.Background(), mock, "c1", 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 9000 {
		t.Errorf("expected port 9000, got %d", port)
	}
}

func TestGetContainerHostPort_NotMapped(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]types.ContainerJSON{
			"c1": {
				ContainerJSONBase: &types.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &types.ContainerState{Status: "running"},
				},
				Config: &container.Config{Labels: map[string]string{}},
				NetworkSettings: &types.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: types.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}

	_, err := GetContainerHostPort(context.Background(), mock, "c1", 3000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetContainerHostPort_InspectError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerInspectError: fmt.Errorf("inspect failed"),
	}

	_, err := GetContainerHostPort(context.Background(), mock, "c1", 3000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
