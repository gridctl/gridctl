package importer

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
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
		name     string
		key      string
		raw      map[string]any
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

func TestMapEntry_OpenCodeNativeArgv(t *testing.T) {
	raw := map[string]any{
		"type":    "local",
		"command": []any{"node", "/synthetic/a b.js", "", "--label=a b", `C:\synthetic\x`},
		"environment": map[string]any{
			"REGION": "us-west",
		},
	}
	before := cloneRaw(raw)
	server, warnings, err := MapEntry("opencode", entry("alpha", raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatalf("raw input mutated: %#v", raw)
	}
	want := []string{"node", "/synthetic/a b.js", "", "--label=a b", `C:\synthetic\x`}
	if !reflect.DeepEqual(server.Command, want) {
		t.Fatalf("command = %#v, want %#v", server.Command, want)
	}
	if server.Transport != "stdio" {
		t.Fatalf("transport = %q", server.Transport)
	}
	if server.Env["REGION"] != "us-west" {
		t.Fatalf("env = %#v", server.Env)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if server.Command[2] != "" {
		t.Fatal("empty non-executable argument was dropped")
	}
}

func TestMapEntry_OpenCodeNativePreservesQuotesAndBackslashes(t *testing.T) {
	server, _, err := MapEntry("opencode", entry("q", map[string]any{
		"command": []any{"node", `a"b`, `keep\slash`, `C:\synthetic\x`},
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"node", `a"b`, `keep\slash`, `C:\synthetic\x`}
	if !reflect.DeepEqual(server.Command, want) {
		t.Fatalf("command = %#v", server.Command)
	}
}

func TestMapEntry_OpenCodeNativeRejects(t *testing.T) {
	tests := []struct {
		name   string
		raw    map[string]any
		reason string
		detail string
	}{
		{"empty array", map[string]any{"command": []any{}}, SkipUnsupported, "command array is empty"},
		{"mixed types", map[string]any{"command": []any{"node", 1}}, SkipUnsupported, "command array must contain only strings"},
		{"null command", map[string]any{"command": nil}, SkipUnsupported, "command is null"},
		{"blank executable", map[string]any{"command": []any{"", "node"}}, SkipUnsupported, "command executable is blank"},
		{"whitespace executable", map[string]any{"command": []any{"  ", "node"}}, SkipUnsupported, "command executable is blank"},
		{"array plus args", map[string]any{"command": []any{"node", "a.js"}, "args": []any{"--flag"}}, SkipAmbiguous, "cannot be combined with args"},
		{
			"conflicting environment",
			map[string]any{"command": []any{"node", "a.js"}, "environment": map[string]any{"A": "1"}, "env": map[string]any{"B": "2"}},
			SkipAmbiguous,
			"conflicts with env or envs",
		},
		{"null environment", map[string]any{"command": []any{"node", "a.js"}, "environment": nil}, SkipUnsupported, "environment is null"},
		{"numeric environment", map[string]any{"command": []any{"node", "a.js"}, "environment": map[string]any{"N": 7}}, SkipUnsupported, "environment values must be strings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := MapEntry("opencode", entry("bad", tt.raw))
			var me *MapError
			if !errors.As(err, &me) {
				t.Fatalf("err = %v, want MapError", err)
			}
			if me.Reason != tt.reason || !strings.Contains(me.Detail, tt.detail) {
				t.Fatalf("got reason=%q detail=%q", me.Reason, me.Detail)
			}
		})
	}
}

func TestMapEntry_OpenCodeNativeReferences(t *testing.T) {
	t.Setenv("FIXTURE_TOKEN", "synthetic-secret-value")
	file := filepath.Join(t.TempDir(), "synthetic.txt")
	if err := os.WriteFile(file, []byte("SYNTHETIC_FILE_CONTENTS_MUST_NOT_APPEAR"), 0o600); err != nil {
		t.Fatal(err)
	}

	server, _, err := MapEntry("opencode", entry("ok", map[string]any{
		"command":     []any{"node", "{env:FIXTURE_TOKEN}"},
		"environment": map[string]any{"API_KEY": "{env:FIXTURE_TOKEN}", "REGION": "us-west", "PLAIN": "ordinary-literal"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if server.Command[1] != "${FIXTURE_TOKEN}" || server.Env["API_KEY"] != "${FIXTURE_TOKEN}" {
		t.Fatalf("converted = command %#v env %#v", server.Command, server.Env)
	}
	if strings.Contains(server.Command[1], "synthetic-secret-value") || strings.Contains(server.Env["API_KEY"], "synthetic-secret-value") {
		t.Fatal("mapping resolved FIXTURE_TOKEN")
	}
	t.Setenv("FIXTURE_TOKEN", "")
	unset, _, err := MapEntry("opencode", entry("unset", map[string]any{
		"command":     []any{"node", "{env:FIXTURE_TOKEN}"},
		"environment": map[string]any{"API_KEY": "{env:FIXTURE_TOKEN}"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if unset.Command[1] != "${FIXTURE_TOKEN}" || unset.Env["API_KEY"] != "${FIXTURE_TOKEN}" {
		t.Fatalf("unset mapping resolved or rewrote the reference: %+v", unset)
	}
	keys := ClassifySecretKeys(server.Env)
	for _, key := range keys {
		if key == "API_KEY" {
			t.Fatal("converted {env} reference was classified as a literal secret")
		}
	}
	if server.Env["REGION"] != "us-west" || server.Env["PLAIN"] != "ordinary-literal" {
		t.Fatalf("literals changed: %#v", server.Env)
	}

	secretEnv := map[string]string{"API_KEY": "synthetic-token-marker", "REGION": "us-west"}
	if got := ClassifySecretKeys(secretEnv); !reflect.DeepEqual(got, []string{"API_KEY"}) {
		t.Fatalf("literal classification = %v", got)
	}

	skips := []struct {
		name   string
		value  string
		detail string
		absent []string
	}{
		{"invalid name", "{env:FOO-BAR}", "not a shell identifier", []string{"FOO-BAR", "${FOO-BAR}"}},
		{"leading digit", "{env:9NAME}", "not a shell identifier", []string{"9NAME"}},
		{"file", "{file:./synthetic.txt}", "native {file} expressions are not imported", []string{"synthetic.txt", "SYNTHETIC_FILE_CONTENTS_MUST_NOT_APPEAR"}},
		{"embedded", "prefix-{env:FIXTURE_TOKEN}", "embedded or unsupported native {env} expression", []string{"FIXTURE_TOKEN", "synthetic-secret-value"}},
	}
	for _, tt := range skips {
		t.Run(tt.name, func(t *testing.T) {
			cases := []map[string]any{
				{
					"command":     []any{"node", "a.js"},
					"environment": map[string]any{"API_KEY": tt.value},
				},
				{"command": []any{"node", tt.value}},
			}
			for _, raw := range cases {
				_, _, err := MapEntry("opencode", entry("bad", raw))
				var me *MapError
				if !errors.As(err, &me) || me.Reason != SkipNativeExpression || !strings.Contains(me.Detail, tt.detail) {
					t.Fatalf("err = %v", err)
				}
				for _, absent := range tt.absent {
					if strings.Contains(me.Detail, absent) || strings.Contains(err.Error(), "SYNTHETIC_FILE_CONTENTS") {
						t.Fatalf("detail disclosed %q: %s", absent, me.Detail)
					}
				}
			}
		})
	}

	kept, _, err := MapEntry("opencode", entry("refs", map[string]any{
		"command": []any{"node", "a.js"},
		"environment": map[string]any{
			"A": "${VAR}",
			"B": "${var:KEY}",
			"C": "${env:HOME}",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if kept.Env["A"] != "${VAR}" || kept.Env["B"] != "${var:KEY}" || kept.Env["C"] != "${env:HOME}" {
		t.Fatalf("existing references changed: %#v", kept.Env)
	}
	if got := ClassifySecretKeys(kept.Env); len(got) != 0 {
		t.Fatalf("references classified as secrets: %v", got)
	}

	expanded, _, empty := config.ExpandString("${FIXTURE_TOKEN}", func(name string) (string, bool) {
		if name == "FIXTURE_TOKEN" {
			return "synthetic-secret-value", true
		}
		return "", false
	})
	if expanded != "synthetic-secret-value" || len(empty) != 0 {
		t.Fatalf("set expansion = %q empty=%v", expanded, empty)
	}
	expanded, _, empty = config.ExpandString("${FIXTURE_TOKEN}", func(string) (string, bool) { return "", false })
	if expanded != "" || len(empty) != 1 || empty[0] != "FIXTURE_TOKEN" {
		t.Fatalf("unset expansion = %q empty=%v", expanded, empty)
	}
}

func TestMapEntry_OpenCodeNativeBridgeStaysLocal(t *testing.T) {
	raw := map[string]any{
		"command":     []any{"npx", "-y", "mcp-remote", "https://mcp.linear.app/sse"},
		"environment": map[string]any{"REGION": "us-west"},
	}
	server, warnings, err := MapEntry("opencode", entry("bridge", raw))
	if err != nil {
		t.Fatal(err)
	}
	if server.URL != "" || !reflect.DeepEqual(server.Command, []string{"npx", "-y", "mcp-remote", "https://mcp.linear.app/sse"}) {
		t.Fatalf("native bridge array was rewritten: %+v", server)
	}
	if server.Env["REGION"] != "us-west" {
		t.Fatalf("env = %#v", server.Env)
	}
	for _, w := range warnings {
		if strings.Contains(w, "mcp-remote") {
			t.Fatalf("native array was unwrapped: %v", warnings)
		}
	}

	cmdArray := map[string]any{"command": []any{"cmd", "/c", "npx", "-y", "server-filesystem"}}
	server, _, err = MapEntry("opencode", entry("files", cmdArray))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(server.Command, []string{"cmd", "/c", "npx", "-y", "server-filesystem"}) {
		t.Fatalf("cmd array unwrapped: %#v", server.Command)
	}
}

func TestMapEntry_OpenCodeLegacyStringStillUnwraps(t *testing.T) {
	server, _, err := MapEntry("opencode", entry("files", map[string]any{
		"command": "cmd",
		"args":    []any{"/c", "npx", "-y", "server-filesystem"},
		"env":     map[string]any{"REGION": "us-west"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(server.Command, []string{"npx", "-y", "server-filesystem"}) {
		t.Fatalf("legacy string command = %#v", server.Command)
	}
	if server.Env["REGION"] != "us-west" {
		t.Fatalf("legacy env = %#v", server.Env)
	}

	bridged, warnings, err := MapEntry("opencode", entry("bridge", map[string]any{
		"command":     "npx",
		"args":        []any{"-y", "mcp-remote", "https://mcp.linear.app/sse"},
		"environment": map[string]any{"REGION": "us-west"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if bridged.URL != "https://mcp.linear.app/sse" || bridged.Env != nil {
		t.Fatalf("legacy bridge = %+v", bridged)
	}
	if len(warnings) < 2 || !strings.Contains(warnings[1], "environment was not transferred") {
		t.Fatalf("warnings = %v", warnings)
	}

	kept, _, err := MapEntry("opencode", entry("legacy-ref", map[string]any{
		"command": "synthetic-server",
		"env":     map[string]any{"API_KEY": "{env:FOO-BAR}"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if kept.Env["API_KEY"] != "{env:FOO-BAR}" {
		t.Fatalf("legacy reference rewritten: %#v", kept.Env)
	}
}

func TestMapEntry_OpenCodeStringEnvironment(t *testing.T) {
	server, _, err := MapEntry("opencode", entry("local", map[string]any{
		"command":     "synthetic-server",
		"environment": map[string]any{"REGION": "us-west", "API_KEY": "{env:FIXTURE_TOKEN}"},
		"enabled":     true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(server.Command, []string{"synthetic-server"}) {
		t.Fatalf("command = %#v", server.Command)
	}
	if server.Env["REGION"] != "us-west" || server.Env["API_KEY"] != "${FIXTURE_TOKEN}" {
		t.Fatalf("env = %#v", server.Env)
	}
}

func TestMapEntry_OpenCodeConstraintsApplyToEveryShape(t *testing.T) {
	cases := []struct {
		name   string
		slug   string
		raw    map[string]any
		reason string
		absent string
	}{
		{
			name:   "string command with environment and enabled false",
			slug:   "opencode",
			raw:    map[string]any{"command": "synthetic-server", "environment": map[string]any{"API_KEY": "{env:FIXTURE_TOKEN}"}, "enabled": false},
			reason: SkipDisabled,
			absent: "FIXTURE_TOKEN",
		},
		{
			name:   "string command with environment and cwd",
			slug:   "opencode",
			raw:    map[string]any{"command": "synthetic-server", "environment": map[string]any{"REGION": "us-west"}, "cwd": "./synthetic-workspace"},
			reason: SkipUntransferredOption,
			absent: "synthetic-workspace",
		},
		{
			name:   "legacy string command with enabled false",
			slug:   "opencode",
			raw:    map[string]any{"command": "synthetic-server", "env": map[string]any{"REGION": "us-west"}, "enabled": false},
			reason: SkipDisabled,
		},
		{
			name:   "legacy string command with cwd",
			slug:   "opencode",
			raw:    map[string]any{"command": "synthetic-server", "cwd": "./synthetic-workspace"},
			reason: SkipUntransferredOption,
			absent: "synthetic-workspace",
		},
		{
			name:   "remote entry with enabled false",
			slug:   "opencode",
			raw:    map[string]any{"type": "remote", "url": "https://secret.example/mcp?token=synthetic", "enabled": false},
			reason: SkipDisabled,
			absent: "secret.example",
		},
		{
			name:   "remote entry with cwd",
			slug:   "opencode",
			raw:    map[string]any{"type": "remote", "url": "https://docs.example/mcp", "cwd": "./synthetic-workspace"},
			reason: SkipUntransferredOption,
			absent: "synthetic-workspace",
		},
		{
			name:   "enabled is not a boolean",
			slug:   "opencode",
			raw:    map[string]any{"command": "synthetic-server", "enabled": "no"},
			reason: SkipUnsupported,
			absent: "no",
		},
		{
			name:   "cwd is not a string",
			slug:   "opencode",
			raw:    map[string]any{"url": "https://docs.example/mcp", "cwd": 12},
			reason: SkipUnsupported,
			absent: "12",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := MapEntry(tt.slug, entry("srv", tt.raw))
			var me *MapError
			if !errors.As(err, &me) || me.Reason != tt.reason {
				t.Fatalf("reason = %v, want %s", err, tt.reason)
			}
			if tt.absent != "" && strings.Contains(me.Detail, tt.absent) {
				t.Fatalf("detail disclosed %q: %s", tt.absent, me.Detail)
			}
		})
	}

	server, _, err := MapEntry("opencode", entry("on", map[string]any{
		"type":    "remote",
		"url":     "https://docs.example/mcp",
		"enabled": true,
	}))
	if err != nil || server.URL != "https://docs.example/mcp" {
		t.Fatalf("enabled remote = %+v err=%v", server, err)
	}

	server, _, err = MapEntry("cursor", entry("off", map[string]any{
		"url":     "https://docs.example/mcp",
		"enabled": false,
		"cwd":     "./synthetic-workspace",
	}))
	if err != nil || server.URL != "https://docs.example/mcp" {
		t.Fatalf("non-opencode constraints = %+v err=%v", server, err)
	}

	server, _, err = MapEntry("opencode", entry("blank", map[string]any{
		"command": "synthetic-server",
		"cwd":     "   ",
		"enabled": nil,
	}))
	if err != nil || !reflect.DeepEqual(server.Command, []string{"synthetic-server"}) {
		t.Fatalf("blank cwd = %+v err=%v", server, err)
	}

	server, _, err = MapEntry("opencode", entry("hdr", map[string]any{
		"type":    "remote",
		"url":     "https://docs.example/mcp",
		"headers": map[string]any{"Authorization": "Bearer synthetic-token"},
	}))
	if err != nil || server.Auth == nil || server.Auth.Type != "bearer" || server.URL != "https://docs.example/mcp" {
		t.Fatalf("remote headers = %+v err=%v", server, err)
	}
}

func TestMapEntry_OpenCodeEnabledCwdTimeout(t *testing.T) {
	_, _, err := MapEntry("opencode", entry("off", map[string]any{
		"command": []any{"node", "a.js"},
		"enabled": false,
	}))
	var me *MapError
	if !errors.As(err, &me) || me.Reason != SkipDisabled || !strings.Contains(me.Detail, "enabled is false") {
		t.Fatalf("enabled false: %v", err)
	}

	server, _, err := MapEntry("opencode", entry("on", map[string]any{
		"command": []any{"node", "a.js"},
		"enabled": true,
	}))
	if err != nil || len(server.Command) != 2 {
		t.Fatalf("enabled true: %+v err=%v", server, err)
	}
	server, _, err = MapEntry("opencode", entry("absent", map[string]any{
		"command": []any{"node", "a.js"},
	}))
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = MapEntry("opencode", entry("cwd", map[string]any{
		"command": []any{"node", "a.js"},
		"cwd":     "./synthetic-workspace",
	}))
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption || strings.Contains(me.Detail, "synthetic-workspace") {
		t.Fatalf("cwd: %v", err)
	}

	server, warnings, err := MapEntry("opencode", entry("slow", map[string]any{
		"command": []any{"node", "a.js"},
		"timeout": 7000,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(server.Command) != 2 {
		t.Fatalf("timeout import dropped the command: %#v", server.Command)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "timeout was not transferred") || strings.Contains(warnings[0], "7000") {
		t.Fatalf("warnings = %v", warnings)
	}

	_, _, err = MapEntry("opencode", entry("auth", map[string]any{
		"command": []any{"node", "a.js"},
		"oauth":   map[string]any{"clientId": "synthetic"},
	}))
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption || strings.Contains(me.Detail, "synthetic") {
		t.Fatalf("oauth: %v", err)
	}
	_, _, err = MapEntry("opencode", entry("hdr", map[string]any{
		"command": []any{"node", "a.js"},
		"headers": map[string]any{"Authorization": "Bearer synthetic-secret"},
	}))
	if !errors.As(err, &me) || me.Reason != SkipUntransferredOption || strings.Contains(me.Detail, "synthetic-secret") {
		t.Fatalf("headers: %v", err)
	}
}

func TestCommandSlice_AndGatewayIdentity_IgnoreNativeArrays(t *testing.T) {
	raw := map[string]any{"command": []any{"npx", "-y", "mcp-remote", "http://localhost:8180/sse"}}
	if got := commandSlice(raw); got != nil {
		t.Fatalf("commandSlice = %#v, want nil", got)
	}
	if IsGatewaySelfEntry("bridge", "gridctl", raw) {
		t.Fatal("native array must not match the legacy bridge filter")
	}
	if !IsGatewaySelfEntry("gridctl", "gridctl", raw) {
		t.Fatal("name match must still flag the entry")
	}
	stringBridge := map[string]any{"command": "npx", "args": []any{"-y", "mcp-remote", "http://localhost:8180/sse"}}
	if !IsGatewaySelfEntry("bridge", "gridctl", stringBridge) {
		t.Fatal("legacy string bridge filter changed")
	}
	if got := commandSlice(map[string]any{"command": "npx", "args": []any{"-y", "pkg"}}); !reflect.DeepEqual(got, []string{"npx", "-y", "pkg"}) {
		t.Fatalf("string commandSlice = %#v", got)
	}
}

func TestDedupe_RetainsSourcePathsWithoutChangingIdentity(t *testing.T) {
	base := func(path, region string) Candidate {
		server, _, err := MapEntry("opencode", entry("alpha", map[string]any{
			"command":     []any{"node", "a.js"},
			"environment": map[string]any{"REGION": region},
		}))
		if err != nil {
			t.Fatal(err)
		}
		return Candidate{Name: "alpha", Server: server, FoundIn: []string{"opencode"}, Source: "opencode", SourcePath: path, SourcePaths: []string{path}}
	}
	out := Dedupe([]Candidate{base("/synthetic/a.json", "us-west"), base("/synthetic/b.json", "eu-central")})
	if len(out) != 1 {
		t.Fatalf("env differences still dedupe, got %d", len(out))
	}
	if out[0].Source != "opencode" || !reflect.DeepEqual(out[0].FoundIn, []string{"opencode"}) {
		t.Fatalf("slug provenance changed: %+v", out[0])
	}
	if out[0].SourcePath != "/synthetic/a.json" || !reflect.DeepEqual(out[0].SourcePaths, []string{"/synthetic/a.json", "/synthetic/b.json"}) {
		t.Fatalf("paths = %q %#v", out[0].SourcePath, out[0].SourcePaths)
	}
}

func TestMapEntry_NonOpenCodeArrayStillRejected(t *testing.T) {
	_, _, err := MapEntry("claude", entry("x", map[string]any{"command": []any{"node", "a.js"}}))
	if err == nil || !strings.Contains(err.Error(), "neither a command nor a URL") {
		t.Fatalf("err = %v", err)
	}
}

func cloneRaw(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case map[string]any:
			out[k] = cloneRaw(val)
		case []any:
			cp := make([]any, len(val))
			copy(cp, val)
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
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
