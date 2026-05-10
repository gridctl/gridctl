package observed

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pricing"
)

// approxEqual compares two USD costs ignoring float-precision noise.
// pricing math chains float multiplications and the resulting bit
// pattern is not bit-exact even when the value is "0.0002".
func approxEqual(a, b float64) bool {
	const eps = 1e-9
	return math.Abs(a-b) < eps
}

// fakeChatModel is a hand-rolled stub satisfying agent.ChatModel.
// Generates capture the request and return a configured response so
// tests can drive specific token counts.
type fakeChatModel struct {
	resp     agent.ChatResponse
	err      error
	chunks   []agent.ChatChunk
	streamOK bool
}

func (f *fakeChatModel) Generate(_ context.Context, _ agent.ChatRequest) (agent.ChatResponse, error) {
	if f.err != nil {
		return agent.ChatResponse{}, f.err
	}
	return f.resp, nil
}

func (f *fakeChatModel) Stream(_ context.Context, _ agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	if !f.streamOK {
		return nil, f.err
	}
	return agent.StreamReaderFromSlice(f.chunks), nil
}

// recordedCost captures one RecordCost invocation for assertions.
type recordedCost struct {
	server string
	cost   metrics.CostBreakdown
}

type fakeAccumulator struct {
	mu      sync.Mutex
	records []recordedCost
}

func (f *fakeAccumulator) RecordCost(serverName string, _ int, cost metrics.CostBreakdown) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, recordedCost{server: serverName, cost: cost})
}

// staticPricingSource returns a fixed rate per model so tests never
// depend on the embedded LiteLLM snapshot.
type staticPricingSource struct{ rates map[string]pricing.Rates }

func (s *staticPricingSource) Lookup(model string) (pricing.Rates, bool) {
	r, ok := s.rates[model]
	return r, ok
}
func (s *staticPricingSource) Name() string { return "test-static" }

// withPricingSource swaps the package-level pricing source for the
// duration of a test, restoring the prior source on cleanup. Required
// because pkg/pricing is global; the test would otherwise depend on
// the embedded LiteLLM table.
func withPricingSource(t *testing.T, s pricing.Source) {
	t.Helper()
	prev := pricing.CurrentSource()
	pricing.SetSource(s)
	t.Cleanup(func() { pricing.SetSource(prev) })
}

func TestNew_RejectsNilInner(t *testing.T) {
	if _, err := New(nil, "llm:test"); err == nil {
		t.Fatal("expected error when inner is nil")
	}
}

func TestNew_RejectsEmptyProviderName(t *testing.T) {
	if _, err := New(&fakeChatModel{}, ""); err == nil {
		t.Fatal("expected error when providerName is empty")
	}
}

func TestGenerate_PassesThroughResponse(t *testing.T) {
	want := agent.ChatResponse{
		Model:   "claude-test",
		Content: "hello",
		Usage:   agent.Usage{InputTokens: 10, OutputTokens: 5},
	}
	inner := &fakeChatModel{resp: want}

	wrap, err := New(inner, "llm:test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := wrap.Generate(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got.Content != want.Content {
		t.Errorf("Content = %q, want %q", got.Content, want.Content)
	}
}

func TestGenerate_RecordsCostWithProviderName(t *testing.T) {
	withPricingSource(t, &staticPricingSource{
		rates: map[string]pricing.Rates{
			"claude-test": {InputPerToken: 1e-6, OutputPerToken: 2e-6},
		},
	})

	inner := &fakeChatModel{resp: agent.ChatResponse{
		Model: "claude-test",
		Usage: agent.Usage{InputTokens: 1000, OutputTokens: 500},
	}}
	acc := &fakeAccumulator{}
	wrap, err := New(inner, "llm:test", WithAccumulator(acc))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := wrap.Generate(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "ping"}},
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(acc.records) != 1 {
		t.Fatalf("RecordCost calls = %d, want 1", len(acc.records))
	}
	rec := acc.records[0]
	if rec.server != "llm:test" {
		t.Errorf("server = %q, want llm:test", rec.server)
	}
	wantInput := 1000 * 1e-6
	wantOutput := 500 * 2e-6
	if !approxEqual(rec.cost.Input, wantInput) {
		t.Errorf("Input = %v, want %v", rec.cost.Input, wantInput)
	}
	if !approxEqual(rec.cost.Output, wantOutput) {
		t.Errorf("Output = %v, want %v", rec.cost.Output, wantOutput)
	}
}

func TestGenerate_SkipsCostWhenModelUnknown(t *testing.T) {
	withPricingSource(t, &staticPricingSource{rates: map[string]pricing.Rates{}})

	inner := &fakeChatModel{resp: agent.ChatResponse{
		Model: "unknown-model",
		Usage: agent.Usage{InputTokens: 100, OutputTokens: 50},
	}}
	acc := &fakeAccumulator{}
	wrap, err := New(inner, "llm:test", WithAccumulator(acc))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := wrap.Generate(context.Background(), agent.ChatRequest{
		Model:    "unknown-model",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "ping"}},
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(acc.records) != 0 {
		t.Errorf("expected no cost record for unknown model; got %d", len(acc.records))
	}
}

func TestGenerate_PropagatesError(t *testing.T) {
	inner := &fakeChatModel{err: errors.New("boom")}
	acc := &fakeAccumulator{}
	wrap, err := New(inner, "llm:test", WithAccumulator(acc))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = wrap.Generate(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "ping"}},
	})
	if err == nil {
		t.Fatal("expected propagated error")
	}
	if len(acc.records) != 0 {
		t.Errorf("error path must not record cost; got %d records", len(acc.records))
	}
}

func TestStream_RecordsCostFromTerminalChunkUsage(t *testing.T) {
	withPricingSource(t, &staticPricingSource{
		rates: map[string]pricing.Rates{
			"claude-test": {InputPerToken: 1e-6, OutputPerToken: 2e-6},
		},
	})

	finalUsage := agent.Usage{InputTokens: 200, OutputTokens: 100}
	inner := &fakeChatModel{
		streamOK: true,
		chunks: []agent.ChatChunk{
			{Delta: "hello"},
			{Delta: " world"},
			{Usage: &finalUsage, StopReason: agent.StopReasonEnd},
		},
	}
	acc := &fakeAccumulator{}
	wrap, err := New(inner, "llm:test", WithAccumulator(acc))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stream, err := wrap.Stream(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()
	if len(acc.records) != 1 {
		t.Fatalf("expected exactly one cost record after stream materialisation; got %d", len(acc.records))
	}
	if !approxEqual(acc.records[0].cost.Input, 200*1e-6) {
		t.Errorf("Input = %v, want %v", acc.records[0].cost.Input, 200*1e-6)
	}
}

func TestStream_PropagatesProviderError(t *testing.T) {
	inner := &fakeChatModel{err: errors.New("provider down")}
	acc := &fakeAccumulator{}
	wrap, err := New(inner, "llm:test", WithAccumulator(acc))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := wrap.Stream(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "ping"}},
	}); err == nil {
		t.Fatal("expected propagated error from inner.Stream")
	}
	if len(acc.records) != 0 {
		t.Errorf("error path must not record cost; got %d records", len(acc.records))
	}
}
