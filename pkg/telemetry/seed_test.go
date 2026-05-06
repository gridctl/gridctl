package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/tracing"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestLogBuffer_SeedFromFileEndToEnd(t *testing.T) {
	// Write three lines through the actual LogRouter → file path, then
	// open a fresh LogBuffer and seed from the same file. Asserts the
	// round-trip preserves message + component + custom attrs.
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	writeBuf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(writeBuf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	logger := slog.New(router).With("component", "github")
	logger.Info("first call", "tool", "list_repos")
	logger.Info("second call", "tool", "create_issue")
	logger.Info("third call")

	// Fresh buffer simulates a restart.
	seedBuf := logging.NewLogBuffer(10)
	if err := seedBuf.SeedFromFile(path, 100); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}
	if got := seedBuf.Count(); got != 3 {
		t.Fatalf("seed count = %d, want 3", got)
	}
	entries := seedBuf.GetRecent(3)
	if entries[0].Message != "first call" || entries[2].Message != "third call" {
		t.Errorf("seeded order wrong: %+v", entries)
	}
	for _, e := range entries {
		if e.Component != "github" {
			t.Errorf("component lost: %+v", e)
		}
	}
}

func TestLogBuffer_SeedFromFile_MissingFileNoError(t *testing.T) {
	// First-ever boot — no file yet — must succeed silently.
	buf := logging.NewLogBuffer(10)
	if err := buf.SeedFromFile(filepath.Join(t.TempDir(), "missing.jsonl"), 100); err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
	if got := buf.Count(); got != 0 {
		t.Errorf("seeded an empty buffer with %d entries; want 0", got)
	}
}

func TestLogBuffer_SeedFromFile_TakesLastNOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	// Write 5 lines then seed with n=2.
	writeBuf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(writeBuf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	logger := slog.New(router).With("component", "github")
	for i := 0; i < 5; i++ {
		logger.Info("entry", "i", i)
	}

	seedBuf := logging.NewLogBuffer(10)
	if err := seedBuf.SeedFromFile(path, 2); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}
	if got := seedBuf.Count(); got != 2 {
		t.Errorf("count = %d, want 2 (last n)", got)
	}
	got := seedBuf.GetRecent(2)
	if iv, ok := got[1].Attrs["i"].(float64); !ok || iv != 4 {
		t.Errorf("last attr i = %v (%T), want 4 (last of 5)", got[1].Attrs["i"], got[1].Attrs["i"])
	}
}

func TestTracingBuffer_SeedFromFileEndToEnd(t *testing.T) {
	// Write spans for two servers via TracesFileClient, then seed a fresh
	// tracing.Buffer from the github file and verify the trace appears.
	dir := t.TempDir()
	githubPath := filepath.Join(dir, "github-traces.jsonl")
	weatherPath := filepath.Join(dir, "weather-traces.jsonl")

	c := NewTracesFileClient()
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	if err := c.AddServer("github", githubPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if err := c.AddServer("weather", weatherPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	rs := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "gridctl/test"},
			Spans: []*tracepb.Span{
				makeSpan("github", "list_repos", 1),
				makeSpan("github", "create_issue", 2),
				makeSpan("weather", "forecast", 3),
			},
		}},
	}
	if err := c.UploadTraces(context.Background(), []*tracepb.ResourceSpans{rs}); err != nil {
		t.Fatalf("UploadTraces: %v", err)
	}

	// Seed an empty trace buffer and confirm it picks up the github traces only.
	buf := tracing.NewBuffer(100, time.Hour)
	if err := buf.SeedFromFile(githubPath, 100); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}
	count := buf.Count()
	if count == 0 {
		t.Fatal("seeded buffer empty after SeedFromFile")
	}
	// Each span we wrote becomes its own root trace (no parent), so we
	// expect 2 traces from the github file.
	if count != 2 {
		t.Errorf("seeded trace count = %d, want 2 (github only)", count)
	}
}

func TestTracingBuffer_SeedFromFile_MissingFileNoError(t *testing.T) {
	buf := tracing.NewBuffer(100, time.Hour)
	if err := buf.SeedFromFile(filepath.Join(t.TempDir(), "missing.jsonl"), 100); err != nil {
		t.Errorf("missing file should not error: %v", err)
	}
	if got := buf.Count(); got != 0 {
		t.Errorf("seeded buffer count = %d, want 0", got)
	}
}

