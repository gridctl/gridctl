//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/limits"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/token"
)

// installLimits compiles cfg against ledgerPath and wires it onto gw.
func installLimits(gw *mcp.Gateway, cfg *config.LimitsConfig, ledgerPath string) *limits.Policy {
	pol := limits.NewPolicy(cfg, ledgerPath, nil)
	if pol != nil {
		gw.SetCallGates(pol.Gates())
		gw.SetCostSettler(pol)
	} else {
		gw.SetCallGates(nil)
		gw.SetCostSettler(nil)
	}
	return pol
}

// TestLimits_EnforcedAtDispatch is the end-to-end guard for budgets and rate
// limits: a real mock MCP server behind the gateway, the real metrics
// observer pricing calls (so budget settlement uses genuinely attributed
// cost), and enforcement asserted on the direct path, across a restart, and
// through the code-mode sandbox.
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
		// Real observer with a fixed model so every call prices against the
		// embedded LiteLLM snapshot and settlement is exercised end to end.
		observer := metrics.NewObserver(token.NewHeuristicCounter(0), metrics.NewAccumulator(100))
		observer.SetModelResolver(func(serverName, clientID string) string { return "gpt-4o" })
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
		}, "")

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

	t.Run("budget_settles_then_denies_and_persists", func(t *testing.T) {
		ledger := filepath.Join(t.TempDir(), "limits-stack.json")
		cfg := &config.LimitsConfig{
			// A cap smaller than any single priced call: the first call is
			// admitted (check-then-settle), its settlement crosses the cap,
			// and the second is denied.
			Budgets: []config.BudgetLimit{{Client: "claude-code", MaxUSD: 0.000001, Period: "daily"}},
		}
		gw := newGateway(t)
		pol := installLimits(gw, cfg, ledger)

		claudeCtx := mcp.WithClientAccessID(ctx, "claude-code")
		cursorCtx := mcp.WithClientAccessID(ctx, "cursor")

		if res := callEcho(gw, claudeCtx); res.IsError {
			t.Fatalf("first call under budget should succeed: %+v", res.Content)
		}
		res := callEcho(gw, claudeCtx)
		if !res.IsError || !strings.Contains(res.Content[0].Text, "Budget exceeded") {
			t.Fatalf("second call should be budget-denied, got %+v", res.Content)
		}
		if !strings.Contains(res.Content[0].Text, "Do not retry") {
			t.Errorf("budget denial missing do-not-retry text: %s", res.Content[0].Text)
		}
		// Non-matching client is unaffected.
		if res := callEcho(gw, cursorCtx); res.IsError {
			t.Errorf("non-matching client should be unaffected: %+v", res.Content)
		}

		// "Restart": flush, rebuild policy from the same ledger on a fresh
		// gateway. Spend must survive and deny immediately.
		pol.Flush(ctx)
		gw2 := newGateway(t)
		pol2 := installLimits(gw2, cfg, ledger)
		if st := pol2.Status(); st.Entries[0].Budget.SpentUSD <= 0 {
			t.Fatalf("reloaded ledger lost spend: %+v", st.Entries[0])
		}
		res = callEcho(gw2, claudeCtx)
		if !res.IsError || !strings.Contains(res.Content[0].Text, "Budget exceeded") {
			t.Fatalf("post-restart call should be denied from persisted spend, got %+v", res.Content)
		}
	})

	t.Run("code_mode_covered", func(t *testing.T) {
		gw := newGateway(t)
		installLimits(gw, &config.LimitsConfig{
			RateLimits: []config.RateLimit{{Server: "alpha", CallsPerMinute: 6, Burst: 1}},
		}, "")
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
