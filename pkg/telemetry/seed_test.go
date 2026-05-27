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

// TestEndToEnd_CostPersistAndReseed mirrors TestEndToEnd_MetricsPersistAndReseed
// for cost. After a daemon restart, both cumulative cost (the Cost KPI on
// the Metrics tab) and the cost time-series ring (the Cost Over Time
// chart) should reflect pre-restart spend. Without RestoreCost +
// ReplaySnapshot's cost replay, both surfaces silently read $0 even when
// the persisted file carries non-zero cost.
func TestEndToEnd_CostPersistAndReseed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	// === Daemon "instance 1" ===
	acc1 := metrics.NewAccumulator(100)
	f1 := NewMetricsFlusher(acc1, time.Hour)
	if err := f1.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	acc1.Record("github", 100, 50)
	acc1.RecordCost("github", -1, metrics.CostBreakdown{Input: 0.05, Output: 0.10})
	f1.flushOnce(time.Now())

	acc1.Record("github", 25, 10)
	acc1.RecordCost("github", -1, metrics.CostBreakdown{
		Input: 0.02, Output: 0.04, CacheRead: 0.01,
	})
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

	// Cumulative cost: every component restored, session = sum across
	// servers (here, single server). Total = 0.05+0.10+0.02+0.04+0.01 = 0.22.
	cs := acc2.CostSnapshot()
	wantTotal := 0.22
	if !approxCostEq(cs.Session.TotalUSD, wantTotal) {
		t.Errorf("seeded session cost = %.6f; want %.6f", cs.Session.TotalUSD, wantTotal)
	}
	if !approxCostEq(cs.PerServer["github"].TotalUSD, wantTotal) {
		t.Errorf("seeded github cost = %.6f; want %.6f", cs.PerServer["github"].TotalUSD, wantTotal)
	}
	// Component breakdown survives — micro-USD round-trip is exact.
	if !approxCostEq(cs.PerServer["github"].CacheReadUSD, 0.01) {
		t.Errorf("seeded github cache-read cost = %.6f; want 0.01", cs.PerServer["github"].CacheReadUSD)
	}

	// Time-series cost: only the second flush's diff replays (Reset is
	// skipped to avoid a synthetic spike). The first flush is the
	// per-server reset and its CostDiff carries the full carry-over —
	// 0.05+0.10 = 0.15 — which does NOT enter the ring. The second flush
	// diff is 0.02+0.04+0.01 = 0.07, which is the only value that should
	// appear in the time-series.
	ts := acc2.QueryCost(time.Hour)
	points := ts.PerServer["github"]
	if len(points) == 0 {
		t.Fatal("github cost time-series points = 0 after seed; chart would be empty")
	}
	var seriesUSD float64
	for _, p := range points {
		seriesUSD += p.USD
	}
	wantSeries := 0.07
	if !approxCostEq(seriesUSD, wantSeries) {
		t.Errorf("seeded cost series total = %.6f; want %.6f (only the non-reset Diff)", seriesUSD, wantSeries)
	}

	// Live cost activity post-restart: next flush emits a real cost diff,
	// not a reset — proving f.prevCost was seeded so the diff math is
	// against the seeded baseline rather than against zero.
	acc2.RecordCost("github", -1, metrics.CostBreakdown{Input: 0.001, Output: 0.002})
	f2.flushOnce(time.Now())

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := splitNonEmpty(string(data))
	var last MetricsSnapshotLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last line: %v", err)
	}
	if last.Reset {
		t.Errorf("post-seed cost flush emitted reset=true; prevCost map was not seeded. line=%s", lines[len(lines)-1])
	}
	if last.CostDiff == nil {
		t.Fatalf("post-seed cost flush has nil CostDiff; want $0.003 diff. line=%s", lines[len(lines)-1])
	}
	wantDiffMicro := int64(3_000) // $0.003 = input 0.001 + output 0.002
	if got := last.CostDiff.TotalMicroUSD(); got != wantDiffMicro {
		t.Errorf("post-seed cost diff total = %d micro; want %d", got, wantDiffMicro)
	}
}