// TestEndToEnd_MetricsPersistAndReseed simulates a daemon restart with
// metrics persistence enabled: record token usage, flush to disk, throw
// away the accumulator + flusher, seed a fresh accumulator + flusher from
// the same file, and verify the totals come back AND the very next live
// flush emits a real diff (no reset, no double-counting).
func TestEndToEnd_MetricsPersistAndReseed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	// === Daemon "instance 1" ===
	acc1 := metrics.NewAccumulator(100)
	f1 := NewMetricsFlusher(acc1, time.Hour)
	if err := f1.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	acc1.Record("github", 100, 50)
	f1.flushOnce(time.Now())
	acc1.Record("github", 25, 10)
	f1.flushOnce(time.Now())

	// "Restart": close instance 1's writers so the file is fully flushed.
	f1.mu.Lock()
	for _, lj := range f1.writers {
		_ = lj.Close()
	}
	f1.mu.Unlock()

	// === Daemon "instance 2" ===
	acc2 := metrics.NewAccumulator(100)
	f2 := NewMetricsFlusher(acc2, time.Hour)
	if err := f2.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	if err := f2.SeedFromFile(path, 100); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}

	// Pre-restart totals should be visible immediately via the snapshot —
	// this is the user-visible fix for "No token data yet" after restart.
	snap := acc2.Snapshot()
	if got := snap.PerServer["github"].TotalTokens; got != 185 {
		t.Errorf("seeded github total = %d; want 185 (100+50+25+10)", got)
	}
	if got := snap.Session.TotalTokens; got != 185 {
		t.Errorf("seeded session total = %d; want 185", got)
	}

	// Pre-restart time-series buckets should also come back so the Token
	// Usage Over Time chart shows pre-restart history continuously rather
	// than a single post-restart point. The first flush is a Reset line
	// (carry-over) and is intentionally skipped; only the second flush's
	// Diff (25 in / 10 out) replays into the per-minute ring.
	ts := acc2.Query(time.Hour)
	githubPoints := ts.PerServer["github"]
	if len(githubPoints) == 0 {
		t.Errorf("github time-series points = 0 after seed; chart would be empty")
	}
	var seriesIn, seriesOut int64
	for _, p := range githubPoints {
		seriesIn += p.InputTokens
		seriesOut += p.OutputTokens
	}
	if seriesIn != 25 || seriesOut != 10 {
		t.Errorf("seeded series totals = (%d,%d); want (25, 10) — only the non-reset Diff", seriesIn, seriesOut)
	}

	// Live activity post-restart. flushOnce must emit a non-reset diff
	// against the seeded baseline rather than re-emitting the seeded
	// totals as if they were fresh.
	acc2.Record("github", 7, 3)
	f2.flushOnce(time.Now())

	// Read everything that's on disk and inspect the last full payload.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := splitNonEmpty(string(data))
	if len(lines) < 1 {
		t.Fatalf("expected at least one line on disk, got 0")
	}
	var last MetricsSnapshotLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last line: %v", err)
	}
	if last.Reset {
		t.Errorf("post-seed flush emitted reset=true; prev map was not seeded. line=%s", lines[len(lines)-1])
	}
	if last.Diff.InputTokens != 7 || last.Diff.OutputTokens != 3 {
		t.Errorf("post-seed diff = %+v; want {7,3,10} (only post-restart activity)", last.Diff)
	}
	if last.Total.InputTokens != 132 || last.Total.OutputTokens != 63 {
		t.Errorf("post-seed total = %+v; want {132,63,195} (seeded + live)", last.Total)
	}
}

// splitNonEmpty splits on '\n' and discards empty entries.
func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestEndToEnd_PersistAndReseed simulates a daemon restart with persistence
// enabled: write logs + traces, throw away the in-memory buffers, seed fresh
// buffers from the same files, and verify history is recovered. This is the
// integration test required by Phase 2's acceptance criteria.
func TestEndToEnd_PersistAndReseed(t *testing.T) {
	dir := t.TempDir()
	logsPath := filepath.Join(dir, "logs.jsonl")
	tracesPath := filepath.Join(dir, "traces.jsonl")

	// === Daemon "instance 1" ===
	buf1 := logging.NewLogBuffer(100)
	router := NewLogRouter(logging.NewBufferHandler(buf1, nil))
	if err := router.AddServer("github", logsPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer logs: %v", err)
	}
	tc := NewTracesFileClient()
	if err := tc.AddServer("github", tracesPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer traces: %v", err)
	}

	logger := slog.New(router).With("component", "github")
	logger.Info("pre-restart entry one")
	logger.Info("pre-restart entry two", "tool", "list_repos")

	rs := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "gridctl/test"},
			Spans: []*tracepb.Span{makeSpan("github", "pre-restart-trace", 1)},
		}},
	}
	if err := tc.UploadTraces(context.Background(), []*tracepb.ResourceSpans{rs}); err != nil {
		t.Fatalf("UploadTraces: %v", err)
	}

	// "Restart": tear down instance 1's writers so the files flush.
	router.Close()
	_ = tc.Stop(context.Background())

	// === Daemon "instance 2" ===
	buf2 := logging.NewLogBuffer(100)
	if err := buf2.SeedFromFile(logsPath, 100); err != nil {
		t.Fatalf("seed logs: %v", err)
	}
	traceBuf2 := tracing.NewBuffer(100, time.Hour)
	if err := traceBuf2.SeedFromFile(tracesPath, 100); err != nil {
		t.Fatalf("seed traces: %v", err)
	}

	if got := buf2.Count(); got != 2 {
		t.Errorf("re-seeded log buffer count = %d, want 2", got)
	}
	if got := traceBuf2.Count(); got != 1 {
		t.Errorf("re-seeded trace buffer count = %d, want 1", got)
	}

	// Verify the on-disk files are mode 0600 (security acceptance).
	for _, p := range []string{logsPath, tracesPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("file %s mode = %v, want 0600", p, got)
		}
	}
}
