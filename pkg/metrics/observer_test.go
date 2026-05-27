package metrics

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pricing"
	"github.com/gridctl/gridctl/pkg/token"
)

func TestObserver_ObserveToolCall(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"query": "hello world"}
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{
			mcp.NewTextContent("This is the response text from the tool."),
		},
	}

	obs.ObserveToolCall("test-server", -1, args, result)

	snap := acc.Snapshot()
	if snap.Session.InputTokens == 0 {
		t.Error("expected non-zero input tokens")
	}
	if snap.Session.OutputTokens == 0 {
		t.Error("expected non-zero output tokens")
	}
	if snap.Session.TotalTokens != snap.Session.InputTokens+snap.Session.OutputTokens {
		t.Error("total should equal input + output")
	}

	serverTokens, ok := snap.PerServer["test-server"]
	if !ok {
		t.Fatal("expected test-server in per-server metrics")
	}
	if serverTokens.TotalTokens != snap.Session.TotalTokens {
		t.Error("server total should equal session total for single server")
	}
}

func TestObserver_ObservePromptGet(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObservePromptGet(mcp.PromptGetObservation{PromptName: "code-review", ClientID: "claude-code"})
	obs.ObservePromptGet(mcp.PromptGetObservation{PromptName: "code-review"})

	snap := acc.PromptUsageSnapshot()
	if got := snap["code-review"].Calls; got != 2 {
		t.Errorf("code-review calls = %d, want 2", got)
	}
	// Prompt serving must never appear in tool usage (Tools Audit Mode).
	if tu := acc.ToolUsageSnapshot(); tu != nil {
		t.Errorf("prompt get leaked into tool usage: %v", tu)
	}
	// And it must not record any token/cost usage.
	if s := acc.Snapshot(); s.Session.TotalTokens != 0 {
		t.Errorf("prompt get recorded tokens = %d, want 0", s.Session.TotalTokens)
	}
}

func TestObserver_PerReplica(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"query": "hello"}
	result := &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("response")}}

	obs.ObserveToolCall("multi", 0, args, result)
	obs.ObserveToolCall("multi", 1, args, result)
	obs.ObserveToolCall("multi", 1, args, result)

	snap := acc.Snapshot()
	serverTotal, ok := snap.PerServer["multi"]
	if !ok {
		t.Fatal("expected per-server entry for multi")
	}
	replicaMap, ok := snap.PerReplica["multi"]
	if !ok {
		t.Fatalf("expected per-replica entry for multi; got %+v", snap.PerReplica)
	}
	if len(replicaMap) != 2 {
		t.Fatalf("expected 2 replicas, got %d", len(replicaMap))
	}
	if replicaMap[1].TotalTokens != 2*replicaMap[0].TotalTokens {
		t.Errorf("replica 1 should have 2× the tokens of replica 0; got %d vs %d",
			replicaMap[1].TotalTokens, replicaMap[0].TotalTokens)
	}
	if sum := replicaMap[0].TotalTokens + replicaMap[1].TotalTokens; sum != serverTotal.TotalTokens {
		t.Errorf("replica totals should sum to server total: %d vs %d", sum, serverTotal.TotalTokens)
	}
}

