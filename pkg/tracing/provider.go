package tracing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/gridctl/gridctl/pkg/logging"
)

// Provider wraps the OTel TracerProvider, the optional MeterProvider, and the
// in-memory trace buffer. Call Init to configure and register the global
// providers. Call Shutdown to flush and shut down cleanly.
type Provider struct {
	cfg      *Config
	logger   *slog.Logger
	provider *sdktrace.TracerProvider
	meter    *sdkmetric.MeterProvider
	errors   *ExportErrorHandler
	identity ResourceIdentity
	Buffer   *Buffer
}

// NewProvider creates a Provider from config. Call Init to activate it.
func NewProvider(cfg *Config) *Provider {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	buf := NewBuffer(bufferSize(cfg), cfg.RetentionDuration())
	buf.SetIncludeInfra(cfg.IncludeInfra)
	return &Provider{
		cfg:      cfg,
		logger:   logging.NewDiscardLogger(),
		identity: NewResourceIdentity(cfg.ServiceName, cfg.ServiceVersion, ""),
		Buffer:   buf,
	}
}

// SetLogger sets the logger for tracing startup messages.
func (p *Provider) SetLogger(logger *slog.Logger) {
	if logger != nil {
		p.logger = logging.WithComponent(logger, "tracing")
	}
}

// SetResourceIdentity overrides the service name and version used for the
// shared resource. Empty name keeps the gridctl-gateway fallback. The
// instance id generated at construction is preserved.
func (p *Provider) SetResourceIdentity(serviceName, version string) {
	if p == nil {
		return
	}
	p.identity = NewResourceIdentity(serviceName, version, p.identity.InstanceID)
	if p.cfg != nil {
		p.cfg.ServiceName = p.identity.ServiceName
		p.cfg.ServiceVersion = version
	}
}

// Identity returns the resource identity. Valid after NewProvider.
func (p *Provider) Identity() ResourceIdentity {
	if p == nil {
		return ResourceIdentity{}
	}
	return p.identity
}

// ErrorHandler returns the export error handler, or nil before Init.
func (p *Provider) ErrorHandler() *ExportErrorHandler {
	if p == nil {
		return nil
	}
	return p.errors
}

// Init initialises the OTel SDK, registers the global TracerProvider,
// MeterProvider, propagator, and error handler, and sets up the in-memory
// buffer plus optional OTLP exporters.
func (p *Provider) Init(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p.errors = NewExportErrorHandler(p.logger)
	otel.SetErrorHandler(p.errors)

	res := BuildResource(p.identity)
	p.logger.Info("opentelemetry resource",
		"service.name", p.identity.ServiceName,
		"service.version", p.identity.Version,
		"service.instance.id", p.identity.InstanceID,
	)

	if p.cfg.Metrics != nil && p.cfg.Metrics.Endpoint != "" {
		mp, err := p.startMetrics(ctx, res)
		if err != nil {
			p.logger.Warn("failed to create OTLP metrics exporter, continuing without metrics export",
				"endpoint_host", EndpointHost(p.cfg.Metrics.Endpoint), "error", err)
		} else {
			p.meter = mp
		}
	}

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
		endpoint, err := ResolveOTLPEndpoint(p.cfg.Endpoint, SignalPathTraces)
		if err != nil {
			p.logger.Warn("failed to resolve OTLP trace endpoint, continuing with in-memory only",
				"endpoint_host", EndpointHost(p.cfg.Endpoint), "error", err)
		} else {
			opts := []otlptracehttp.Option{
				otlptracehttp.WithEndpointURL(endpoint),
				otlptracehttp.WithTimeout(5 * time.Second),
			}
			if len(p.cfg.Headers) > 0 {
				opts = append(opts, otlptracehttp.WithHeaders(p.cfg.Headers))
			}
			exp, expErr := otlptracehttp.New(ctx, opts...)
			if expErr != nil {
				p.logger.Warn("failed to create OTLP exporter, continuing with in-memory only",
					"endpoint_host", EndpointHost(endpoint), "error", expErr)
			} else {
				processors = append(processors,
					sdktrace.NewBatchSpanProcessor(classifyingSpanExporter{inner: exp}),
				)
				p.errors.SetHost(SignalTraces, EndpointHost(endpoint))
				p.logger.Info("OTLP trace exporter configured",
					"endpoint_host", EndpointHost(endpoint),
					"header_names", headerNames(p.cfg.Headers),
				)
			}
		}
	}

	sampler := sdktrace.AlwaysSample()
	if p.cfg.Sampling < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(p.cfg.Sampling)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
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

// Shutdown flushes pending spans and metrics, then shuts the providers down.
// Cancellation of ctx is honored. ForceFlush runs before Shutdown for each
// signal that was started.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var errs []error
	if p.provider != nil {
		if err := p.provider.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracing: flush: %w", err))
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
		} else if err := p.provider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tracing: shutdown: %w", err))
		}
	}
	if p.meter != nil {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
		} else if err := p.meter.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("metrics: flush: %w", err))
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
		} else if err := p.meter.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("metrics: shutdown: %w", err))
		}
	}
	return errors.Join(errs...)
}

// RegisterExporter attaches an additional SpanExporter to the live
// TracerProvider via a BatchSpanProcessor. Used by the telemetry persistence
// layer to plug a per-server file exporter alongside the in-memory buffer
// and the optional OTLP exporter without rebuilding the provider. No-op
// when tracing is disabled or Init has not yet run.
func (p *Provider) RegisterExporter(exp sdktrace.SpanExporter) {
	if p == nil || p.provider == nil || exp == nil {
		return
	}
	p.provider.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(exp))
}

// classifyingSpanExporter prefixes export errors so the global handler
// counts them against the traces signal. The metrics exporter uses the
// same pattern. A URL that contains the word metric is not a signal.
type classifyingSpanExporter struct {
	inner sdktrace.SpanExporter
}

func (e classifyingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := e.inner.ExportSpans(ctx, spans); err != nil {
		return fmt.Errorf("trace export: %w", err)
	}
	return nil
}

func (e classifyingSpanExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}

// bufferSize returns the effective ring buffer capacity from config.
func bufferSize(cfg *Config) int {
	if cfg.MaxTraces > 0 {
		return cfg.MaxTraces
	}
	return 1000
}
