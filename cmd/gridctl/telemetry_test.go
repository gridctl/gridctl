package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/telemetry"
)

func setTempHomeTelemetry(t *testing.T) {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	os.Setenv("HOME", t.TempDir())
}

// seedTelemetry writes a telemetry file with the given content under
// ~/.gridctl/telemetry/<stack>/<server>/<signal>.jsonl. The mtime is set so
// inventory ordering tests are deterministic.
func seedTelemetry(t *testing.T, stack, server, signal, content string, mtime time.Time) string {
	t.Helper()
	if err := state.EnsureTelemetryServerDir(stack, server); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	path := state.TelemetryServerPath(stack, server, signal)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	return path
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1500, "1.5 KiB"},
		{10 * 1024, "10 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
	}
	for _, c := range cases {
		got := formatBytes(c.in)
		if got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatInventoryTime(t *testing.T) {
	if got := formatInventoryTime(time.Time{}); got != "—" {
		t.Errorf("zero time = %q, want em-dash", got)
	}
	ts := time.Date(2026, 4, 30, 12, 5, 0, 0, time.UTC)
	if got := formatInventoryTime(ts); got != "2026-04-30 12:05" {
		t.Errorf("ts = %q", got)
	}
}

func TestListTelemetryStacks(t *testing.T) {
	setTempHomeTelemetry(t)

	// No telemetry dir yet — should return nil without error.
	stacks, err := listTelemetryStacks()
	if err != nil {
		t.Fatalf("listTelemetryStacks empty: %v", err)
	}
	if len(stacks) != 0 {
		t.Errorf("expected no stacks, got %v", stacks)
	}

	// Seed two stacks; they should come back sorted.
	seedTelemetry(t, "beta", "github", "logs", "{}\n", time.Time{})
	seedTelemetry(t, "alpha", "slack", "metrics", "{}\n", time.Time{})

	stacks, err = listTelemetryStacks()
	if err != nil {
		t.Fatalf("listTelemetryStacks: %v", err)
	}
	want := []string{"alpha", "beta"}
	if len(stacks) != len(want) || stacks[0] != want[0] || stacks[1] != want[1] {
		t.Errorf("stacks = %v, want %v", stacks, want)
	}
}

func TestGatherInventories_AllStacks(t *testing.T) {
	setTempHomeTelemetry(t)

	now := time.Now()
	seedTelemetry(t, "alpha", "github", "logs", `{"msg":"hi"}`+"\n", now)
	seedTelemetry(t, "beta", "slack", "traces", `{"trace":1}`+"\n", now)

	invs, err := gatherInventories("")
	if err != nil {
		t.Fatalf("gatherInventories: %v", err)
	}
	if len(invs) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(invs))
	}
	// Ordering follows listTelemetryStacks (sorted) → alpha first.
	if invs[0].Stack != "alpha" || invs[1].Stack != "beta" {
		t.Errorf("stack order = %s,%s", invs[0].Stack, invs[1].Stack)
	}
}

