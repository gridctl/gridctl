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

func TestProviderInit_otlpHTTP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Export = "otlp"
	cfg.Endpoint = "http://localhost:4318"
	p := NewProvider(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init with OTLP HTTP endpoint returned error: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()
	if p.provider == nil {
		t.Error("provider should be non-nil after Init with OTLP config")
	}
}

func TestProviderInit_otlpMissingEndpoint(t *testing.T) {
	// Export set to "otlp" but no endpoint — OTLP branch is skipped,
	// ring buffer still initialises normally.
	cfg := DefaultConfig()
	cfg.Export = "otlp"
	cfg.Endpoint = ""
	p := NewProvider(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init with missing endpoint returned error: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()
	if p.provider == nil {
		t.Error("provider should be non-nil even when OTLP endpoint is empty")
	}
}

func TestProviderShutdown_afterOTLPInit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Export = "otlp"
	cfg.Endpoint = "http://localhost:4318"
	p := NewProvider(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after OTLP init returned error: %v", err)
	}
}

func TestProviderInit_unreachableCollector(t *testing.T) {
	// The OTel SDK dials lazily — Init must succeed even when the collector
	// is not listening. Span export failures are handled at export time.
	cfg := DefaultConfig()
	cfg.Export = "otlp"
	cfg.Endpoint = "http://localhost:19999"
	p := NewProvider(cfg)
	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init with unreachable collector returned error: %v", err)
	}
	defer func() { _ = p.Shutdown(context.Background()) }()
	if p.provider == nil {
		t.Error("provider should be non-nil even with unreachable collector")
	}
}
