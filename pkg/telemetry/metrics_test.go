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
