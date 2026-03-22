package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/gridctl/gridctl/pkg/logging"
)

// Provider wraps the OTel TracerProvider and the in-memory trace buffer.
// Call Init to configure and register the global OTel provider.
// Call Shutdown to flush and shut down cleanly.
type Provider struct {
	cfg      *Config
	logger   *slog.Logger
	provider *sdktrace.TracerProvider
	Buffer   *Buffer
}

// NewProvider creates a Provider from config. Call Init to activate it.
func NewProvider(cfg *Config) *Provider {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Provider{
		cfg:    cfg,
		logger: logging.NewDiscardLogger(),
		Buffer: NewBuffer(bufferSize(cfg), cfg.RetentionDuration()),
	}
}

// SetLogger sets the logger for tracing startup messages.
func (p *Provider) SetLogger(logger *slog.Logger) {
	if logger != nil {
		p.logger = logging.WithComponent(logger, "tracing")
	}
}

// Init initialises the OTel SDK, registers the global TracerProvider and
// propagator, and sets up the in-memory buffer (+ optional OTLP exporter).
func (p *Provider) Init(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	var processors []sdktrace.SpanProcessor

	// Always add the in-memory buffer exporter.
	processors = append(processors,
		sdktrace.NewSimpleSpanProcessor(p.Buffer),
	)

	// Optional OTLP exporter.
	if p.cfg.Export == "otlp" && p.cfg.Endpoint != "" {
		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(p.cfg.Endpoint),
			otlptracehttp.WithInsecure(),
			otlptracehttp.WithTimeout(5*time.Second),
		)
		if err != nil {
			p.logger.Warn("failed to create OTLP exporter, continuing with in-memory only",
				"endpoint", p.cfg.Endpoint, "error", err)
		} else {
			processors = append(processors,
				sdktrace.NewBatchSpanProcessor(exp),
			)
			p.logger.Info("OTLP trace exporter configured", "endpoint", p.cfg.Endpoint)
		}
	}

	sampler := sdktrace.AlwaysSample()
	if p.cfg.Sampling < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(p.cfg.Sampling)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
	)
	for _, sp := range processors {
		tp.RegisterSpanProcessor(sp)
	}

	p.provider = tp

	// Register as global provider and propagator.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	p.logger.Info("distributed tracing initialised",
		"sampling", p.cfg.Sampling,
		"retention", p.cfg.Retention,
		"export", p.cfg.Export,
		"max_traces", bufferSize(p.cfg),
	)
	return nil
}

// Shutdown flushes pending spans and shuts down the TracerProvider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	if err := p.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("tracing: shutdown: %w", err)
	}
	return nil
}

// bufferSize returns the effective ring buffer capacity from config.
func bufferSize(cfg *Config) int {
	if cfg.MaxTraces > 0 {
		return cfg.MaxTraces
	}
	return 1000
}
