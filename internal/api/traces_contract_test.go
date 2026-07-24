package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/tracing"
)

// newTracedServer wires a real SDK tracer into the test server's trace buffer
// so seeded spans travel the same ExportSpans path as production spans.
func newTracedServer(t *testing.T) (*Server, trace.Tracer) {
	t.Helper()
	srv := newTestServer(t)
	buf := tracing.NewBuffer(100, time.Hour)
	srv.SetTraceBuffer(buf)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buf)),
	)
	return srv, tp.Tracer("gridctl.test")
}

// seedTrace records a single-span trace with a controlled duration and
// returns its trace ID.
func seedTrace(tracer trace.Tracer, name string, dur time.Duration) string {
	start := time.Now().Add(-time.Minute)
	_, sp := tracer.Start(context.Background(), name, trace.WithTimestamp(start))
	sp.End(trace.WithTimestamp(start.Add(dur)))
	return sp.SpanContext().TraceID().String()
}

// TestHandleTraceDetail_contract asserts the raw JSON keys of the span
// payload. Deliberately decoded into maps, not the DTO struct: decoding into
// the struct that produced the JSON would mask key renames and omissions,
// which is how endTime/parentSpanId drifted from the UI in the first place.
func TestHandleTraceDetail_contract(t *testing.T) {
	srv, tracer := newTracedServer(t)

	start := time.Now().Add(-time.Minute)
	ctx, root := tracer.Start(context.Background(), "github › create_issue", trace.WithTimestamp(start))
	root.SetAttributes(attribute.String("server.name", "github"))
	_, child := tracer.Start(ctx, "mcp.client.call_tool", trace.WithTimestamp(start.Add(5*time.Millisecond)))
	child.AddEvent("retry", trace.WithAttributes(attribute.String("reason", "backoff")))
	child.End(trace.WithTimestamp(start.Add(40 * time.Millisecond)))
	root.End(trace.WithTimestamp(start.Add(42 * time.Millisecond)))

	traceID := root.SpanContext().TraceID().String()
	rootSpanID := root.SpanContext().SpanID().String()

	req := httptest.NewRequest(http.MethodGet, "/api/traces/"+traceID, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload struct {
		TraceID string           `json:"traceId"`
		Spans   []map[string]any `json:"spans"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(payload.Spans))
	}

	for _, sp := range payload.Spans {
		if _, ok := sp["parentSpanId"]; !ok {
			t.Errorf("span %v is missing key %q", sp["name"], "parentSpanId")
		}
		st, err := time.Parse(time.RFC3339Nano, sp["startTime"].(string))
		if err != nil {
			t.Fatalf("startTime unparseable: %v", err)
		}
		endRaw, ok := sp["endTime"].(string)
		if !ok {
			t.Fatalf("span %v is missing key %q", sp["name"], "endTime")
		}
		et, err := time.Parse(time.RFC3339Nano, endRaw)
		if err != nil {
			t.Fatalf("endTime unparseable: %v", err)
		}
		if et.Before(st) {
			t.Errorf("span %v: endTime %v before startTime %v", sp["name"], et, st)
		}
	}

	// Child links to root via parentSpanId; its event round-trips.
	for _, sp := range payload.Spans {
		if sp["name"] != "mcp.client.call_tool" {
			continue
		}
		if got := sp["parentSpanId"]; got != rootSpanID {
			t.Errorf("child parentSpanId = %v, want %v", got, rootSpanID)
		}
		events, ok := sp["events"].([]any)
		if !ok || len(events) != 1 {
			t.Fatalf("child events = %v, want 1 event", sp["events"])
		}
		ev := events[0].(map[string]any)
		if ev["name"] != "retry" {
			t.Errorf("event name = %v, want retry", ev["name"])
		}
		if _, err := time.Parse(time.RFC3339Nano, ev["timestamp"].(string)); err != nil {
			t.Errorf("event timestamp unparseable: %v", err)
		}
	}
}

// TestHandleTraces_minDurationForms asserts filtering behavior for both
// accepted forms and rejection of garbage. Regression test for bare-integer
// input being silently ignored.
func TestHandleTraces_minDurationForms(t *testing.T) {
	srv, tracer := newTracedServer(t)
	fastID := seedTrace(tracer, "fast", 10*time.Millisecond)
	slowID := seedTrace(tracer, "slow", 900*time.Millisecond)

	cases := []struct {
		name     string
		query    string
		wantCode int
		wantIDs  map[string]bool
	}{
		{"bare integer is milliseconds", "minDuration=500", http.StatusOK, map[string]bool{slowID: true}},
		{"go duration", "minDuration=500ms", http.StatusOK, map[string]bool{slowID: true}},
		{"zero threshold keeps all", "minDuration=0", http.StatusOK, map[string]bool{fastID: true, slowID: true}},
		{"garbage is rejected", "minDuration=bogus", http.StatusBadRequest, nil},
		{"negative is rejected", "minDuration=-5", http.StatusBadRequest, nil},
		{"negative duration is rejected", "minDuration=-5ms", http.StatusBadRequest, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/traces?"+tc.query, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantCode != http.StatusOK {
				return
			}
			var result traceListDTO
			if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := map[string]bool{}
			for _, tr := range result.Traces {
				got[tr.TraceID] = true
			}
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("returned %d traces %v, want %d", len(got), got, len(tc.wantIDs))
			}
			for id := range tc.wantIDs {
				if !got[id] {
					t.Errorf("missing expected trace %s", id)
				}
			}
		})
	}
}
