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
