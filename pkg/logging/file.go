package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// FileOpts configures log file rotation for a file-backed log handler.
type FileOpts struct {
	// MaxSizeMB is the maximum size of the log file in megabytes before rotation (default: 100).
	MaxSizeMB int
	// MaxAgeDays is the maximum number of days to retain old log files (default: 7).
	MaxAgeDays int
	// MaxBackups is the maximum number of compressed old log files to keep (default: 3).
	MaxBackups int
}

// NewFileHandler creates a slog.Handler that writes JSON-formatted log entries to a
// lumberjack-backed rotating log file. Returns an error if the file cannot be opened
// or the directory does not exist.
func NewFileHandler(path string, opts FileOpts) (slog.Handler, error) {
	// Validate that the parent directory exists before handing off to lumberjack.
	dir := dirOf(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, fmt.Errorf(
			"log file directory %q does not exist (create it with: mkdir -p %s)",
			dir, dir,
		)
	}

	// Apply defaults.
	if opts.MaxSizeMB <= 0 {
		opts.MaxSizeMB = 100
	}
	if opts.MaxAgeDays <= 0 {
		opts.MaxAgeDays = 7
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 3
	}

	lj := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    opts.MaxSizeMB,
		MaxAge:     opts.MaxAgeDays,
		MaxBackups: opts.MaxBackups,
		Compress:   true,
	}

	// Verify write access by attempting to open (or create) the file.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf(
			"cannot open log file %q: %w\n  Tip: check that the directory exists and you have write permission",
			path, err,
		)
	}
	f.Close()

	return slog.NewJSONHandler(lj, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					return slog.String("ts", t.Format(time.RFC3339Nano))
				}
			}
			if a.Key == slog.MessageKey {
				a.Key = "msg"
			}
			return a
		},
	}), nil
}

// dirOf returns the directory component of path, defaulting to "." for bare filenames.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}

// multiHandler fans out slog records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler returns a slog.Handler that forwards each record to all provided handlers.
func NewMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: hs}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: hs}
}
