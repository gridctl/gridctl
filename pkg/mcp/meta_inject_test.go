package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func setupTestTracer(t *testing.T) func() {
	t.Helper()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
	))
	return func() { _ = tp.Shutdown(context.Background()) }
}

func TestInjectMetaTraceparent_noActiveSpan(t *testing.T) {
	// Without an active span, params should be returned unchanged.
	original := json.RawMessage(`{"name":"test"}`)
	got := injectMetaTraceparent(context.Background(), original)
	if string(got) != string(original) {
		t.Errorf("no-op injection changed params: got %s, want %s", got, original)
	}
}

func TestInjectMetaTraceparent_withActiveSpan(t *testing.T) {
	cleanup := setupTestTracer(t)
	defer cleanup()

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	params := json.RawMessage(`{"name":"tool"}`)
	result := injectMetaTraceparent(ctx, params)

	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	meta, ok := obj["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("_meta not injected into params, got: %v", obj)
	}

	if _, ok := meta["traceparent"]; !ok {
		t.Error("traceparent not in _meta")
	}
}

func TestInjectMetaTraceparent_nilParams(t *testing.T) {
	cleanup := setupTestTracer(t)
	defer cleanup()

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	result := injectMetaTraceparent(ctx, nil)

	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := obj["_meta"]; !ok {
		t.Error("_meta not present in result from nil params")
	}
}

func TestInjectMetaTraceparent_existingMeta(t *testing.T) {
	cleanup := setupTestTracer(t)
	defer cleanup()

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	// Params already has _meta with some other key.
	params := json.RawMessage(`{"_meta":{"sessionId":"sess123"}}`)
	result := injectMetaTraceparent(ctx, params)

	var obj map[string]any
	if err := json.Unmarshal(result, &obj); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	meta, ok := obj["_meta"].(map[string]any)
	if !ok {
		t.Fatal("_meta is not a map")
	}
	if meta["sessionId"] != "sess123" {
		t.Error("existing _meta key was lost")
	}
	if _, ok := meta["traceparent"]; !ok {
		t.Error("traceparent not injected alongside existing _meta")
	}
}

func TestInjectMetaTraceparent_nonObjectParams(t *testing.T) {
	cleanup := setupTestTracer(t)
	defer cleanup()

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test")
	defer span.End()

	// Params is a JSON array — should be returned unchanged.
	params := json.RawMessage(`["a","b"]`)
	result := injectMetaTraceparent(ctx, params)
	if string(result) != string(params) {
		t.Errorf("non-object params should be unchanged, got %s", result)
	}
}
