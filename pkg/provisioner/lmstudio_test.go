package provisioner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- LM Studio Tests (mcpServers wrapper, url-only native streamable HTTP) ---

func TestLMStudio_Link(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	l := newLMStudio()
	opts := defaultLinkOpts()

	writeTestJSON(t, configPath, map[string]any{})

	if err := l.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["url"] != "http://localhost:8180/mcp" {
		t.Errorf("expected url=http://localhost:8180/mcp, got %v", entry["url"])
	}
	// LM Studio's in-app editor strips unknown keys; the entry must be
	// url-only so a round-trip through the editor cannot change it.
	if len(entry) != 1 {
		t.Errorf("expected url-only entry, got %v", entry)
	}
	if strings.Contains(entry["url"].(string), "/sse") {
		t.Errorf("entry writes an /sse URL: %v", entry["url"])
	}
}

func TestLMStudio_Link_CreatesFileFromDirOnly(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	l := newLMStudio()
	if err := l.Link(configPath, defaultLinkOpts()); err != nil {
		t.Fatal(err)
	}

	entry := readTestJSON(t, configPath)["mcpServers"].(map[string]any)["gridctl"].(map[string]any)
	if entry["url"] != "http://localhost:8180/mcp" {
		t.Errorf("expected url written into fresh mcp.json, got %v", entry)
	}
}

func TestLMStudio_Link_PortGroupClientID(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	l := newLMStudio()
	opts := LinkOptions{
		GatewayURL: "http://localhost:9999/mcp?client=ci",
		Port:       9999,
		ServerName: "gridctl-dev",
		ClientID:   "ci",
		Group:      "dev",
	}

	if err := l.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	entry := readTestJSON(t, configPath)["mcpServers"].(map[string]any)["gridctl-dev"].(map[string]any)
	if entry["url"] != "http://localhost:9999/groups/dev/mcp?client=ci" {
		t.Errorf("expected group URL with client param, got %v", entry["url"])
	}
}

func TestLMStudio_Unlink(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"gridctl": map[string]any{"url": "http://localhost:8180/mcp"},
			"other":   map[string]any{"url": "https://example.com/mcp"},
		},
	})

	l := newLMStudio()
	if err := l.Unlink(configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}

	servers := readTestJSON(t, configPath)["mcpServers"].(map[string]any)
	if _, ok := servers["gridctl"]; ok {
		t.Error("gridctl should have been removed")
	}
	if _, ok := servers["other"]; !ok {
		t.Error("other should be preserved")
	}
}

func TestLMStudio_Link_ConflictWithoutForce(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"gridctl": map[string]any{"url": "https://example.com/mcp"},
		},
	})

	l := newLMStudio()
	err := l.Link(configPath, defaultLinkOpts())
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict on foreign entry, got %v", err)
	}
}

func TestLMStudio_Detect_DirExists(t *testing.T) {
	dir := t.TempDir()
	lmDir := filepath.Join(dir, ".lmstudio")
	if err := os.MkdirAll(lmDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(lmDir, "mcp.json")

	l := newLMStudio()
	l.paths = map[string]string{
		"linux":   configPath,
		"darwin":  configPath,
		"windows": configPath,
	}

	path, found := l.Detect()
	if !found {
		t.Error("expected Detect to find LM Studio via .lmstudio/ dir without mcp.json")
	}
	if path != configPath {
		t.Errorf("expected path %q, got %q", configPath, path)
	}
}

func TestLMStudio_Detect_NothingExists(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".lmstudio", "mcp.json")

	l := newLMStudio()
	l.paths = map[string]string{
		"linux":   configPath,
		"darwin":  configPath,
		"windows": configPath,
	}

	if _, found := l.Detect(); found {
		t.Error("expected Detect to not find LM Studio")
	}
}

// TestDryRunDiff_LMStudio is the regression guard for the getProvisionerBase
// type switch: a *LMStudio missing from that switch makes simulateLink a
// silent no-op, so the dry-run diff (and the web UI link preview) would show
// before == after while a real Link works.
func TestDryRunDiff_LMStudio(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	writeTestJSON(t, configPath, map[string]any{"mcpServers": map[string]any{}})

	l := newLMStudio()
	before, after, err := DryRunDiff(configPath, l, defaultLinkOpts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before, "gridctl") {
		t.Error("before should not contain gridctl")
	}
	if !strings.Contains(after, "gridctl") {
		t.Error("after should contain gridctl")
	}
	if !strings.Contains(after, "http://localhost:8180/mcp") {
		t.Error("after should contain the streamable HTTP gateway URL")
	}
}

func TestLMStudio_PostLinkNotes(t *testing.T) {
	notes := PostLinkNotesFor(newLMStudio())
	if len(notes) == 0 {
		t.Fatal("expected LM Studio to declare post-link notes")
	}
	joined := strings.Join(notes, " ")
	for _, want := range []string{"Program tab", "1234", "--group"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes should mention %q, got %v", want, notes)
		}
	}

	// Clients without the optional interface report none.
	if got := PostLinkNotesFor(newCursor()); got != nil {
		t.Errorf("expected nil notes for cursor, got %v", got)
	}
}

func TestLMStudio_NeedsBridge(t *testing.T) {
	if newLMStudio().NeedsBridge() {
		t.Error("LM Studio links over native streamable HTTP; NeedsBridge must be false")
	}
}
