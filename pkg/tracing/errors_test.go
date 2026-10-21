package tracing

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestExportErrorHandler_RateLimitAndCounter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := NewExportErrorHandler(logger)
	now := time.Now()
	h.now = func() time.Time { return now }
	h.SetHost(SignalTraces, "collector.example:4318")
	var recorded []string
	h.SetRecorder(func(signal string) { recorded = append(recorded, signal) })

	err := errors.New("trace export connection refused")
	h.Handle(err)
	h.Handle(err)
	if h.Failures(SignalTraces) != 2 {
		t.Fatalf("failures = %d, want 2", h.Failures(SignalTraces))
	}
	if len(recorded) != 2 || recorded[0] != SignalTraces {
		t.Fatalf("recorder = %v", recorded)
	}
	if strings.Count(buf.String(), "otlp export failed") != 1 {
		t.Fatalf("log count, want 1:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "collector.example:4318") {
		t.Fatalf("log missing endpoint host:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "secret") {
		t.Fatal("log must not carry header values")
	}

	now = now.Add(time.Minute)
	h.Handle(err)
	if strings.Count(buf.String(), "otlp export failed") != 2 {
		t.Fatalf("expected a second log after the window:\n%s", buf.String())
	}

	h.Note(SignalMetrics, errors.New("metric export timeout"))
	if h.Failures(SignalMetrics) != 1 {
		t.Fatalf("metric failures = %d", h.Failures(SignalMetrics))
	}
}

func TestExportErrorHandler_ClassifiesMetric(t *testing.T) {
	h := NewExportErrorHandler(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	h.Handle(errors.New("metric export: connection refused"))
	if h.Failures(SignalMetrics) != 1 || h.Failures(SignalTraces) != 0 {
		t.Fatalf("traces=%d metrics=%d", h.Failures(SignalTraces), h.Failures(SignalMetrics))
	}
}