// TestEndToEnd_CostOnlyFlushWritesLine guards the skip-zero gate change:
// a minute with no token activity but non-zero priced cost must still
// emit a flush line so the cost reaches disk. Pre-fix the gate looked at
// tokens only and a cost-only minute would be silently dropped.
func TestEndToEnd_CostOnlyFlushWritesLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	// First flush emits a Reset (full snapshot) — counts as one payload.
	acc.RecordCost("github", -1, metrics.CostBreakdown{Input: 0.05})
	f.flushOnce(time.Now())

	// More cost, zero new tokens. Pre-fix this would be a no-op.
	acc.RecordCost("github", -1, metrics.CostBreakdown{Output: 0.07})
	beforeLines := readLineCount(t, path)
	f.flushOnce(time.Now())
	afterLines := readLineCount(t, path)

	if afterLines <= beforeLines {
		t.Fatalf("cost-only flush did not append a line: before=%d after=%d", beforeLines, afterLines)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	all := splitNonEmpty(string(data))
	var last MetricsSnapshotLine
	if err := json.Unmarshal([]byte(all[len(all)-1]), &last); err != nil {
		t.Fatalf("unmarshal last: %v", err)
	}
	if last.Reset {
		t.Error("cost-only follow-up flush emitted reset=true")
	}
	if last.CostDiff == nil || last.CostDiff.OutputMicroUSD != 70_000 {
		t.Errorf("cost-only diff = %+v; want OutputMicroUSD=70000 ($0.07)", last.CostDiff)
	}
	// Token diff zero is the explicit signal the line was written for cost.
	if last.Diff.InputTokens != 0 || last.Diff.OutputTokens != 0 {
		t.Errorf("cost-only diff carried token deltas; got %+v", last.Diff)
	}
}

// TestEndToEnd_CostOnlyResetPreservesTokenDiff guards the high-severity
// edge case where ClearCost runs between flushes without a token Clear.
// Before the CostReset/Reset split, the resulting line was marked Reset
// with Diff = current (full token snapshot) — and SeedFromFile skipped
// Reset lines from time-series replay, silently dropping that minute's
// token activity from the Token Over Time chart on the next restart.
func TestEndToEnd_CostOnlyResetPreservesTokenDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	acc1 := metrics.NewAccumulator(100)
	f1 := NewMetricsFlusher(acc1, time.Hour)
	if err := f1.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	// First flush: prime token + cost state.
	acc1.Record("github", 100, 50)
	acc1.RecordCost("github", -1, metrics.CostBreakdown{Input: 0.05})
	f1.flushOnce(time.Now())

	// Cost-only reset: ClearCost wipes cost atomics, but tokens advance
	// monotonically. This is exactly the user-facing "DELETE
	// /api/metrics/cost" path while a session is running.
	acc1.ClearCost()
	acc1.Record("github", 25, 10)
	f1.flushOnce(time.Now())

	// Inspect the on-disk line: must be CostReset=true, Reset=false, and
	// Diff carries the real token delta (25/10) rather than a full
	// snapshot.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := splitNonEmpty(string(data))
	var last MetricsSnapshotLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last: %v", err)
	}
	if last.Reset {
		t.Errorf("cost-only reset emitted Reset=true; that conflates dimensions and drops the token diff on replay")
	}
	if !last.CostReset {
		t.Errorf("cost-only reset did not emit CostReset=true; SeedFromFile cannot tell to skip cost replay")
	}
	if last.Diff.InputTokens != 25 || last.Diff.OutputTokens != 10 {
		t.Errorf("cost-only reset Diff = %+v; want real token delta {25,10,35}", last.Diff)
	}

	// Restart simulation: fresh accumulator, seed from disk. The token
	// time-series ring should contain the 25/10 delta — proving the
	// CostReset/Reset split prevented the silent drop.
	f1.mu.Lock()
	for _, lj := range f1.writers {
		_ = lj.Close()
	}
	f1.mu.Unlock()

	acc2 := metrics.NewAccumulator(100)
	f2 := NewMetricsFlusher(acc2, time.Hour)
	if err := f2.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer 2: %v", err)
	}
	if err := f2.SeedFromFile(path, 100); err != nil {
		t.Fatalf("SeedFromFile: %v", err)
	}

	ts := acc2.Query(time.Hour)
	var seriesIn, seriesOut int64
	for _, p := range ts.PerServer["github"] {
		seriesIn += p.InputTokens
		seriesOut += p.OutputTokens
	}
	if seriesIn != 25 || seriesOut != 10 {
		t.Errorf("seeded token series = (%d,%d); want (25,10) — the cost-only reset's token activity must survive replay", seriesIn, seriesOut)
	}

	// Cumulative tokens reflect both pre- and post-reset activity.
	if got := acc2.Snapshot().PerServer["github"].TotalTokens; got != 185 {
		t.Errorf("seeded github token total = %d; want 185 (full carryover via Total)", got)
	}

	// Cost time-series should NOT replay across the cost-only reset
	// boundary — the post-reset cost is zero, and the line carries
	// CostDiff = currentCost as the boundary marker, not a per-minute
	// delta. SeedFromFile skips it for cost replay.
	costTS := acc2.QueryCost(time.Hour)
	var seriesUSD float64
	for _, p := range costTS.PerServer["github"] {
		seriesUSD += p.USD
	}
	if !approxCostEq(seriesUSD, 0) {
		t.Errorf("post-cost-reset cost series = %v; want 0", seriesUSD)
	}
}

