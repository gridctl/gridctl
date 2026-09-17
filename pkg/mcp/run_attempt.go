package mcp

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type noopAttempt struct {
	ctx context.Context
}

func (n noopAttempt) Context() context.Context          { return n.ctx }
func (noopAttempt) SetOutcome(string, string, string)   {}
func (noopAttempt) SetResolved(string, string, int)     {}
func (noopAttempt) SetDownstreamDuration(time.Duration) {}
func (noopAttempt) SetTraceID(string)                   {}
func (noopAttempt) SetLabels(string, string)            {}
func (noopAttempt) SetPreviousAttemptID(string)         {}
func (noopAttempt) Finish()                             {}

func (g *Gateway) startRunAttempt(ctx context.Context, requestedName string) RunAttempt {
	g.mu.RLock()
	sink := g.runSink
	g.mu.RUnlock()
	if sink == nil {
		return noopAttempt{ctx: ctx}
	}
	a := sink.Begin(ctx, requestedName)
	if a == nil {
		return noopAttempt{ctx: ctx}
	}
	a.SetLabels(ClientIDFromContext(ctx), ClientAccessIDFromContext(ctx))
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() && sc.IsSampled() {
		a.SetTraceID(sc.TraceID().String())
	}
	if relay := mrtrRelayFromContext(ctx); relay != nil && relay.AttemptID != "" {
		a.SetPreviousAttemptID(relay.AttemptID)
	}
	return a
}

func classifyDownstream(a RunAttempt, result *ToolCallResult, callErr error, ctx context.Context) {
	out := classifyCall(result, callErr, ctx)
	a.SetOutcome(out.Disposition, out.Stage, out.Reason)
}
