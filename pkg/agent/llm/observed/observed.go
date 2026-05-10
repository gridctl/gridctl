// Package observed wraps an agent.ChatModel with the gridctl
// observability surface — OTel spans, pricing.CalculateBreakdown for USD
// cost, and metrics.Accumulator.RecordCost with a synthetic provider
// name (no MCP envelope spoofing). The wrapped model satisfies the same
// agent.ChatModel interface, so it is a drop-in replacement at the call
// site (sandbox bindings, Go skill direct calls, the playground
// service).
//
// Why a wrapper rather than per-provider instrumentation: each provider
// (anthropic, openai, google, gateway) implements a single concern —
// translating a gridctl ChatRequest to/from a vendor wire format. The
// observability layer is orthogonal to that concern. Centralising it
// here means one place to maintain span attribute names, one place to
// keep cost recording synchronised with the metrics package shape, and
// one place to evolve the contract when (for example) per-client cost
// attribution lands.
//
// Tracing: Every Generate / Stream call opens a span named
// `agent.llm.generate` or `agent.llm.stream` under the tracer
// `gridctl.agent.llm`. The span carries gen_ai.* attributes (model,
// provider, prompt_tokens, completion_tokens) so it slots into the
// existing observability stack alongside `mcp.routing` parents.
//
// Pricing & metrics: After a non-error response, the wrapper computes
// per-component cost via pricing.CalculateBreakdown and records it via
// metrics.Accumulator.RecordCost using the configured ProviderName
// (e.g. "llm:anthropic"). Streaming responses bookkeep usage from the
// final ChatChunk; if the provider does not surface usage on the stream
// terminator, no cost is recorded — a known limitation rather than a
// silent miscount.
package observed

import (
	"context"
	"errors"
	"fmt"
	"io"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pricing"
)

const tracerName = "gridctl.agent.llm"

// Accumulator is the subset of *metrics.Accumulator the wrapper needs.
// Defined here so callers can inject a fake in tests without depending
// on the real Accumulator's atomic internals.
type Accumulator interface {
	RecordCost(serverName string, replicaID int, cost metrics.CostBreakdown)
}

// Provider wraps an underlying agent.ChatModel and adds OTel tracing,
// USD cost calculation, and metrics recording. The zero value is not
// usable; construct via New.
type Provider struct {
	inner        agent.ChatModel
	providerName string
	accumulator  Accumulator
	tracer       trace.Tracer
}

// Option configures a Provider during construction.
type Option func(*Provider)

// WithAccumulator wires a metrics.Accumulator (or a test fake) for
// cost recording. Without one, the wrapper still emits spans but skips
// the RecordCost call. nil is a no-op so callers can pass a possibly-
// nil accumulator without branching.
func WithAccumulator(acc Accumulator) Option {
	return func(p *Provider) {
		if acc != nil {
			p.accumulator = acc
		}
	}
}

// WithTracer overrides the OTel tracer used to open spans. Defaults to
// otel.Tracer("gridctl.agent.llm"). Useful in tests that want a
// deterministic tracer attached to a specific provider; nil is a no-op.
func WithTracer(tr trace.Tracer) Option {
	return func(p *Provider) {
		if tr != nil {
			p.tracer = tr
		}
	}
}

