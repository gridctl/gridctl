package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
)

// MetricsConfig is the resolved OTLP metrics export block. HTTP/protobuf only.
type MetricsConfig struct {
	Endpoint string
	Headers  map[string]string
	Interval time.Duration
	Timeout  time.Duration
}

// DefaultMetricsInterval is the periodic reader interval when unset.
const DefaultMetricsInterval = 60 * time.Second

// DefaultMetricsTimeout is the exporter timeout when unset.
const DefaultMetricsTimeout = 10 * time.Second

func (c MetricsConfig) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return DefaultMetricsInterval
}

func (c MetricsConfig) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultMetricsTimeout
}

// startMetrics builds the meter provider and registers it globally. A
// construction failure is logged by the caller; export is skipped.
func (p *Provider) startMetrics(ctx context.Context, res *sdkresource.Resource) (*sdkmetric.MeterProvider, error) {
	cfg := p.cfg.Metrics
	if cfg == nil || cfg.Endpoint == "" {
		return nil, nil
	}
	endpoint, err := ResolveOTLPEndpoint(cfg.Endpoint, SignalPathMetrics)
	if err != nil {
		return nil, fmt.Errorf("metrics endpoint: %w", err)
	}
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithTimeout(cfg.timeout()),
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
	}
	exp, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	reader := sdkmetric.NewPeriodicReader(
		classifyingExporter{inner: exp},
		sdkmetric.WithInterval(cfg.interval()),
	)
	providerOpts := []sdkmetric.Option{
		sdkmetric.WithReader(reader),
		sdkmetric.WithView(durationView(InstrumentServerDuration)),
		sdkmetric.WithView(durationView(InstrumentClientDuration)),
	}
	if res != nil {
		providerOpts = append(providerOpts, sdkmetric.WithResource(res))
	}
	mp := sdkmetric.NewMeterProvider(providerOpts...)
	otel.SetMeterProvider(mp)
	if p.errors != nil {
		p.errors.SetHost(SignalMetrics, EndpointHost(endpoint))
	}
	p.logger.Info("OTLP metrics exporter configured",
		"endpoint_host", EndpointHost(endpoint),
		"interval", cfg.interval().String(),
		"header_names", headerNames(cfg.Headers),
	)
	return mp, nil
}

func durationView(name string) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: name},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
			Boundaries: DurationHistogramBuckets,
		}},
	)
}

func headerNames(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	return names
}

// classifyingExporter prefixes export errors so the global handler counts
// them against the metrics signal. The periodic reader reports the returned
// error through otel.Handle; this wrapper does not count on its own.
type classifyingExporter struct {
	inner sdkmetric.Exporter
}

func (e classifyingExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return e.inner.Temporality(k)
}

func (e classifyingExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return e.inner.Aggregation(k)
}

func (e classifyingExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if err := e.inner.Export(ctx, rm); err != nil {
		return fmt.Errorf("metric export: %w", err)
	}
	return nil
}

func (e classifyingExporter) ForceFlush(ctx context.Context) error {
	if err := e.inner.ForceFlush(ctx); err != nil {
		return fmt.Errorf("metric export: %w", err)
	}
	return nil
}

func (e classifyingExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}