// readLineCount returns the number of non-empty lines currently in path.
// Used to assert the flusher appended (or did not append) a line on a
// given tick.
func readLineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return len(splitNonEmpty(string(data)))
}

// approxCostEq is a 1-micro-USD tolerance compare for the seeded-cost
// assertions. Mirrors the helper in pkg/metrics/accumulator_test.go;
// duplicated here because the telemetry tests are a different package.
func approxCostEq(a, b float64) bool {
	const eps = 1e-6
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// TestEndToEnd_PromptUsagePersistAndReseed is the prompt-usage analogue of
// TestEndToEnd_MetricsPersistAndReseed: after a daemon restart, per-skill
// prompts/get counts and last-used timestamps come back from disk so the
// Skills Library "Never used" facet stays honest across restarts. This guards
// the persistence-writer pitfall (without the dedicated prompt-usage writer
// the counts would record in memory but never reach disk or seed).
func TestEndToEnd_PromptUsagePersistAndReseed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.jsonl")

	// === Daemon "instance 1" ===
	acc1 := metrics.NewAccumulator(100)
	f1 := NewMetricsFlusher(acc1, time.Hour)
	if err := f1.SetPromptUsageWriter(path, LogOpts{}); err != nil {
		t.Fatalf("SetPromptUsageWriter: %v", err)
	}
	acc1.RecordPromptGet("code-review")
	acc1.RecordPromptGet("code-review")
	acc1.RecordPromptGet("summarize")
	f1.flushOnce(time.Now())

	// "Restart": close instance 1's writer so the file is fully flushed.
	f1.mu.Lock()
	if f1.promptWriter != nil {
		_ = f1.promptWriter.Close()
	}
	f1.mu.Unlock()

	// === Daemon "instance 2" ===
	acc2 := metrics.NewAccumulator(100)
	f2 := NewMetricsFlusher(acc2, time.Hour)
	if err := f2.SetPromptUsageWriter(path, LogOpts{}); err != nil {
		t.Fatalf("SetPromptUsageWriter: %v", err)
	}
	if err := f2.SeedPromptUsageFromFile(path, 100); err != nil {
		t.Fatalf("SeedPromptUsageFromFile: %v", err)
	}

	// Pre-restart counts are visible immediately via the snapshot.
	snap := acc2.PromptUsageSnapshot()
	if got := snap["code-review"].Calls; got != 2 {
		t.Errorf("seeded code-review calls = %d, want 2", got)
	}
	if got := snap["summarize"].Calls; got != 1 {
		t.Errorf("seeded summarize calls = %d, want 1", got)
	}
	if snap["code-review"].LastCalledAt.IsZero() {
		t.Error("seeded code-review LastCalledAt should be non-zero")
	}

	// A live call after restart continues from the restored count, and a flush
	// emits the new cumulative snapshot (3 for code-review).
	acc2.RecordPromptGet("code-review")
	f2.flushOnce(time.Now())

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := splitNonEmpty(string(data))
	var last MetricsSnapshotLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last line: %v", err)
	}
	if last.Server != PromptUsageNamespace {
		t.Errorf("last line server = %q, want %q", last.Server, PromptUsageNamespace)
	}
	if got := last.PromptUsage["code-review"].Calls; got != 3 {
		t.Errorf("post-seed code-review calls = %d, want 3 (2 seeded + 1 live)", got)
	}
}

// TestMetricsFlusher_PromptUsageMissingFileNoError confirms seeding from an
// absent prompt-usage file is a clean no-op (expected on first run).
func TestMetricsFlusher_PromptUsageMissingFileNoError(t *testing.T) {
	acc := metrics.NewAccumulator(100)
	f := NewMetricsFlusher(acc, time.Hour)
	if err := f.SeedPromptUsageFromFile(filepath.Join(t.TempDir(), "missing.jsonl"), 100); err != nil {
		t.Errorf("missing file should be a no-op, got %v", err)
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