// New wraps an inner agent.ChatModel with observability. providerName
// is the synthetic server name the wrapper passes to RecordCost
// (convention: "llm:anthropic", "llm:openai", "llm:google", or
// "llm:unknown" when the model prefix does not match a known family —
// callers typically derive it from the provider package they're wiring).
//
// inner must be non-nil; an empty providerName is rejected so misconfigured
// callers fail at construction rather than recording cost under an
// empty server bucket.
func New(inner agent.ChatModel, providerName string, opts ...Option) (*Provider, error) {
	if inner == nil {
		return nil, errors.New("agent/llm/observed: inner ChatModel is required")
	}
	if providerName == "" {
		return nil, errors.New("agent/llm/observed: providerName is required")
	}
	p := &Provider{
		inner:        inner,
		providerName: providerName,
		tracer:       otel.Tracer(tracerName),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// Generate dispatches the request through the inner provider, opens an
// `agent.llm.generate` span, and records cost via the configured
// accumulator. Errors are recorded on the span and propagated unchanged.
func (p *Provider) Generate(ctx context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	ctx, span := p.tracer.Start(ctx, "agent.llm.generate", trace.WithAttributes(
		attribute.String("gen_ai.system", p.providerName),
		attribute.String("gen_ai.request.model", req.Model),
	))
	defer span.End()

	resp, err := p.inner.Generate(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return resp, err
	}
	p.annotateUsage(span, resp.Model, resp.Usage)
	p.recordCost(resp.Model, resp.Usage)
	return resp, nil
}

// Stream dispatches the streaming request through the inner provider
// and returns a wrapped StreamReader that emits the same chunks while
// accumulating usage. The wrapper records cost when the StreamReader is
// closed (or fully drained) using the final usage observed on the
// stream — providers that emit usage only on the terminal chunk are
// the common case and are handled correctly.
func (p *Provider) Stream(ctx context.Context, req agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	ctx, span := p.tracer.Start(ctx, "agent.llm.stream", trace.WithAttributes(
		attribute.String("gen_ai.system", p.providerName),
		attribute.String("gen_ai.request.model", req.Model),
	))

	stream, err := p.inner.Stream(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		return nil, err
	}

	chunks, finalModel, finalUsage, drainErr := drainStream(stream, req.Model)
	if drainErr != nil {
		span.RecordError(drainErr)
		span.SetStatus(codes.Error, drainErr.Error())
		span.End()
		return nil, drainErr
	}
	p.annotateUsage(span, finalModel, finalUsage)
	p.recordCost(finalModel, finalUsage)
	span.End()

	return agent.StreamReaderFromSlice(chunks), nil
}

// drainStream reads every chunk into a slice while tracking the final
// usage and model. Returning a buffered slice rather than a wrapped
// reader keeps the cost-recording deterministic — the alternative
// (recording inside a Close hook on the live reader) would mean cost is
// recorded only when the caller bothers to Close, which is not a
// guarantee the agent.ChatModel contract makes.
//
// The trade-off: full materialisation defers the first byte until the
// last byte. For the playground UI this is invisible (the Stream is
// already a one-shot completion) but the wrapper is not the right
// shape for true token-by-token interactive streaming. A future
// pkg/agent/llm/observed.Streaming variant can ship if that use case
// emerges.
func drainStream(stream *agent.StreamReader[agent.ChatChunk], reqModel string) ([]agent.ChatChunk, string, agent.Usage, error) {
	if stream == nil {
		return nil, reqModel, agent.Usage{}, errors.New("agent/llm/observed: nil stream returned by inner provider")
	}
	defer stream.Close() //nolint:errcheck // best-effort close on inner stream
	var chunks []agent.ChatChunk
	var usage agent.Usage
	model := reqModel
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, model, usage, fmt.Errorf("agent/llm/observed: stream recv: %w", err)
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		chunks = append(chunks, chunk)
	}
	return chunks, model, usage, nil
}

// annotateUsage attaches gen_ai.* token-count attributes so trace
// consumers can render per-call cost without re-running pricing.
func (p *Provider) annotateUsage(span trace.Span, model string, usage agent.Usage) {
	if model != "" {
		span.SetAttributes(attribute.String("gen_ai.response.model", model))
	}
	if usage.InputTokens > 0 {
		span.SetAttributes(attribute.Int("gen_ai.usage.prompt_tokens", usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		span.SetAttributes(attribute.Int("gen_ai.usage.completion_tokens", usage.OutputTokens))
	}
	if usage.CacheReadTokens > 0 {
		span.SetAttributes(attribute.Int("gen_ai.usage.cache_read_tokens", usage.CacheReadTokens))
	}
	if usage.CacheWriteTokens > 0 {
		span.SetAttributes(attribute.Int("gen_ai.usage.cache_write_tokens", usage.CacheWriteTokens))
	}
}

// recordCost prices the call via pkg/pricing and forwards the
// breakdown to the accumulator. Skipped silently when no accumulator
// is wired, when the model is unknown to the active pricing source, or
// when usage is empty (no tokens to price).
func (p *Provider) recordCost(model string, usage agent.Usage) {
	if p.accumulator == nil || model == "" {
		return
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 &&
		usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0 {
		return
	}
	cost, ok := pricing.CalculateBreakdown(model, pricing.Usage{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	})
	if !ok {
		return
	}
	p.accumulator.RecordCost(p.providerName, -1, metrics.CostBreakdown{
		Input:      cost.Input,
		Output:     cost.Output,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
	})
}
