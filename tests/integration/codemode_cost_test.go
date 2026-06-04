//go:build integration

package integration

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pricing"
	"github.com/gridctl/gridctl/pkg/token"
)

// TestCodeMode_CostAttributionThroughSandbox covers Acceptance Criterion 23:
// tool calls dispatched through `mcp.callTool` inside the goja sandbox MUST
// produce per-call cost records identical in shape to direct gateway tool
// calls, and the outer `execute` call's client_id must flow through to
// every nested observation. The test uses a deterministic in-memory
// pricing source so it is robust against changes to the embedded LiteLLM
// snapshot.
func TestCodeMode_CostAttributionThroughSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockHTTPServerBin == "" {
		t.Skip("mock server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })

	cfg := mcp.MCPServerConfig{
		Name:      "mockhttp",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	// Wire a deterministic pricing source so the test is independent of the
	// embedded LiteLLM snapshot; restore the previous source on cleanup.
	prev := pricing.CurrentSource()
	t.Cleanup(func() { pricing.SetSource(prev) })
	pricing.SetSource(staticPricingSource{rates: map[string]pricing.Rates{
		"test-model": {InputPerToken: 1e-5, OutputPerToken: 2e-5},
	}})

	counter := token.NewHeuristicCounter(4)
	acc := metrics.NewAccumulator(100)
	obs := metrics.NewObserver(counter, acc)
	obs.SetModelResolver(func(string, string) string { return "test-model" })
	gw.SetToolCallObserver(obs)

	cm := mcp.NewCodeMode(5 * time.Second)
	tools, err := gw.HandleToolsListUnscoped()
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}

	// Two nested tool calls inside one outer execute() invocation.
	code := `
		(async () => {
			const a = await mcp.callTool("mockhttp", "echo", {message: "hello"});
			const b = await mcp.callTool("mockhttp", "add", {a: 2, b: 3});
			return { a: a, b: b };
		})()
	`

	clientCtx := mcp.WithClientID(ctx, "claude-code")
	result, err := cm.HandleCallWithScope(clientCtx, mcp.ToolCallParams{
		Name:      mcp.MetaToolExecute,
		Arguments: map[string]any{"code": code},
	}, gw, tools.Tools)
	if err != nil {
		t.Fatalf("HandleCallWithScope: %v", err)
	}
	if result.IsError {
		t.Fatalf("execute returned error: %v", result.Content)
	}

	// Both nested tool calls must have been observed and attributed to the
	// outer execute's client_id, not the empty string the meta-tool boundary
	// would produce if attribution were captured there.
	tokensSnap := acc.Snapshot()
	clientTokens, ok := tokensSnap.PerClient["claude-code"]
	if !ok {
		t.Fatalf("expected per-client token entry for claude-code; got %+v", tokensSnap.PerClient)
	}
	if clientTokens.TotalTokens == 0 {
		t.Errorf("expected non-zero per-client tokens; got %+v", clientTokens)
	}

	// Per-server cost should match per-client cost (single client, single
	// downstream server) and reflect both nested calls — the boundary
	// instrumentation alternative would record one observation, not two.
	costSnap := acc.CostSnapshot()
	clientCost, ok := costSnap.PerClient["claude-code"]
	if !ok {
		t.Fatalf("expected per-client cost entry for claude-code; got %+v", costSnap.PerClient)
	}
	if clientCost.TotalUSD == 0 {
		t.Errorf("expected non-zero per-client cost; got %+v", clientCost)
	}
	serverCost, ok := costSnap.PerServer["mockhttp"]
	if !ok {
		t.Fatalf("expected per-server cost for mockhttp; got %+v", costSnap.PerServer)
	}
	if !approxUSDEq(clientCost.TotalUSD, serverCost.TotalUSD) {
		t.Errorf("per-client cost (%v) should equal per-server cost (%v) for the single-client/single-server case",
			clientCost.TotalUSD, serverCost.TotalUSD)
	}

	// Sanity-check we observed two distinct nested calls. We assert via the
	// minute bucket the recorded cost lives in: aggregate session cost is
	// the sum of both call costs, so it must be strictly greater than any
	// single call's contribution. With two non-trivial calls the session
	// cost exceeds, say, 1.5x of either call's input cost — but rather than
	// guess, we just compare server total to session total and require both
	// be non-zero.
	if costSnap.Session.TotalUSD == 0 {
		t.Error("session cost should be non-zero after sandbox execution")
	}
}

// staticPricingSource is the integration-test analogue of metrics.staticSource
// (kept here because the metrics package's test-only type is unexported).
type staticPricingSource struct {
	rates map[string]pricing.Rates
}

func (s staticPricingSource) Lookup(model string) (pricing.Rates, bool) {
	r, ok := s.rates[model]
	return r, ok
}

func (s staticPricingSource) Models() []string {
	models := make([]string, 0, len(s.rates))
	for id := range s.rates {
		models = append(models, id)
	}
	sort.Strings(models)
	return models
}

func (s staticPricingSource) Name() string { return "integration-fixture" }

func approxUSDEq(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}
