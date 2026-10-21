// Package tracing provides distributed tracing for the gridctl MCP gateway.
// It initializes the OpenTelemetry SDK, propagates W3C trace context, stores
// completed traces in an in-memory ring buffer, and optionally exports traces
// and metrics to OTLP.
package tracing

import "time"

// Config holds tracing configuration for the gateway.
type Config struct {
	// Enabled controls whether tracing is active. Default: true.
	Enabled bool `yaml:"enabled"`

	// Sampling is the head-based sampling rate [0.0, 1.0]. Default: 1.0 (sample all).
	Sampling float64 `yaml:"sampling"`

	// Retention is how long completed traces are kept in the in-memory buffer.
	// Parsed as a Go duration string (e.g. "24h", "1h"). Default: "24h".
	Retention string `yaml:"retention"`

	// Export selects an exporter: "otlp" or "" (none). Default: "".
	Export string `yaml:"export,omitempty"`

	// Endpoint is the OTLP endpoint URL (e.g. "http://localhost:4318").
	// Required when Export is "otlp". A pathless URL is posted to /v1/traces.
	Endpoint string `yaml:"endpoint,omitempty"`

	// Headers are sent on the OTLP trace export request. Values are redacted
	// in logs. Names only are logged at startup.
	Headers map[string]string `yaml:"headers,omitempty"`

	// MaxTraces is the ring buffer capacity. Default: 1000.
	MaxTraces int `yaml:"max_traces,omitempty"`

	// IncludeInfra admits spans from non-gridctl instrumentation scopes
	// (e.g. Docker SDK HTTP self-instrumentation) into the in-memory buffer.
	// The OTLP export path is unaffected. Default: false.
	IncludeInfra bool `yaml:"include_infra,omitempty"`

	// ServiceName is the resource service.name. Empty falls back to gridctl-gateway.
	ServiceName string `yaml:"-"`

	// ServiceVersion is the resource service.version.
	ServiceVersion string `yaml:"-"`

	// Metrics configures the optional OTLP metrics exporter. Nil disables it.
	Metrics *MetricsConfig `yaml:"-"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:   true,
		Sampling:  1.0,
		Retention: "24h",
		MaxTraces: 1000,
	}
}

// RetentionDuration parses Retention as a time.Duration, returning the default (24h) on error.
func (c *Config) RetentionDuration() time.Duration {
	if c.Retention == "" {
		return 24 * time.Hour
	}
	d, err := time.ParseDuration(c.Retention)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}
