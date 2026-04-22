package controller

import (
	"context"
	"fmt"
	"io"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// ClientBuilder abstracts the Gateway helper that constructs and initialises
// an AgentClient from an MCPServerConfig. Kept as an interface so unit tests
// can supply fakes without spinning up a full gateway.
type ClientBuilder interface {
	BuildAgentClient(ctx context.Context, cfg mcp.MCPServerConfig) (mcp.AgentClient, error)
}

// ProcessSpawner spawns local-process MCP replicas. Each Spawn clones the
// template MCPServerConfig and delegates construction to the gateway's
// BuildAgentClient so the same transport plumbing the static path uses
// also services autoscaled spawns.
type ProcessSpawner struct {
	builder  ClientBuilder
	template mcp.MCPServerConfig
}

// NewProcessSpawner returns a Spawner for LocalProcess (and SSH; see below)
// replicas. template.Name is the logical server name; it's reused for every
// replica and does not need per-replica uniqueness at this layer because the
// ReplicaSet assigns monotonic ids on AddReplica.
func NewProcessSpawner(builder ClientBuilder, template mcp.MCPServerConfig) *ProcessSpawner {
	return &ProcessSpawner{builder: builder, template: template}
}

// Spawn constructs a new replica's AgentClient, initialises MCP, and returns
// the ready-to-serve client. Callers (the Autoscaler) add the result to the
// ReplicaSet via AddReplica.
func (s *ProcessSpawner) Spawn(ctx context.Context) (mcp.AgentClient, error) {
	if s.builder == nil {
		return nil, fmt.Errorf("process spawner %q: nil client builder", s.template.Name)
	}
	return s.builder.BuildAgentClient(ctx, s.template)
}

// Reap closes the replica's client so its process exits.
func (s *ProcessSpawner) Reap(_ context.Context, r *mcp.Replica) error {
	if r == nil {
		return nil
	}
	if closer, ok := r.Client().(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
