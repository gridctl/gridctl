package telemetry

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/metrics"
	"gopkg.in/natefinch/lumberjack.v2"
)

// DefaultMetricsFlushInterval is the period at which the metrics flusher
// snapshots the accumulator and appends a per-server diff line. 60s matches
// the in-memory bucket granularity (1-minute buckets in metrics.Accumulator).
const DefaultMetricsFlushInterval = 60 * time.Second

// MetricsSnapshotLine is the on-disk schema for one NDJSON entry in
// metrics.jsonl. Time, Server, and Diff are populated for every line; Reset
// is true on the first line written after a counter reset (server restart,
// Accumulator.Clear) and the diff for that line is the *full* snapshot, not
// a negative delta.
type MetricsSnapshotLine struct {
	Time   time.Time            `json:"ts"`
	Server string               `json:"server"`
	Reset  bool                 `json:"reset,omitempty"`
	Diff   metrics.TokenCounts  `json:"diff"`
	Total  metrics.TokenCounts  `json:"total"`
}

// MetricsFlusher periodically serializes per-server token counters from a
// metrics.Accumulator and appends one NDJSON line per server with non-zero
// deltas. Single goroutine; one-shot Start/Stop pair (re-Starting after Stop
// is a no-op). Failed writes are logged via the self logger and do not crash
// the goroutine.
type MetricsFlusher struct {
	acc      *metrics.Accumulator
	interval time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	writers map[string]*lumberjack.Logger  // serverName -> writer
	prev    map[string]metrics.TokenCounts // serverName -> last snapshot

	stop     chan struct{}
	done     chan struct{}
	started  bool
	stopOnce sync.Once
}

// NewMetricsFlusher creates a flusher with the given accumulator and
// per-flush interval. interval <= 0 falls back to DefaultMetricsFlushInterval.
func NewMetricsFlusher(acc *metrics.Accumulator, interval time.Duration) *MetricsFlusher {
	if interval <= 0 {
		interval = DefaultMetricsFlushInterval
	}
	return &MetricsFlusher{
		acc:      acc,
		interval: interval,
		writers:  make(map[string]*lumberjack.Logger),
		prev:     make(map[string]metrics.TokenCounts),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// SetLogger configures where flush errors are logged. Pass a logger backed
// by the in-memory buffer so users see write failures in the UI.
func (f *MetricsFlusher) SetLogger(logger *slog.Logger) {
	if logger != nil {
		f.logger = logger.With("subsystem", "telemetry")
	}
}

// AddServer registers a per-server output file. Idempotent: re-adding a
// server replaces the prior writer (the lumberjack handle is closed). The
// previous-snapshot tracking is preserved so re-adding does not synthesize a
// reset.
func (f *MetricsFlusher) AddServer(name, path string, opts LogOpts) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if existing, ok := f.writers[name]; ok && existing != nil {
		_ = existing.Close()
	}

	if opts.MaxSizeMB <= 0 {
		opts.MaxSizeMB = 100
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 5
	}
	if opts.MaxAgeDays <= 0 {
		opts.MaxAgeDays = 7
	}

	lj := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    opts.MaxSizeMB,
		MaxBackups: opts.MaxBackups,
		MaxAge:     opts.MaxAgeDays,
		Compress:   true,
	}
	// Touch the file so it gets created with mode 0600 even if no flush
	// happens before the next AddServer / Close cycle. lumberjack itself
	// creates files on first write but with the umask applied — explicit
	// open guarantees 0600 to match vault/state convention.
	if err := touchMode0600(path); err != nil {
		return fmt.Errorf("telemetry metrics writer for %q: %w", name, err)
	}
	f.writers[name] = lj
	return nil
}

// RemoveServer stops persisting metrics for a server and closes its writer.
// The previous-snapshot tracking is dropped so re-adding produces a fresh
// reset line as the first entry.
func (f *MetricsFlusher) RemoveServer(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if existing, ok := f.writers[name]; ok && existing != nil {
		_ = existing.Close()
		delete(f.writers, name)
	}
	delete(f.prev, name)
}

// ConfiguredServers returns the names currently persisting metrics.
func (f *MetricsFlusher) ConfiguredServers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.writers))
	for n := range f.writers {
		names = append(names, n)
	}
	return names
}

// Start launches the flush goroutine. Safe to call once; subsequent calls
// are no-ops. The goroutine runs until Stop is called.
func (f *MetricsFlusher) Start() {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return
	}
	f.started = true
	f.mu.Unlock()

	go f.run()
}

