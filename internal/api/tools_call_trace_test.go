package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/tracing"
)

// installTracerProvider swaps the global tracer provider for one test. Tests
// that call it must not run in parallel with each other.
func installTracerProvider(t *testing.T, sampler sdktrace.Sampler, buf *tracing.Buffer) {
	t.Helper()
	opts := []sdktrace.TracerProviderOption{sdktrace.WithSampler(sampler)}
	if buf != nil {
		opts = append(opts, sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buf)))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(old)
		_ = tp.Shutdown(t.Context())
	})
}

func disableTracing(t *testing.T) {
	t.Helper()
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(old) })
}

func rawCallEnvelope(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	return raw
}

func echoStub() *callStubClient {
	return &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo", Description: "Echo"}}}
}

func TestHandleToolsCall_TraceIDPresentWhenRecorded(t *testing.T) {
	buf := tracing.NewBuffer(10, time.Hour)
	installTracerProvider(t, sdktrace.AlwaysSample(), buf)
	srv := newCallServer(t, echoStub())
	srv.SetTraceBuffer(buf)

	rec := postToolsCall(srv.Handler(), `{"name":"echo__echo","arguments":{},"client":"ui"}`, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	env := decodeCallEnvelope(t, rec)
	if env.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d", env.SchemaVersion)
	}
	if len(env.TraceID) != 32 {
		t.Fatalf("trace_id = %q, want 32 hex chars", env.TraceID)
	}
	if buf.GetByID(env.TraceID) == nil {
		t.Fatalf("trace_id %q not found in the trace buffer", env.TraceID)
	}
}

func TestHandleToolsCall_TraceIDPresentOnDenial(t *testing.T) {
	buf := tracing.NewBuffer(10, time.Hour)
	installTracerProvider(t, sdktrace.AlwaysSample(), buf)
	srv := newCallServer(t, echoStub())
	srv.SetTraceBuffer(buf)

	rec := postToolsCall(srv.Handler(), `{"name":"echo__missing"}`, "application/json")
	env := decodeCallEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != errCodeUnknownTool {
		t.Fatalf("error = %+v", env.Error)
	}
	if env.TraceID == "" || buf.GetByID(env.TraceID) == nil {
		t.Fatalf("denied dispatch trace_id = %q, want a recorded trace", env.TraceID)
	}
}

func TestHandleToolsCall_TraceIDOmitted(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, srv *Server)
		body   string
		status int
	}{
		{
			name: "tracing disabled with buffer",
			setup: func(t *testing.T, srv *Server) {
				disableTracing(t)
				srv.SetTraceBuffer(tracing.NewBuffer(10, time.Hour))
			},
			body:   `{"name":"echo__echo"}`,
			status: http.StatusOK,
		},
		{
			name: "span not sampled",
			setup: func(t *testing.T, srv *Server) {
				buf := tracing.NewBuffer(10, time.Hour)
				installTracerProvider(t, sdktrace.NeverSample(), buf)
				srv.SetTraceBuffer(buf)
			},
			body:   `{"name":"echo__echo"}`,
			status: http.StatusOK,
		},
		{
			name: "no trace buffer",
			setup: func(t *testing.T, srv *Server) {
				installTracerProvider(t, sdktrace.AlwaysSample(), nil)
			},
			body:   `{"name":"echo__echo"}`,
			status: http.StatusOK,
		},
		{
			name: "validation error",
			setup: func(t *testing.T, srv *Server) {
				buf := tracing.NewBuffer(10, time.Hour)
				installTracerProvider(t, sdktrace.AlwaysSample(), buf)
				srv.SetTraceBuffer(buf)
			},
			body:   `{"name":"echo"}`,
			status: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newCallServer(t, echoStub())
			tt.setup(t, srv)
			rec := postToolsCall(srv.Handler(), tt.body, "application/json")
			if rec.Code != tt.status {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			raw := rawCallEnvelope(t, rec.Body.Bytes())
			if _, ok := raw["trace_id"]; ok {
				t.Fatalf("trace_id present: %s", rec.Body.String())
			}
			if string(raw["schema_version"]) != "1" {
				t.Fatalf("schema_version = %s", raw["schema_version"])
			}
		})
	}
}

func TestCallOutcome_TraceIDNotSerialized(t *testing.T) {
	b, err := json.Marshal(mcp.CallOutcome{Disposition: "completed", TraceID: "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for k := range raw {
		if k == "trace_id" || k == "TraceID" {
			t.Fatalf("CallOutcome JSON leaks the trace ID: %s", b)
		}
	}
}
