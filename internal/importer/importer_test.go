package importer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/provisioner"
)

func entry(name string, raw map[string]any) provisioner.ServerEntry {
	return provisioner.ServerEntry{Name: name, Raw: raw}
}

func TestMapEntry_StdioShapes(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		entry   provisioner.ServerEntry
		wantCmd []string
	}{
		{
			name:    "standard command plus args",
			slug:    "claude",
			entry:   entry("github", map[string]any{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-github"}}),
			wantCmd: []string{"npx", "-y", "@modelcontextprotocol/server-github"},
		},
		{
			name:    "cursor command string with embedded args",
			slug:    "cursor",
			entry:   entry("weather", map[string]any{"command": "uvx weather-mcp --region 'us west'"}),
			wantCmd: []string{"uvx", "weather-mcp", "--region", "us west"},
		},
		{
			name:    "windows cmd /c wrapper unwrapped",
			slug:    "claude",
			entry:   entry("files", map[string]any{"command": "cmd", "args": []any{"/c", "npx", "-y", "server-filesystem"}}),
			wantCmd: []string{"npx", "-y", "server-filesystem"},
		},
		{
			name:    "goose cmd key",
			slug:    "goose",
			entry:   entry("tavily", map[string]any{"type": "stdio", "cmd": "npx", "args": []any{"-y", "tavily-mcp"}}),
			wantCmd: []string{"npx", "-y", "tavily-mcp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, err := MapEntry(tt.slug, tt.entry)
			if err != nil {
				t.Fatal(err)
			}
			if server.Transport != "stdio" {
				t.Errorf("transport = %q, want stdio", server.Transport)
			}
			if !reflect.DeepEqual(server.Command, tt.wantCmd) {
				t.Errorf("command = %v, want %v", server.Command, tt.wantCmd)
			}
		})
	}
}

func TestMapEntry_RemoteShapes(t *testing.T) {
	tests := []struct {
		name          string
		entry         provisioner.ServerEntry
		wantURL       string
		wantTransport string
	}{
		{"cursor url no type", entry("linear", map[string]any{"url": "https://mcp.linear.app/sse"}), "https://mcp.linear.app/sse", "sse"},
		{"plain url defaults http", entry("api", map[string]any{"url": "https://api.example.com/mcp"}), "https://api.example.com/mcp", "http"},
		{"windsurf serverUrl", entry("docs", map[string]any{"serverUrl": "https://docs.example.com/mcp"}), "https://docs.example.com/mcp", "http"},
		{"goose uri with streamable_http", entry("jira", map[string]any{"type": "streamable_http", "uri": "https://jira.example.com/mcp"}), "https://jira.example.com/mcp", "http"},
		{"gemini httpUrl wins and forces http", entry("g", map[string]any{"httpUrl": "https://g.example.com/x", "url": "https://g.example.com/sse"}), "https://g.example.com/x", "http"},
		{"cline streamableHttp camelCase", entry("c", map[string]any{"type": "streamableHttp", "url": "https://c.example.com/mcp"}), "https://c.example.com/mcp", "http"},
		{"roo streamable-http hyphenated", entry("r", map[string]any{"type": "streamable-http", "url": "https://r.example.com/mcp"}), "https://r.example.com/mcp", "http"},
		{"opencode remote type", entry("o", map[string]any{"type": "remote", "url": "https://o.example.com/mcp"}), "https://o.example.com/mcp", "http"},
		{"continue nested transport", entry("browser", map[string]any{"transport": map[string]any{"type": "sse", "url": "https://b.example.com/sse"}}), "https://b.example.com/sse", "sse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, err := MapEntry("test", tt.entry)
			if err != nil {
				t.Fatal(err)
			}
			if server.URL != tt.wantURL || server.Transport != tt.wantTransport {
				t.Errorf("got url=%q transport=%q, want %q/%q", server.URL, server.Transport, tt.wantURL, tt.wantTransport)
			}
		})
	}
}

