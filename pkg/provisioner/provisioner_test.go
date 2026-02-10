package provisioner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testProvisioner creates a provisioner with paths pointing to a temp directory.
func testMCPServersProvisioner(t *testing.T, configFile string, bridge bool) (*mcpServersProvisioner, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, configFile)

	p := &mcpServersProvisioner{
		name:   "Test Client",
		slug:   "test",
		bridge: bridge,
		paths: map[string]string{
			"linux":   configPath,
			"darwin":  configPath,
			"windows": configPath,
		},
	}
	if bridge {
		p.buildEntry = func(opts LinkOptions) map[string]any {
			return bridgeConfig(opts.GatewayURL)
		}
	} else {
		p.buildEntry = func(opts LinkOptions) map[string]any {
			return sseConfig("serverUrl", opts.GatewayURL)
		}
	}

	return p, configPath
}

func defaultLinkOpts() LinkOptions {
	return LinkOptions{
		GatewayURL: "http://localhost:8180/sse",
		ServerName: "gridctl",
	}
}

// --- Registry Tests ---

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	slugs := r.AllSlugs()

	expected := []string{"claude", "cursor", "windsurf", "vscode", "continue", "cline", "anythingllm", "roo"}
	if len(slugs) != len(expected) {
		t.Fatalf("expected %d clients, got %d: %v", len(expected), len(slugs), slugs)
	}
	for i, s := range expected {
		if slugs[i] != s {
			t.Errorf("expected slug[%d]=%q, got %q", i, s, slugs[i])
		}
	}
}

func TestRegistry_FindBySlug(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		slug  string
		found bool
		name  string
	}{
		{"claude", true, "Claude Desktop"},
		{"cursor", true, "Cursor"},
		{"windsurf", true, "Windsurf"},
		{"vscode", true, "VS Code"},
		{"continue", true, "Continue"},
		{"cline", true, "Cline"},
		{"anythingllm", true, "AnythingLLM"},
		{"roo", true, "Roo Code"},
		{"nonexistent", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			prov, found := r.FindBySlug(tt.slug)
			if found != tt.found {
				t.Errorf("FindBySlug(%q): found=%v, want %v", tt.slug, found, tt.found)
			}
			if found && prov.Name() != tt.name {
				t.Errorf("FindBySlug(%q): name=%q, want %q", tt.slug, prov.Name(), tt.name)
			}
		})
	}
}

// --- Link/Unlink Tests (mcpServers-based clients) ---

func TestLink_ConfigDoesNotExist_CreatesFile(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["serverUrl"] != "http://localhost:8180/sse" {
		t.Errorf("unexpected entry: %v", entry)
	}
}

func TestLink_ConfigExists_NoEntry_AddsEntry(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", true)
	opts := defaultLinkOpts()

	// Write existing config with other servers
	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"other-server": map[string]any{"command": "other"},
		},
	})

	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)

	// Original entry preserved
	if _, ok := servers["other-server"]; !ok {
		t.Error("original 'other-server' entry was lost")
	}

	// New entry added
	entry := servers["gridctl"].(map[string]any)
	if entry["command"] != "npx" {
		t.Errorf("expected command=npx, got %v", entry["command"])
	}
}

func TestLink_Idempotent_SameConfig(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	// Link once
	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	// Link again — should be idempotent
	err := p.Link(configPath, opts)
	if err != ErrAlreadyLinked {
		t.Errorf("expected ErrAlreadyLinked, got: %v", err)
	}
}

func TestLink_DifferentPort_UpdatesEntry(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	// Link with port 8180
	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	// Link with port 9090 — should update (not conflict since it's localhost)
	opts.GatewayURL = "http://localhost:9090/sse"
	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["serverUrl"] != "http://localhost:9090/sse" {
		t.Errorf("expected updated URL, got: %v", entry["serverUrl"])
	}
}

