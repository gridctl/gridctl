package docker

import (
	"context"
	"fmt"
	"testing"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runtime"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

func TestNewWithClient(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	if rt == nil {
		t.Fatal("expected non-nil runtime")
	}
	if rt.Client() != mock {
		t.Error("expected Client() to return the mock")
	}
}

func TestDockerRuntime_SetLogger(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	// Should not panic with nil
	rt.SetLogger(nil)

	logger := logging.NewDiscardLogger()
	rt.SetLogger(logger)
}

func TestDockerRuntime_Ping(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.Ping(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.Calls) != 1 || mock.Calls[0] != "Ping" {
		t.Errorf("expected Ping call, got %v", mock.Calls)
	}
}

func TestDockerRuntime_Ping_Error(t *testing.T) {
	mock := &MockDockerClient{
		PingError: fmt.Errorf("daemon unavailable"),
	}
	rt := NewWithClient(mock)

	err := rt.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_Close(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.Calls) != 1 || mock.Calls[0] != "Close" {
		t.Errorf("expected Close call, got %v", mock.Calls)
	}
}

func TestDockerRuntime_Start_NewContainer(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	cfg := runtime.WorkloadConfig{
		Name:        "server1",
		Stack:       "test-stack",
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "mcp:latest",
		ExposedPort: 3000,
		HostPort:    9000,
		NetworkName: "test-net",
		Labels: map[string]string{
			LabelManaged:   "true",
			LabelStack:     "test-stack",
			LabelMCPServer: "server1",
		},
	}

	status, err := rt.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status == nil {
		t.Fatal("expected non-nil status")
	}

	// Verify container was created and started
	if len(mock.CreatedContainers) != 1 {
		t.Errorf("expected 1 container created, got %d", len(mock.CreatedContainers))
	}
	if len(mock.StartedContainers) != 1 {
		t.Errorf("expected 1 container started, got %d", len(mock.StartedContainers))
	}
}

func TestDockerRuntime_Start_ExistingContainer(t *testing.T) {
	containerName := ContainerName("test-stack", "server1")
	mock := &MockDockerClient{
		Containers: []container.Summary{
			{
				ID:    "existing-container-id",
				Names: []string{"/" + containerName},
			},
		},
	}
	rt := NewWithClient(mock)

	cfg := runtime.WorkloadConfig{
		Name:        "server1",
		Stack:       "test-stack",
		Image:       "mcp:latest",
		ExposedPort: 3000,
		NetworkName: "test-net",
	}

	status, err := rt.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status == nil {
		t.Fatal("expected non-nil status")
	}

	// Should not create new container, just start existing
	if len(mock.CreatedContainers) != 0 {
		t.Errorf("expected no containers created, got %d", len(mock.CreatedContainers))
	}
	if len(mock.StartedContainers) != 1 || mock.StartedContainers[0] != "existing-container-id" {
		t.Errorf("expected existing container to be started, got %v", mock.StartedContainers)
	}
}

func TestDockerRuntime_Start_CreateError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerCreateError: fmt.Errorf("create failed"),
	}
	rt := NewWithClient(mock)

	cfg := runtime.WorkloadConfig{
		Name:        "server1",
		Stack:       "test-stack",
		Image:       "mcp:latest",
		NetworkName: "test-net",
	}

	_, err := rt.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_Start_StartError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerStartError: fmt.Errorf("start failed"),
	}
	rt := NewWithClient(mock)

	cfg := runtime.WorkloadConfig{
		Name:        "server1",
		Stack:       "test-stack",
		Image:       "mcp:latest",
		NetworkName: "test-net",
	}

	_, err := rt.Start(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_Stop(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.Stop(context.Background(), runtime.WorkloadID("container-123"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.StoppedContainers) != 1 || mock.StoppedContainers[0] != "container-123" {
		t.Errorf("expected container-123 stopped, got %v", mock.StoppedContainers)
	}
}

func TestDockerRuntime_Stop_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerStopError: fmt.Errorf("stop failed"),
	}
	rt := NewWithClient(mock)

	err := rt.Stop(context.Background(), runtime.WorkloadID("container-123"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_Remove(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.Remove(context.Background(), runtime.WorkloadID("container-123"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedContainers) != 1 || mock.RemovedContainers[0] != "container-123" {
		t.Errorf("expected container-123 removed, got %v", mock.RemovedContainers)
	}
}

func TestDockerRuntime_Remove_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerRemoveError: fmt.Errorf("remove failed"),
	}
	rt := NewWithClient(mock)

	err := rt.Remove(context.Background(), runtime.WorkloadID("container-123"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_Status_Running(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "running"},
				},
				Config: &container.Config{
					Image: "mcp:latest",
					Labels: map[string]string{
						LabelManaged:   "true",
						LabelStack:     "test",
						LabelMCPServer: "server1",
					},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{
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
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.State != runtime.WorkloadStateRunning {
		t.Errorf("expected state 'running', got %q", status.State)
	}
	if status.Name != "gridctl-test-server1" {
		t.Errorf("expected name 'gridctl-test-server1', got %q", status.Name)
	}
	if status.Type != runtime.WorkloadTypeMCPServer {
		t.Errorf("expected type 'mcp-server', got %q", status.Type)
	}
	if status.HostPort != 9000 {
		t.Errorf("expected host port 9000, got %d", status.HostPort)
	}
	if status.Endpoint != "localhost:9000" {
		t.Errorf("expected endpoint 'localhost:9000', got %q", status.Endpoint)
	}
	if status.Stack != "test" {
		t.Errorf("expected stack 'test', got %q", status.Stack)
	}
	if status.Image != "mcp:latest" {
		t.Errorf("expected image 'mcp:latest', got %q", status.Image)
	}
}

func TestDockerRuntime_Status_Stopped(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "exited"},
				},
				Config: &container.Config{
					Labels: map[string]string{},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.State != runtime.WorkloadStateStopped {
		t.Errorf("expected state 'stopped', got %q", status.State)
	}
}

func TestDockerRuntime_Status_Creating(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "created"},
				},
				Config: &container.Config{
					Labels: map[string]string{},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.State != runtime.WorkloadStateCreating {
		t.Errorf("expected state 'creating', got %q", status.State)
	}
}

func TestDockerRuntime_Status_Dead(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "dead"},
				},
				Config: &container.Config{
					Labels: map[string]string{},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.State != runtime.WorkloadStateStopped {
		t.Errorf("expected state 'stopped' for dead container, got %q", status.State)
	}
}

func TestDockerRuntime_Status_Unknown(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "paused"},
				},
				Config: &container.Config{
					Labels: map[string]string{},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.State != runtime.WorkloadStateUnknown {
		t.Errorf("expected state 'unknown' for unrecognized status, got %q", status.State)
	}
}

func TestDockerRuntime_Status_ResourceType(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-postgres",
					State: &container.State{Status: "running"},
				},
				Config: &container.Config{
					Labels: map[string]string{
						LabelManaged:  "true",
						LabelStack:    "test",
						LabelResource: "postgres",
					},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Type != runtime.WorkloadTypeResource {
		t.Errorf("expected type 'resource', got %q", status.Type)
	}
}


func TestDockerRuntime_Status_NoPort(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "running"},
				},
				Config: &container.Config{
					Labels: map[string]string{},
				},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	status, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.HostPort != 0 {
		t.Errorf("expected host port 0, got %d", status.HostPort)
	}
	if status.Endpoint != "" {
		t.Errorf("expected empty endpoint, got %q", status.Endpoint)
	}
}

