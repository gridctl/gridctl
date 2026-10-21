package tracing

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ExportErrorHandler logs exporter failures at warn at most once per minute
// per distinct message and counts them per signal. Installed via
// otel.SetErrorHandler in Provider.Init.
type ExportErrorHandler struct {
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time
	record   func(signal string)

	mu       sync.Mutex
	last     map[string]time.Time
	failures map[string]uint64
	hosts    map[string]string
}

// NewExportErrorHandler returns a handler that rate-limits identical messages
// to once per minute.
func NewExportErrorHandler(logger *slog.Logger) *ExportErrorHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExportErrorHandler{
		logger:   logger,
		interval: time.Minute,
		now:      time.Now,
		last:     map[string]time.Time{},
		failures: map[string]uint64{},
		hosts:    map[string]string{},
	}
}

// SetRecorder installs the hook that increments gridctl.otlp.export.failures.
// Nil disables the metric side; the in-process counter still increments.
func (h *ExportErrorHandler) SetRecorder(fn func(signal string)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.record = fn
	h.mu.Unlock()
}

// SetHost records the endpoint host logged with failures for a signal.
// Header values are never stored here.
func (h *ExportErrorHandler) SetHost(signal, host string) {
	if h == nil || signal == "" {
		return
	}
	h.mu.Lock()
	h.hosts[signal] = host
	h.mu.Unlock()
}

// Handle implements otel.ErrorHandler. SDK errors that do not name a metric
// export are counted as traces.
func (h *ExportErrorHandler) Handle(err error) {
	if h == nil || err == nil {
		return
	}
	h.Note(classifyExportSignal(err), err)
}

// Note records one export failure for signal, logs at warn when the message
// has not been logged in the last minute, and increments the in-process counter.
func (h *ExportErrorHandler) Note(signal string, err error) {
	if h == nil || err == nil {
		return
	}
	if signal == "" {
		signal = SignalTraces
	}
	msg := err.Error()
	now := h.now()

	h.mu.Lock()
	h.failures[signal]++
	host := h.hosts[signal]
	record := h.record
	last, seen := h.last[msg]
	logNow := !seen || now.Sub(last) >= h.interval
	if logNow {
		h.last[msg] = now
	}
	h.mu.Unlock()

	if record != nil {
		record(signal)
	}
	if logNow {
		h.logger.Warn("otlp export failed", "signal", signal, "endpoint_host", host, "error", err)
	}
}

// Failures returns the in-process count for signal.
func (h *ExportErrorHandler) Failures(signal string) uint64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures[signal]
}

func classifyExportSignal(err error) string {
	if strings.Contains(strings.ToLower(err.Error()), "metric") {
		return SignalMetrics
	}
	return SignalTraces
}
