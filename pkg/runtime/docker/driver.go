package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runtime"

	"github.com/docker/go-connections/nat"
)

// DockerRuntime implements runtime.WorkloadRuntime using Docker.
type DockerRuntime struct {
	executionMu sync.Mutex
	executions  map[string]*execution.ExecutionContract
	cli         dockerclient.DockerClient
	logger      *slog.Logger
	runtimeInfo *runtime.RuntimeInfo
}

// New creates a new DockerRuntime instance.
func New() (*DockerRuntime, error) {
	cli, err := NewDockerClient()
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{cli: cli, logger: logging.NewDiscardLogger()}, nil
}

// NewWithInfo creates a DockerRuntime using explicit RuntimeInfo for socket selection.
func NewWithInfo(info *runtime.RuntimeInfo) (*DockerRuntime, error) {
	cli, err := NewDockerClientWithHost(info.DockerHost())
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{cli: cli, logger: logging.NewDiscardLogger(), runtimeInfo: info}, nil
}

// NewWithClient creates a DockerRuntime with an existing client (for testing).
func NewWithClient(cli dockerclient.DockerClient) *DockerRuntime {
	return &DockerRuntime{cli: cli, logger: logging.NewDiscardLogger()}
}

// SetLogger sets the logger for Docker runtime operations.
func (d *DockerRuntime) SetLogger(logger *slog.Logger) {
	if logger != nil {
		d.logger = logger
	}
}

// Client returns the underlying Docker client for advanced use cases.
// This is needed by MCP gateway for stdio transport and container logs.
func (d *DockerRuntime) Client() dockerclient.DockerClient {
	return d.cli
}

// RuntimeInfo returns the runtime detection info.
func (d *DockerRuntime) RuntimeInfo() *runtime.RuntimeInfo {
	return d.runtimeInfo
}

// Start starts a workload and returns its status.
func (d *DockerRuntime) Start(ctx context.Context, cfg runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
	if cfg.Execution != nil {
		if cfg.Type != runtime.WorkloadTypeMCPServer || cfg.Execution.Mode != "hardened" {
			return nil, fmt.Errorf("execution.mode: only managed MCP containers are covered")
		}
		if _, err := d.executionPreflight(ctx, cfg.Execution); err != nil {
			return nil, err
		}
	}
	containerName := ContainerName(cfg.Stack, cfg.Name)
	wasRunning := false

	// Check if already exists
	exists, containerID, err := ContainerExists(ctx, d.cli, containerName)
	if err != nil {
		return nil, err
	}

	if exists {
		if cfg.Execution != nil {
			current, imageErr := d.cli.ContainerInspect(ctx, containerID)
			if imageErr != nil {
				return nil, fmt.Errorf("execution.reuse: image observation unavailable")
			}
			wasRunning = current.State != nil && current.State.Running
			_, controlErr := d.inspectExecution(ctx, containerID, cfg.Execution, false)
			if controlErr != nil || current.Config == nil || current.Config.Image != cfg.Image {
				if err := StopContainer(ctx, d.cli, containerID, 5); err != nil {
					return nil, fmt.Errorf("execution.cleanup: stop superseded instance failed")
				}
				if err := RemoveContainer(ctx, d.cli, containerID, false); err != nil {
					return nil, fmt.Errorf("execution.cleanup: remove superseded instance failed")
				}
				exists = false
				wasRunning = false
			}
		}
	}
	if exists {
		if err := StartContainer(ctx, d.cli, containerID); err != nil {
			if cfg.Execution != nil {
				return nil, d.failedExecutionStart(ctx, containerID)
			}
			return nil, err
		}
		if cfg.Execution != nil {
			if _, err := d.CheckExecution(ctx, containerID, cfg.Execution); err != nil {
				if ctx.Err() != nil && !wasRunning {
					return nil, errors.Join(err, d.removeExecutionInstance(ctx, containerID))
				}
				return nil, err
			}
		}
		return d.Status(ctx, runtime.WorkloadID(containerID))
	}

	// Create container config from WorkloadConfig
	dockerCfg := ContainerConfig{
		Execution:   cfg.Execution,
		Name:        containerName,
		LogicalName: cfg.Name, // short name used as DNS alias on the network
		Image:       cfg.Image,
		Command:     cfg.Command,
		Env:         cfg.Env,
		Port:        cfg.ExposedPort,
		HostPort:    cfg.HostPort,
		NetworkName: cfg.NetworkName,
		Labels:      cfg.Labels,
		Transport:   cfg.Transport,
		Volumes:     cfg.Volumes,
		RuntimeInfo: d.runtimeInfo,
	}

	containerID, err = CreateContainer(ctx, d.cli, dockerCfg)
	if err != nil {
		return nil, err
	}
	if cfg.Execution != nil {
		if _, err := d.inspectExecution(ctx, containerID, cfg.Execution, false); err != nil {
			cleanupErr := RemoveContainer(ctx, d.cli, containerID, false)
			if cleanupErr != nil {
				return nil, errors.Join(err, fmt.Errorf("execution.cleanup: remove noncompliant instance failed"))
			}
			return nil, err
		}
	}

	if err := StartContainer(ctx, d.cli, containerID); err != nil {
		if cfg.Execution != nil {
			return nil, d.failedExecutionStart(ctx, containerID)
		}
		return nil, err
	}
	if cfg.Execution != nil {
		if _, err := d.CheckExecution(ctx, containerID, cfg.Execution); err != nil {
			if ctx.Err() != nil {
				return nil, errors.Join(err, d.removeExecutionInstance(ctx, containerID))
			}
			return nil, err
		}
	}

	return d.Status(ctx, runtime.WorkloadID(containerID))
}

