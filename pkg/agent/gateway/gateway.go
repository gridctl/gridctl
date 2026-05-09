// Package gateway adapts the existing pkg/mcp.Gateway into the
// agent.ToolCaller surface the runtime invokes during agentic loops.
// Tool calls flow through Gateway.CallTool so existing tracing,
// pricing, replica routing, vault auth, and tool whitelisting apply
// unchanged — the runtime never sees a tool any differently than an
// upstream MCP client would.
//
// The package is intentionally small. It exists because pkg/agent
// must not import pkg/mcp at the type-surface level (the surface is
// used by provider packages that should remain decoupled from gateway
// internals); the adapter constructs a thin agent.ToolCaller from a
// *mcp.Gateway so wiring code in pkg/controller/gateway_builder.go
// (which already imports both packages) can hand the runtime a
// caller without introducing a new dependency edge.
package gateway

import (
	"context"
	"errors"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// ErrNilGateway is returned by NewToolCaller when the gateway argument
// is nil. Callers that hold a nullable *mcp.Gateway should branch on
// the value before constructing a caller; the explicit error keeps
// nil-deref crashes out of agent code paths.
var ErrNilGateway = errors.New("agent/gateway: nil *mcp.Gateway")

// toolCaller is the concrete adapter. It is unexported so callers
// cannot mutate the wrapped gateway pointer after construction.
type toolCaller struct {
	gw *mcp.Gateway
}

// NewToolCaller wraps a *mcp.Gateway as an agent.ToolCaller. Returns
// (nil, ErrNilGateway) when gw is nil. The returned caller delegates
// directly to gw.CallTool; the gateway's existing observers,
// schema-pinning gate, and replica routing apply on every call.
func NewToolCaller(gw *mcp.Gateway) (agent.ToolCaller, error) {
	if gw == nil {
		return nil, ErrNilGateway
	}
	return &toolCaller{gw: gw}, nil
}

// CallTool dispatches to the underlying gateway. The arguments map is
// passed through verbatim; provider-specific argument coercion happens
// at the runtime layer before this call.
func (t *toolCaller) CallTool(ctx context.Context, name string, arguments map[string]any) (*agent.ToolCallResult, error) {
	return t.gw.CallTool(ctx, name, arguments)
}