func TestGatherInventories_SkipsEmpty(t *testing.T) {
	setTempHomeTelemetry(t)

	// An empty server directory should not produce a stack record.
	if err := state.EnsureTelemetryServerDir("alpha", "github"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	invs, err := gatherInventories("")
	if err != nil {
		t.Fatalf("gatherInventories: %v", err)
	}
	if len(invs) != 0 {
		t.Errorf("expected 0 stacks for empty dir, got %d", len(invs))
	}
}

func TestFilterInventories(t *testing.T) {
	invs := []stackInventory{
		{
			Stack: "alpha",
			Records: []telemetry.InventoryRecord{
				{Server: "github", Signal: "logs"},
				{Server: "github", Signal: "traces"},
				{Server: "slack", Signal: "logs"},
			},
		},
		{
			Stack: "beta",
			Records: []telemetry.InventoryRecord{
				{Server: "slack", Signal: "metrics"},
			},
		},
	}

	t.Run("no filters returns input", func(t *testing.T) {
		got := filterInventories(invs, "", "")
		if len(got) != 2 || len(got[0].Records) != 3 {
			t.Errorf("expected pass-through, got %+v", got)
		}
	})

	t.Run("filter by server drops non-matching stacks", func(t *testing.T) {
		got := filterInventories(invs, "github", "")
		if len(got) != 1 || got[0].Stack != "alpha" {
			t.Fatalf("expected only alpha, got %+v", got)
		}
		if len(got[0].Records) != 2 {
			t.Errorf("expected 2 github records, got %d", len(got[0].Records))
		}
	})

	t.Run("filter by signal", func(t *testing.T) {
		got := filterInventories(invs, "", "logs")
		if len(got) != 1 || got[0].Stack != "alpha" {
			t.Fatalf("expected only alpha (only stack with logs), got %+v", got)
		}
		if len(got[0].Records) != 2 {
			t.Errorf("expected 2 logs records, got %d", len(got[0].Records))
		}
	})

	t.Run("filter by both", func(t *testing.T) {
		got := filterInventories(invs, "slack", "metrics")
		if len(got) != 1 || got[0].Stack != "beta" {
			t.Fatalf("expected beta, got %+v", got)
		}
		if len(got[0].Records) != 1 {
			t.Errorf("expected 1 record, got %d", len(got[0].Records))
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := filterInventories(invs, "missing", "")
		if len(got) != 0 {
			t.Errorf("expected empty, got %+v", got)
		}
	})
}

func TestSummarizeInventories(t *testing.T) {
	invs := []stackInventory{
		{
			Stack: "alpha",
			Records: []telemetry.InventoryRecord{
				{Server: "github", SizeBytes: 100, FileCount: 2},
				{Server: "slack", SizeBytes: 200, FileCount: 1},
			},
		},
		{
			Stack: "beta",
			Records: []telemetry.InventoryRecord{
				{Server: "slack", SizeBytes: 50, FileCount: 1},
			},
		},
	}
	bytes, files, servers := summarizeInventories(invs)
	if bytes != 350 {
		t.Errorf("bytes = %d, want 350", bytes)
	}
	if files != 4 {
		t.Errorf("files = %d, want 4", files)
	}
	if len(servers) != 2 || servers[0] != "github" || servers[1] != "slack" {
		t.Errorf("servers = %v", servers)
	}
}

func TestPrintWipeSummary(t *testing.T) {
	invs := []stackInventory{
		{
			Stack: "alpha",
			Records: []telemetry.InventoryRecord{
				{Server: "github", Signal: "logs", SizeBytes: 1024 * 1024, FileCount: 2},
			},
		},
	}

	var buf bytes.Buffer
	printWipeSummary(&buf, invs, "", "")
	out := buf.String()
	for _, want := range []string{"alpha", "github", "Files:", "1.0 MiB", "everything"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	printWipeSummary(&buf, invs, "github", "logs")
	out = buf.String()
	if !strings.Contains(out, "logs for server github") {
		t.Errorf("scoped summary missing 'logs for server github':\n%s", out)
	}
}

func TestRunTelemetryStatus_Empty(t *testing.T) {
	setTempHomeTelemetry(t)

	stdout := captureStdout(t, func() {
		if err := runTelemetryStatus("", false); err != nil {
			t.Fatalf("runTelemetryStatus: %v", err)
		}
	})
	if !strings.Contains(stdout, "No persisted telemetry") {
		t.Errorf("expected empty-state message, got: %q", stdout)
	}

	stdout = captureStdout(t, func() {
		if err := runTelemetryStatus("missing", false); err != nil {
			t.Fatalf("runTelemetryStatus: %v", err)
		}
	})
	if !strings.Contains(stdout, `stack "missing"`) {
		t.Errorf("expected per-stack empty-state message, got: %q", stdout)
	}
}

func TestRunTelemetryStatus_JSON(t *testing.T) {
	setTempHomeTelemetry(t)

	now := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	seedTelemetry(t, "alpha", "github", "logs", `{"msg":"hi"}`+"\n", now)

	stdout := captureStdout(t, func() {
		if err := runTelemetryStatus("", true); err != nil {
			t.Fatalf("runTelemetryStatus: %v", err)
		}
	})

	var rows []statusRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Stack != "alpha" || rows[0].Server != "github" || rows[0].Signal != "logs" {
		t.Errorf("row = %+v", rows[0])
	}
	if rows[0].FileCount != 1 {
		t.Errorf("file count = %d", rows[0].FileCount)
	}
}

func TestRunTelemetryWipe_NothingToWipe(t *testing.T) {
	setTempHomeTelemetry(t)

	stdout := captureStdout(t, func() {
		if err := runTelemetryWipe("", "", "", true); err != nil {
			t.Fatalf("runTelemetryWipe: %v", err)
		}
	})
	if !strings.Contains(stdout, "Nothing to wipe") {
		t.Errorf("expected 'Nothing to wipe', got: %q", stdout)
	}
}

func TestRunTelemetryWipe_Wildcard(t *testing.T) {
	setTempHomeTelemetry(t)

	now := time.Now()
	p1 := seedTelemetry(t, "alpha", "github", "logs", `{"a":1}`+"\n", now)
	p2 := seedTelemetry(t, "beta", "slack", "traces", `{"b":2}`+"\n", now)

	stdout := captureStdout(t, func() {
		if err := runTelemetryWipe("", "", "", true); err != nil {
			t.Fatalf("runTelemetryWipe: %v", err)
		}
	})
	if !strings.Contains(stdout, "Wiped") {
		t.Errorf("expected 'Wiped' summary, got: %q", stdout)
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err = %v", p1, err)
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err = %v", p2, err)
	}
}

func TestRunTelemetryWipe_ScopedToServer(t *testing.T) {
	setTempHomeTelemetry(t)

	now := time.Now()
	keep := seedTelemetry(t, "alpha", "github", "logs", `{"a":1}`+"\n", now)
	drop := seedTelemetry(t, "alpha", "slack", "logs", `{"b":2}`+"\n", now)

	if err := runTelemetryWipe("alpha", "slack", "", true); err != nil {
		t.Fatalf("runTelemetryWipe: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("expected %s to survive, got: %v", keep, err)
	}
	if _, err := os.Stat(drop); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err = %v", drop, err)
	}
}

func TestRunTelemetryWipe_InvalidSignal(t *testing.T) {
	setTempHomeTelemetry(t)
	err := runTelemetryWipe("", "", "bogus", true)
	if err == nil || !strings.Contains(err.Error(), "invalid signal") {
		t.Errorf("expected invalid-signal error, got: %v", err)
	}
}

func TestRunTelemetryTail_MissingDir(t *testing.T) {
	setTempHomeTelemetry(t)
	err := runTelemetryTail("missing", "server", "logs")
	if err == nil || !strings.Contains(err.Error(), "no telemetry directory") {
		t.Errorf("expected missing-dir error, got: %v", err)
	}
}

func TestRunTelemetryTail_InvalidSignal(t *testing.T) {
	setTempHomeTelemetry(t)
	err := runTelemetryTail("alpha", "github", "bogus")
	if err == nil || !strings.Contains(err.Error(), "invalid signal") {
		t.Errorf("expected invalid-signal error, got: %v", err)
	}
}

func TestTailReader_DrainEmitsCompleteLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")
	if err := os.WriteFile(path, []byte("first line\nsecond line\npartial"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	tr := newTailReader(path, &buf)
	if err := tr.openAtStart(); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tr.close()
	tr.drain()

	got := buf.String()
	if !strings.Contains(got, "first line\n") || !strings.Contains(got, "second line\n") {
		t.Errorf("expected both complete lines, got: %q", got)
	}
	if strings.Contains(got, "partial") {
		t.Errorf("partial trailing line should not have been emitted: %q", got)
	}
}

func TestTailReader_HoldsPartialUntilNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")
	if err := os.WriteFile(path, []byte("partial"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	tr := newTailReader(path, &buf)
	if err := tr.openAtStart(); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tr.close()
	tr.drain()
	if buf.Len() != 0 {
		t.Fatalf("partial line should not be emitted yet, got: %q", buf.String())
	}

	// Append the rest of the line plus a fully terminated next line.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("reopen for append: %v", err)
	}
	if _, err := f.WriteString(" rest\nnext line\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = f.Close()

	tr.drain()
	got := buf.String()
	if !strings.Contains(got, "partial rest\n") {
		t.Errorf("partial line should have been completed: %q", got)
	}
	if !strings.Contains(got, "next line\n") {
		t.Errorf("next line should have been emitted: %q", got)
	}
}

func TestTailReader_OpenAtEndSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")
	if err := os.WriteFile(path, []byte("preexisting line\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	tr := newTailReader(path, &buf)
	if err := tr.openAtEnd(); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer tr.close()
	tr.drain()

	if buf.Len() != 0 {
		t.Errorf("openAtEnd should not have emitted preexisting content, got: %q", buf.String())
	}

	if err := os.WriteFile(path, []byte("preexisting line\nnew line\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	tr.drain()
	if !strings.Contains(buf.String(), "new line") {
		t.Errorf("expected new line in output, got: %q", buf.String())
	}
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns whatever fn
// wrote. Used by the run* tests that print directly via fmt.Print rather
// than through an injected writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	out := <-done
	os.Stdout = orig
	return out
}
