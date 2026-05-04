// Package telemetry implements opt-in disk persistence for the three signal
// types gridctl already captures in memory: logs, metrics, and traces. All
// writers are async with respect to the request path — a failed disk write
// logs an error and continues so a telemetry persistence fault never breaks
// an MCP call.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gridctl/gridctl/pkg/logging"
)

// LogRouter is a slog.Handler that fans every record out to the inner handler
// (the existing buffer/redact chain that powers the UI) AND, when a record's
// resolved `component` attribute matches a configured server, to a per-server
// lumberjack file. The component attribute is set today by every server-bound
// logger via `logger.With("component", serverName)` — slog stores that on the
// derived handler, not on the record, so this router tracks its own
// accumulated attrs across WithAttrs/WithGroup the way BufferHandler does.
//
// Records without a configured component pass through to the inner handler
// only.
type LogRouter struct {
	inner slog.Handler

	component string      // resolved from accumulated attrs (live across WithAttrs)
	attrs     []slog.Attr // handler-level attrs accumulated through .With()

	// servers is shared by every derived router (WithAttrs, WithGroup) — the
	// per-server writer set is logically a property of the root router, not
	// of any particular logger view.
	servers *serverWriterSet

	// selfLog is also shared so persistence errors get the same destination
	// regardless of which logger view emitted the record.
	selfLog **slog.Logger
}

type serverWriterSet struct {
	mu      sync.RWMutex
	writers map[string]*serverLogWriter
}

type serverLogWriter struct {
	handler slog.Handler
	// The underlying file fd lives inside logging.NewFileHandler's
	// lumberjack instance, which is package-private. lumberjack uses
	// POSIX append semantics so we don't need our own handle for
	// rotation/cleanup, and we explicitly don't close the writer on
	// RemoveServer (matches Pitfall #5 in the spec — lumberjack reopens
	// transparently). The file fd is released at process exit.
}

// LogOpts mirrors lumberjack rotation knobs and matches RetentionConfig in
// pkg/config. Zero values fall back to lumberjack defaults via
// logging.NewFileHandler.
type LogOpts struct {
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
}

// NewLogRouter wraps an existing slog.Handler chain. The router itself is a
// slog.Handler — install it in place of the existing chain to enable per-
// server fan-out. inner must not be nil; pass the existing redact/buffer
// chain that powers the in-memory ring buffer.
func NewLogRouter(inner slog.Handler) *LogRouter {
	var selfPtr *slog.Logger
	return &LogRouter{
		inner:   inner,
		servers: &serverWriterSet{writers: make(map[string]*serverLogWriter)},
		selfLog: &selfPtr,
	}
}

// SetSelfLogger configures where the router itself logs persistence errors.
// Pass a logger backed by the in-memory buffer so users see write failures
// surface in the UI. The setting propagates to all derived router views.
func (r *LogRouter) SetSelfLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	tagged := logger.With("subsystem", "telemetry")
	*r.selfLog = tagged
}

// AddServer registers a per-server file handler. Subsequent records with a
// resolved component=name will be appended to path. AddServer is idempotent:
// calling it twice for the same server replaces the previous handler (the
// underlying lumberjack file fd persists until process exit). A failure to
// open the file is returned to the caller; gateway_builder logs and
// proceeds.
func (r *LogRouter) AddServer(name, path string, opts LogOpts) error {
	r.servers.mu.Lock()
	defer r.servers.mu.Unlock()

	handler, err := newPerServerLogHandler(path, opts)
	if err != nil {
		return fmt.Errorf("telemetry log writer for %q: %w", name, err)
	}
	r.servers.writers[name] = &serverLogWriter{handler: handler}
	return nil
}

// RemoveServer stops persisting logs for a server. Safe to call for an
// unregistered server. The on-disk file fd is not closed here — lumberjack
// owns it, and POSIX append semantics make explicit close unnecessary.
func (r *LogRouter) RemoveServer(name string) {
	r.servers.mu.Lock()
	defer r.servers.mu.Unlock()
	delete(r.servers.writers, name)
}

// ConfiguredServers returns the names currently persisting logs.
func (r *LogRouter) ConfiguredServers() []string {
	r.servers.mu.RLock()
	defer r.servers.mu.RUnlock()
	names := make([]string, 0, len(r.servers.writers))
	for n := range r.servers.writers {
		names = append(names, n)
	}
	return names
}

// Close drops every per-server writer entry. The on-disk file fds remain
// open until process exit (lumberjack-owned). The router itself remains
// usable; records continue to flow to the inner handler.
func (r *LogRouter) Close() {
	r.servers.mu.Lock()
	defer r.servers.mu.Unlock()
	r.servers.writers = make(map[string]*serverLogWriter)
}

