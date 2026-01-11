package docker

import (
	"context"
	"fmt"

	"agentlab/pkg/dockerclient"
	"agentlab/pkg/runtime"

	"github.com/docker/go-connections/nat"
)

// DockerRuntime implements runtime.WorkloadRuntime using Docker.
type DockerRuntime struct {
	cli dockerclient.DockerClient
}

// New creates a new DockerRuntime instance.
func New() (*DockerRuntime, error) {
	cli, err := NewDockerClient()
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{cli: cli}, nil
}

// NewWithClient creates a DockerRuntime with an existing client (for testing).
func NewWithClient(cli dockerclient.DockerClient) *DockerRuntime {
	return &DockerRuntime{cli: cli}
}

// Client returns the underlying Docker client for advanced use cases.
// This is needed by MCP gateway for stdio transport and container logs.
func (d *DockerRuntime) Client() dockerclient.DockerClient {
	return d.cli
}

// Start starts a workload and returns its status.
func (d *DockerRuntime) Start(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
	containerName := ContainerName(cfg.Topology, cfg.Name)

	// Check if already exists
	exists, containerID, err := ContainerExists(ctx, d.cli, containerName)
	if err != nil {
		return nil, err
	}

	if exists {
		if err := StartContainer(ctx, d.cli, containerID); err != nil {
			return nil, err
		}
		return d.Status(ctx, runtime.WorkloadID(containerID))
	}

	// Create container config from WorkloadConfig
	dockerCfg := ContainerConfig{
		Name:        containerName,
		Image:       cfg.Image,
		Command:     cfg.Command,
		Env:         cfg.Env,
		Port:        cfg.ExposedPort,
		HostPort:    cfg.HostPort,
		NetworkName: cfg.NetworkName,
		Labels:      cfg.Labels,
		Transport:   cfg.Transport,
		Volumes:     cfg.Volumes,
	}

	containerID, err = CreateContainer(ctx, d.cli, dockerCfg)
	if err != nil {
		return nil, err
	}

	if err := StartContainer(ctx, d.cli, containerID); err != nil {
		return nil, err
	}

	return d.Status(ctx, runtime.WorkloadID(containerID))
}

// Stop stops a running workload.
func (d *DockerRuntime) Stop(ctx context.Context, id runtime.WorkloadID) error {
	return StopContainer(ctx, d.cli, string(id), 10)
}

// Remove removes a stopped workload.
func (d *DockerRuntime) Remove(ctx context.Context, id runtime.WorkloadID) error {
	return RemoveContainer(ctx, d.cli, string(id), true)
}

// Status returns the current status of a workload.
func (d *DockerRuntime) Status(ctx context.Context, id runtime.WorkloadID) (*runtime.WorkloadStatus, error) {
	info, err := d.cli.ContainerInspect(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("inspecting container: %w", err)
	}

	// Convert Docker state to WorkloadState
	state := runtime.WorkloadStateUnknown
	switch info.State.Status {
	case "running":
		state = runtime.WorkloadStateRunning
	case "exited", "dead":
		state = runtime.WorkloadStateStopped
	case "created", "restarting":
		state = runtime.WorkloadStateCreating
	}

	// Extract name (strip leading /)
	name := info.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	// Extract host port (find first mapped port)
	hostPort := 0
	for _, bindings := range info.NetworkSettings.Ports {
		if len(bindings) > 0 {
			_, _ = fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
			break
		}
	}

	// Determine type from labels
	workloadType := runtime.WorkloadType("")
	if info.Config.Labels != nil {
		if _, ok := info.Config.Labels[LabelMCPServer]; ok {
			workloadType = runtime.WorkloadTypeMCPServer
		} else if _, ok := info.Config.Labels[LabelResource]; ok {
			workloadType = runtime.WorkloadTypeResource
		} else if _, ok := info.Config.Labels[LabelAgent]; ok {
			workloadType = runtime.WorkloadTypeAgent
		}
	}

	// Build endpoint
	endpoint := ""
	if hostPort > 0 {
		endpoint = fmt.Sprintf("localhost:%d", hostPort)
	}

	return &runtime.WorkloadStatus{
		ID:       id,
		Name:     name,
		Topology: info.Config.Labels[LabelTopology],
		Type:     workloadType,
		State:    state,
		Message:  info.State.Status,
		Endpoint: endpoint,
		HostPort: hostPort,
		Image:    info.Config.Image,
		Labels:   info.Config.Labels,
	}, nil
}

