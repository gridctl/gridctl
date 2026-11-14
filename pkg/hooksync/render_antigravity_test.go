package hooksync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const antigravityScriptDir = "/home/u/.gridctl/hooks/block-force-push@antigravity"

func renderAntigravityRaw(t *testing.T, raw, name string) rendered {
	t.Helper()
	target, ok := FindTarget("antigravity")
	if !ok {
		t.Fatal("antigravity is not a target")
	}
	r, err := renderFor(target, []byte(raw), name, antigravityScriptDir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// antigravityValueBytes is the key value as the golden files hold it.
func antigravityValueBytes(t *testing.T, r rendered) []byte {
	t.Helper()
	data, err := json.MarshalIndent(r.Value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestAntigravityTools_KeysAreClaudeTools(t *testing.T) {
	seen := map[string]string{}
	for claude, mapped := range antigravityTools {
		if !claudeToolNames[claude] {
			t.Errorf("antigravityTools key %q is not a Claude Code tool", claude)
		}
		for _, tool := range strings.Split(mapped, "|") {
			if !identifierAlternation.MatchString(tool) {
				t.Errorf("antigravityTools[%q] names %q, which is not a plain tool name", claude, tool)
			}
			if prev, dup := seen[tool]; dup {
				t.Errorf("Antigravity tool %q is mapped from %q and %q", tool, prev, claude)
			}
			seen[tool] = claude
		}
	}
}

func TestRenderAntigravity_Golden(t *testing.T) {
	cases := map[string]string{
		"block-force-push": identityFixtures["example"],
		"every-key":        strings.Replace(identityFixtures["every-key"], `"name":"keys"`, `"name":"every-key"`, 1),
		"two-groups-event": identityFixtures["two-groups-event"],
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r := renderAntigravityRaw(t, raw, name)
			if r.Value == nil || len(r.Bytes) != 0 || len(r.Groups) != 0 {
				t.Fatalf("render = %+v, want a key value only", r)
			}
			got := antigravityValueBytes(t, r)
			assertHookGolden(t, filepath.Join("testdata", "antigravity", name+".json"), got)
			enabled := bytes.Index(got, []byte(`"enabled"`))
			pre := bytes.Index(got, []byte(`"PreToolUse"`))
			post := bytes.Index(got, []byte(`"PostToolUse"`))
			if enabled != 4 || (pre >= 0 && post >= 0 && pre > post) {
				t.Fatal("key order must be enabled, PreToolUse, PostToolUse")
			}
		})
	}
}

func TestRenderAntigravity_Shape(t *testing.T) {
	every := renderAntigravityRaw(t, strings.Replace(identityFixtures["every-key"], `"name":"keys"`, `"name":"every-key"`, 1), "every-key")
	q := func(s string) string { return "'" + s + "'" }
	want := antigravityHook{
		Enabled: true,
		PreToolUse: []antigravityGroup{{
			Matcher: "*",
			Hooks: []antigravityHandler{
				{Type: "command", Command: q(filepath.Join(antigravityScriptDir, "scripts", "a.sh")), Timeout: 600},
				{Type: "command", Command: q(filepath.Join(antigravityScriptDir, "scripts", "b.sh")), Timeout: 2},
			},
		}},
		PostToolUse: []antigravityGroup{{
			Matcher: "write_to_file|replace_file_content|multi_replace_file_content",
			Hooks: []antigravityHandler{
				{Type: "command", Command: q(filepath.Join(antigravityScriptDir, "scripts", "fmt.sh")) + " '--check' 'a b'", Timeout: 30},
			},
		}},
	}
	if !reflect.DeepEqual(every.Value, want) {
		t.Fatalf("value = %+v\nwant    %+v", every.Value, want)
	}
	wantDetail := `dropped groups: Stop; lossy antigravity render: dropped handler keys async, asyncRewake, custom, onFailure, statusMessage`
	if every.detail() != wantDetail {
		t.Fatalf("detail = %q, want %q", every.detail(), wantDetail)
	}

	example := renderAntigravityRaw(t, identityFixtures["example"], "block-force-push")
	if example.detail() != `dropped if "Bash(git push *)": the handler runs on every matched call` {
		t.Fatalf("detail = %q", example.detail())
	}

	two := renderAntigravityRaw(t, identityFixtures["two-groups-event"], "two")
	groups := two.Value.(antigravityHook).PreToolUse
	if len(groups) != 2 || groups[0].Matcher != "run_command" || groups[1].Matcher != "*" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestRenderAntigravity_Matchers(t *testing.T) {
	cases := map[string]string{
		"":               "*",
		"*":              "*",
		"Bash":           "run_command",
		"Bash|Read":      "run_command|view_file",
		"Edit|Edit":      "replace_file_content|multi_replace_file_content",
		"Glob|Grep":      "find_by_name|grep_search",
		"WebFetch|Write": "read_url_content|write_to_file",
		"WebSearch":      "search_web",
	}
	for matcher, want := range cases {
		raw := `{"name":"h","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":` + strconvQuote(matcher) + `,"hooks":[{"type":"command","command":"./a.sh"}]}]}}`
		r := renderAntigravityRaw(t, raw, "h")
		if len(r.Unexpressible) != 0 {
			t.Fatalf("matcher %q: %+v", matcher, r)
		}
		if got := r.Value.(antigravityHook).PreToolUse[0].Matcher; got != want {
			t.Errorf("matcher %q rendered as %q, want %q", matcher, got, want)
		}
	}
	absent := renderAntigravityRaw(t, `{"name":"h","description":"d","dialect":"claude-code","hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"./a.sh"}]}]}}`, "h")
	if got := absent.Value.(antigravityHook).PostToolUse[0].Matcher; got != "*" {
		t.Fatalf("absent matcher rendered as %q", got)
	}
}

func TestRenderAntigravity_Unexpressible(t *testing.T) {
	cases := []struct {
		name     string
		hooks    string
		rendered bool
		reasons  string
		detail   string
	}{
		{
			name:    "stop only",
			hooks:   `{"Stop":[{"hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `event "Stop" has no antigravity mapping`,
		},
		{
			name:    "unmapped token",
			hooks:   `{"PreToolUse":[{"matcher":"Bash|Task","hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `matcher token "Task" has no antigravity tool`,
		},
		{
			name:    "regex matcher",
			hooks:   `{"PreToolUse":[{"matcher":"Notebook.*","hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `matcher "Notebook.*" is not a plain tool alternation`,
		},
		{
			name:     "mixed",
			hooks:    `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./a.sh"}]},{"matcher":"Task","hooks":[{"type":"command","command":"./a.sh"}]}],"Stop":[{"hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			rendered: true,
			reasons:  `matcher token "Task" has no antigravity tool; event "Stop" has no antigravity mapping`,
			detail:   `dropped groups: matcher "Task"; Stop`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"name":"h","description":"d","dialect":"claude-code","hooks":` + tc.hooks + `}`
			r := renderAntigravityRaw(t, raw, "h")
			if r.expressible() != tc.rendered {
				t.Fatalf("expressible = %v, want %v", r.expressible(), tc.rendered)
			}
			if got := strings.Join(r.Unexpressible, "; "); got != tc.reasons {
				t.Fatalf("reasons = %q, want %q", got, tc.reasons)
			}
			if tc.rendered && r.detail() != tc.detail {
				t.Fatalf("detail = %q, want %q", r.detail(), tc.detail)
			}
		})
	}
}

func TestRenderAntigravity_RefusesUnquotable(t *testing.T) {
	target, _ := FindTarget("antigravity")
	for _, bad := range []string{"it's", "line\nbreak", "nul\x00byte"} {
		raw, err := json.Marshal(map[string]any{
			"name": "h", "description": "d", "dialect": "claude-code",
			"hooks": map[string]any{"PreToolUse": []any{map[string]any{
				"matcher": "Bash",
				"hooks":   []any{map[string]any{"type": "command", "command": "./a.sh", "args": []string{"ok", bad}}},
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = renderFor(target, raw, "h", antigravityScriptDir)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", bad)) {
			t.Fatalf("argument %q: err = %v, want a refusal naming the value", bad, err)
		}
	}
	dir := "/home/o'brien/.gridctl/hooks/h@antigravity"
	_, err := renderFor(target, []byte(identityFixtures["unit"]), "h", dir)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", filepath.Join(dir, "scripts", "run.sh"))) {
		t.Fatalf("script directory with a quote: err = %v", err)
	}
	if _, err := renderFor(target, []byte(identityFixtures["unit"]), `x"y`, antigravityScriptDir); err == nil {
		t.Fatal("an invalid hook name must not render")
	}
}

func TestRenderAntigravity_ArgumentsStayLiteral(t *testing.T) {
	raw := `{"name":"h","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./a.sh","args":["$HOME","a b; rm -rf /","` + "`id`" + `","\"q\""]}]}]}}`
	r := renderAntigravityRaw(t, raw, "h")
	got := r.Value.(antigravityHook).PreToolUse[0].Hooks[0].Command
	want := `'` + filepath.Join(antigravityScriptDir, "a.sh") + `' '$HOME' 'a b; rm -rf /' '` + "`id`" + `' '"q"'`
	if got != want {
		t.Fatalf("command = %s\nwant      %s", got, want)
	}
}

func TestRenderAntigravity_Deterministic(t *testing.T) {
	for name, raw := range identityFixtures {
		first := antigravityValueBytes(t, renderAntigravityRaw(t, raw, "block-force-push"))
		for range 5 {
			again := antigravityValueBytes(t, renderAntigravityRaw(t, raw, "block-force-push"))
			if !bytes.Equal(first, again) {
				t.Fatalf("%s: render is not deterministic", name)
			}
		}
	}
}
