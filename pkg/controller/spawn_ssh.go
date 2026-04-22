package controller

import (
	"context"
	"io"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// SSHSpawner spawns SSH-transport MCP replicas. Each Spawn opens a fresh SSH
// session via the gateway's transport switch (BuildAgentClient handles the
// ssh command construction). The template carries SSHHost / SSHUser /
// SSHPort / SSHIdentityFile etc. and is reused verbatim per replica: gridctl
// already tolerates many concurrent ssh sessions to the same host.
type SSHSpawner struct {
	builder  ClientBuilder
	template mcp.MCPServerConfig
}

// NewSSHSpawner returns a Spawner for SSH-transport replicas.
func NewSSHSpawner(builder ClientBuilder, template mcp.MCPServerConfig) *SSHSpawner {
	return &SSHSpawner{builder: builder, template: template}
}

// Spawn constructs and initialises a new SSH-backed AgentClient.
func (s *SSHSpawner) Spawn(ctx context.Context) (mcp.AgentClient, error) {
	return s.builder.BuildAgentClient(ctx, s.template)
}

// Reap closes the SSH stream so the remote process exits.
func (s *SSHSpawner) Reap(_ context.Context, r *mcp.Replica) error {
	if r == nil {
		return nil
	}
	if closer, ok := r.Client().(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