func TestDockerRuntime_Status_InspectError(t *testing.T) {
	mock := &MockDockerClient{
		ContainerInspectError: fmt.Errorf("inspect failed"),
	}
	rt := NewWithClient(mock)

	_, err := rt.Status(context.Background(), runtime.WorkloadID("c1"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_Exists(t *testing.T) {
	containerName := ContainerName("test", "server1")
	mock := &MockDockerClient{
		Containers: []container.Summary{
			{
				ID:    "abc123",
				Names: []string{"/" + containerName},
			},
		},
	}
	rt := NewWithClient(mock)

	exists, id, err := rt.Exists(context.Background(), containerName)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected container to exist")
	}
	if id != runtime.WorkloadID("abc123") {
		t.Errorf("expected ID 'abc123', got %q", id)
	}
}

func TestDockerRuntime_Exists_NotFound(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	exists, _, err := rt.Exists(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected container to not exist")
	}
}

func TestDockerRuntime_List(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []container.Summary{
			{
				ID:    "c1",
				Names: []string{"/gridctl-test-server1"},
				State: "running",
				Image: "mcp:latest",
				Labels: map[string]string{
					LabelManaged:   "true",
					LabelStack:     "test",
					LabelMCPServer: "server1",
				},
			},
			{
				ID:    "c2",
				Names: []string{"/gridctl-test-postgres"},
				State: "running",
				Image: "postgres:16",
				Labels: map[string]string{
					LabelManaged:  "true",
					LabelStack:    "test",
					LabelResource: "postgres",
				},
			},
		},
	}
	rt := NewWithClient(mock)

	statuses, err := rt.List(context.Background(), runtime.WorkloadFilter{Stack: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Verify first container
	if statuses[0].ID != runtime.WorkloadID("c1") {
		t.Errorf("expected ID 'c1', got %q", statuses[0].ID)
	}
	if statuses[0].Name != "gridctl-test-server1" {
		t.Errorf("expected name 'gridctl-test-server1', got %q", statuses[0].Name)
	}
	if statuses[0].State != runtime.WorkloadStateRunning {
		t.Errorf("expected state 'running', got %q", statuses[0].State)
	}
	if statuses[0].Type != runtime.WorkloadTypeMCPServer {
		t.Errorf("expected type 'mcp-server', got %q", statuses[0].Type)
	}

	// Verify second container
	if statuses[1].Type != runtime.WorkloadTypeResource {
		t.Errorf("expected type 'resource', got %q", statuses[1].Type)
	}
}

func TestDockerRuntime_List_StoppedContainers(t *testing.T) {
	mock := &MockDockerClient{
		Containers: []container.Summary{
			{
				ID:     "c1",
				Names:  []string{"/gridctl-test-server1"},
				State:  "exited",
				Labels: map[string]string{LabelManaged: "true", LabelStack: "test"},
			},
		},
	}
	rt := NewWithClient(mock)

	statuses, err := rt.List(context.Background(), runtime.WorkloadFilter{Stack: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != runtime.WorkloadStateStopped {
		t.Errorf("expected state 'stopped', got %q", statuses[0].State)
	}
}

func TestDockerRuntime_List_Empty(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	statuses, err := rt.List(context.Background(), runtime.WorkloadFilter{Stack: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}

func TestDockerRuntime_List_Error(t *testing.T) {
	mock := &MockDockerClient{
		ContainerListError: fmt.Errorf("list failed"),
	}
	rt := NewWithClient(mock)

	_, err := rt.List(context.Background(), runtime.WorkloadFilter{Stack: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_GetHostPort(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "running"},
				},
				Config: &container.Config{Labels: map[string]string{}},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{
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
	rt := NewWithClient(mock)

	port, err := rt.GetHostPort(context.Background(), runtime.WorkloadID("c1"), 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 9000 {
		t.Errorf("expected port 9000, got %d", port)
	}
}

func TestDockerRuntime_GetHostPort_NotMapped(t *testing.T) {
	mock := &MockDockerClient{
		ContainerDetails: map[string]container.InspectResponse{
			"c1": {
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/gridctl-test-server1",
					State: &container.State{Status: "running"},
				},
				Config: &container.Config{Labels: map[string]string{}},
				NetworkSettings: &container.NetworkSettings{
					Networks:           map[string]*network.EndpointSettings{},
					NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{}},
				},
			},
		},
	}
	rt := NewWithClient(mock)

	_, err := rt.GetHostPort(context.Background(), runtime.WorkloadID("c1"), 3000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDockerRuntime_EnsureNetwork(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.EnsureNetwork(context.Background(), "test-net", runtime.NetworkOptions{
		Driver: "bridge",
		Stack:  "my-stack",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.CreatedNetworks) != 1 || mock.CreatedNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' created, got %v", mock.CreatedNetworks)
	}
}

func TestDockerRuntime_ListNetworks(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{Name: "net-1"},
			{Name: "net-2"},
		},
	}
	rt := NewWithClient(mock)

	names, err := rt.ListNetworks(context.Background(), "my-stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 networks, got %d", len(names))
	}
}

func TestDockerRuntime_RemoveNetwork(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.RemoveNetwork(context.Background(), "test-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedNetworks) != 1 || mock.RemovedNetworks[0] != "test-net" {
		t.Errorf("expected 'test-net' removed, got %v", mock.RemovedNetworks)
	}
}

func TestDockerRuntime_EnsureImage(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)

	err := rt.EnsureImage(context.Background(), "test:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.PulledImages) != 1 || mock.PulledImages[0] != "test:latest" {
		t.Errorf("expected 'test:latest' pulled, got %v", mock.PulledImages)
	}
}