func TestMapEntry_BridgeUnwrapping(t *testing.T) {
	server, warnings, err := MapEntry("claude", entry("linear", map[string]any{
		"command": "npx",
		"args":    []any{"-y", "mcp-remote", "https://mcp.linear.app/sse", "--allow-http"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if server.URL != "https://mcp.linear.app/sse" || server.Transport != "sse" || server.Command != nil {
		t.Errorf("bridge not unwrapped: %+v", server)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "mcp-remote") {
		t.Errorf("expected bridge conversion warning, got %v", warnings)
	}

	// The same shape wrapped in cmd /c (the Windows idiom).
	server, _, err = MapEntry("claude", entry("linear", map[string]any{
		"command": "cmd", "args": []any{"/c", "npx", "-y", "mcp-remote", "https://mcp.linear.app/sse"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if server.URL != "https://mcp.linear.app/sse" {
		t.Errorf("cmd /c wrapped bridge not unwrapped: %+v", server)
	}
}

func TestMapEntry_UnsupportedAndInvalid(t *testing.T) {
	if _, _, err := MapEntry("t", entry("ws", map[string]any{"type": "websocket", "url": "wss://x"})); err == nil {
		t.Error("websocket must be rejected")
	}
	if _, _, err := MapEntry("goose", entry("b", map[string]any{"type": "builtin", "name": "developer"})); err == nil {
		t.Error("goose builtin extension must be rejected")
	}
	if _, _, err := MapEntry("t", entry("empty", map[string]any{})); err == nil {
		t.Error("entry with neither command nor URL must be rejected")
	}
}

func TestMapEntry_HeadersToAuth(t *testing.T) {
	server, _, err := MapEntry("cursor", entry("api", map[string]any{
		"url":     "https://api.example.com/mcp",
		"headers": map[string]any{"Authorization": "Bearer sk-live-abc"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if server.Auth == nil || server.Auth.Type != "bearer" || server.Auth.Token != "sk-live-abc" {
		t.Errorf("auth = %+v, want bearer sk-live-abc", server.Auth)
	}

	server, _, err = MapEntry("cursor", entry("api", map[string]any{
		"url":     "https://api.example.com/mcp",
		"headers": map[string]any{"X-API-Key": "k-123"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if server.Auth == nil || server.Auth.Type != "header" || server.Auth.Header != "X-API-Key" {
		t.Errorf("auth = %+v, want custom header", server.Auth)
	}
}

func TestMapEntry_NameSanitization(t *testing.T) {
	server, warnings, err := MapEntry("t", entry("Figma Desktop", map[string]any{"command": "figma-mcp"}))
	if err != nil {
		t.Fatal(err)
	}
	if server.Name != "Figma_Desktop" {
		t.Errorf("name = %q, want Figma_Desktop", server.Name)
	}
	if len(warnings) == 0 {
		t.Error("expected rename warning")
	}
}

func TestIsGatewaySelfEntry(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		raw   map[string]any
		wantSelf bool
	}{
		{"link server name", "gridctl", map[string]any{"url": "http://localhost:8180/sse"}, true},
		{"gateway sse url other name", "gw", map[string]any{"url": "http://localhost:8180/sse"}, true},
		{"gateway mcp url", "gw", map[string]any{"url": "http://127.0.0.1:8180/mcp"}, true},
		{"gateway via serverUrl", "gw", map[string]any{"serverUrl": "http://localhost:8180/mcp"}, true},
		{"bridge to gateway", "gw", map[string]any{"command": "npx", "args": []any{"-y", "mcp-remote", "http://localhost:8180/sse", "--allow-http"}}, true},
		{"user localhost dev server", "mydev", map[string]any{"url": "http://localhost:3000/api/mcp-endpoint"}, false},
		{"remote server", "linear", map[string]any{"url": "https://mcp.linear.app/sse"}, false},
		{"stdio server", "github", map[string]any{"command": "npx", "args": []any{"-y", "server-github"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGatewaySelfEntry(tt.key, "gridctl", tt.raw); got != tt.wantSelf {
				t.Errorf("IsGatewaySelfEntry = %v, want %v", got, tt.wantSelf)
			}
		})
	}
}

func TestDedupe(t *testing.T) {
	github := func(slug string) Candidate {
		server, _, _ := MapEntry(slug, entry("github", map[string]any{"command": "npx", "args": []any{"-y", "server-github"}}))
		return Candidate{Name: "github", Server: server, FoundIn: []string{slug}, Source: slug}
	}
	variant, _, _ := MapEntry("zed", entry("github", map[string]any{"command": "docker", "args": []any{"run", "github-mcp"}}))

	out := Dedupe([]Candidate{
		github("claude"),
		github("cursor"),
		{Name: "github", Server: variant, FoundIn: []string{"zed"}, Source: "zed"},
		github("vscode"),
	})
	if len(out) != 2 {
		t.Fatalf("expected identical entries merged and the variant kept, got %d candidates", len(out))
	}
	if !reflect.DeepEqual(out[0].FoundIn, []string{"claude", "cursor", "vscode"}) {
		t.Errorf("provenance = %v", out[0].FoundIn)
	}
	if out[0].Source != "claude" {
		t.Errorf("canonical source = %q, want first in registry order", out[0].Source)
	}
	if len(out[1].Warnings) == 0 {
		t.Error("same-name different-definition candidate must carry a review warning")
	}
}

func TestClassifySecretKeys(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN":   "ghp_literal",
		"API_KEY":        "k-123",
		"REGION":         "us-east-1",
		"ENV_REF":        "${env:HOME}",
		"INPUT_REF":      "${input:apiKey}",
		"OP_REF":         "op://vault/item/field",
		"FILE_REF":       "${file:/etc/secret}",
		"CONT_REF":       "${{ secrets.NPM_TOKEN }}",
		"BARE_REF":       "$HOME_TOKEN",
		"WIN_REF":        "%APPDATA_KEY%",
		"VAR_REF":        "${var:EXISTING}",
		"EMPTY_PASSWORD": "",
	}
	got := ClassifySecretKeys(env)
	want := []string{"API_KEY", "GITHUB_TOKEN"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ClassifySecretKeys = %v, want %v (references and non-secret keys excluded)", got, want)
	}
}

func TestShellSplit(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"npx -y pkg", []string{"npx", "-y", "pkg"}},
		{`node "/Users/a b/server.js" --flag`, []string{"node", "/Users/a b/server.js", "--flag"}},
		{"single", []string{"single"}},
		{"a  'b c'  d", []string{"a", "b c", "d"}},
	}
	for _, tt := range tests {
		if got := shellSplit(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("shellSplit(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsReferenceValue(t *testing.T) {
	references := []string{
		"${env:HOME}", "${input:apiKey}", "${file:/etc/secret}", "${VAR}",
		"${VAR:-default}", "${{ secrets.NPM_TOKEN }}", "$HOME", "%APPDATA%",
		"op://vault/item/field", "${var:EXISTING}", "  ${env:PAD}  ",
	}
	for _, v := range references {
		if !IsReferenceValue(v) {
			t.Errorf("IsReferenceValue(%q) = false, want true", v)
		}
	}
	literals := []string{"ghp_abc123", "sk-live-xyz", "plain value", "http://x", "", "100%"}
	for _, v := range literals {
		if IsReferenceValue(v) {
			t.Errorf("IsReferenceValue(%q) = true, want false", v)
		}
	}
}
