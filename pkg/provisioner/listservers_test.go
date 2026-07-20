package provisioner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture writes content to a temp file and returns its path.
func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func entryNames(entries []ServerEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func TestListServers_StandardMCPServers(t *testing.T) {
	path := writeFixture(t, "claude_desktop_config.json", `{
  "globalShortcut": "",
  "mcpServers": {
    "github": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"], "env": {"GITHUB_TOKEN": "ghp_abc"}},
    "weather": {"command": "uvx", "args": ["weather-mcp"]},
    "broken": "not-an-object"
  }
}`)
	entries, err := newClaudeDesktop().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryNames(entries); len(got) != 2 || got[0] != "github" || got[1] != "weather" {
		t.Errorf("entries = %v, want [github weather] (non-objects skipped, sorted)", got)
	}
	if cmd, _ := entries[0].Raw["command"].(string); cmd != "npx" {
		t.Errorf("raw entry not preserved: %+v", entries[0].Raw)
	}
}

func TestListServers_JSONCAndBOM(t *testing.T) {
	path := writeFixture(t, "mcp.json", "\xef\xbb\xbf"+`{
  // user comment
  "mcpServers": {
    "linear": {"url": "https://mcp.linear.app/sse"}, // trailing comment
  },
}`)
	entries, err := newCursor().ListServers(path)
	if err != nil {
		t.Fatalf("JSONC with BOM must parse: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "linear" {
		t.Errorf("entries = %v", entryNames(entries))
	}
}

func TestListServers_MissingEmptyAndCorrupt(t *testing.T) {
	prov := newCursor()

	if entries, err := prov.ListServers(filepath.Join(t.TempDir(), "absent.json")); err != nil || entries != nil {
		t.Errorf("missing file: entries=%v err=%v, want nil/nil", entries, err)
	}
	if entries, err := prov.ListServers(writeFixture(t, "empty.json", "  \n")); err != nil || entries != nil {
		t.Errorf("empty file: entries=%v err=%v, want nil/nil", entries, err)
	}
	if _, err := prov.ListServers(writeFixture(t, "trunc.json", `{"mcpServers": {"a": {`)); err == nil {
		t.Error("truncated file must return an error so the scanner can warn and continue")
	}
}

func TestListServers_VSCodeServersKey(t *testing.T) {
	path := writeFixture(t, "mcp.json", `{
  "inputs": [{"id": "apiKey", "type": "promptString", "password": true}],
  "servers": {
    "fetch": {"type": "http", "url": "https://api.example.com/mcp", "headers": {"X-Key": "${input:apiKey}"}}
  }
}`)
	entries, err := newVSCode().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "fetch" {
		t.Errorf("entries = %v", entryNames(entries))
	}
}

func TestListServers_ZedContextServers(t *testing.T) {
	path := writeFixture(t, "settings.json", `{
  "theme": "One Dark",
  "context_servers": {"postgres": {"command": "pg-mcp", "args": []}}
}`)
	entries, err := newZed().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "postgres" {
		t.Errorf("entries = %v", entryNames(entries))
	}
}

func TestListServers_OpenCodeMCPKey(t *testing.T) {
	path := writeFixture(t, "opencode.json", `{
  "mcp": {"docs": {"type": "remote", "url": "https://docs.example.com/mcp"}}
}`)
	entries, err := newOpenCode().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "docs" {
		t.Errorf("entries = %v", entryNames(entries))
	}
}

func TestListServers_GrokTOML(t *testing.T) {
	path := writeFixture(t, "config.toml", `
[mcp_servers.jira]
url = "https://jira.example.com/mcp"
type = "http"
enabled = true
`)
	entries, err := newGrokBuild().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "jira" {
		t.Errorf("entries = %v", entryNames(entries))
	}
	if u, _ := entries[0].Raw["url"].(string); u != "https://jira.example.com/mcp" {
		t.Errorf("raw = %+v", entries[0].Raw)
	}
}

func TestListServers_GooseYAMLExtensions(t *testing.T) {
	path := writeFixture(t, "config.yaml", `
extensions:
  tavily:
    name: tavily
    type: stdio
    cmd: npx
    args: ["-y", "tavily-mcp"]
    envs: {TAVILY_API_KEY: "tvly-abc"}
    timeout: 300
`)
	entries, err := newGoose().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "tavily" {
		t.Fatalf("entries = %v", entryNames(entries))
	}
	if cmd, _ := entries[0].Raw["cmd"].(string); cmd != "npx" {
		t.Errorf("goose raw entry not normalized to string map: %+v", entries[0].Raw)
	}
}

func TestListServers_ContinueArray(t *testing.T) {
	path := writeFixture(t, "config.json", `{
  "experimental": {
    "mcpServers": [
      {"name": "browser", "transport": {"type": "sse", "url": "https://b.example.com/sse"}},
      {"transport": {"type": "stdio"}},
      "junk"
    ]
  }
}`)
	entries, err := newContinueDev().ListServers(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "browser" {
		t.Errorf("nameless and non-object elements must be skipped, got %v", entryNames(entries))
	}
}

func TestClaudeCodeDetect_HonorsClaudeConfigDir(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(custom, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	path, found := newClaudeCode().Detect()
	if !found || path != custom {
		t.Errorf("Detect = (%q, %v), want CLAUDE_CONFIG_DIR file %q", path, found, custom)
	}

	// Unset or pointing at a dir without the file falls through to defaults.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if path, _ := newClaudeCode().Detect(); path == custom {
		t.Error("stale custom path returned after env changed")
	}
}

func TestCreateBackup(t *testing.T) {
	path := writeFixture(t, "stack.yaml", "name: demo\n")
	backup, err := CreateBackup(path)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" || !strings.Contains(backup, ".gridctl-backup-") {
		t.Errorf("backup path = %q", backup)
	}
	data, err := os.ReadFile(backup)
	if err != nil || string(data) != "name: demo\n" {
		t.Errorf("backup content = %q err=%v", data, err)
	}
	// A missing source is not an error: there is nothing to protect.
	if got, err := CreateBackup(filepath.Join(t.TempDir(), "absent.yaml")); err != nil || got != "" {
		t.Errorf("missing source: (%q, %v), want empty/nil", got, err)
	}
}
