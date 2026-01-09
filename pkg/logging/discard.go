// Package logging provides shared logging utilities for agentlab.
package logging

import (
	"context"
	"log/slog"
)

// DiscardHandler is a slog.Handler that discards all log records.
// Use this to create a no-op logger: slog.New(logging.DiscardHandler{})
type DiscardHandler struct{}

func (DiscardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (DiscardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d DiscardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d DiscardHandler) WithGroup(string) slog.Handler           { return d }

// NewDiscardLogger returns a logger that discards all output.
// This is useful as a default logger when no logging is configured.
func NewDiscardLogger() *slog.Logger {
	return slog.New(DiscardHandler{})
}
