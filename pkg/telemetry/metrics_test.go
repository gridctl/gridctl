package telemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/metrics"
)

func TestMetricsFlusher_FirstFlushIsFullSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	acc := metrics.NewAccumulator(100)
	acc.Record("github", 100, 50)

	f := NewMetricsFlusher(acc, time.Hour) // long interval — we trigger flushOnce manually
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	now := time.Now()
	f.flushOnce(now)

	lines := readMetricsLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (reset sentinel + payload), got %d", len(lines))
	}
	// First line is a reset sentinel — itself valid NDJSON so strict
	// line-by-line parsers don't choke. Confirm shape.
	var sentinel struct {
		Reset  bool   `json:"reset"`
		Server string `json:"server"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &sentinel); err != nil {
		t.Fatalf("reset sentinel not valid JSON: %v (line=%q)", err, lines[0])
	}
	if !sentinel.Reset || sentinel.Server != "github" {
		t.Errorf("sentinel = %+v, want reset=true server=github", sentinel)
	}
	var entry MetricsSnapshotLine
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !entry.Reset {
		t.Errorf("first JSON entry reset = false, want true")
	}
	if entry.Server != "github" {
		t.Errorf("server = %q, want %q", entry.Server, "github")
	}
	if entry.Diff.InputTokens != 100 || entry.Diff.OutputTokens != 50 {
		t.Errorf("diff = %+v, want full snapshot 100/50", entry.Diff)
	}
}

func TestMetricsFlusher_DiffsBetweenFlushes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	acc.Record("github", 100, 50)
	f.flushOnce(time.Now())

	// Add 25 more input tokens and flush again — expect a diff line, no
	// reset, no `// reset` separator.
	acc.Record("github", 25, 0)
	f.flushOnce(time.Now())

	lines := readMetricsLines(t, path)
	// First two lines are `// reset` + initial snapshot (handled by previous test).
	// The third line should be a plain diff with reset=false.
	if len(lines) < 3 {
		t.Fatalf("expected >=3 lines after second flush, got %d: %v", len(lines), lines)
	}
	var entry MetricsSnapshotLine
	if err := json.Unmarshal([]byte(lines[2]), &entry); err != nil {
		t.Fatalf("unmarshal third line: %v", err)
	}
	if entry.Reset {
		t.Errorf("third line reset = true, want false")
	}
	if entry.Diff.InputTokens != 25 || entry.Diff.OutputTokens != 0 {
		t.Errorf("diff = %+v, want delta {25,0,25}", entry.Diff)
	}
}

func TestMetricsFlusher_IdleSkipsZeroDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	acc.Record("github", 100, 50)
	f.flushOnce(time.Now())

	// Second flush with no new activity — should NOT append a zero diff.
	beforeLen := len(readMetricsLines(t, path))
	f.flushOnce(time.Now())
	afterLen := len(readMetricsLines(t, path))
	if afterLen != beforeLen {
		t.Errorf("idle flush appended %d new lines; want 0", afterLen-beforeLen)
	}
}

func TestMetricsFlusher_DetectsCounterReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	acc.Record("github", 100, 50)
	f.flushOnce(time.Now())

	// Simulate a counter reset (Accumulator.Clear or process restart) and
	// give the server a non-zero state again so PerServer is populated.
	acc.Clear()
	acc.Record("github", 25, 10)
	f.flushOnce(time.Now())

	lines := readMetricsLines(t, path)
	// Count reset sentinels — they are now valid JSON objects with a
	// distinguishing `reset:true` plus no `total` key.
	resetCount := 0
	for _, l := range lines {
		var probe map[string]any
		if err := json.Unmarshal([]byte(l), &probe); err != nil {
			continue
		}
		_, hasReset := probe["reset"]
		_, hasTotal := probe["total"]
		if hasReset && !hasTotal {
			resetCount++
		}
	}
	if resetCount != 2 {
		t.Errorf("expected 2 reset sentinels in %v, got %d", lines, resetCount)
	}

	// The last full payload line should be the post-reset full snapshot.
	var last MetricsSnapshotLine
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(lines[i], "{") {
			continue
		}
		if err := json.Unmarshal([]byte(lines[i]), &last); err != nil {
			continue
		}
		if last.Total.TotalTokens != 0 {
			break
		}
	}
	if !last.Reset {
		t.Error("post-reset entry reset = false; want true")
	}
	if last.Diff.InputTokens != 25 || last.Diff.OutputTokens != 10 {
		t.Errorf("post-reset diff = %+v, want full {25,10,35}", last.Diff)
	}
}

