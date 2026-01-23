package runtime

import (
	"context"

	"github.com/gridctl/gridctl/pkg/dockerclient"
)

// NewFunc is the type for a factory function that creates an Orchestrator.
// This is set by the docker package at init time to avoid import cycles.
var NewFunc func() (*Orchestrator, error)

// New creates a new Orchestrator with a DockerRuntime.
// This is the backward-compatible constructor that delegates to the registered factory.
func New() (*Orchestrator, error) {
	if NewFunc == nil {
		panic("runtime.New called but no factory registered - import github.com/gridctl/gridctl/pkg/runtime/docker")
	}
	return NewFunc()
}

// GetContainerHostPort is a backward-compatible helper.
// This is set by the docker package at init time.
var GetContainerHostPortFunc func(ctx context.Context, cli dockerclient.DockerClient, containerID string, containerPort int) (int, error)

// GetContainerHostPort returns the host port for a container.
func GetContainerHostPort(ctx context.Context, cli dockerclient.DockerClient, containerID string, containerPort int) (int, error) {
	if GetContainerHostPortFunc == nil {
		panic("GetContainerHostPort called but no implementation registered - import github.com/gridctl/gridctl/pkg/runtime/docker")
	}
	return GetContainerHostPortFunc(ctx, cli, containerID, containerPort)
}

// DockerClientGetter is an interface for types that can return a Docker client.
type DockerClientGetter interface {
	Client() dockerclient.DockerClient
}

// DockerClient returns the Docker client for use by other components.
// This is needed for MCP gateway stdio transport and container logs.
func (o *Orchestrator) DockerClient() dockerclient.DockerClient {
	if getter, ok := o.runtime.(DockerClientGetter); ok {
		return getter.Client()
	}
	return nil
}
