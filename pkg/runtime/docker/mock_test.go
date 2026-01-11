package docker

import (
	"context"
	"io"
	"strings"

	"agentlab/pkg/dockerclient"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// MockDockerClient is a mock implementation of DockerClient for testing.
type MockDockerClient struct {
	// State
	Containers []types.Container
	Networks   []network.Summary
	Images     []image.Summary

	// ContainerJSON responses for ContainerInspect (keyed by container ID)
	ContainerDetails map[string]types.ContainerJSON

	// Error injection per method
	PingError             error
	ContainerCreateError  error
	ContainerStartError   error
	ContainerStopError    error
	ContainerRemoveError  error
	ContainerListError    error
	ContainerInspectError error
	NetworkCreateError    error
	NetworkRemoveError    error
	NetworkListError      error
	ImageListError        error
	ImagePullError        error

	// Call tracking
	Calls []string

	// Created containers (for tracking what was created)
	CreatedContainers []string
	// Started containers
	StartedContainers []string
	// Stopped containers
	StoppedContainers []string
	// Removed containers
	RemovedContainers []string
	// Created networks
	CreatedNetworks []string
	// Removed networks
	RemovedNetworks []string
	// Pulled images
	PulledImages []string

	// Last host config passed to ContainerCreate (for verifying volume mounts, etc.)
	LastHostConfig *container.HostConfig
}

func (m *MockDockerClient) recordCall(name string) {
	m.Calls = append(m.Calls, name)
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error) {
	m.recordCall("ContainerCreate")
	if m.ContainerCreateError != nil {
		return container.CreateResponse{}, m.ContainerCreateError
	}
	m.CreatedContainers = append(m.CreatedContainers, containerName)
	m.LastHostConfig = hostConfig
	return container.CreateResponse{ID: "mock-container-" + containerName}, nil
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.recordCall("ContainerStart")
	if m.ContainerStartError != nil {
		return m.ContainerStartError
	}
	m.StartedContainers = append(m.StartedContainers, containerID)
	return nil
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.recordCall("ContainerStop")
	if m.ContainerStopError != nil {
		return m.ContainerStopError
	}
	m.StoppedContainers = append(m.StoppedContainers, containerID)
	return nil
}

func (m *MockDockerClient) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	m.recordCall("ContainerRestart")
	return nil
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.recordCall("ContainerRemove")
	if m.ContainerRemoveError != nil {
		return m.ContainerRemoveError
	}
	m.RemovedContainers = append(m.RemovedContainers, containerID)
	return nil
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
	m.recordCall("ContainerLogs")
	return io.NopCloser(strings.NewReader("mock log line")), nil
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	m.recordCall("ContainerList")
	if m.ContainerListError != nil {
		return nil, m.ContainerListError
	}
	return m.Containers, nil
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	m.recordCall("ContainerInspect")
	if m.ContainerInspectError != nil {
		return types.ContainerJSON{}, m.ContainerInspectError
	}
	if m.ContainerDetails != nil {
		if details, ok := m.ContainerDetails[containerID]; ok {
			return details, nil
		}
	}
	// Return default empty response
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			Name: "/" + containerID,
			State: &types.ContainerState{
				Status: "running",
			},
		},
		Config: &container.Config{
			Labels: map[string]string{},
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: make(map[string]*network.EndpointSettings),
			NetworkSettingsBase: types.NetworkSettingsBase{
				Ports: nat.PortMap{},
			},
		},
	}, nil
}

func (m *MockDockerClient) ContainerAttach(ctx context.Context, container string, options container.AttachOptions) (types.HijackedResponse, error) {
	m.recordCall("ContainerAttach")
	return types.HijackedResponse{}, nil
}

func (m *MockDockerClient) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error) {
	m.recordCall("NetworkList")
	if m.NetworkListError != nil {
		return nil, m.NetworkListError
	}
	return m.Networks, nil
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	m.recordCall("NetworkCreate")
	if m.NetworkCreateError != nil {
		return network.CreateResponse{}, m.NetworkCreateError
	}
	m.CreatedNetworks = append(m.CreatedNetworks, name)
	return network.CreateResponse{ID: "mock-network-" + name}, nil
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	m.recordCall("NetworkRemove")
	if m.NetworkRemoveError != nil {
		return m.NetworkRemoveError
	}
	m.RemovedNetworks = append(m.RemovedNetworks, networkID)
	return nil
}

func (m *MockDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	m.recordCall("ImageList")
	if m.ImageListError != nil {
		return nil, m.ImageListError
	}
	return m.Images, nil
}

func (m *MockDockerClient) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	m.recordCall("ImagePull")
	if m.ImagePullError != nil {
		return nil, m.ImagePullError
	}
	m.PulledImages = append(m.PulledImages, ref)
	return io.NopCloser(strings.NewReader(`{"status":"Pull complete"}`)), nil
}

func (m *MockDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	m.recordCall("ImageBuild")
	return types.ImageBuildResponse{
		Body: io.NopCloser(strings.NewReader(`{"stream":"Successfully built mock-image"}`)),
	}, nil
}

func (m *MockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	m.recordCall("Ping")
	return types.Ping{}, m.PingError
}

func (m *MockDockerClient) Close() error {
	m.recordCall("Close")
	return nil
}

// Ensure MockDockerClient implements DockerClient
var _ dockerclient.DockerClient = &MockDockerClient{}