func TestLink_Conflict_NonGridctlEntry(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	// Write config with a non-gridctl entry using the same name
	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"gridctl": map[string]any{
				"command": "some-other-tool",
				"args":    []any{"--flag"},
			},
		},
	})

	err := p.Link(configPath, opts)
	if err != ErrConflict {
		t.Errorf("expected ErrConflict, got: %v", err)
	}
}

func TestLink_Force_OverwritesConflict(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()
	opts.Force = true

	// Write conflicting entry
	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"gridctl": map[string]any{
				"command": "some-other-tool",
			},
		},
	})

	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["serverUrl"] != "http://localhost:8180/sse" {
		t.Errorf("expected overwritten entry, got: %v", entry)
	}
}

func TestLink_DryRun_NoFileModification(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()
	opts.DryRun = true

	// Write initial config
	writeTestJSON(t, configPath, map[string]any{"mcpServers": map[string]any{}})

	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	// Verify file wasn't modified
	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	if _, ok := servers["gridctl"]; ok {
		t.Error("dry run should not have modified the file")
	}
}

func TestLink_MalformedJSON_ReturnsError(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	if err := os.WriteFile(configPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	err := p.Link(configPath, opts)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestLink_JSONC_WithComments(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	// Write JSONC with comments
	if err := os.WriteFile(configPath, []byte(`{
  // MCP server configuration
  "mcpServers": {
    "existing": {
      "command": "test"
    }
  }
}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	if _, ok := servers["gridctl"]; !ok {
		t.Error("gridctl entry not added")
	}
	if _, ok := servers["existing"]; !ok {
		t.Error("existing entry was lost")
	}
}

func TestLink_JSONC_TrailingCommas(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)
	opts := defaultLinkOpts()

	if err := os.WriteFile(configPath, []byte(`{
  "mcpServers": {
    "existing": {
      "command": "test",
    },
  },
}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	if _, ok := servers["gridctl"]; !ok {
		t.Error("gridctl entry not added")
	}
}

func TestUnlink_EntryExists_RemovesOnly(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)

	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"gridctl":      map[string]any{"serverUrl": "http://localhost:8180/sse"},
			"other-server": map[string]any{"command": "other"},
		},
	})

	if err := p.Unlink(configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	if _, ok := servers["gridctl"]; ok {
		t.Error("gridctl entry should have been removed")
	}
	if _, ok := servers["other-server"]; !ok {
		t.Error("other-server entry should have been preserved")
	}
}

func TestUnlink_EntryMissing_NoOp(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)

	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"other-server": map[string]any{"command": "other"},
		},
	})

	err := p.Unlink(configPath, "gridctl")
	if err != ErrNotLinked {
		t.Errorf("expected ErrNotLinked, got: %v", err)
	}
}

func TestUnlink_FileDoesNotExist_NoOp(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)

	err := p.Unlink(configPath, "gridctl")
	if err != ErrNotLinked {
		t.Errorf("expected ErrNotLinked, got: %v", err)
	}
}

func TestIsLinked(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", false)

	// Not linked when file doesn't exist
	linked, err := p.IsLinked(configPath, "gridctl")
	if err != nil || linked {
		t.Errorf("expected not linked, got linked=%v err=%v", linked, err)
	}

	// Write config with entry
	writeTestJSON(t, configPath, map[string]any{
		"mcpServers": map[string]any{
			"gridctl": map[string]any{"serverUrl": "http://localhost:8180/sse"},
		},
	})

	linked, err = p.IsLinked(configPath, "gridctl")
	if err != nil || !linked {
		t.Errorf("expected linked, got linked=%v err=%v", linked, err)
	}
}

// --- Full link/unlink cycle ---

func TestLinkUnlinkCycle(t *testing.T) {
	p, configPath := testMCPServersProvisioner(t, "config.json", true)
	opts := defaultLinkOpts()

	// Link
	if err := p.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	// Verify linked
	linked, _ := p.IsLinked(configPath, "gridctl")
	if !linked {
		t.Error("expected linked after Link()")
	}

	// Unlink
	if err := p.Unlink(configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}

	// Verify unlinked
	linked, _ = p.IsLinked(configPath, "gridctl")
	if linked {
		t.Error("expected not linked after Unlink()")
	}
}

