package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

func TestProviderInit_defaultConfig(t *testing.T) {
	p := NewProvider(nil)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()

	// After init, the global TracerProvider should produce valid spans.
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test.span")
	span.End()
}

func TestProviderInit_disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	p := NewProvider(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init (disabled) returned error: %v", err)
	}
	if p.provider != nil {
		t.Error("provider should be nil when tracing is disabled")
	}
}

func TestProviderShutdown_noop(t *testing.T) {
	// Shutdown without Init should not panic.
	p := NewProvider(nil)
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown without Init returned error: %v", err)
	}
}

func TestProviderBuffer_populated(t *testing.T) {
	p := NewProvider(nil)
	if p.Buffer == nil {
		t.Fatal("Buffer should be non-nil after NewProvider")
	}
	if p.Buffer.Count() != 0 {
		t.Errorf("Buffer.Count() = %d, want 0", p.Buffer.Count())
	}
}
