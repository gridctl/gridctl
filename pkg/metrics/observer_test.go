package metrics

import (
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
