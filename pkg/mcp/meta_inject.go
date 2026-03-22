package mcp

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/tracing"
)

// injectMetaTraceparent injects W3C trace context into the JSON-RPC params
// _meta field for stdio/process transports. The traceparent and tracestate
// values are propagated per MCP spec PR #414.
//
// If params is nil or empty, a minimal {"_meta": {...}} object is returned.
// If params already has a _meta key, the trace values are merged in.
// Returns the original paramsBytes unchanged if no active span is present.
func injectMetaTraceparent(ctx context.Context, paramsBytes json.RawMessage) json.RawMessage {
	// No-op when there is no active sampled span.
	if !oteltrace.SpanFromContext(ctx).SpanContext().IsValid() {
		return paramsBytes
	}

	// Unmarshal params to an object (handle nil/empty as empty object).
	var obj map[string]any
	if len(paramsBytes) > 0 {
		if err := json.Unmarshal(paramsBytes, &obj); err != nil {
			// Not a JSON object — leave params unchanged.
			return paramsBytes
		}
	}
	if obj == nil {
		obj = make(map[string]any)
	}

	// Get or create the _meta map.
	var meta map[string]any
	if existing, ok := obj["_meta"]; ok {
		if m, ok := existing.(map[string]any); ok {
			meta = m
		}
	}
	carrier := tracing.NewMetaCarrier(meta)

	// Inject trace context into the carrier (populates traceparent/tracestate).
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	obj["_meta"] = carrier.Map()

	result, err := json.Marshal(obj)
	if err != nil {
		return paramsBytes
	}
	return result
}
