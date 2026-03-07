package runtime

import (
	"context"

	"github.com/gridctl/gridctl/pkg/dockerclient"
)

// NewFunc is the type for a factory function that creates an Orchestrator.
// This is set by the docker package at init time to avoid import cycles.
var NewFunc func() (*Orchestrator, error)

// NewWithInfoFunc is the factory function that accepts RuntimeInfo.
// This is set by the docker package at init time to avoid import cycles.
var NewWithInfoFunc func(info *RuntimeInfo) (*Orchestrator, error)

// New creates a new Orchestrator with default runtime detection.
// This is the backward-compatible constructor that delegates to the registered factory.
func New() (*Orchestrator, error) {
	if NewFunc == nil {
		panic("runtime.New called but no factory registered - import github.com/gridctl/gridctl/pkg/runtime/docker")
	}
	return NewFunc()
}

// NewWithInfo creates a new Orchestrator using the provided RuntimeInfo.
// This allows callers to pass pre-detected runtime configuration.
func NewWithInfo(info *RuntimeInfo) (*Orchestrator, error) {
	if NewWithInfoFunc == nil {
		panic("runtime.NewWithInfo called but no factory registered - import github.com/gridctl/gridctl/pkg/runtime/docker")
	}
	return NewWithInfoFunc(info)
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
