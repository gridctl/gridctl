package telemetry

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
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
