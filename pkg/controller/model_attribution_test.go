package controller

import (
	"testing"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pricing"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/token"
)

// staticPricingSource is a deterministic pricing.Source for wiring tests.
type staticPricingSource struct {
	rates map[string]pricing.Rates
}

func (s staticPricingSource) Lookup(model string) (pricing.Rates, bool) {
	r, ok := s.rates[model]
	return r, ok
}

func (s staticPricingSource) Name() string { return "controller-test-fixture" }

func newAttributionFixture(t *testing.T, stack *config.Stack) (*GatewayBuilder, *metrics.Observer, *metrics.Accumulator, *api.Server) {
	t.Helper()
	prev := pricing.CurrentSource()
	t.Cleanup(func() { pricing.SetSource(prev) })
	pricing.SetSource(staticPricingSource{rates: map[string]pricing.Rates{
		"claude-fixture": {InputPerToken: 3e-6, OutputPerToken: 15e-6},
		"fallback-model": {InputPerToken: 1e-6, OutputPerToken: 5e-6},
	}})

	builder := NewGatewayBuilder(Config{}, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	acc := metrics.NewAccumulator(100)
	observer := metrics.NewObserver(token.NewHeuristicCounter(4), acc)
	apiServer := api.NewServer(mcp.NewGateway(), nil)
	builder.wireModelAttribution(observer, apiServer)
	return builder, observer, acc, apiServer
}

func observeCall(observer *metrics.Observer, serverName string) {
	observer.ObserveToolCall(serverName, -1, map[string]any{"q": "hello"},
		&mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("response")}})
}

// TestWireModelAttribution_RecordsCostWithoutManualWiring is the regression
// test for the original defect: production code never called
// SetModelResolver, so cost stayed zero in every deployment. A tool call
// observed through the builder-wired observer must record cost with no
// test-side resolver setup.
func TestWireModelAttribution_RecordsCostWithoutManualWiring(t *testing.T) {
	stack := &config.Stack{
		Name: "test",
		MCPServers: []config.MCPServer{
			{Name: "a", Model: "claude-fixture"},
			{Name: "b"},
		},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	observeCall(observer, "a")

	snap := acc.CostSnapshot()
	if snap.Session.TotalUSD <= 0 {
		t.Fatal("expected non-zero session cost for a model-attributed server")
	}
	if snap.PerServer["a"].TotalUSD <= 0 {
		t.Error("expected non-zero per-server cost for server a")
	}

	// Server b has no attribution: tokens record, cost does not move.
	before := snap.Session.TotalUSD
	observeCall(observer, "b")
	after := acc.CostSnapshot().Session.TotalUSD
	if after != before {
		t.Errorf("unattributed server moved session cost: %v -> %v", before, after)
	}
	if acc.Snapshot().PerServer["b"].TotalTokens == 0 {
		t.Error("expected tokens recorded for unattributed server b")
	}
}

// TestWireModelAttribution_DefaultModelFallback verifies a server without
// its own model: prices against gateway.default_model.
func TestWireModelAttribution_DefaultModelFallback(t *testing.T) {
	stack := &config.Stack{
		Name:       "test",
		Gateway:    &config.GatewayConfig{DefaultModel: "fallback-model"},
		MCPServers: []config.MCPServer{{Name: "b"}},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	observeCall(observer, "b")

	if acc.CostSnapshot().PerServer["b"].TotalUSD <= 0 {
		t.Error("expected default_model to price calls for servers without model:")
	}
}

// TestWireModelAttribution_NoModelsPreservesZeroCost pins Article IX: a
// stack with no model configuration behaves exactly as before the fix —
// tokens recorded, zero cost.
func TestWireModelAttribution_NoModelsPreservesZeroCost(t *testing.T) {
	stack := &config.Stack{
		Name:       "test",
		MCPServers: []config.MCPServer{{Name: "a"}, {Name: "b"}},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	observeCall(observer, "a")
	observeCall(observer, "b")

	if got := acc.CostSnapshot().Session.TotalUSD; got != 0 {
		t.Errorf("expected zero cost without attribution; got %v", got)
	}
	if acc.Snapshot().Session.TotalTokens == 0 {
		t.Error("expected tokens recorded even without attribution")
	}
}

// TestRefreshModelAttribution_HotReload verifies the onConfigApplied path: a
// reloaded stack with a changed model prices subsequent calls against the
// new mapping without rebuilding the observer.
func TestRefreshModelAttribution_HotReload(t *testing.T) {
	stack := &config.Stack{
		Name:       "test",
		MCPServers: []config.MCPServer{{Name: "b"}},
	}
	builder, observer, acc, _ := newAttributionFixture(t, stack)

	observeCall(observer, "b")
	if got := acc.CostSnapshot().Session.TotalUSD; got != 0 {
		t.Fatalf("expected zero cost before attribution; got %v", got)
	}

	builder.refreshModelAttribution(&config.Stack{
		Name:       "test",
		MCPServers: []config.MCPServer{{Name: "b", Model: "claude-fixture"}},
	})

	observeCall(observer, "b")
	if acc.CostSnapshot().PerServer["b"].TotalUSD <= 0 {
		t.Error("expected reloaded model: to price subsequent calls")
	}
}