// Exists checks if a workload exists by name.
func (d *DockerRuntime) Exists(ctx context.Context, name string) (bool, runtime.WorkloadID, error) {
	exists, id, err := ContainerExists(ctx, d.cli, name)
	return exists, runtime.WorkloadID(id), err
}

// List returns all workloads matching the filter.
func (d *DockerRuntime) List(ctx context.Context, filter runtime.WorkloadFilter) ([]runtime.WorkloadStatus, error) {
	containers, err := ListManagedContainers(ctx, d.cli, filter.Topology)
	if err != nil {
		return nil, err
	}

	var statuses []runtime.WorkloadStatus
	for _, c := range containers {
		// Extract name (strip leading /)
		name := c.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}

		// Convert state
		state := runtime.WorkloadStateUnknown
		switch c.State {
		case "running":
			state = runtime.WorkloadStateRunning
		case "exited", "dead":
			state = runtime.WorkloadStateStopped
		case "created", "restarting":
			state = runtime.WorkloadStateCreating
		}

		// Determine type from labels
		workloadType := runtime.WorkloadType("")
		if c.Labels != nil {
			if _, ok := c.Labels[LabelMCPServer]; ok {
				workloadType = runtime.WorkloadTypeMCPServer
			} else if _, ok := c.Labels[LabelResource]; ok {
				workloadType = runtime.WorkloadTypeResource
			} else if _, ok := c.Labels[LabelAgent]; ok {
				workloadType = runtime.WorkloadTypeAgent
			}
		}

		statuses = append(statuses, runtime.WorkloadStatus{
			ID:       runtime.WorkloadID(c.ID),
			Name:     name,
			Topology: c.Labels[LabelTopology],
			Type:     workloadType,
			State:    state,
			Message:  c.Status,
			Image:    c.Image,
			Labels:   c.Labels,
		})
	}

	return statuses, nil
}

// GetHostPort returns the host port for a workload's exposed port.
func (d *DockerRuntime) GetHostPort(ctx context.Context, id runtime.WorkloadID, exposedPort int) (int, error) {
	info, err := d.cli.ContainerInspect(ctx, string(id))
	if err != nil {
		return 0, fmt.Errorf("inspecting container: %w", err)
	}

	portKey := nat.Port(fmt.Sprintf("%d/tcp", exposedPort))
	if bindings, ok := info.NetworkSettings.Ports[portKey]; ok && len(bindings) > 0 {
		var hostPort int
		_, _ = fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
		return hostPort, nil
	}
	return 0, fmt.Errorf("no host port binding for container port %d", exposedPort)
}

// EnsureNetwork creates the network if it doesn't exist.
func (d *DockerRuntime) EnsureNetwork(ctx context.Context, name string, opts runtime.NetworkOptions) error {
	_, err := EnsureNetwork(ctx, d.cli, name, opts.Driver, opts.Topology)
	return err
}

// ListNetworks returns all managed networks for a topology.
func (d *DockerRuntime) ListNetworks(ctx context.Context, topology string) ([]string, error) {
	return ListManagedNetworks(ctx, d.cli, topology)
}

// RemoveNetwork removes a network by name.
func (d *DockerRuntime) RemoveNetwork(ctx context.Context, name string) error {
	return RemoveNetwork(ctx, d.cli, name)
}

// EnsureImage ensures the image is available locally.
func (d *DockerRuntime) EnsureImage(ctx context.Context, imageName string) error {
	return EnsureImage(ctx, d.cli, imageName)
}

// Ping checks if the runtime is accessible.
func (d *DockerRuntime) Ping(ctx context.Context) error {
	return Ping(ctx, d.cli)
}

// Close releases runtime resources.
func (d *DockerRuntime) Close() error {
	return d.cli.Close()
}

// Ensure DockerRuntime implements WorkloadRuntime
var _ runtime.WorkloadRuntime = (*DockerRuntime)(nil)