// TestObserver_RecordsCostWithCacheTokens covers Acceptance Criterion 21:
// when a tool result reports cache-read and cache-write tokens, the
// observer prices them at the provider's cache rates and records the
// breakdown — never conflating cache traffic into the input-token rate.
//
// The fixture uses a deterministic pricing.Source so the test is robust
// against changes to the embedded LiteLLM snapshot.
func TestObserver_RecordsCostWithCacheTokens(t *testing.T) {
	prev := pricing.CurrentSource()
	defer pricing.SetSource(prev)

	rates := pricing.Rates{
		InputPerToken:      3e-6,    // $3 / M input
		OutputPerToken:     15e-6,   // $15 / M output
		CacheReadPerToken:  0.3e-6,  // $0.30 / M cache read (~10% of input)
		CacheWritePerToken: 3.75e-6, // $3.75 / M cache write (~125% of input)
	}
	pricing.SetSource(staticSource{name: "fixture", rates: map[string]pricing.Rates{
		"claude-fixture": rates,
	}})

	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)
	obs.SetModelResolver(func(string) string { return "claude-fixture" })

	args := map[string]any{"q": "hello"}
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("response")},
		Usage: &mcp.CallUsage{
			CacheReadTokens:     10000,
			CacheCreationTokens: 200,
		},
	}
	obs.ObserveToolCall("server-a", -1, args, result)

	snap := acc.CostSnapshot()
	got := snap.Session

	inputTokens := token.CountJSON(counter, args)
	outputTokens := counter.Count("response")

	wantInput := float64(inputTokens) * rates.InputPerToken
	wantOutput := float64(outputTokens) * rates.OutputPerToken
	wantCacheRead := float64(10000) * rates.CacheReadPerToken
	wantCacheWrite := float64(200) * rates.CacheWritePerToken
	wantTotal := wantInput + wantOutput + wantCacheRead + wantCacheWrite

	if !approxUSDEq(got.InputUSD, wantInput) {
		t.Errorf("InputUSD = %v, want %v", got.InputUSD, wantInput)
	}
	if !approxUSDEq(got.OutputUSD, wantOutput) {
		t.Errorf("OutputUSD = %v, want %v", got.OutputUSD, wantOutput)
	}
	if !approxUSDEq(got.CacheReadUSD, wantCacheRead) {
		t.Errorf("CacheReadUSD = %v, want %v", got.CacheReadUSD, wantCacheRead)
	}
	if !approxUSDEq(got.CacheWriteUSD, wantCacheWrite) {
		t.Errorf("CacheWriteUSD = %v, want %v", got.CacheWriteUSD, wantCacheWrite)
	}
	if !approxUSDEq(got.TotalUSD, wantTotal) {
		t.Errorf("TotalUSD = %v, want %v", got.TotalUSD, wantTotal)
	}

	// Conflated calculation (cache-read tokens priced at input rate)
	// would yield a meaningfully larger cache-read component because
	// input rate is 10x cache-read rate; assert the test would catch
	// that regression.
	conflatedCacheRead := float64(10000) * rates.InputPerToken
	if approxUSDEq(got.CacheReadUSD, conflatedCacheRead) {
		t.Fatalf("CacheReadUSD %v matches conflated value %v — cache pricing regressed", got.CacheReadUSD, conflatedCacheRead)
	}

	// And at the total: a regression where every "input-shaped" token
	// is priced at the input rate (input + cache_read + cache_write
	// summed and multiplied by InputPerToken) would land near a value
	// the per-rate calculation never reaches. Assert the recorded
	// total is not equal to that conflated total.
	conflatedTotal := float64(inputTokens+10000+200)*rates.InputPerToken +
		float64(outputTokens)*rates.OutputPerToken
	if approxUSDEq(got.TotalUSD, conflatedTotal) {
		t.Fatalf("TotalUSD %v matches fully-conflated total %v — cache pricing regressed", got.TotalUSD, conflatedTotal)
	}

	// Per-server breakdown carries the same numbers.
	srv, ok := snap.PerServer["server-a"]
	if !ok {
		t.Fatal("expected per-server cost entry for server-a")
	}
	if !approxUSDEq(srv.TotalUSD, wantTotal) {
		t.Errorf("PerServer[server-a].TotalUSD = %v, want %v", srv.TotalUSD, wantTotal)
	}
}

