// Package logging provides shared logging utilities for gridctl.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"
)

// LogFormat specifies the output format for structured logging.
type LogFormat string

const (
	// FormatJSON outputs logs as JSON objects (machine-readable).
	FormatJSON LogFormat = "json"
	// FormatText outputs logs as human-readable text with colors.
	FormatText LogFormat = "text"
)

// Config holds configuration for structured logging.
type Config struct {
	// Level sets the minimum log level (default: INFO).
	Level slog.Level
	// Format sets the output format (default: JSON).
	Format LogFormat
	// Output sets the writer for log output (default: os.Stderr).
	Output io.Writer
	// AddSource adds source file and line information to logs.
	AddSource bool
	// Component identifies the logging component (e.g., "gateway", "orchestrator").
	Component string
}

// DefaultConfig returns a default logging configuration.
func DefaultConfig() Config {
	return Config{
		Level:     slog.LevelInfo,
		Format:    FormatJSON,
		Output:    os.Stderr,
		AddSource: false,
	}
}

// NewStructuredLogger creates a new structured logger with the given configuration.
func NewStructuredLogger(cfg Config) *slog.Logger {
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}

	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.AddSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize time format for readability
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String("ts", t.Format(time.RFC3339Nano))
				}
			}
			// Rename message key to "msg" for consistency
			if a.Key == slog.MessageKey {
				a.Key = "msg"
			}
			return a
		},
	}

	var handler slog.Handler
	switch cfg.Format {
	case FormatText:
		handler = slog.NewTextHandler(cfg.Output, opts)
	default:
		handler = slog.NewJSONHandler(cfg.Output, opts)
	}

	// Wrap with component logger if component is specified
	if cfg.Component != "" {
		handler = &componentHandler{
			Handler:   handler,
			component: cfg.Component,
		}
	}

	return slog.New(handler)
}

// componentHandler wraps a handler to add component field to all records.
type componentHandler struct {
	slog.Handler
	component string
}

func (h *componentHandler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("component", h.component))
	return h.Handler.Handle(ctx, r)
}

func (h *componentHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &componentHandler{
		Handler:   h.Handler.WithAttrs(attrs),
		component: h.component,
	}
}

func (h *componentHandler) WithGroup(name string) slog.Handler {
	return &componentHandler{
		Handler:   h.Handler.WithGroup(name),
		component: h.component,
	}
}

// WithTraceID returns a new logger with the given trace ID.
func WithTraceID(logger *slog.Logger, traceID string) *slog.Logger {
	return logger.With(slog.String("trace_id", traceID))
}

// WithComponent returns a new logger with the given component name.
func WithComponent(logger *slog.Logger, component string) *slog.Logger {
	return logger.With(slog.String("component", component))
}

// LogEntry represents a structured log entry for frontend consumption.
// This is the schema that the frontend expects when parsing logs.
type LogEntry struct {
	Level     string            `json:"level"`
	Timestamp string            `json:"ts"`
	Message   string            `json:"msg"`
	Component string            `json:"component,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
	Logger    string            `json:"logger,omitempty"`
	Meta      map[string]any    `json:"meta,omitempty"`
}

// ParseLevel converts a string level to slog.Level.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseFormat converts a string format to LogFormat.
func ParseFormat(format string) LogFormat {
	switch strings.ToLower(format) {
	case "text", "pretty":
		return FormatText
	case "json":
		return FormatJSON
	default:
		return FormatJSON
	}
}

// Caller returns the file and line of the caller at the given depth.
// Useful for adding source information to logs.
func Caller(depth int) (file string, line int) {
	_, file, line, ok := runtime.Caller(depth + 1)
	if !ok {
		return "unknown", 0
	}
	// Extract just the filename from the path
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		file = file[idx+1:]
	}
	return file, line
}
