package pricing

import (
	"sync"
	"testing"
)

// fakeSource is a deterministic in-memory Source for swap tests.
type fakeSource struct {
	name  string
	rates map[string]Rates
}

func (s *fakeSource) Lookup(model string) (Rates, bool) {
	r, ok := s.rates[model]
	return r, ok
}

func (s *fakeSource) Name() string { return s.name }

// withSource installs s as the active Source for the duration of fn,
// restoring the prior Source on exit.
func withSource(t *testing.T, s Source, fn func()) {
	t.Helper()
	prev := CurrentSource()
	SetSource(s)
	defer SetSource(prev)
	fn()
}

// TestLookup_KnownModel exercises Acceptance Criterion 1: a Lookup against a
// real LiteLLM-published model returns non-zero rates.
func TestLookup_KnownModel(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantInput float64 // sentinel; we assert > 0 not exact equality so a
		// pricing refresh doesn't churn the test.
	}{
		{name: "claude-opus-4-7", model: "claude-opus-4-7", wantInput: 5e-6},
		{name: "claude-sonnet-4-6", model: "claude-sonnet-4-6", wantInput: 3e-6},
		{name: "claude-haiku-4-5", model: "claude-haiku-4-5", wantInput: 1e-6},
		{name: "gpt-4o", model: "gpt-4o", wantInput: 2.5e-6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := Lookup(tt.model)
			if !ok {
				t.Fatalf("Lookup(%q) reported unknown; expected pricing data", tt.model)
			}
			if r.InputPerToken <= 0 {
				t.Fatalf("Lookup(%q) returned zero input rate; rates=%+v", tt.model, r)
			}
			if r.OutputPerToken <= 0 {
				t.Fatalf("Lookup(%q) returned zero output rate; rates=%+v", tt.model, r)
			}
		})
	}
}

// TestLookup_UnknownModelReturnsFalse covers Acceptance Criterion 2: an
// unknown model returns (_, false) and emits a single WARN. We do not assert
// on log output here (slog routing is a global concern); we assert that the
// warn-once gate fires once per unique key by exercising warnOnce directly.
func TestLookup_UnknownModelReturnsFalse(t *testing.T) {
	if _, ok := Lookup("fake-model-xyz"); ok {
		t.Fatal("expected Lookup of unknown model to return false")
	}
	if _, ok := Lookup(""); ok {
		t.Fatal("expected Lookup of empty model to return false")
	}
}

func TestWarnOnce_FiresOncePerKey(t *testing.T) {
	var w warnOnce
	var mu sync.Mutex
	calls := map[string]int{}
	for i := 0; i < 5; i++ {
		w.do("a", func() { mu.Lock(); calls["a"]++; mu.Unlock() })
		w.do("b", func() { mu.Lock(); calls["b"]++; mu.Unlock() })
	}
	if calls["a"] != 1 {
		t.Errorf("expected 1 fire for key a, got %d", calls["a"])
	}
	if calls["b"] != 1 {
		t.Errorf("expected 1 fire for key b, got %d", calls["b"])
	}
}

