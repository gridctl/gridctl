package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	hexTraceID = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hexSpanID  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// TestHandleTraceOTLP_specShape asserts the OTLP/JSON encoding rules the spec
// mandates and generic protojson gets wrong: lowerCamelCase keys, trace/span
// IDs as hex strings (not base64), and uint64 nano timestamps as strings.
func TestHandleTraceOTLP_specShape(t *testing.T) {
	srv, tracer := newTracedServer(t)

	start := time.Now().Add(-time.Minute)
	ctx, root := tracer.Start(context.Background(), "github › create_issue", trace.WithTimestamp(start))
	root.SetAttributes(attribute.String("server.name", "github"))
	root.SetStatus(codes.Ok, "")
	_, child := tracer.Start(ctx, "mcp.client.call_tool", trace.WithTimestamp(start.Add(time.Millisecond)))
	child.AddEvent("retry", trace.WithAttributes(attribute.String("reason", "backoff")))
	child.SetStatus(codes.Error, "downstream failed")
	child.End(trace.WithTimestamp(start.Add(40 * time.Millisecond)))
	root.End(trace.WithTimestamp(start.Add(42 * time.Millisecond)))
	traceID := root.SpanContext().TraceID().String()
	rootSpanID := root.SpanContext().SpanID().String()

	req := httptest.NewRequest(http.MethodGet, "/api/traces/"+traceID+"/otlp", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Error("missing Content-Disposition header")
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	resourceSpans, ok := payload["resourceSpans"].([]any)
	if !ok || len(resourceSpans) != 1 {
		t.Fatalf("resourceSpans = %v, want 1 entry under lowerCamelCase key", payload["resourceSpans"])
	}
	rs := resourceSpans[0].(map[string]any)
	if _, ok := rs["resource"]; !ok {
		t.Error("resourceSpans[0] missing resource")
	}
	scopeSpans, ok := rs["scopeSpans"].([]any)
	if !ok || len(scopeSpans) != 1 {
		t.Fatalf("scopeSpans = %v, want 1 entry", rs["scopeSpans"])
	}
	spans, ok := scopeSpans[0].(map[string]any)["spans"].([]any)
	if !ok || len(spans) != 2 {
		t.Fatalf("spans = %v, want 2", scopeSpans[0].(map[string]any)["spans"])
	}

	var sawChild bool
	for _, raw := range spans {
		sp := raw.(map[string]any)
		id, _ := sp["traceId"].(string)
		if !hexTraceID.MatchString(id) {
			t.Errorf("traceId %q is not 32-char lowercase hex (base64 means protojson leaked through)", id)
		}
		spanID, _ := sp["spanId"].(string)
		if !hexSpanID.MatchString(spanID) {
			t.Errorf("spanId %q is not 16-char lowercase hex", spanID)
		}
		for _, key := range []string{"startTimeUnixNano", "endTimeUnixNano"} {
			if _, ok := sp[key].(string); !ok {
				t.Errorf("span %v: %s must be a JSON string per OTLP/JSON uint64 rules, got %T",
					sp["name"], key, sp[key])
			}
		}
		if parent, ok := sp["parentSpanId"].(string); ok {
			sawChild = true
			if parent != rootSpanID {
				t.Errorf("parentSpanId = %q, want root %q", parent, rootSpanID)
			}
			events, ok := sp["events"].([]any)
			if !ok || len(events) != 1 {
				t.Fatalf("child events = %v, want 1", sp["events"])
			}
			ev := events[0].(map[string]any)
			if _, ok := ev["timeUnixNano"].(string); !ok {
				t.Errorf("event timeUnixNano must be a string, got %T", ev["timeUnixNano"])
			}
			// Error status maps to the OTLP enum name.
			if code := sp["status"].(map[string]any)["code"]; code != "STATUS_CODE_ERROR" {
				t.Errorf("child status code = %v, want STATUS_CODE_ERROR", code)
			}
		} else {
			// Root: Ok status enum and attribute round-trip.
			if code := sp["status"].(map[string]any)["code"]; code != "STATUS_CODE_OK" {
				t.Errorf("root status code = %v, want STATUS_CODE_OK", code)
			}
			attrs, ok := sp["attributes"].([]any)
			if !ok || len(attrs) == 0 {
				t.Fatalf("root attributes = %v, want at least server.name", sp["attributes"])
			}
			found := false
			for _, raw := range attrs {
				kv := raw.(map[string]any)
				if kv["key"] == "server.name" {
					found = true
					if v := kv["value"].(map[string]any)["stringValue"]; v != "github" {
						t.Errorf("server.name = %v, want github", v)
					}
				}
			}
			if !found {
				t.Error("root attributes missing server.name key/value pair")
			}
		}
	}
	if !sawChild {
		t.Error("no span carried parentSpanId; child linkage lost in export")
	}
}

func TestHandleTraceOTLP_notFound(t *testing.T) {
	srv, _ := newTracedServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/traces/ffffffffffffffffffffffffffffffff/otlp", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
