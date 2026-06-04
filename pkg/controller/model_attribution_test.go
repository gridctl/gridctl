package controller

import (
	"context"
	"sort"
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

func (s staticPricingSource) Models() []string {
	models := make([]string, 0, len(s.rates))
	for id := range s.rates {
		models = append(models, id)
	}
	sort.Strings(models)
	return models
}

func (s staticPricingSource) Name() string { return "controller-test-fixture" }

func newAttributionFixture(t *testing.T, stack *config.Stack) (*GatewayBuilder, *metrics.Observer, *metrics.Accumulator, *api.Server) {
	t.Helper()
	prev := pricing.CurrentSource()
	t.Cleanup(func() { pricing.SetSource(prev) })
	pricing.SetSource(staticPricingSource{rates: map[string]pricing.Rates{
		"claude-fixture": {InputPerToken: 3e-6, OutputPerToken: 15e-6},
		"fallback-model": {InputPerToken: 1e-6, OutputPerToken: 5e-6},
		"client-fixture": {InputPerToken: 30e-6, OutputPerToken: 150e-6},
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

func observeClientCall(observer *metrics.Observer, serverName, clientID string) mcp.ToolCallSummary {
	return observer.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: serverName,
		ReplicaID:  -1,
		ClientID:   clientID,
		ToolName:   "demo",
		Arguments:  map[string]any{"q": "hello"},
		Result:     &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("response")}},
	})
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

// TestWireModelAttribution_ClientBeatsServer pins the resolution precedence:
// a calling client's declared model (client_models) outranks the target
// server's model. The rates differ by 10x so a precedence regression cannot
// hide in rounding.
func TestWireModelAttribution_ClientBeatsServer(t *testing.T) {
	stack := &config.Stack{
		Name:         "test",
		MCPServers:   []config.MCPServer{{Name: "a", Model: "claude-fixture"}},
		ClientModels: map[string]string{"claude-code": "client-fixture"},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	summary := observeClientCall(observer, "a", "claude-code")
	if summary.Model != "client-fixture" {
		t.Fatalf("resolved model = %q, want client-fixture (client tier must beat server tier)", summary.Model)
	}

	// The recorded per-client cost reflects the client rate, not the server's.
	clientCost := acc.CostSnapshot().PerClient["claude-code"]
	inputTokens := summary.InputTokens
	wantInput := float64(inputTokens) * 30e-6
	if !approxUSD(clientCost.InputUSD, wantInput) {
		t.Errorf("PerClient InputUSD = %v, want %v (client-fixture rate)", clientCost.InputUSD, wantInput)
	}
}

// TestWireModelAttribution_UndeclaredClientFallsToServer verifies the second
// tier: a client with no client_models entry prices against the target
// server's effective model.
func TestWireModelAttribution_UndeclaredClientFallsToServer(t *testing.T) {
	stack := &config.Stack{
		Name:         "test",
		MCPServers:   []config.MCPServer{{Name: "a", Model: "claude-fixture"}},
		ClientModels: map[string]string{"gemini-cli": "client-fixture"},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	summary := observeClientCall(observer, "a", "claude-code")
	if summary.Model != "claude-fixture" {
		t.Fatalf("resolved model = %q, want claude-fixture (server tier)", summary.Model)
	}
	if acc.CostSnapshot().PerClient["claude-code"].TotalUSD <= 0 {
		t.Error("expected per-client cost recorded at the server rate")
	}
}

// TestWireModelAttribution_AnonymousSkipsClientTier pins the #772
// behavior-preservation guarantee: an empty ClientID (anonymous sessions,
// the legacy ObserveToolCall path) never consults client_models and lands on
// the server tier exactly as before this feature.
func TestWireModelAttribution_AnonymousSkipsClientTier(t *testing.T) {
	stack := &config.Stack{
		Name:         "test",
		MCPServers:   []config.MCPServer{{Name: "a", Model: "claude-fixture"}},
		ClientModels: map[string]string{"claude-code": "client-fixture"},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	// Legacy path: no client attribution at all.
	observeCall(observer, "a")
	srv := acc.CostSnapshot().PerServer["a"]
	tokens := acc.Snapshot().PerServer["a"]
	wantInput := float64(tokens.InputTokens) * 3e-6
	if !approxUSD(srv.InputUSD, wantInput) {
		t.Errorf("anonymous InputUSD = %v, want %v (server rate, never the client rate)", srv.InputUSD, wantInput)
	}
}

// TestWireModelAttribution_ClientOnlyStack verifies the pricing-only
// configuration: client_models with no server models and no default_model
// prices declared clients and leaves everything else at zero cost.
func TestWireModelAttribution_ClientOnlyStack(t *testing.T) {
	stack := &config.Stack{
		Name:         "test",
		MCPServers:   []config.MCPServer{{Name: "a"}},
		ClientModels: map[string]string{"claude-code": "client-fixture"},
	}
	_, observer, acc, _ := newAttributionFixture(t, stack)

	observeClientCall(observer, "a", "claude-code")
	if acc.CostSnapshot().PerClient["claude-code"].TotalUSD <= 0 {
		t.Error("expected declared client to be priced with no server attribution present")
	}

	before := acc.CostSnapshot().Session.TotalUSD
	observeClientCall(observer, "a", "gemini-cli")
	if after := acc.CostSnapshot().Session.TotalUSD; after != before {
		t.Errorf("undeclared client on unattributed server moved cost: %v -> %v", before, after)
	}
}

// TestRefreshModelAttribution_ClientModelsHotReload verifies the
// onConfigApplied path for the client tier: a reloaded stack with a changed
// client_models entry prices subsequent calls against the new mapping.
func TestRefreshModelAttribution_ClientModelsHotReload(t *testing.T) {
	stack := &config.Stack{
		Name:       "test",
		MCPServers: []config.MCPServer{{Name: "a"}},
	}
	builder, observer, acc, _ := newAttributionFixture(t, stack)

	observeClientCall(observer, "a", "claude-code")
	if got := acc.CostSnapshot().Session.TotalUSD; got != 0 {
		t.Fatalf("expected zero cost before client attribution; got %v", got)
	}

	builder.refreshModelAttribution(&config.Stack{
		Name:         "test",
		MCPServers:   []config.MCPServer{{Name: "a"}},
		ClientModels: map[string]string{"claude-code": "client-fixture"},
	})

	observeClientCall(observer, "a", "claude-code")
	if acc.CostSnapshot().PerClient["claude-code"].TotalUSD <= 0 {
		t.Error("expected reloaded client_models to price subsequent calls")
	}
}

// TestClientModelsAccessInert pins the design constraint that drove the
// top-level map: a stack declaring only client_models produces no access
// policy spec, so no client is denied anything.
func TestClientModelsAccessInert(t *testing.T) {
	stack := &config.Stack{
		Name:         "test",
		MCPServers:   []config.MCPServer{{Name: "a"}},
		ClientModels: map[string]string{"claude-code": "client-fixture"},
	}
	if spec := clientAccessSpec(stack); spec != nil {
		t.Errorf("client_models must not create an access spec; got %+v", spec)
	}
}

// approxUSD mirrors the metrics package's float comparison for USD values.
func approxUSD(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