// TestCalculate_CacheTokensPricedSeparately covers Acceptance Criterion 21:
// when a call reports cache-read and cache-write tokens, those are priced at
// the cache-specific rates rather than rolled into the input-token rate.
// Conflating them mis-prices Anthropic by ~10x because cache rates are ~10%
// (read) and ~125% (write) of input rates.
func TestCalculate_CacheTokensPricedSeparately(t *testing.T) {
	rates := Rates{
		InputPerToken:      3e-6,    // $3 per million input
		OutputPerToken:     15e-6,   // $15 per million output
		CacheReadPerToken:  0.3e-6,  // $0.30 per million cache read (~10% of input)
		CacheWritePerToken: 3.75e-6, // $3.75 per million cache write (~125% of input)
	}
	src := &fakeSource{name: "fake", rates: map[string]Rates{"fixture": rates}}
	withSource(t, src, func() {
		usage := Usage{
			InputTokens:      1000,
			OutputTokens:     500,
			CacheReadTokens:  10000,
			CacheWriteTokens: 200,
		}
		got, ok := CalculateBreakdown("fixture", usage)
		if !ok {
			t.Fatal("expected fixture model to be priced")
		}
		want := Cost{
			Input:      1000 * 3e-6,
			Output:     500 * 15e-6,
			CacheRead:  10000 * 0.3e-6,
			CacheWrite: 200 * 3.75e-6,
		}
		if !approxEqual(got.Input, want.Input) || !approxEqual(got.Output, want.Output) ||
			!approxEqual(got.CacheRead, want.CacheRead) || !approxEqual(got.CacheWrite, want.CacheWrite) {
			t.Errorf("breakdown mismatch:\n  got:  %+v\n  want: %+v", got, want)
		}
		// Conflated calculation (treating cache-read tokens as input tokens)
		// would produce a materially different total.
		conflated := float64(usage.InputTokens+usage.CacheReadTokens)*rates.InputPerToken +
			float64(usage.OutputTokens)*rates.OutputPerToken +
			float64(usage.CacheWriteTokens)*rates.InputPerToken
		if approxEqual(got.Total(), conflated) {
			t.Fatalf("per-rate total %v matches conflated total %v — pricing is conflating cache tokens", got.Total(), conflated)
		}
	})
}

func TestCalculate_UnknownModelReturnsZero(t *testing.T) {
	got, ok := Calculate("fake-model-xyz", Usage{InputTokens: 100})
	if ok {
		t.Fatalf("expected Calculate of unknown model to return false; got cost=%v", got)
	}
	if got != 0 {
		t.Errorf("expected zero cost for unknown model, got %v", got)
	}
}

// TestSetSource_SwapsActiveSource covers Acceptance Criterion 22: an
// alternate Source can be installed via SetSource and observed by the
// top-level Lookup/Calculate functions without modifying call sites.
func TestSetSource_SwapsActiveSource(t *testing.T) {
	prev := CurrentSource()
	defer SetSource(prev)

	src := &fakeSource{
		name: "deterministic",
		rates: map[string]Rates{
			"my-model": {InputPerToken: 1, OutputPerToken: 2},
		},
	}
	SetSource(src)

	if name := CurrentSource().Name(); name != "deterministic" {
		t.Fatalf("CurrentSource().Name() = %q, want %q", name, "deterministic")
	}
	r, ok := Lookup("my-model")
	if !ok {
		t.Fatal("expected fake source to know my-model")
	}
	if r.InputPerToken != 1 || r.OutputPerToken != 2 {
		t.Errorf("rates not threaded through Lookup; got %+v", r)
	}
	cost, ok := Calculate("my-model", Usage{InputTokens: 10, OutputTokens: 5})
	if !ok || cost != 10*1+5*2 {
		t.Errorf("Calculate did not use installed source; cost=%v ok=%v", cost, ok)
	}

	// Nil swap is a no-op so callers cannot accidentally clear the source.
	SetSource(nil)
	if name := CurrentSource().Name(); name != "deterministic" {
		t.Errorf("nil SetSource should not clear source; got %q", name)
	}
}

func TestCost_Total(t *testing.T) {
	c := Cost{Input: 1, Output: 2, CacheRead: 0.5, CacheWrite: 0.25}
	if got := c.Total(); !approxEqual(got, 3.75) {
		t.Errorf("Total() = %v, want 3.75", got)
	}
}

// BenchmarkLookup verifies that hot-path Lookup is allocation-free in
// steady state. Run with `go test -bench=Lookup -benchmem ./pkg/pricing/`
// to confirm 0 allocs/op.
func BenchmarkLookup(b *testing.B) {
	model := "claude-opus-4-7"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Lookup(model)
	}
}

func BenchmarkCalculate(b *testing.B) {
	model := "claude-opus-4-7"
	usage := Usage{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 200, CacheWriteTokens: 50}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Calculate(model, usage)
	}
}

func approxEqual(a, b float64) bool {
	const eps = 1e-12
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}