// TestObserver_SkipsCostWhenModelUnknown verifies the observer records
// tokens but no cost when no model can be resolved for the call. This is
// the default behavior in PR 1 because no MCPServer config field carries
// a model yet — the cost path goes live in PR 2 once attribution lands.
func TestObserver_SkipsCostWhenModelUnknown(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"q": "hello"}
	result := &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("ok")}}
	obs.ObserveToolCall("anonymous-server", -1, args, result)

	tokens := acc.Snapshot()
	if tokens.Session.TotalTokens == 0 {
		t.Error("tokens should have been recorded even when model is unknown")
	}
	cost := acc.CostSnapshot()
	if cost.Session.TotalUSD != 0 {
		t.Errorf("expected zero session cost when no model resolved; got %v", cost.Session.TotalUSD)
	}
}

// TestObserver_CallLevelModelOverridesResolver confirms a model carried in
// the call's CallUsage takes precedence over the server-level resolver —
// gateway operators can override per-server defaults at the call site
// without touching configuration.
func TestObserver_CallLevelModelOverridesResolver(t *testing.T) {
	prev := pricing.CurrentSource()
	defer pricing.SetSource(prev)

	pricing.SetSource(staticSource{name: "fixture", rates: map[string]pricing.Rates{
		"call-model":   {InputPerToken: 1, OutputPerToken: 2},
		"server-model": {InputPerToken: 10, OutputPerToken: 20},
	}})

	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)
	obs.SetModelResolver(func(string) string { return "server-model" })

	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("x")},
		Usage:   &mcp.CallUsage{Model: "call-model"},
	}
	obs.ObserveToolCall("s", -1, map[string]any{"a": 1}, result)

	cost := acc.CostSnapshot()
	if cost.Session.InputUSD == 0 {
		t.Fatal("expected input cost > 0")
	}
	// "call-model" rates are 10x smaller than "server-model"; if the
	// resolver had won, costs would be ~10x larger. We just assert that
	// the rates chosen match the call-model side.
	tokensInput := token.CountJSON(counter, map[string]any{"a": 1})
	wantInput := float64(tokensInput) * 1.0 // call-model.InputPerToken
	if !approxUSDEq(cost.Session.InputUSD, wantInput) {
		t.Errorf("InputUSD = %v, want %v (call-model rate, not server-model)", cost.Session.InputUSD, wantInput)
	}
}

// staticSource is a deterministic pricing.Source for tests.
type staticSource struct {
	name  string
	rates map[string]pricing.Rates
}

func (s staticSource) Lookup(model string) (pricing.Rates, bool) {
	r, ok := s.rates[model]
	return r, ok
}
func (s staticSource) Name() string { return s.name }

func approxUSDEq(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// TestObserver_ImplementsClientObserver guarantees the Observer satisfies
// the ClientObserver interface; the gateway type-asserts on this to opt
// into synchronous, client-aware dispatch.
func TestObserver_ImplementsClientObserver(t *testing.T) {
	var _ mcp.ClientObserver = (*Observer)(nil)
}

// TestObserver_ObserveToolCallWithClient_AttributesPerClient verifies that
// the new ClientObserver entry point routes both tokens and cost to the
// per-client maps without breaking session/per-server aggregates.
func TestObserver_ObserveToolCallWithClient_AttributesPerClient(t *testing.T) {
	prev := pricing.CurrentSource()
	defer pricing.SetSource(prev)

	rates := pricing.Rates{InputPerToken: 1e-6, OutputPerToken: 2e-6}
	pricing.SetSource(staticSource{name: "fixture", rates: map[string]pricing.Rates{
		"call-model": rates,
	}})

	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	args := map[string]any{"q": "hello"}
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("answer")},
		Usage:   &mcp.CallUsage{Model: "call-model"},
	}
	summary := obs.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "server-a",
		ReplicaID:  -1,
		ClientID:   "claude-code",
		ToolName:   "demo",
		Arguments:  args,
		Result:     result,
	})

	if summary.InputTokens == 0 || summary.OutputTokens == 0 {
		t.Errorf("expected non-zero token counts; got %+v", summary)
	}
	if summary.Model != "call-model" {
		t.Errorf("Model = %q, want %q", summary.Model, "call-model")
	}
	if !summary.HasCost || summary.CostUSD == 0 {
		t.Errorf("expected HasCost=true and non-zero CostUSD; got %+v", summary)
	}

	tokens := acc.Snapshot()
	clientTokens, ok := tokens.PerClient["claude-code"]
	if !ok {
		t.Fatal("expected per-client tokens for claude-code")
	}
	if clientTokens.TotalTokens != tokens.Session.TotalTokens {
		t.Errorf("per-client tokens (%d) should equal session (%d) for single client",
			clientTokens.TotalTokens, tokens.Session.TotalTokens)
	}

	costs := acc.CostSnapshot()
	if _, ok := costs.PerClient["claude-code"]; !ok {
		t.Fatal("expected per-client cost for claude-code")
	}
}

