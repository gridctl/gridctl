package docker

import (
	"context"
	"fmt"

	"github.com/gridctl/gridctl/pkg/dockerclient"

	"github.com/docker/docker/client"
)

// NewDockerClient creates a new Docker client using environment defaults.
func NewDockerClient() (dockerclient.DockerClient, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	return cli, nil
}

// NewDockerClientWithHost creates a new Docker client pointing at a specific host.
// If host is empty, falls back to environment defaults.
func NewDockerClientWithHost(host string) (dockerclient.DockerClient, error) {
	if host == "" {
		return NewDockerClient()
	}
	cli, err := client.NewClientWithOpts(
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	return cli, nil
}

// Ping checks if the Docker daemon is accessible.
func Ping(ctx context.Context, cli dockerclient.DockerClient) error {
	_, err := cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("docker daemon not accessible: %w", err)
	}
	return nil
}