// Stop stops a running workload.
func (d *DockerRuntime) Stop(ctx context.Context, id runtime.WorkloadID) error {
	return StopContainer(ctx, d.cli, string(id), 10)
}

// Remove removes a stopped workload.
func (d *DockerRuntime) Remove(ctx context.Context, id runtime.WorkloadID) error {
	if err := RemoveContainer(ctx, d.cli, string(id), true); err != nil {
		return err
	}
	d.executionMu.Lock()
	delete(d.executions, string(id))
	d.executionMu.Unlock()
	return nil
}

// Status returns the current status of a workload.
func (d *DockerRuntime) Status(ctx context.Context, id runtime.WorkloadID) (*runtime.WorkloadStatus, error) {
	d.executionMu.Lock()
	contract := d.executions[string(id)]
	d.executionMu.Unlock()
	var report *execution.Report
	if contract != nil {
		var checkErr error
		report, checkErr = d.CheckExecution(ctx, string(id), contract)
		if checkErr != nil {
			d.logger.Warn("execution eligibility withdrawn", "control", "runtime")
		}
	}
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
	if workloadType == runtime.WorkloadTypeResource && report == nil {
		report = &execution.Report{Mode: "not-covered", Outcome: "not-covered", Runtime: "docker-compatible"}
	}
	endpoint := ""
	if hostPort > 0 {
		endpoint = fmt.Sprintf("localhost:%d", hostPort)
	}

	return &runtime.WorkloadStatus{
		Execution: report,
		ID:        id,
		Name:      name,
		Stack:     info.Config.Labels[LabelStack],
		Type:      workloadType,
		State:     state,
		Message:   info.State.Status,
		Endpoint:  endpoint,
		HostPort:  hostPort,
		Image:     info.Config.Image,
		Labels:    info.Config.Labels,
	}, nil
}

// Exists checks if a workload exists by name.
func (d *DockerRuntime) Exists(ctx context.Context, name string) (bool, runtime.WorkloadID, error) {
	exists, id, err := ContainerExists(ctx, d.cli, name)
	return exists, runtime.WorkloadID(id), err
}

// List returns all workloads matching the filter.
func (d *DockerRuntime) List(ctx context.Context, filter runtime.WorkloadFilter) ([]runtime.WorkloadStatus, error) {
	containers, err := ListManagedContainers(ctx, d.cli, filter.Stack)
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
			ID:      runtime.WorkloadID(c.ID),
			Name:    name,
			Stack:   c.Labels[LabelStack],
			Type:    workloadType,
			State:   state,
			Message: c.Status,
			Image:   c.Image,
			Labels:  c.Labels,
		})
		if workloadType == runtime.WorkloadTypeResource {
			statuses[len(statuses)-1].Execution = &execution.Report{Mode: "not-covered", Outcome: "not-covered", Runtime: "docker-compatible"}
		}
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
	_, err := EnsureNetwork(ctx, d.cli, name, opts.Driver, opts.Stack)
	return err
}

// ListNetworks returns all managed networks for a stack.
func (d *DockerRuntime) ListNetworks(ctx context.Context, stack string) ([]string, error) {
	return ListManagedNetworks(ctx, d.cli, stack)
}

// RemoveNetwork removes a network by name.
func (d *DockerRuntime) RemoveNetwork(ctx context.Context, name string) error {
	return RemoveNetwork(ctx, d.cli, name)
}

// EnsureImage ensures the image is available locally.
func (d *DockerRuntime) EnsureImage(ctx context.Context, imageName string) error {
	return EnsureImage(ctx, d.cli, imageName, d.logger)
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
