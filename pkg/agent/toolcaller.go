package agent

import (
	"context"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// ToolCallResult is the gridctl-shaped result envelope returned by a
// ToolCaller. It is a type alias of mcp.ToolCallResult so the agent
// runtime and the gateway speak the same vocabulary — the runtime is
// layered on top of the gateway, not parallel to it. Phase C extends
// the gateway's CallTool path to expose typed Skill results through
// this same envelope; see pkg/registry/server.go.
type ToolCallResult = mcp.ToolCallResult

// ToolCaller invokes tools across the gateway's aggregated servers.
// It mirrors mcp.ToolCaller exactly so a *mcp.Gateway satisfies the
// interface; the wrapper in pkg/agent/gateway exists to convert the
// import direction (agent depends on mcp; mcp does not depend on
// agent) and to give the runtime a place to attach observability that
// only matters for agent-initiated calls.
type ToolCaller interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error)
}
