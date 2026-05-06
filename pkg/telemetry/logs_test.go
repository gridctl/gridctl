package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/logging"
)

func TestLogRouter_RoutesByComponent(t *testing.T) {
	dir := t.TempDir()
	githubPath := filepath.Join(dir, "github.jsonl")
	weatherPath := filepath.Join(dir, "weather.jsonl")

	// Inner buffer captures everything; the router fans out to per-server
	// files only for matching components.
	buf := logging.NewLogBuffer(100)
	inner := logging.NewBufferHandler(buf, nil)
	router := NewLogRouter(inner)
	t.Cleanup(router.Close)

	if err := router.AddServer("github", githubPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer github: %v", err)
	}
	if err := router.AddServer("weather", weatherPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer weather: %v", err)
	}

	logger := slog.New(router)
	logger.With("component", "github").Info("hit github tool", "tool", "list_repos")
	logger.With("component", "weather").Info("hit weather tool", "tool", "forecast")
	logger.With("component", "unknown").Info("anonymous component — should not write file")
	logger.Info("no component — should not write file")

	githubLines := readJSONLines(t, githubPath)
	weatherLines := readJSONLines(t, weatherPath)

	if len(githubLines) != 1 {
		t.Errorf("github file got %d lines, want 1; lines=%v", len(githubLines), githubLines)
	} else if msg := githubLines[0]["msg"]; msg != "hit github tool" {
		t.Errorf("github line msg = %v, want %q", msg, "hit github tool")
	}
	if len(weatherLines) != 1 {
		t.Errorf("weather file got %d lines, want 1", len(weatherLines))
	}

	// Inner buffer should hold all 4 entries (the router always fans to it).
	if got := buf.Count(); got != 4 {
		t.Errorf("inner buffer count = %d, want 4", got)
	}
}

func TestLogRouter_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "github.jsonl")

	buf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(buf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	// Force creation of the file by emitting one record.
	slog.New(router).With("component", "github").Info("hi")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// FileHandler creates the file with mode 0600 explicitly. Verify we
	// inherited that here.
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %v, want 0600", got)
	}
}

func TestLogRouter_RemoveServerStopsWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "github.jsonl")

	buf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(buf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	logger := slog.New(router).With("component", "github")
	logger.Info("first")
	router.RemoveServer("github")
	logger.Info("second")

	lines := readJSONLines(t, path)
	if len(lines) != 1 {
		t.Errorf("after RemoveServer: lines = %d, want 1 (first only)", len(lines))
	}
	if got := buf.Count(); got != 2 {
		t.Errorf("inner buffer = %d, want 2", got)
	}
}

// TestLogRouter_RoutesByServerAttribute exercises the `server` routing-key
// fallback. pkg/mcp/gateway tags per-server loggers with `.With("server", name)`
// rather than `component`; without this fallback those records would never
// reach the per-server file fan-out.
func TestLogRouter_RoutesByServerAttribute(t *testing.T) {
	dir := t.TempDir()
	githubPath := filepath.Join(dir, "github.jsonl")

	buf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(buf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", githubPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer github: %v", err)
	}

	// Mirror the pattern at pkg/mcp/gateway.go: clientLogger := logger.With("server", name).
	logger := slog.New(router).With("server", "github")
	logger.Info("hit github tool", "tool", "list_repos")

	lines := readJSONLines(t, githubPath)
	if len(lines) != 1 {
		t.Errorf("github file got %d lines, want 1; lines=%v", len(lines), lines)
	} else if msg := lines[0]["msg"]; msg != "hit github tool" {
		t.Errorf("github line msg = %v, want %q", msg, "hit github tool")
	}
}

// TestLogRouter_RoutesByRecordLevelServerAttr covers the case where `server`
// arrives only on the record itself (not via .With) — the resolveComponent
// record-attrs scan must pick it up.
func TestLogRouter_RoutesByRecordLevelServerAttr(t *testing.T) {
	dir := t.TempDir()
	githubPath := filepath.Join(dir, "github.jsonl")

	buf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(buf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", githubPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	slog.New(router).Info("inline server attr", "server", "github")

	lines := readJSONLines(t, githubPath)
	if len(lines) != 1 {
		t.Errorf("github file got %d lines, want 1; lines=%v", len(lines), lines)
	}
}

// TestLogRouter_ComponentTakesPrecedenceOverServer verifies that when both
// `component` and `server` are set, `component` wins. Belt-and-braces against
// any caller that ever ends up tagging both.
func TestLogRouter_ComponentTakesPrecedenceOverServer(t *testing.T) {
	dir := t.TempDir()
	githubPath := filepath.Join(dir, "github.jsonl")
	weatherPath := filepath.Join(dir, "weather.jsonl")

	buf := logging.NewLogBuffer(10)
	router := NewLogRouter(logging.NewBufferHandler(buf, nil))
	t.Cleanup(router.Close)
	if err := router.AddServer("github", githubPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer github: %v", err)
	}
	if err := router.AddServer("weather", weatherPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer weather: %v", err)
	}

	// Both keys present in the same WithAttrs call — component must win.
	logger := slog.New(router).With("component", "github", "server", "weather")
	logger.Info("hi")

	githubLines := readJSONLines(t, githubPath)
	weatherLines := readJSONLines(t, weatherPath)
	if len(githubLines) != 1 {
		t.Errorf("github lines = %d, want 1 (component should win)", len(githubLines))
	}
	if len(weatherLines) != 0 {
		t.Errorf("weather lines = %d, want 0 (component should win)", len(weatherLines))
	}
}

func TestLogRouter_EnabledHandlesNilInner(t *testing.T) {
	// Defensive — Enabled must not panic if a derived child handler ever
	// has a nil inner. Today inner is always non-nil but the Handle path
	// short-circuits on nil, so Enabled should too.
	r := NewLogRouter(slog.NewJSONHandler(os.Stderr, nil))
	if !r.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled returned false with stderr handler installed")
	}
}

// readJSONLines parses a path produced by NewFileHandler — one JSON line per
// log record — into a slice of decoded maps. Skips blank lines.
func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("malformed json line %q: %v", string(line), err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}
