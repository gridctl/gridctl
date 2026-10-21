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

	// Merge through json.RawMessage, never map[string]any: params can
	// carry integers beyond float64's 53-bit mantissa, and a full
	// decode-reencode would silently corrupt them (the
	// stampStatelessMeta precedent). Only the _meta trace keys are
	// touched; every sibling byte passes through unmodified.
	obj := map[string]json.RawMessage{}
	if len(paramsBytes) > 0 {
		if err := json.Unmarshal(paramsBytes, &obj); err != nil {
			// Not a JSON object — leave params unchanged.
			return paramsBytes
		}
	}
	meta := map[string]json.RawMessage{}
	if existing, ok := obj["_meta"]; ok {
		if err := json.Unmarshal(existing, &meta); err != nil {
			// _meta is not an object — leave params unchanged.
			return paramsBytes
		}
	}

	// Inject trace context into a fresh carrier (populates
	// traceparent/tracestate/baggage), then graft its string values in.
	// An oversized baggage value is dropped rather than forwarded.
	carrier := tracing.NewMetaCarrier(nil)
	otel.GetTextMapPropagator().Inject(ctx, tracing.NewCappedCarrier(carrier))
	for key, value := range carrier.Map() {
		s, ok := value.(string)
		if !ok {
			continue
		}
		if _, dropped := tracing.CapBaggage(s); dropped {
			delete(meta, key)
			continue
		}
		encoded, err := json.Marshal(s)
		if err != nil {
			continue
		}
		meta[key] = encoded
	}
	if raw, ok := meta["baggage"]; ok {
		var existing string
		if json.Unmarshal(raw, &existing) == nil {
			if _, dropped := tracing.CapBaggage(existing); dropped {
				delete(meta, "baggage")
			}
		}
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return paramsBytes
	}
	obj["_meta"] = metaBytes
	result, err := json.Marshal(obj)
	if err != nil {
		return paramsBytes
	}
	return result
}