// Enabled implements slog.Handler. Returns true if either inner or any per-
// server handler is enabled at this level — keeps log emission cheap when
// everything is disabled.
func (r *LogRouter) Enabled(ctx context.Context, level slog.Level) bool {
	if r.inner != nil && r.inner.Enabled(ctx, level) {
		return true
	}
	r.servers.mu.RLock()
	defer r.servers.mu.RUnlock()
	for _, w := range r.servers.writers {
		if w.handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle implements slog.Handler. Always forwards to inner; additionally
// forwards to the per-server file handler when the resolved component (from
// either accumulated handler-level attrs or this record's own attrs) matches
// a configured server. Errors from the per-server write are reported via
// SelfLogger and swallowed.
func (r *LogRouter) Handle(ctx context.Context, record slog.Record) error {
	var innerErr error
	if r.inner != nil {
		innerErr = r.inner.Handle(ctx, record.Clone())
	}

	component := r.resolveComponent(record)
	if component == "" {
		return innerErr
	}

	r.servers.mu.RLock()
	w, ok := r.servers.writers[component]
	r.servers.mu.RUnlock()
	if !ok {
		return innerErr
	}

	// The per-server file handler is a stock JSON handler; it doesn't see
	// attrs that we accumulated in our own .With chain. Replay them onto a
	// cloned record so component/trace_id/etc make it to disk.
	enriched := record.Clone()
	if len(r.attrs) > 0 {
		enriched.AddAttrs(r.attrs...)
	}
	if err := w.handler.Handle(ctx, enriched); err != nil {
		if logger := *r.selfLog; logger != nil {
			logger.Warn("telemetry log write failed", "component", component, "error", err)
		}
	}
	return innerErr
}

// resolveComponent looks up `component` in accumulated handler-level attrs
// first (set via .With at construction time on the per-server logger), then
// falls back to the record's own attrs. The router-level value wins because
// .With is the canonical way every gridctl component tags itself.
//
// Records emitted by the router's own self logger are tagged `subsystem=
// telemetry`; routing those would feed write-failure warnings back into the
// failing server's file, so they are explicitly ignored here.
func (r *LogRouter) resolveComponent(record slog.Record) string {
	if r.isSelfLog(record) {
		return ""
	}
	if r.component != "" {
		return r.component
	}
	var component string
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = a.Value.String()
			return false
		}
		return true
	})
	return component
}

// isSelfLog returns true when this record was produced by the router's own
// self logger (any attr tier carries `subsystem=telemetry`). The check is
// O(handler-attrs + record-attrs) and runs on every Handle call, so it
// short-circuits on first match.
func (r *LogRouter) isSelfLog(record slog.Record) bool {
	for _, a := range r.attrs {
		if a.Key == "subsystem" && a.Value.String() == "telemetry" {
			return true
		}
	}
	var found bool
	record.Attrs(func(a slog.Attr) bool {
		if a.Key == "subsystem" && a.Value.String() == "telemetry" {
			found = true
			return false
		}
		return true
	})
	return found
}

// WithAttrs implements slog.Handler. Returns a derived router that carries
// the additional attrs alongside the inner-with-attrs handler. The shared
// per-server map is unchanged.
func (r *LogRouter) WithAttrs(attrs []slog.Attr) slog.Handler {
	derived := &LogRouter{
		inner:     r.inner.WithAttrs(attrs),
		component: r.component,
		attrs:     append(append([]slog.Attr(nil), r.attrs...), attrs...),
		servers:   r.servers,
		selfLog:   r.selfLog,
	}
	for _, a := range attrs {
		if a.Key == "component" {
			derived.component = a.Value.String()
		}
	}
	return derived
}

// WithGroup implements slog.Handler. Group nesting does not affect component
// resolution — component is conventionally a top-level attr.
func (r *LogRouter) WithGroup(name string) slog.Handler {
	return &LogRouter{
		inner:     r.inner.WithGroup(name),
		component: r.component,
		attrs:     r.attrs,
		servers:   r.servers,
		selfLog:   r.selfLog,
	}
}

// newPerServerLogHandler builds a JSON-formatted slog.Handler backed by a
// lumberjack rotator owned internally by logging.NewFileHandler. Reusing
// that helper preserves mode 0600 + slog field renaming (msg, ts) +
// parent-dir validation in one place.
func newPerServerLogHandler(path string, opts LogOpts) (slog.Handler, error) {
	if opts.MaxSizeMB <= 0 {
		opts.MaxSizeMB = 100
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 5
	}
	if opts.MaxAgeDays <= 0 {
		opts.MaxAgeDays = 7
	}

	handler, err := logging.NewFileHandler(path, logging.FileOpts{
		MaxSizeMB:  opts.MaxSizeMB,
		MaxBackups: opts.MaxBackups,
		MaxAgeDays: opts.MaxAgeDays,
	})
	if err != nil {
		return nil, err
	}
	return handler, nil
}