// Stop signals the flush goroutine to exit and waits for it to drain — one
// final flush is performed before exit so the on-disk file reflects the
// last in-memory state. Safe to call multiple times concurrently; the
// stop-channel close is sync.Once-guarded so racing Stop() calls don't
// panic with a "close of closed channel".
func (f *MetricsFlusher) Stop() {
	f.mu.Lock()
	started := f.started
	f.mu.Unlock()
	if !started {
		return
	}

	f.stopOnce.Do(func() { close(f.stop) })
	<-f.done

	// Close all per-server writers after the final flush.
	f.mu.Lock()
	for _, lj := range f.writers {
		if lj != nil {
			_ = lj.Close()
		}
	}
	f.mu.Unlock()
}

// run is the flush goroutine.
func (f *MetricsFlusher) run() {
	defer close(f.done)
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-f.stop:
			f.flushOnce(time.Now())
			return
		case t := <-ticker.C:
			f.flushOnce(t)
		}
	}
}

// flushOnce snapshots the accumulator and writes one NDJSON line per
// configured server with a non-zero delta vs the previous snapshot. The
// per-server writer map is snapshotted under the mutex; disk I/O happens
// outside the lock so a slow writer can't block AddServer/RemoveServer.
func (f *MetricsFlusher) flushOnce(now time.Time) {
	if f.acc == nil {
		return
	}
	snap := f.acc.Snapshot()

	type planned struct {
		writer *lumberjack.Logger
		line   MetricsSnapshotLine
	}
	var plan []planned

	f.mu.Lock()
	for name, writer := range f.writers {
		current, ok := snap.PerServer[name]
		if !ok {
			continue
		}
		prev, hadPrev := f.prev[name]
		line := MetricsSnapshotLine{
			Time:   now.UTC(),
			Server: name,
			Total:  current,
		}
		switch {
		case !hadPrev:
			line.Reset = true
			line.Diff = current
		case isCounterReset(prev, current):
			line.Reset = true
			line.Diff = current
		default:
			line.Diff = metrics.TokenCounts{
				InputTokens:  current.InputTokens - prev.InputTokens,
				OutputTokens: current.OutputTokens - prev.OutputTokens,
				TotalTokens:  current.TotalTokens - prev.TotalTokens,
			}
			if line.Diff.InputTokens == 0 && line.Diff.OutputTokens == 0 && line.Diff.TotalTokens == 0 {
				continue
			}
		}
		plan = append(plan, planned{writer: writer, line: line})
		// Update prev under the lock — even if the write fails the in-memory
		// state advances; lumberjack rotates rather than retaining failed
		// writes, so retry would emit the same delta on the next tick anyway.
		f.prev[name] = current
	}
	f.mu.Unlock()

	for _, p := range plan {
		if p.line.Reset {
			// Reset sentinel is itself valid NDJSON so strict line-by-line
			// JSON parsers (e.g. otelcol filelog receiver) don't choke.
			data, err := json.Marshal(struct {
				Reset  bool      `json:"reset"`
				Time   time.Time `json:"ts"`
				Server string    `json:"server"`
			}{Reset: true, Time: p.line.Time, Server: p.line.Server})
			if err == nil {
				data = append(data, '\n')
				if _, werr := p.writer.Write(data); werr != nil && f.logger != nil {
					f.logger.Warn("telemetry metrics reset marker write failed", "server", p.line.Server, "error", werr)
				}
			}
		}

		data, err := json.Marshal(p.line)
		if err != nil {
			if f.logger != nil {
				f.logger.Warn("telemetry metrics marshal failed", "server", p.line.Server, "error", err)
			}
			continue
		}
		data = append(data, '\n')
		if _, err := p.writer.Write(data); err != nil && f.logger != nil {
			f.logger.Warn("telemetry metrics write failed", "server", p.line.Server, "error", err)
		}
	}
}

// isCounterReset returns true when any counter in current is strictly less
// than its corresponding value in prev — a hard signal that the counter
// space restarted.
func isCounterReset(prev, current metrics.TokenCounts) bool {
	return current.InputTokens < prev.InputTokens ||
		current.OutputTokens < prev.OutputTokens ||
		current.TotalTokens < prev.TotalTokens
}

// touchMode0600 ensures the file exists with mode 0600. lumberjack would
// otherwise apply the process umask on first write; an explicit open
// guarantees the vault/state convention. POSIX append semantics let
// lumberjack continue using the same path independently.
func touchMode0600(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}
