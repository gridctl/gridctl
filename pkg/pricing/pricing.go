// Package pricing computes USD cost for MCP tool calls using model rate
// tables sourced from LiteLLM's `model_prices_and_context_window.json`.
//
// The package is intentionally small: it owns model-rate lookup, model-ID
// normalization, and cost computation. It has no dependencies on other
// gridctl packages so any layer (gateway, CLI, web API) can call it.
//
// The default Source is backed by an embedded snapshot of LiteLLM's pricing
// JSON. To swap in an alternate source (a deterministic fixture in tests, a
// future Anthropic/OpenAI native source, a community-maintained JSON), call
// SetSource. The package-level Lookup and Calculate functions read through
// the active Source via an atomic pointer so swaps are safe under concurrent
// readers.
//
// Cache-read and cache-write tokens are priced separately from input tokens
// using the LiteLLM cache_read_input_token_cost and
// cache_creation_input_token_cost fields. Conflating them with input tokens
// mis-prices providers like Anthropic by roughly an order of magnitude
// because cache rates are ~10% / ~125% of input rates.
package pricing

import (
	"sync/atomic"
)

// Rates are the per-token USD prices for a single model. Cache fields are
// zero when the provider does not publish cache rates; in that case any
// reported cache tokens are treated as free (LiteLLM omits the fields
// rather than zero-pricing them, but the caller cannot distinguish "free"
// from "absent" — pricing is best-effort, not a billing source of truth).
type Rates struct {
	InputPerToken      float64
	OutputPerToken     float64
	CacheReadPerToken  float64
	CacheWritePerToken float64
}

// Usage is the per-call token breakdown supplied to Calculate. Cache fields
// default to zero and are priced separately from InputTokens.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Cost is the per-component USD breakdown for a single call. Total returns
// the sum across all components.
type Cost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// Total returns the sum of all component costs.
func (c Cost) Total() float64 {
	return c.Input + c.Output + c.CacheRead + c.CacheWrite
}

// Source is the abstraction for a per-model rate table. Implementations are
// expected to be safe for concurrent Lookup. Name is a short identifier
// (e.g. "litellm") used in logs and diagnostic output.
type Source interface {
	Lookup(model string) (Rates, bool)
	Name() string
}

// sourceHolder wraps the active Source for use with atomic.Pointer (which
// requires a concrete pointer type, not an interface).
type sourceHolder struct {
	s Source
}

// activeSource holds the package-level Source. Reads are lock-free; writes
// happen via SetSource which is rare (process startup, tests).
var activeSource atomic.Pointer[sourceHolder]

func init() {
	activeSource.Store(&sourceHolder{s: NewLiteLLMSource()})
}

// SetSource swaps the package-level Source used by Lookup and Calculate.
// Safe to call from any goroutine; subsequent Lookup/Calculate calls observe
// the new Source after the store completes.
func SetSource(s Source) {
	if s == nil {
		return
	}
	activeSource.Store(&sourceHolder{s: s})
}

// CurrentSource returns the active Source. Useful for diagnostics — most
// callers should use Lookup or Calculate.
func CurrentSource() Source {
	return activeSource.Load().s
}

// Lookup returns the per-token rates for a model, normalizing the model ID
// against the active Source's known keys. Returns (zero Rates, false) when
// the model is unknown.
func Lookup(model string) (Rates, bool) {
	return activeSource.Load().s.Lookup(model)
}

// Calculate returns the total USD cost for a tool call. When the model has
// no pricing data the second return is false and the cost is zero — callers
// should treat that as "pricing unavailable" rather than "free."
func Calculate(model string, usage Usage) (float64, bool) {
	c, ok := CalculateBreakdown(model, usage)
	if !ok {
		return 0, false
	}
	return c.Total(), true
}

// CalculateBreakdown returns the per-component USD cost. Cache-read and
// cache-write tokens are priced separately from input tokens; callers that
// only want a session total should use Calculate.
func CalculateBreakdown(model string, usage Usage) (Cost, bool) {
	rates, ok := Lookup(model)
	if !ok {
		return Cost{}, false
	}
	return rates.cost(usage), true
}

// cost computes the per-component USD breakdown for a Usage given these
// rates. Unexported so callers go through the package-level entry points.
func (r Rates) cost(u Usage) Cost {
	return Cost{
		Input:      float64(u.InputTokens) * r.InputPerToken,
		Output:     float64(u.OutputTokens) * r.OutputPerToken,
		CacheRead:  float64(u.CacheReadTokens) * r.CacheReadPerToken,
		CacheWrite: float64(u.CacheWriteTokens) * r.CacheWritePerToken,
	}
}