func TestMetricsFlusher_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")
	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %v, want 0600", got)
	}
}

func TestMetricsFlusher_StartStopFinalFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")
	acc := metrics.NewAccumulator(100)

	// Long interval so the only flush we get during this test is the
	// final one driven by Stop().
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	f.Start()
	acc.Record("github", 7, 3)
	f.Stop()

	lines := readMetricsLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected final flush to write at least 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestMetricsFlusher_SeedFromFile(t *testing.T) {
	t.Run("missing file is no-op", func(t *testing.T) {
		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(filepath.Join(t.TempDir(), "missing.jsonl"), 100); err != nil {
			t.Errorf("missing file should not error: %v", err)
		}
		if got := acc.Snapshot().Session.TotalTokens; got != 0 {
			t.Errorf("session total = %d after missing-file seed; want 0", got)
		}
	})

	t.Run("empty file is no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write empty file: %v", err)
		}
		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Errorf("empty file should not error: %v", err)
		}
		if got := acc.Snapshot().Session.TotalTokens; got != 0 {
			t.Errorf("session total = %d after empty-file seed; want 0", got)
		}
	})

	t.Run("single line seeds totals and prev", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		// Write a single full snapshot line for github.
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Reset:  true,
			Diff:   metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
			Total:  metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}

		snap := acc.Snapshot()
		if got := snap.Session.TotalTokens; got != 150 {
			t.Errorf("session total = %d; want 150", got)
		}
		if got, ok := snap.PerServer["github"]; !ok || got.TotalTokens != 150 {
			t.Errorf("per-server github = %+v; want total 150", got)
		}

		// prev must mirror the seeded total so the next flushOnce produces a
		// real diff rather than a fresh reset.
		f.mu.Lock()
		prev, ok := f.prev["github"]
		f.mu.Unlock()
		if !ok || prev.TotalTokens != 150 {
			t.Errorf("prev[github] = %+v (ok=%v); want total 150", prev, ok)
		}
	})

	t.Run("reset sentinel mid-stream uses post-reset totals", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		// Pre-reset history.
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Reset:  true,
			Diff:   metrics.TokenCounts{InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500},
			Total:  metrics.TokenCounts{InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500},
		})
		// Reset sentinel (lightweight form — reset/ts/server only).
		writeRawLine(t, path, `{"reset":true,"ts":"2026-05-06T00:00:00Z","server":"github"}`)
		// Post-reset full line with smaller totals.
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Reset:  true,
			Diff:   metrics.TokenCounts{InputTokens: 25, OutputTokens: 10, TotalTokens: 35},
			Total:  metrics.TokenCounts{InputTokens: 25, OutputTokens: 10, TotalTokens: 35},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}
		got := acc.Snapshot().PerServer["github"]
		if got.TotalTokens != 35 {
			t.Errorf("github total = %d after post-reset seed; want 35", got.TotalTokens)
		}
	})

	t.Run("malformed line is skipped", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		writeRawLine(t, path, `{not json`)
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Total:  metrics.TokenCounts{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}
		if got := acc.Snapshot().PerServer["github"].TotalTokens; got != 15 {
			t.Errorf("github total = %d; want 15 (malformed line should not block valid one)", got)
		}
	})

	t.Run("non-reset diffs replay as time-series buckets", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		// Two flush windows, each with a real Diff. The first line is a
		// Reset line (carry-over from previous session) — its Diff should
		// NOT replay into the time-series. The next two lines are normal
		// diffs and SHOULD show up in the per-minute ring.
		base := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Minute)
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   base,
			Server: "github",
			Reset:  true,
			Diff:   metrics.TokenCounts{InputTokens: 10000, OutputTokens: 5000, TotalTokens: 15000},
			Total:  metrics.TokenCounts{InputTokens: 10000, OutputTokens: 5000, TotalTokens: 15000},
		})
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   base.Add(time.Minute),
			Server: "github",
			Diff:   metrics.TokenCounts{InputTokens: 7, OutputTokens: 3, TotalTokens: 10},
			Total:  metrics.TokenCounts{InputTokens: 10007, OutputTokens: 5003, TotalTokens: 15010},
		})
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   base.Add(2 * time.Minute),
			Server: "github",
			Diff:   metrics.TokenCounts{InputTokens: 11, OutputTokens: 5, TotalTokens: 16},
			Total:  metrics.TokenCounts{InputTokens: 10018, OutputTokens: 5008, TotalTokens: 15026},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}

		ts := acc.Query(10 * time.Minute)
		points, ok := ts.PerServer["github"]
		if !ok {
			t.Fatalf("github time-series missing from Query: %+v", ts.PerServer)
		}
		// The Reset line must NOT show up — only the two real diffs.
		if len(points) != 2 {
			t.Errorf("github points = %d; want 2 (Reset Diff should be skipped). points=%+v", len(points), points)
		}
		var totalIn, totalOut int64
		for _, p := range points {
			totalIn += p.InputTokens
			totalOut += p.OutputTokens
		}
		if totalIn != 18 || totalOut != 8 {
			t.Errorf("replayed totals = (%d,%d); want (18, 8) — only the two non-reset Diffs", totalIn, totalOut)
		}
		// Aggregate ring should also have the same minute-buckets.
		if len(ts.Points) != 2 {
			t.Errorf("aggregate points = %d; want 2", len(ts.Points))
		}
	})

	t.Run("post-seed flush emits diff not reset", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		// Seed with a baseline.
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Reset:  true,
			Diff:   metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
			Total:  metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.AddServer("github", path, LogOpts{}); err != nil {
			t.Fatalf("AddServer: %v", err)
		}
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}

		// Live activity post-restart.
		acc.Record("github", 25, 10)
		f.flushOnce(time.Now())

		lines := readMetricsLines(t, path)
		// Expect exactly one new line appended (a non-reset diff). No
		// extra reset sentinel — that would mean prev was not seeded.
		// The seed line is the original one written; the new line is last.
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 lines after post-seed flush, got %d: %v", len(lines), lines)
		}
		var last MetricsSnapshotLine
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
			t.Fatalf("unmarshal last line: %v", err)
		}
		if last.Reset {
			t.Errorf("post-seed flush emitted reset=true; prev was not seeded correctly. line=%s", lines[len(lines)-1])
		}
		if last.Diff.InputTokens != 25 || last.Diff.OutputTokens != 10 {
			t.Errorf("post-seed diff = %+v; want {25,10,35} (only the new activity)", last.Diff)
		}
		if last.Total.InputTokens != 125 || last.Total.OutputTokens != 60 {
			t.Errorf("post-seed total = %+v; want {125,60,185} (seeded baseline + new activity)", last.Total)
		}
	})
}

