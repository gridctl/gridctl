package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/tracing"
)

type upstreamProtocolKey struct{}
type traceLinksKey struct{}

// WithUpstreamProtocolVersion stores the negotiated upstream protocol version
// for the root span. Handshake sessions pass the session version; the
// stateless path passes RequestMeta.ProtocolVersion.
func WithUpstreamProtocolVersion(ctx context.Context, version string) context.Context {
	if version == "" {
		return ctx
	}
	return context.WithValue(ctx, upstreamProtocolKey{}, version)
}

func upstreamProtocolVersion(ctx context.Context) string {
	v, _ := ctx.Value(upstreamProtocolKey{}).(string)
	return v
}

func withTraceLinks(ctx context.Context, links []trace.Link) context.Context {
	if len(links) == 0 {
		return ctx
	}
	return context.WithValue(ctx, traceLinksKey{}, links)
}

func traceLinksFromContext(ctx context.Context) []trace.Link {
	links, _ := ctx.Value(traceLinksKey{}).([]trace.Link)
	return links
}

// withoutTraceLinks shadows a header-derived link so a nested dispatch does
// not attach it again. The outer root already received the link.
func withoutTraceLinks(ctx context.Context) context.Context {
	if len(traceLinksFromContext(ctx)) == 0 {
		return ctx
	}
	return context.WithValue(ctx, traceLinksKey{}, []trace.Link(nil))
}

// extractMetaTraceContext extracts the configured propagator from params._meta.
// Only the _meta object is decoded, and only its string members, so sibling
// integers beyond 53 bits are never touched.
func extractMetaTraceContext(ctx context.Context, params json.RawMessage) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, metaStringCarrier(params))
}

// parentContextFromRequest chooses the root span's parent. A valid
// _meta.traceparent wins. When that is absent, HTTP headers are the parent.
// When both are present, _meta is the parent and the header context is a
// span link.
func parentContextFromRequest(ctx context.Context, params json.RawMessage, headers http.Header) context.Context {
	metaSC := trace.SpanContextFromContext(extractMetaTraceContext(context.Background(), params))
	headerSC := trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(headers)))
	switch {
	case metaSC.IsValid():
		ctx = extractMetaTraceContext(ctx, params)
		if headerSC.IsValid() {
			ctx = withTraceLinks(ctx, []trace.Link{{SpanContext: headerSC}})
		}
		return ctx
	case headerSC.IsValid():
		return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
	default:
		return ctx
	}
}

func metaStringCarrier(params json.RawMessage) propagation.TextMapCarrier {
	carrier := tracing.NewMetaCarrier(nil)
	for key, value := range w3cMetaStrings(params) {
		carrier.Set(key, value)
	}
	return carrier
}

// w3cMetaStrings reads traceparent, tracestate, and baggage from the raw
// _meta object. Other members, including big integers, are not decoded.
func w3cMetaStrings(params json.RawMessage) map[string]string {
	out := map[string]string{}
	if len(params) == 0 {
		return out
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil || envelope.Meta == nil {
		return out
	}
	for _, key := range []string{"traceparent", "tracestate", "baggage"} {
		raw, ok := envelope.Meta[key]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		out[key] = value
	}
	return out
}