// --- VS Code Tests (different config structure) ---

func TestVSCode_Link(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	v := &VSCode{
		name: "VS Code",
		slug: "vscode",
		paths: map[string]string{
			"linux":   configPath,
			"darwin":  configPath,
			"windows": configPath,
		},
	}

	opts := defaultLinkOpts()
	if err := v.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["servers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["type"] != "sse" {
		t.Errorf("expected type=sse, got %v", entry["type"])
	}
	if entry["url"] != "http://localhost:8180/sse" {
		t.Errorf("expected url, got %v", entry["url"])
	}
}

func TestVSCode_Unlink(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	v := &VSCode{
		name: "VS Code",
		slug: "vscode",
		paths: map[string]string{
			"linux":   configPath,
			"darwin":  configPath,
			"windows": configPath,
		},
	}

	writeTestJSON(t, configPath, map[string]any{
		"servers": map[string]any{
			"gridctl": map[string]any{"type": "sse", "url": "http://localhost:8180/sse"},
			"other":   map[string]any{"type": "sse", "url": "http://other:3000"},
		},
	})

	if err := v.Unlink(configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["servers"].(map[string]any)
	if _, ok := servers["gridctl"]; ok {
		t.Error("gridctl should have been removed")
	}
	if _, ok := servers["other"]; !ok {
		t.Error("other entry should be preserved")
	}
}

// --- Continue.dev Tests (array-based config) ---

func TestContinueDev_Link(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	c := newContinueDev()
	opts := defaultLinkOpts()

	if err := c.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	exp := data["experimental"].(map[string]any)
	servers := exp["mcpServers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	entry := servers[0].(map[string]any)
	if entry["name"] != "gridctl" {
		t.Errorf("expected name=gridctl, got %v", entry["name"])
	}
	transport := entry["transport"].(map[string]any)
	if transport["type"] != "sse" || transport["url"] != "http://localhost:8180/sse" {
		t.Errorf("unexpected transport: %v", transport)
	}
}

func TestContinueDev_Link_Idempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	c := newContinueDev()
	opts := defaultLinkOpts()

	if err := c.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	err := c.Link(configPath, opts)
	if err != ErrAlreadyLinked {
		t.Errorf("expected ErrAlreadyLinked, got: %v", err)
	}
}

func TestContinueDev_Unlink(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	writeTestJSON(t, configPath, map[string]any{
		"experimental": map[string]any{
			"mcpServers": []any{
				map[string]any{
					"name":      "gridctl",
					"transport": map[string]any{"type": "sse", "url": "http://localhost:8180/sse"},
				},
				map[string]any{
					"name":      "other",
					"transport": map[string]any{"type": "sse", "url": "http://other:3000/sse"},
				},
			},
		},
	})

	c := newContinueDev()
	if err := c.Unlink(configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	exp := data["experimental"].(map[string]any)
	servers := exp["mcpServers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("expected 1 server after unlink, got %d", len(servers))
	}
	entry := servers[0].(map[string]any)
	if entry["name"] != "other" {
		t.Errorf("wrong server remaining: %v", entry["name"])
	}
}

// --- AnythingLLM Tests (flat map) ---

func TestAnythingLLM_Link(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	a := newAnythingLLM()
	opts := defaultLinkOpts()

	writeTestJSON(t, configPath, map[string]any{})

	if err := a.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	entry := data["gridctl"].(map[string]any)
	if entry["command"] != "npx" {
		t.Errorf("expected command=npx, got %v", entry["command"])
	}
}

func TestAnythingLLM_Unlink(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	writeTestJSON(t, configPath, map[string]any{
		"gridctl": map[string]any{"command": "npx"},
		"other":   map[string]any{"command": "other"},
	})

	a := newAnythingLLM()
	if err := a.Unlink(configPath, "gridctl"); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	if _, ok := data["gridctl"]; ok {
		t.Error("gridctl should have been removed")
	}
	if _, ok := data["other"]; !ok {
		t.Error("other should be preserved")
	}
}

// --- Roo Code Tests ---

func TestRooCode_Link(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp_settings.json")

	r := newRooCode()
	opts := defaultLinkOpts()

	if err := r.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["url"] != "http://localhost:8180/sse" {
		t.Errorf("expected url, got %v", entry["url"])
	}
	if entry["transportType"] != "sse" {
		t.Errorf("expected transportType=sse, got %v", entry["transportType"])
	}
	if entry["disabled"] != false {
		t.Errorf("expected disabled=false, got %v", entry["disabled"])
	}
}

// --- Cline Tests ---

func TestCline_Link(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "cline_mcp_settings.json")

	c := newCline()
	opts := defaultLinkOpts()

	if err := c.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)
	if entry["command"] != "npx" {
		t.Errorf("expected command=npx, got %v", entry["command"])
	}
	if entry["disabled"] != false {
		t.Errorf("expected disabled=false, got %v", entry["disabled"])
	}
	if _, ok := entry["alwaysAllow"]; !ok {
		t.Error("expected alwaysAllow key")
	}
}

