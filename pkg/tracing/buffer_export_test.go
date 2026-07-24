package tracing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// newBufferTracer wires a Buffer as a synchronous exporter behind a real SDK
// tracer so ExportSpans is exercised with real ReadOnlySpans.
func newBufferTracer(b *Buffer, scope string) trace.Tracer {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(b)),
	)
	return tp.Tracer(scope)
}

func TestExportSpans_parentedTree(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	tracer := newBufferTracer(b, "gridctl.test")

	ctx, root := tracer.Start(context.Background(), "root")
	_, child1 := tracer.Start(ctx, "child1")
	child1.End()
	_, child2 := tracer.Start(ctx, "child2")
	child2.End()
	root.End()

	if got := b.Count(); got != 1 {
		t.Fatalf("Count = %d, want 1 finalised trace", got)
	}
	tr := b.GetRecent(1)[0]
	if tr.SpanCount != 3 {
		t.Fatalf("SpanCount = %d, want 3", tr.SpanCount)
	}

	rootID := ""
	for _, sp := range tr.Spans {
		if sp.ParentID == "" {
			rootID = sp.SpanID
			if sp.Name != "root" {
				t.Errorf("root span name = %q, want %q", sp.Name, "root")
			}
		}
	}
	if rootID == "" {
		t.Fatal("no root span (ParentID == \"\") in trace record")
	}
	for _, sp := range tr.Spans {
		if sp.Name == "child1" || sp.Name == "child2" {
			if sp.ParentID != rootID {
				t.Errorf("span %q ParentID = %q, want root %q", sp.Name, sp.ParentID, rootID)
			}
		}
	}
}

func TestExportSpans_capturesEvents(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	tracer := newBufferTracer(b, "gridctl.test")

	_, span := tracer.Start(context.Background(), "op")
	span.AddEvent("retry", trace.WithAttributes(attribute.String("reason", "backoff")))
	span.End()

	tr := b.GetRecent(1)
	if len(tr) != 1 || len(tr[0].Spans) != 1 {
		t.Fatalf("expected 1 trace with 1 span, got %+v", tr)
	}
	events := tr[0].Spans[0].Events
	if len(events) != 1 {
		t.Fatalf("Events len = %d, want 1", len(events))
	}
	if events[0].Name != "retry" {
		t.Errorf("event name = %q, want %q", events[0].Name, "retry")
	}
	if events[0].Attrs["reason"] != "backoff" {
		t.Errorf("event attrs = %v, want reason=backoff", events[0].Attrs)
	}
	if events[0].Timestamp.IsZero() {
		t.Error("event timestamp is zero")
	}
}

func TestExportSpans_scopeFilter(t *testing.T) {
	infraScope := "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	b := NewBuffer(10, time.Hour)
	tracer := newBufferTracer(b, infraScope)
	_, span := tracer.Start(context.Background(), "GET /v1.51/containers/json")
	span.End()
	if got := b.Count(); got != 0 {
		t.Errorf("Count = %d, want 0: non-gridctl scope must be excluded by default", got)
	}

	b2 := NewBuffer(10, time.Hour)
	b2.SetIncludeInfra(true)
	tracer2 := newBufferTracer(b2, infraScope)
	_, span2 := tracer2.Start(context.Background(), "GET /v1.51/containers/json")
	span2.End()
	if got := b2.Count(); got != 1 {
		t.Errorf("Count = %d, want 1: include_infra must admit infra scopes", got)
	}

	// gridctl's own scopes always pass.
	b3 := NewBuffer(10, time.Hour)
	tracer3 := newBufferTracer(b3, "gridctl.gateway")
	_, span3 := tracer3.Start(context.Background(), "mcp.tools.call")
	span3.End()
	if got := b3.Count(); got != 1 {
		t.Errorf("Count = %d, want 1: gridctl scope must be admitted", got)
	}
}

func TestSeedFromFile_scopeFilter(t *testing.T) {
	makeScopeSpans := func(scope, name string, traceID byte) *tracepb.ScopeSpans {
		now := time.Now().Add(-time.Minute).UnixNano()
		tid := make([]byte, 16)
		tid[15] = traceID
		sid := make([]byte, 8)
		sid[7] = traceID
		return &tracepb.ScopeSpans{
			Scope: &commonpb.InstrumentationScope{Name: scope},
			Spans: []*tracepb.Span{{
				TraceId:           tid,
				SpanId:            sid,
				Name:              name,
				StartTimeUnixNano: uint64(now),
				EndTimeUnixNano:   uint64(now + int64(5*time.Millisecond)),
			}},
		}
	}

	td := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{
				makeScopeSpans("gridctl.gateway", "github › create_issue", 1),
				makeScopeSpans("go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp", "GET /v1.51/containers/json", 2),
			},
		}},
	}
	line, err := protojson.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "traces.jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	b := NewBuffer(10, time.Hour)
	if err := b.SeedFromFile(path, 10); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}
	if got := b.Count(); got != 1 {
		t.Fatalf("Count = %d, want 1: persisted infra spans must not reseed into the UI buffer", got)
	}
	if op := b.GetRecent(1)[0].Operation; op != "github › create_issue" {
		t.Errorf("seeded operation = %q, want the gridctl-scoped span", op)
	}

	b2 := NewBuffer(10, time.Hour)
	b2.SetIncludeInfra(true)
	if err := b2.SeedFromFile(path, 10); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}
	if got := b2.Count(); got != 2 {
		t.Errorf("Count = %d, want 2: include_infra must admit persisted infra spans", got)
	}
}
