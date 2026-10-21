package controller

import (
	"log/slog"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/tracing"
)

func TestBuildMetricsConfig(t *testing.T) {
	if buildMetricsConfig(nil) != nil {
		t.Fatal("nil gateway returned a config")
	}
	if buildMetricsConfig(&config.GatewayConfig{}) != nil {
		t.Fatal("omitted metrics block returned a config")
	}
	if buildMetricsConfig(&config.GatewayConfig{Metrics: &config.MetricsConfig{}}) != nil {
		t.Fatal("omitted otlp block returned a config")
	}

	cfg := buildMetricsConfig(&config.GatewayConfig{Metrics: &config.MetricsConfig{OTLP: &config.OTLPMetricsConfig{
		Endpoint: "http://localhost:4318",
	}}})
	if cfg == nil || cfg.Endpoint != "http://localhost:4318" || cfg.Interval != tracing.DefaultMetricsInterval || cfg.Timeout != tracing.DefaultMetricsTimeout {
		t.Fatalf("defaults = %+v", cfg)
	}

	cfg = buildMetricsConfig(&config.GatewayConfig{Metrics: &config.MetricsConfig{OTLP: &config.OTLPMetricsConfig{
		Endpoint: "http://localhost:4318",
		Interval: "15s",
		Timeout:  "2s",
		Headers:  map[string]string{"X-Team": "v"},
	}}})
	if cfg.Interval != 15*time.Second || cfg.Timeout != 2*time.Second || cfg.Headers["X-Team"] != "v" {
		t.Fatalf("parsed = %+v", cfg)
	}

	cfg = buildMetricsConfig(&config.GatewayConfig{Metrics: &config.MetricsConfig{OTLP: &config.OTLPMetricsConfig{
		Endpoint: "http://localhost:4318",
		Interval: "nope",
		Timeout:  "also-nope",
	}}})
	if cfg.Interval != tracing.DefaultMetricsInterval || cfg.Timeout != tracing.DefaultMetricsTimeout {
		t.Fatalf("invalid duration replaced defaults: %+v", cfg)
	}
}

func TestRunsDropCallback_NilOrDisabled(t *testing.T) {
	if runsDropCallback(nil) != nil {
		t.Fatal("nil recorder should omit the drops callback")
	}
	rec, err := runs.NewRecorder(runs.Config{Enabled: false}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rec.Close)
	cb := runsDropCallback(rec)
	if cb == nil {
		t.Fatal("disabled recorder should still supply a callback")
	}
	if drops := cb(); drops != nil {
		t.Fatalf("disabled drops = %v, want nil", drops)
	}
}
