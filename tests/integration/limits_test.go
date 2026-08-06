//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/limits"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/token"
)

// installLimits compiles cfg and wires it onto gw.
func installLimits(gw *mcp.Gateway, cfg *config.LimitsConfig) *limits.Policy {
	pol := limits.NewPolicy(cfg, nil)
	if pol != nil {
		gw.SetCallGates(pol.Gates())
	} else {
		gw.SetCallGates(nil)
	}
	return pol
}

// TestLimits_EnforcedAtDispatch is the end-to-end guard for rate limits: a
// real mock MCP server behind the gateway, the real metrics observer, and
// enforcement asserted on the direct path and through the code-mode sandbox.
func TestLimits_EnforcedAtDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	newGateway := func(t *testing.T) *mcp.Gateway {
		t.Helper()
		gw := mcp.NewGateway()
		t.Cleanup(func() { gw.Close() })
		if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:      "alpha",
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		}); err != nil {
			t.Fatalf("RegisterMCPServer: %v", err)
		}
		// Real observer so the dispatch path exercises observation end to end.
		observer := metrics.NewObserver(token.NewHeuristicCounter(0), metrics.NewAccumulator(100))
		gw.SetToolCallObserver(observer)
		return gw
	}

	callEcho := func(gw *mcp.Gateway, callCtx context.Context) *mcp.ToolCallResult {
		t.Helper()
		res, err := gw.HandleToolsCall(callCtx, mcp.ToolCallParams{
			Name:      "alpha__echo",
			Arguments: map[string]any{"message": "hello"},
		})
		if err != nil {
			t.Fatalf("HandleToolsCall: %v", err)
		}
		return res
	}

	t.Run("rate_limit_burst_then_deny", func(t *testing.T) {
		gw := newGateway(t)
		installLimits(gw, &config.LimitsConfig{
			RateLimits: []config.RateLimit{{Server: "alpha", CallsPerMinute: 6, Burst: 2}},
		})

		clientCtx := mcp.WithClientAccessID(ctx, "cursor")
		for i := range 2 {
			if res := callEcho(gw, clientCtx); res.IsError {
				t.Fatalf("burst call %d should succeed: %+v", i, res.Content)
			}
		}
		res := callEcho(gw, clientCtx)
		if !res.IsError || !strings.Contains(res.Content[0].Text, "Rate limit exceeded") {
			t.Fatalf("third call should be rate limited, got %+v", res.Content)
		}
		if !strings.Contains(res.Content[0].Text, "Retry after") {
			t.Errorf("rate denial missing retry hint: %s", res.Content[0].Text)
		}
	})

	t.Run("code_mode_covered", func(t *testing.T) {
		gw := newGateway(t)
		installLimits(gw, &config.LimitsConfig{
			RateLimits: []config.RateLimit{{Server: "alpha", CallsPerMinute: 6, Burst: 1}},
		})
		gw.SetCodeMode(10 * time.Second)

		clientCtx := mcp.WithClientAccessID(ctx, "cursor")

		// First sandboxed call consumes the burst; the second must be denied
		// by the gate inside the re-entrant HandleToolsCall, proving code
		// mode cannot bypass limits.
		ok, err := gw.HandleToolsCall(clientCtx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "echo", {message: "one"}); })()`,
			},
		})
		if err != nil {
			t.Fatalf("code-mode execute: %v", err)
		}
		if ok.IsError {
			t.Fatalf("first sandboxed call should succeed: %+v", ok.Content)
		}

		denied, err := gw.HandleToolsCall(clientCtx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "echo", {message: "two"}); })()`,
			},
		})
		if err != nil {
			t.Fatalf("code-mode execute(second): %v", err)
		}
		if !denied.IsError || !strings.Contains(denied.Content[0].Text, "Rate limit exceeded") {
			t.Fatalf("sandboxed call should surface the rate denial, got %+v", denied.Content)
		}
	})
}