// --- Backup Tests ---

func TestBackup_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"original": true}`), 0644); err != nil {
		t.Fatal(err)
	}

	backupPath, err := createBackup(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if backupPath == "" {
		t.Fatal("expected backup path")
	}
	if !strings.Contains(backupPath, ".gridctl-backup-") {
		t.Errorf("unexpected backup name: %s", backupPath)
	}

	// Verify backup content matches original
	data, _ := os.ReadFile(backupPath)
	if string(data) != `{"original": true}` {
		t.Errorf("backup content mismatch: %s", string(data))
	}
}

func TestBackup_PrunesOldBackups(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create 5 backups
	for i := 0; i < 5; i++ {
		suffix := backupSuffix + "2026020" + string(rune('1'+i)) + "-120000"
		if err := os.WriteFile(configPath+suffix, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Run prune
	if err := pruneBackups(configPath); err != nil {
		t.Fatal(err)
	}

	// Should have maxBackups remaining
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".gridctl-backup-") {
			count++
		}
	}
	if count != maxBackups {
		t.Errorf("expected %d backups after prune, got %d", maxBackups, count)
	}
}

func TestBackup_FileDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "nonexistent.json")

	backupPath, err := createBackup(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if backupPath != "" {
		t.Error("expected empty backup path for nonexistent file")
	}
}

// --- JSON Handling Tests ---

func TestReadJSONFile_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	if err := os.WriteFile(path, []byte(`{"key": "value"}`), 0644); err != nil {
		t.Fatal(err)
	}

	data, hasComments, err := readJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hasComments {
		t.Error("expected no comments")
	}
	if data["key"] != "value" {
		t.Errorf("expected key=value, got %v", data["key"])
	}
}

func TestReadJSONFile_JSONC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonc")
	if err := os.WriteFile(path, []byte(`{
  // This is a comment
  "key": "value",  // inline comment
  "list": [1, 2, 3,],  // trailing comma
}`), 0644); err != nil {
		t.Fatal(err)
	}

	data, hasComments, err := readJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !hasComments {
		t.Error("expected hasComments=true")
	}
	if data["key"] != "value" {
		t.Errorf("expected key=value, got %v", data["key"])
	}
}

func TestReadJSONFile_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not json at all`), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := readJSONFile(path)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestReadOrCreateJSONFile_FileDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")

	data, _, err := readOrCreateJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty map, got %v", data)
	}
}

func TestWriteJSONFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "deep", "config.json")

	err := writeJSONFile(path, map[string]any{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, path)
	if data["key"] != "value" {
		t.Errorf("expected key=value, got %v", data["key"])
	}
}

