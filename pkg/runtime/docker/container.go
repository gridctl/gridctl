package docker

import (
	"context"
	"fmt"

	"agentlab/pkg/dockerclient"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// ContainerConfig holds the configuration for creating a container.
type ContainerConfig struct {
	Name        string
	Image       string
	Command     []string // Override container command
	Env         map[string]string
	Port        int // Container port
	HostPort    int // Host port to publish (0 = auto-assign)
	NetworkName string
	Labels      map[string]string
	Transport   string   // "http" or "stdio"
	Volumes     []string // Volume mounts in "host:container" or "host:container:mode" format
}

// CreateContainer creates a new container with the given configuration.
func CreateContainer(ctx context.Context, cli dockerclient.DockerClient, cfg ContainerConfig) (string, error) {
	// Convert env map to slice
	var envSlice []string
	for k, v := range cfg.Env {
		envSlice = append(envSlice, k+"="+v)
	}

	// Configure exposed port
	exposedPorts := nat.PortSet{}
	if cfg.Port > 0 {
		port := nat.Port(fmt.Sprintf("%d/tcp", cfg.Port))
		exposedPorts[port] = struct{}{}
	}

	containerConfig := &container.Config{
		Image:        cfg.Image,
		Cmd:          cfg.Command,
		Env:          envSlice,
		Labels:       cfg.Labels,
		ExposedPorts: exposedPorts,
		OpenStdin:    cfg.Transport == "stdio",
		AttachStdin:  cfg.Transport == "stdio",
		AttachStdout: cfg.Transport == "stdio",
		AttachStderr: cfg.Transport == "stdio",
	}

	// Configure port bindings
	portBindings := nat.PortMap{}
	if cfg.Port > 0 {
		port := nat.Port(fmt.Sprintf("%d/tcp", cfg.Port))
		hostPort := ""
		if cfg.HostPort > 0 {
			hostPort = fmt.Sprintf("%d", cfg.HostPort)
		}
		portBindings[port] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: hostPort},
		}
	}

	hostConfig := &container.HostConfig{
		NetworkMode:  container.NetworkMode(cfg.NetworkName),
		PortBindings: portBindings,
		Binds:        cfg.Volumes,
		ExtraHosts:   []string{"host.docker.internal:host-gateway"},
	}

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			cfg.NetworkName: {
				Aliases: []string{cfg.Name}, // DNS name within network
			},
		},
	}

	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, cfg.Name)
	if err != nil {
		return "", fmt.Errorf("creating container %s: %w", cfg.Name, err)
	}

	return resp.ID, nil
}

// StartContainer starts a container by ID.
func StartContainer(ctx context.Context, cli dockerclient.DockerClient, containerID string) error {
	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}
	return nil
}

// StopContainer stops a container gracefully.
func StopContainer(ctx context.Context, cli dockerclient.DockerClient, containerID string, timeoutSecs int) error {
	timeout := timeoutSecs
	if err := cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("stopping container: %w", err)
	}
	return nil
}

// RemoveContainer removes a container.
func RemoveContainer(ctx context.Context, cli dockerclient.DockerClient, containerID string, force bool) error {
	if err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}
	return nil
}

// ContainerExists checks if a container with the given name exists.
func ContainerExists(ctx context.Context, cli dockerclient.DockerClient, name string) (bool, string, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return false, "", fmt.Errorf("listing containers: %w", err)
	}

	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name || n == name {
				return true, c.ID, nil
			}
		}
	}
	return false, "", nil
}

// ListManagedContainers returns all containers managed by agentlab.
func ListManagedContainers(ctx context.Context, cli dockerclient.DockerClient, topology string) ([]types.Container, error) {
	filterArgs := filters.NewArgs(
		filters.Arg("label", LabelManaged+"=true"),
	)
	if topology != "" {
		filterArgs.Add("label", LabelTopology+"="+topology)
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	return containers, nil
}

// GetContainerIP returns the IP address of a container on the specified network.
func GetContainerIP(ctx context.Context, cli dockerclient.DockerClient, containerID, networkName string) (string, error) {
	info, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}

	if network, ok := info.NetworkSettings.Networks[networkName]; ok {
		return network.IPAddress, nil
	}
	return "", fmt.Errorf("container not connected to network %s", networkName)
}

// GetContainerHostPort returns the host port mapped to a container port.
func GetContainerHostPort(ctx context.Context, cli dockerclient.DockerClient, containerID string, containerPort int) (int, error) {
	info, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("inspecting container: %w", err)
	}

	portKey := nat.Port(fmt.Sprintf("%d/tcp", containerPort))
	if bindings, ok := info.NetworkSettings.Ports[portKey]; ok && len(bindings) > 0 {
		var hostPort int
		_, _ = fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
		return hostPort, nil
	}
	return 0, fmt.Errorf("no host port binding for container port %d", containerPort)
}