// TestSeedFromFile_LegacyTokenOnly verifies that metrics.jsonl files
// written before cost persistence shipped (no cost_diff / cost_total
// fields) continue to load cleanly. Token state restores; cost state
// stays zero. Backward compatibility is the entire reason the cost
// fields are pointer + omitempty.
func TestSeedFromFile_LegacyTokenOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	// Hand-write the legacy on-disk format: a reset sentinel followed by
	// a reset payload, then a non-reset diff. No cost_diff / cost_total
	// fields anywhere — exactly what a pre-cost-persistence daemon would
	// have produced.
	writeRawLine(t, path, `{"reset":true,"ts":"2026-05-05T12:00:00Z","server":"github"}`)
	writeRawLine(t, path,
		`{"ts":"2026-05-05T12:00:00Z","server":"github","reset":true,`+
			`"diff":{"input_tokens":100,"output_tokens":50,"total_tokens":150},`+
			`"total":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}`)
	writeRawLine(t, path,
		`{"ts":"2026-05-05T12:01:00Z","server":"github",`+
			`"diff":{"input_tokens":25,"output_tokens":10,"total_tokens":35},`+
			`"total":{"input_tokens":125,"output_tokens":60,"total_tokens":185}}`)

	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if err := f.SeedFromFile(path, 100); err != nil {
		t.Fatalf("SeedFromFile of legacy file: %v", err)
	}

	// Tokens restore as if nothing changed.
	snap := acc.Snapshot()
	if got := snap.PerServer["github"].TotalTokens; got != 185 {
		t.Errorf("legacy seed token total = %d; want 185", got)
	}
	if got := snap.Session.TotalTokens; got != 185 {
		t.Errorf("legacy seed session token total = %d; want 185", got)
	}

	// Cost stays zero — nothing on disk to restore.
	cost := acc.CostSnapshot()
	if cost.Session.TotalUSD != 0 {
		t.Errorf("legacy seed produced non-zero session cost = %v; want 0", cost.Session.TotalUSD)
	}
	if got, ok := cost.PerServer["github"]; ok && got.TotalUSD != 0 {
		t.Errorf("legacy seed produced non-zero github cost = %v; want 0", got.TotalUSD)
	}
}