// --- Bridge Detection Tests ---

func TestNpxAvailable(t *testing.T) {
	// Save and restore
	original := NpxAvailable
	defer func() { NpxAvailable = original }()

	NpxAvailable = func() bool { return true }
	if !NpxAvailable() {
		t.Error("expected npx available")
	}

	NpxAvailable = func() bool { return false }
	if NpxAvailable() {
		t.Error("expected npx not available")
	}
}

// --- DryRunDiff Tests ---

func TestDryRunDiff_mcpServers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	writeTestJSON(t, configPath, map[string]any{"mcpServers": map[string]any{}})

	p := newClaudeDesktop()
	opts := defaultLinkOpts()

	before, after, err := DryRunDiff(configPath, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, `"mcpServers"`) {
		t.Error("before should contain mcpServers")
	}
	if !strings.Contains(after, `"gridctl"`) {
		t.Error("after should contain gridctl entry")
	}
}

func TestDryRunDiff_VSCode(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	writeTestJSON(t, configPath, map[string]any{})

	v := newVSCode()
	opts := defaultLinkOpts()

	_, after, err := DryRunDiff(configPath, v, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, `"servers"`) {
		t.Error("after should contain servers key")
	}
}

// --- Transport Description Tests ---

func TestTransportDescription(t *testing.T) {
	if TransportDescription(true) != "mcp-remote bridge" {
		t.Error("expected mcp-remote bridge")
	}
	if TransportDescription(false) != "native SSE" {
		t.Error("expected native SSE")
	}
}

func TestGatewayURL(t *testing.T) {
	url := GatewayURL(9090)
	if url != "http://localhost:9090/sse" {
		t.Errorf("expected http://localhost:9090/sse, got %s", url)
	}
}

// --- Client Interface Compliance ---

func TestClientProvisioners_ImplementInterface(t *testing.T) {
	clients := []ClientProvisioner{
		newClaudeDesktop(),
		newCursor(),
		newWindsurf(),
		newVSCode(),
		newContinueDev(),
		newCline(),
		newAnythingLLM(),
		newRooCode(),
	}

	for _, c := range clients {
		t.Run(c.Slug(), func(t *testing.T) {
			if c.Name() == "" {
				t.Error("Name() should not be empty")
			}
			if c.Slug() == "" {
				t.Error("Slug() should not be empty")
			}
		})
	}
}

// --- Bridge Config Tests ---

func TestBridgeConfig(t *testing.T) {
	cfg := bridgeConfig("http://localhost:8180/sse")
	if cfg["command"] != "npx" {
		t.Errorf("expected command=npx, got %v", cfg["command"])
	}
	args := cfg["args"].([]any)
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
	if args[0] != "-y" || args[1] != "mcp-remote" || args[2] != "http://localhost:8180/sse" || args[3] != "--allow-http" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestSSEConfig(t *testing.T) {
	cfg := sseConfig("serverUrl", "http://localhost:8180/sse")
	if cfg["serverUrl"] != "http://localhost:8180/sse" {
		t.Errorf("unexpected config: %v", cfg)
	}
}

// --- Extra Keys Tests ---

func TestExtraKeys_Cline(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "settings.json")

	c := newCline()
	opts := defaultLinkOpts()
	if err := c.Link(configPath, opts); err != nil {
		t.Fatal(err)
	}

	data := readTestJSON(t, configPath)
	servers := data["mcpServers"].(map[string]any)
	entry := servers["gridctl"].(map[string]any)

	if entry["disabled"] != false {
		t.Errorf("expected disabled=false")
	}
	arr, ok := entry["alwaysAllow"].([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("expected empty alwaysAllow array")
	}
}

// --- Helper Functions ---

func readTestJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parsing %s: %v\ncontent: %s", path, err, string(data))
	}
	return result
}

func writeTestJSON(t *testing.T, path string, data map[string]any) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
