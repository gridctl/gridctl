package telemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
// is true on the first line written after a token counter reset (server
// restart, Accumulator.Clear) and the diff for that line is the *full*
// snapshot. CostReset signals an independent cost-side reset (e.g.
// Accumulator.ClearCost between flushes) — the two flags are independent
// so a cost-only clear does not invalidate the token diff on the same
// line. Cost fields are pointer + omitempty so token-only minutes emit
// lines byte-identical to the pre-cost-persistence schema.
type MetricsSnapshotLine struct {
	Time      time.Time                   `json:"ts"`
	Server    string                      `json:"server"`
	Reset     bool                        `json:"reset,omitempty"`
	CostReset bool                        `json:"cost_reset,omitempty"`
	Diff      metrics.TokenCounts         `json:"diff"`
	Total     metrics.TokenCounts         `json:"total"`
	CostDiff  *metrics.CostMicroUSDCounts `json:"cost_diff,omitempty"`
	CostTotal *metrics.CostMicroUSDCounts `json:"cost_total,omitempty"`
	// ToolUsage carries the server's *cumulative* per-tool call counters
	// (toolName -> calls + last-called) at flush time — the analogue of
	// Total for tools, not a per-minute diff. omitempty keeps token-only
	// minutes and legacy pre-tool-usage files byte-identical; SeedFromFile
	// takes the most recent non-nil ToolUsage per server (resetting on a
	// token Reset) to rehydrate Audit Mode's usage history across restarts.
	ToolUsage map[string]metrics.ToolStat `json:"tool_usage,omitempty"`
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

	mu        sync.Mutex
	writers   map[string]*lumberjack.Logger          // serverName -> writer
	prev      map[string]metrics.TokenCounts         // serverName -> last token snapshot
	prevCost  map[string]metrics.CostMicroUSDCounts  // serverName -> last cost snapshot (parallel to prev)
	prevTools map[string]map[string]metrics.ToolStat // serverName -> last per-tool snapshot (parallel to prev)

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
		acc:       acc,
		interval:  interval,
		writers:   make(map[string]*lumberjack.Logger),
		prev:      make(map[string]metrics.TokenCounts),
		prevCost:  make(map[string]metrics.CostMicroUSDCounts),
		prevTools: make(map[string]map[string]metrics.ToolStat),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
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
	delete(f.prevCost, name)
	delete(f.prevTools, name)
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
// configured server with a non-zero delta vs the previous snapshot. A
// "non-zero delta" means *either* the token diff or the cost diff is
// non-zero — a minute that records a priced fixture without token
// attribution still emits a line so its cost reaches disk. The
// per-server writer map is snapshotted under the mutex; disk I/O happens
// outside the lock so a slow writer can't block AddServer/RemoveServer.
func (f *MetricsFlusher) flushOnce(now time.Time) {
	if f.acc == nil {
		return
	}
	snap := f.acc.Snapshot()
	costSnap := f.acc.CostMicroSnapshot()
	toolSnap := f.acc.ToolUsageSnapshot()

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
		// Cost may legitimately be missing from the cost snapshot when no
		// priced call has hit the server yet — treat a missing entry as
		// the zero CostMicroUSDCounts so the diff math stays uniform.
		currentCost := costSnap[name]
		currentTools := toolSnap[name]
		prev, hadPrev := f.prev[name]
		prevCost, hadPrevCost := f.prevCost[name]
		// Per-tool usage has no diff/reset machinery — it persists as a
		// cumulative snapshot. toolChanged forces a line when call counts
		// advanced even if tokens and cost did not (a tool call need not
		// attribute tokens), mirroring how cost forces a line independently
		// of the token diff.
		toolChanged := toolUsageChanged(f.prevTools[name], currentTools)
		line := MetricsSnapshotLine{
			Time:   now.UTC(),
			Server: name,
			Total:  current,
		}
		// Reset detection runs independently per dimension. A token reset
		// (first flush, or strictly-decreasing component) writes the full
		// token snapshot in Diff and sets Reset=true. A cost reset
		// (ClearCost between flushes — only flagged when prior cost
		// existed) writes the full cost snapshot in CostDiff and sets
		// CostReset=true. Splitting the flags is what lets a cost-only
		// clear preserve the token Diff on the same line; conflating
		// them would silently drop the token activity for that minute on
		// SeedFromFile replay.
		tokenReset := !hadPrev || isCounterReset(prev, current)
		costReset := hadPrevCost && isCostCounterReset(prevCost, currentCost)

		var tokenDiff metrics.TokenCounts
		if tokenReset {
			line.Reset = true
			tokenDiff = current
		} else {
			tokenDiff = metrics.TokenCounts{
				InputTokens:  current.InputTokens - prev.InputTokens,
				OutputTokens: current.OutputTokens - prev.OutputTokens,
				TotalTokens:  current.TotalTokens - prev.TotalTokens,
			}
		}
		line.Diff = tokenDiff

		var costDiff metrics.CostMicroUSDCounts
		switch {
		case costReset:
			line.CostReset = true
			costDiff = currentCost
		case tokenReset:
			// Token reset implies a fresh-server boundary. Match the
			// existing token contract by carrying the full cost
			// snapshot in CostDiff so the post-restart cumulative
			// reconstruction reads the same way for both dimensions.
			costDiff = currentCost
		default:
			costDiff = metrics.CostMicroUSDCounts{
				InputMicroUSD:      currentCost.InputMicroUSD - prevCost.InputMicroUSD,
				OutputMicroUSD:     currentCost.OutputMicroUSD - prevCost.OutputMicroUSD,
				CacheReadMicroUSD:  currentCost.CacheReadMicroUSD - prevCost.CacheReadMicroUSD,
				CacheWriteMicroUSD: currentCost.CacheWriteMicroUSD - prevCost.CacheWriteMicroUSD,
			}
		}

		// Skip lines whose every dimension is empty: token diff zero AND
		// cost diff zero AND neither dimension reset. A reset line always
		// emits because it carries the post-reset boundary signal even
		// when the post-reset state is zero.
		tokenDiffZero := tokenDiff.InputTokens == 0 && tokenDiff.OutputTokens == 0 && tokenDiff.TotalTokens == 0
		if !line.Reset && !line.CostReset && tokenDiffZero && costDiff.IsZero() && !toolChanged {
			continue
		}

		if !costDiff.IsZero() || line.CostReset {
			cd := costDiff
			line.CostDiff = &cd
		}
		if !currentCost.IsZero() {
			ct := currentCost
			line.CostTotal = &ct
		}
		// Always carry the freshest cumulative tool usage on every emitted
		// line (not only when toolChanged) so the most recent line per
		// server — whatever dimension triggered it — holds the latest
		// snapshot for SeedFromFile to restore.
		if len(currentTools) > 0 {
			line.ToolUsage = currentTools
		}

		plan = append(plan, planned{writer: writer, line: line})
		// Update prev / prevCost under the lock — even if the write fails
		// the in-memory state advances; lumberjack rotates rather than
		// retaining failed writes, so retry would emit the same delta on
		// the next tick anyway. Both maps advance together so a partial
		// failure cannot leave them out of sync for the next tick's diff.
		f.prev[name] = current
		f.prevCost[name] = currentCost
		f.prevTools[name] = currentTools
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

// isCostCounterReset is the cost analogue of isCounterReset. ClearCost
// can produce a strictly-decreasing cost component without touching tokens;
// flushOnce records that as a CostReset (independent of token Reset) so
// SeedFromFile knows to skip the line's CostDiff for time-series replay
// while still consuming its token Diff normally.
func isCostCounterReset(prev, current metrics.CostMicroUSDCounts) bool {
	return current.InputMicroUSD < prev.InputMicroUSD ||
		current.OutputMicroUSD < prev.OutputMicroUSD ||
		current.CacheReadMicroUSD < prev.CacheReadMicroUSD ||
		current.CacheWriteMicroUSD < prev.CacheWriteMicroUSD
}

// toolUsageChanged reports whether the cumulative per-tool call counts for a
// server differ between two snapshots. Comparing call counts alone suffices:
// RecordToolCall bumps the count on every call, so a changed LastCalledAt
// always coincides with a changed count. A new tool key (len differs) or any
// per-tool count delta returns true. A token Reset clears tool usage, which
// surfaces here as a shrink (len differs) — but reset lines already force a
// flush, so this only needs to catch the steady-state tool-only delta.
func toolUsageChanged(prev, current map[string]metrics.ToolStat) bool {
	if len(prev) != len(current) {
		return true
	}
	for tool, cur := range current {
		if p, ok := prev[tool]; !ok || p.Calls != cur.Calls {
			return true
		}
	}
	return false
}

// SeedFromFile reads up to the last n NDJSON entries from path and seeds
// these surfaces atomically: cumulative per-server token totals (via
// Restore), cumulative per-server cost totals (via RestoreCost), cumulative
// per-(server, tool) call counts for Audit Mode (via RestoreToolUsage),
// per-minute time-series ring buckets — both tokens and cost — (via
// ReplaySnapshot), and this flusher's previous-snapshot maps (prev +
// prevCost + prevTools). The Token
// Usage Over Time and Cost Over Time charts are backed by the time-series
// ring; without the bucket replay each would show only a single post-restart
// point. The Cost KPI card is backed by the cumulative atomics; without
// RestoreCost it would silently read $0 even when pre-restart cost was
// non-zero.
//
// On-disk format mirrors flushOnce's output: full MetricsSnapshotLine
// entries plus lighter reset sentinels ({reset, ts, server} only). Reset
// sentinels parse with a zero Total and are immediately followed by a full
// reset line whose Total carries the post-reset state — so taking the most
// recent Total per server yields the correct seed value in either case.
//
// For time-series, only non-reset lines are replayed: a Reset line's Diff
// carries the carry-over from prior sessions (full snapshot), not a single
// minute's activity, so replaying it would create a synthetic spike at the
// reset boundary. The same skip applies to cost replay.
//
// Legacy files predating cost persistence have no CostDiff / CostTotal
// fields; they unmarshal with nil pointers, the cost diff sums to zero,
// the cost replay no-ops, and RestoreCost is invoked with an empty map
// (which itself no-ops). Token state restores normally — the file remains
// fully readable with no warning.
//
// Missing or empty files return nil (expected on first run with persistence
// enabled). Malformed lines are skipped without aborting; a single corrupt
// line should not lose the rest of the history.
func (f *MetricsFlusher) SeedFromFile(path string, n int) error {
	if path == "" || f.acc == nil {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("seed metrics from %q: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("seed metrics scan %q: %w", path, err)
	}

	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	type seriesPoint struct {
		server string
		ts     time.Time
		input  int64
		output int64
		cost   int64 // rolled-up micro-USD total for the bucket
	}

	// Latest Total / CostTotal per server feeds Restore + RestoreCost +
	// prev / prevCost. Non-reset Diff entries feed ReplaySnapshot in
	// chronological file order so per-minute buckets appear in the same
	// shape they had during live operation.
	latest := make(map[string]metrics.TokenCounts)
	latestCost := make(map[string]metrics.CostMicroUSDCounts)
	latestTools := make(map[string]map[string]metrics.ToolStat)
	series := make([]seriesPoint, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec MetricsSnapshotLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Server == "" {
			continue
		}
		latest[rec.Server] = rec.Total
		if rec.CostTotal != nil {
			latestCost[rec.Server] = *rec.CostTotal
		}
		// Tool usage is a cumulative snapshot, not a diff. A token Reset
		// means the accumulator cleared (server restart / Clear), wiping
		// tool usage too — drop the carried-over snapshot so a post-reset
		// run with no tool calls restores nothing rather than stale counts.
		// Any ToolUsage on this same line (post-reset activity) then wins.
		if rec.Reset {
			delete(latestTools, rec.Server)
		}
		if rec.ToolUsage != nil {
			latestTools[rec.Server] = rec.ToolUsage
		}
		// Reset and CostReset are independent: a token-reset line skips
		// token replay (Diff is the full carryover, replaying would spike
		// the bucket) but its cost diff may still be a real per-minute
		// delta. Symmetrically, a cost-reset-only line keeps its real
		// token diff but skips the cost component.
		var input, output, costMicro int64
		if !rec.Reset {
			input = rec.Diff.InputTokens
			output = rec.Diff.OutputTokens
		}
		if !rec.CostReset && !rec.Reset && rec.CostDiff != nil {
			// Token-reset lines carry CostDiff = currentCost as a
			// fresh-server boundary marker, not a per-minute delta —
			// skip cost replay for those too so we don't emit a
			// synthetic spike alongside the token reset.
			costMicro = rec.CostDiff.TotalMicroUSD()
		}
		if input == 0 && output == 0 && costMicro == 0 {
			continue
		}
		series = append(series, seriesPoint{
			server: rec.Server,
			ts:     rec.Time,
			input:  input,
			output: output,
			cost:   costMicro,
		})
	}

	if len(latest) == 0 {
		return nil
	}

	// Replay time-series buckets first so the ring buffer fills in
	// chronological order; then restore cumulative counters; then seed
	// the flusher's prev / prevCost maps under the lock so the next
	// flushOnce computes a real diff against the seeded baseline.
	for _, p := range series {
		f.acc.ReplaySnapshot(p.server, p.ts, p.input, p.output, p.cost)
	}
	f.acc.Restore(latest)
	f.acc.RestoreCost(latestCost)
	f.acc.RestoreToolUsage(latestTools)
	f.mu.Lock()
	for name, counts := range latest {
		f.prev[name] = counts
	}
	for name, counts := range latestCost {
		f.prevCost[name] = counts
	}
	for name, tools := range latestTools {
		f.prevTools[name] = tools
	}
	f.mu.Unlock()
	return nil
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