// TestMetricsSnapshotLine_OmitsCostFieldsWhenZero pins the wire format
// guarantee: a token-only flush (no priced calls in the minute)
// serializes byte-identically to the pre-cost-persistence schema, so
// older daemons reading new files round-trip token state without any
// surprises.
func TestMetricsSnapshotLine_OmitsCostFieldsWhenZero(t *testing.T) {
	line := MetricsSnapshotLine{
		Time:   time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Server: "github",
		Diff:   metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		Total:  metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "cost_diff") {
		t.Errorf("token-only line carried cost_diff field: %s", got)
	}
	if strings.Contains(got, "cost_total") {
		t.Errorf("token-only line carried cost_total field: %s", got)
	}
}

// TestMetricsSnapshotLine_IncludesCostFieldsWhenPresent is the inverse —
// once cost is non-zero, the new fields appear with the documented JSON
// names so the seed path can find them.
func TestMetricsSnapshotLine_IncludesCostFieldsWhenPresent(t *testing.T) {
	cd := metrics.CostMicroUSDCounts{InputMicroUSD: 50_000}
	ct := metrics.CostMicroUSDCounts{InputMicroUSD: 100_000}
	line := MetricsSnapshotLine{
		Time:      time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Server:    "github",
		Diff:      metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		Total:     metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		CostDiff:  &cd,
		CostTotal: &ct,
	}
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"cost_diff":{"input_micro_usd":50000}`) {
		t.Errorf("line missing cost_diff: %s", got)
	}
	if !strings.Contains(got, `"cost_total":{"input_micro_usd":100000}`) {
		t.Errorf("line missing cost_total: %s", got)
	}
}

// TestMetricsSnapshotLine_OmitsToolUsageWhenEmpty pins the wire-format
// guarantee for the tool_usage extension: a line with no per-tool activity
// (and legacy files predating Audit Mode) serializes without the field, so
// the addition is backward-compatible exactly like the cost fields.
func TestMetricsSnapshotLine_OmitsToolUsageWhenEmpty(t *testing.T) {
	line := MetricsSnapshotLine{
		Time:   time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Server: "github",
		Diff:   metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		Total:  metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "tool_usage") {
		t.Errorf("tool-usage-free line carried tool_usage field: %s", data)
	}
}

// TestMetricsFlusher_ToolUsagePersistence covers Audit Mode's per-tool usage
// surviving a gateway restart: a flush writes the cumulative per-tool counts,
// a fresh accumulator restores them via SeedFromFile, a tool-only delta still
// reaches disk, and a token reset drops carried-over usage so a wiped
// accumulator does not resurrect stale counts.
func TestMetricsFlusher_ToolUsagePersistence(t *testing.T) {
	t.Run("flush persists cumulative tool usage and seed restores it", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		acc := metrics.NewAccumulator(100)
		acc.Record("github", 100, 50)
		acc.RecordToolCall("github", "create_issue")
		acc.RecordToolCall("github", "create_issue")
		acc.RecordToolCall("github", "list_repos")

		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.AddServer("github", path, LogOpts{}); err != nil {
			t.Fatalf("AddServer: %v", err)
		}
		f.flushOnce(time.Now())

		// The latest payload line carries the cumulative per-tool snapshot.
		var persisted map[string]metrics.ToolStat
		for _, l := range readMetricsLines(t, path) {
			var rec MetricsSnapshotLine
			if err := json.Unmarshal([]byte(l), &rec); err == nil && rec.ToolUsage != nil {
				persisted = rec.ToolUsage
			}
		}
		if persisted == nil {
			t.Fatal("no flushed line carried tool_usage")
		}
		if got := persisted["create_issue"].Calls; got != 2 {
			t.Errorf("persisted create_issue calls = %d, want 2", got)
		}

		// Simulate a restart: a fresh accumulator seeded from the same file.
		acc2 := metrics.NewAccumulator(100)
		f2 := NewMetricsFlusher(acc2, time.Hour)
		if err := f2.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}
		snap := acc2.ToolUsageSnapshot()
		if got := snap["github"]["create_issue"].Calls; got != 2 {
			t.Errorf("restored create_issue calls = %d, want 2", got)
		}
		if got := snap["github"]["list_repos"].Calls; got != 1 {
			t.Errorf("restored list_repos calls = %d, want 1", got)
		}
	})

	t.Run("tool-only delta forces a line without a token change", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		acc := metrics.NewAccumulator(100)
		acc.Record("github", 100, 50)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.AddServer("github", path, LogOpts{}); err != nil {
			t.Fatalf("AddServer: %v", err)
		}
		f.flushOnce(time.Now()) // token baseline
		before := len(readMetricsLines(t, path))

		// A tool call with no token delta must still produce a flush line so
		// the usage reaches disk (mirrors cost's independent delta path).
		acc.RecordToolCall("github", "create_issue")
		f.flushOnce(time.Now())

		lines := readMetricsLines(t, path)
		if len(lines) <= before {
			t.Fatalf("tool-only delta produced no new line: before=%d after=%d", before, len(lines))
		}
		var last MetricsSnapshotLine
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
			t.Fatalf("unmarshal last line: %v", err)
		}
		if got := last.ToolUsage["create_issue"].Calls; got != 1 {
			t.Errorf("forced line tool usage = %+v, want create_issue calls 1", last.ToolUsage)
		}
	})

	t.Run("token reset drops carried-over tool usage on seed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		// Pre-reset history with tool usage.
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:      time.Now().Add(-time.Hour).UTC(),
			Server:    "github",
			Total:     metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
			ToolUsage: map[string]metrics.ToolStat{"create_issue": {Calls: 9, LastCalledAt: time.Now().Add(-time.Hour)}},
		})
		// Reset sentinel + post-reset full line with no tool usage (Clear
		// wiped it): the seed must not resurrect the pre-reset counts.
		writeRawLine(t, path, `{"reset":true,"ts":"2026-05-06T00:00:00Z","server":"github"}`)
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Reset:  true,
			Total:  metrics.TokenCounts{},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}
		if snap := acc.ToolUsageSnapshot(); snap != nil {
			t.Errorf("post-reset seed should restore no tool usage; got %v", snap)
		}
	})
}

// TestMetricsFlusher_ModelCostPersistence covers effective-model provenance
// surviving a gateway restart: a flush writes the cumulative per-model cost
// histogram, a fresh accumulator restores it via SeedFromFile so a replayed
// cost arrives with its provenance intact, and a token reset drops the
// carried-over histogram so a wiped accumulator does not resurrect stale
// model attribution.
func TestMetricsFlusher_ModelCostPersistence(t *testing.T) {
	t.Run("flush persists model histogram and seed restores it", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		acc := metrics.NewAccumulator(100)
		acc.RecordCostWithModel("github", -1, "claude-code", "claude-opus-4-7", 100, 50, metrics.CostBreakdown{Input: 0.30, Output: 0.10})

		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.AddServer("github", path, LogOpts{}); err != nil {
			t.Fatalf("AddServer: %v", err)
		}
		f.flushOnce(time.Now())

		var persisted map[string]metrics.ModelMicroCounts
		for _, l := range readMetricsLines(t, path) {
			var rec MetricsSnapshotLine
			if err := json.Unmarshal([]byte(l), &rec); err == nil && rec.ModelCost != nil {
				persisted = rec.ModelCost
			}
		}
		if persisted == nil {
			t.Fatal("no flushed line carried model_cost")
		}
		if got := persisted["claude-opus-4-7"].CostMicroUSD; got != 400_000 {
			t.Errorf("persisted opus cost = %d micro-USD, want 400000", got)
		}

		// Restart: fresh accumulator seeded from the same file.
		acc2 := metrics.NewAccumulator(100)
		f2 := NewMetricsFlusher(acc2, time.Hour)
		if err := f2.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}
		snap := acc2.CostSnapshot()
		m := snap.PerServerModels["github"]["claude-opus-4-7"]
		if m.CostUSD < 0.399 || m.CostUSD > 0.401 {
			t.Errorf("restored opus cost = %v, want ~0.40", m.CostUSD)
		}
		if m.InputTokens != 100 || m.OutputTokens != 50 {
			t.Errorf("restored opus tokens = (%d,%d), want (100,50)", m.InputTokens, m.OutputTokens)
		}
	})

	t.Run("token reset drops carried-over model histogram on seed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "metrics.jsonl")

		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:      time.Now().Add(-time.Hour).UTC(),
			Server:    "github",
			Total:     metrics.TokenCounts{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
			ModelCost: map[string]metrics.ModelMicroCounts{"claude-opus-4-7": {CostMicroUSD: 400_000, InputTokens: 100, OutputTokens: 50}},
		})
		writeRawLine(t, path, `{"reset":true,"ts":"2026-05-06T00:00:00Z","server":"github"}`)
		writeMetricsLine(t, path, MetricsSnapshotLine{
			Time:   time.Now().UTC(),
			Server: "github",
			Reset:  true,
			Total:  metrics.TokenCounts{},
		})

		acc := metrics.NewAccumulator(100)
		f := NewMetricsFlusher(acc, time.Hour)
		if err := f.SeedFromFile(path, 100); err != nil {
			t.Fatalf("SeedFromFile: %v", err)
		}
		if snap := acc.CostSnapshot(); snap.PerServerModels != nil {
			t.Errorf("post-reset seed should restore no model histogram; got %v", snap.PerServerModels)
		}
	})
}

// writeMetricsLine appends one MetricsSnapshotLine as NDJSON to path.
func writeMetricsLine(t *testing.T, path string, line MetricsSnapshotLine) {
	t.Helper()
	data, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	writeRawLine(t, path, string(data))
}

// writeRawLine appends a raw line + newline to path.
func writeRawLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readMetricsLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return lines
}
