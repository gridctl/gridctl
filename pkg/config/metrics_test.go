package config

import (
	"strings"
	"testing"
)

func TestValidate_MetricsOTLP(t *testing.T) {
	base := func() *Stack {
		return &Stack{Name: "demo", Network: Network{Name: "demo-net"}, Gateway: &GatewayConfig{Metrics: &MetricsConfig{OTLP: &OTLPMetricsConfig{}}}}
	}
	cases := []struct {
		name    string
		mutate  func(*OTLPMetricsConfig)
		field   string
		wantErr bool
	}{
		{"missing endpoint", func(c *OTLPMetricsConfig) {}, "gateway.metrics.otlp.endpoint", true},
		{"bad interval", func(c *OTLPMetricsConfig) {
			c.Endpoint = "http://localhost:4318"
			c.Interval = "soon"
		}, "gateway.metrics.otlp.interval", true},
		{"interval under one second", func(c *OTLPMetricsConfig) {
			c.Endpoint = "http://localhost:4318"
			c.Interval = "500ms"
		}, "gateway.metrics.otlp.interval", true},
		{"ok", func(c *OTLPMetricsConfig) {
			c.Endpoint = "http://localhost:4318"
			c.Interval = "60s"
			c.Timeout = "10s"
			c.Headers = map[string]string{"X-Team": "${var:OTEL_HEADER}"}
		}, "", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			tt.mutate(s.Gateway.Metrics.OTLP)
			err := Validate(s)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.field) {
					t.Fatalf("Validate error = %v, want field %s", err, tt.field)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestExpand_TracingAndMetricsHeaders(t *testing.T) {
	s := &Stack{
		Name: "demo",
		Gateway: &GatewayConfig{
			Tracing: &TracingConfig{
				Endpoint: "${OTEL_ENDPOINT}",
				Headers:  map[string]string{"X-Team": "${var:OTEL_HEADER}"},
			},
			Metrics: &MetricsConfig{OTLP: &OTLPMetricsConfig{
				Endpoint: "http://localhost:4318",
				Headers:  map[string]string{"X-Metric": "${var:OTEL_HEADER}"},
			}},
		},
	}
	expandStackVars(s, func(name string) (string, bool) {
		if name == "OTEL_HEADER" {
			return "secret-value", true
		}
		return "", false
	})
	if s.Gateway.Tracing.Headers["X-Team"] != "secret-value" {
		t.Fatalf("tracing header = %q", s.Gateway.Tracing.Headers["X-Team"])
	}
	if s.Gateway.Metrics.OTLP.Headers["X-Metric"] != "secret-value" {
		t.Fatalf("metrics header = %q", s.Gateway.Metrics.OTLP.Headers["X-Metric"])
	}
	var fields []string
	for _, c := range s.References["OTEL_HEADER"] {
		fields = append(fields, c.Field)
	}
	joined := strings.Join(fields, ",")
	if !strings.Contains(joined, "tracing.headers.X-Team") || !strings.Contains(joined, "metrics.otlp.headers.X-Metric") {
		t.Fatalf("header consumers = %#v", s.References)
	}
}