// TestObserver_ObserveToolCallWithClient_EmptyClientNoAttribution covers
// the case where a tool call carries no session attribution: tokens and
// cost still record under session/per-server, but per-client maps stay
// empty so anonymous traffic does not pollute attribution dimensions.
func TestObserver_ObserveToolCallWithClient_EmptyClientNoAttribution(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "server-a",
		ReplicaID:  -1,
		ClientID:   "",
		Arguments:  map[string]any{"q": 1},
		Result:     &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("a")}},
	})

	snap := acc.Snapshot()
	if len(snap.PerClient) != 0 {
		t.Errorf("expected no per-client entries; got %v", snap.PerClient)
	}
	if snap.Session.TotalTokens == 0 {
		t.Error("session totals should still update when client is unknown")
	}
}

// TestObserver_ObserveToolCallWithClient_SummaryMatchesLegacyPath ensures
// the legacy ObserveToolCall path and the new ObserveToolCallWithClient
// path record identical aggregates for the same input — only attribution
// dimensions differ.
func TestObserver_ObserveToolCallWithClient_SummaryMatchesLegacyPath(t *testing.T) {
	prev := pricing.CurrentSource()
	defer pricing.SetSource(prev)
	pricing.SetSource(staticSource{name: "fixture", rates: map[string]pricing.Rates{
		"m": {InputPerToken: 1, OutputPerToken: 2},
	}})

	counter := token.NewHeuristicCounter(4)
	args := map[string]any{"q": "x"}
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("ok")},
		Usage:   &mcp.CallUsage{Model: "m"},
	}

	accLegacy := NewAccumulator(100)
	NewObserver(counter, accLegacy).ObserveToolCall("s", -1, args, result)

	accV2 := NewAccumulator(100)
	NewObserver(counter, accV2).ObserveToolCallWithClient(context.Background(), mcp.ToolCallObservation{
		ServerName: "s",
		ReplicaID:  -1,
		Arguments:  args,
		Result:     result,
	})

	if accLegacy.Snapshot().Session != accV2.Snapshot().Session {
		t.Errorf("session token snapshots diverged: legacy=%v v2=%v",
			accLegacy.Snapshot().Session, accV2.Snapshot().Session)
	}
	if !approxUSDEq(accLegacy.CostSnapshot().Session.TotalUSD,
		accV2.CostSnapshot().Session.TotalUSD) {
		t.Errorf("session cost snapshots diverged: legacy=%v v2=%v",
			accLegacy.CostSnapshot().Session.TotalUSD,
			accV2.CostSnapshot().Session.TotalUSD)
	}
}

func TestObserver_NilResult(t *testing.T) {
	counter := token.NewHeuristicCounter(4)
	acc := NewAccumulator(100)
	obs := NewObserver(counter, acc)

	obs.ObserveToolCall("test-server", -1, map[string]any{"key": "val"}, nil)

	snap := acc.Snapshot()
	if snap.Session.InputTokens == 0 {
		t.Error("expected non-zero input tokens even with nil result")
	}
	if snap.Session.OutputTokens != 0 {
		t.Errorf("expected 0 output tokens for nil result, got %d", snap.Session.OutputTokens)
	}
}
