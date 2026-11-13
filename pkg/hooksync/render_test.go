package hooksync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// legacyRenderDefinition is the Claude Code render as it stood before
// the intermediate form. It stays here as the reference the identity
// render must reproduce byte for byte.
func legacyRenderDefinition(raw []byte, name, scriptDir string) ([]renderedGroup, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hooks must be an object")
	}
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	var groups []renderedGroup
	for _, event := range events {
		for _, item := range hooks[event].([]any) {
			group := item.(map[string]any)
			out := map[string]any{}
			if matcher, ok := group["matcher"]; ok {
				out["matcher"] = matcher
			}
			var handlers []any
			for _, h := range group["hooks"].([]any) {
				handler := h.(map[string]any)
				command, _ := handler["command"].(string)
				rel := strings.TrimPrefix(command, "./")
				rendered := map[string]any{
					"type":    "command",
					"command": filepath.Join(scriptDir, filepath.FromSlash(rel)),
				}
				if args, ok := handler["args"]; ok {
					rendered["args"] = args
				} else {
					rendered["args"] = []any{}
				}
				for key, value := range handler {
					if key == "type" || key == "command" || key == "args" {
						continue
					}
					rendered[key] = value
				}
				if _, ok := rendered["statusMessage"]; !ok {
					rendered["statusMessage"] = "gridctl hook " + name
				}
				handlers = append(handlers, rendered)
			}
			if handlers == nil {
				handlers = []any{}
			}
			out["hooks"] = handlers
			groups = append(groups, renderedGroup{Event: event, Value: out})
		}
	}
	return groups, nil
}

// identityFixtures are the hook definitions the existing unit,
// characterization, and integration tests project, plus every handler
// key the identity render passes through.
var identityFixtures = map[string]string{
	"unit":             `{"name":"block","description":"block force push","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`,
	"integration":      `{"name":"block-force-push","description":"Deny git push --force","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/block.sh","args":[],"if":"Bash(git *)"}]}]}}`,
	"example":          `{"name":"block-force-push","description":"Deny git push --force before it runs","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/block-force-push.sh","if":"Bash(git push *)"}]}]}}`,
	"every-key":        `{"name":"keys","description":"d","dialect":"claude-code","hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"scripts/fmt.sh","args":["--check","a b"],"timeout":30,"statusMessage":"formatting","async":true,"asyncRewake":false,"onFailure":"ignore","custom":{"x":1}}]}],"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/a.sh"},{"type":"command","command":"./scripts/b.sh","timeout":1.5}]}],"Stop":[{"matcher":"","hooks":[{"type":"command","command":"./scripts/stop.sh"}]}]}}`,
	"two-groups-event": `{"name":"two","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./a.sh"}]},{"matcher":"*","hooks":[{"type":"command","command":"./b.sh"}]}]}}`,
}

func TestRenderIdentity_MatchesLegacyGroupsHash(t *testing.T) {
	scriptDir := "/home/u/.gridctl/hooks/block"
	for name, raw := range identityFixtures {
		t.Run(name, func(t *testing.T) {
			legacy, err := legacyRenderDefinition([]byte(raw), "block", scriptDir)
			if err != nil {
				t.Fatal(err)
			}
			r, err := renderFor(Targets()[0], []byte(raw), "block", scriptDir)
			if err != nil {
				t.Fatal(err)
			}
			if r.Bytes != nil || r.detail() != "" || len(r.Unexpressible) > 0 {
				t.Fatalf("identity render reported loss: %+v", r)
			}
			want, err := groupsHash(legacy)
			if err != nil {
				t.Fatal(err)
			}
			got, err := groupsHash(r.Groups)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("groupsHash = %s, want %s", got, want)
			}
			if canonicalText(r.Groups) != canonicalText(legacy) {
				t.Fatalf("groups differ:\n got %s\nwant %s", canonicalText(r.Groups), canonicalText(legacy))
			}
		})
	}
}

func TestMatcherTokens(t *testing.T) {
	cases := []struct {
		name    string
		group   specGroup
		tokens  []string
		all, ok bool
	}{
		{"absent", specGroup{}, nil, true, true},
		{"empty", specGroup{HasMatcher: true, Matcher: ""}, nil, true, true},
		{"star", specGroup{HasMatcher: true, Matcher: "*"}, nil, true, true},
		{"one", specGroup{HasMatcher: true, Matcher: "Bash"}, []string{"Bash"}, false, true},
		{"alternation", specGroup{HasMatcher: true, Matcher: "Write|Edit"}, []string{"Write", "Edit"}, false, true},
		{"mcp", specGroup{HasMatcher: true, Matcher: "mcp__github__create_issue"}, []string{"mcp__github__create_issue"}, false, true},
		{"regex", specGroup{HasMatcher: true, Matcher: "Notebook.*"}, nil, false, false},
		{"spaces", specGroup{HasMatcher: true, Matcher: "Write | Edit"}, nil, false, false},
		{"empty token", specGroup{HasMatcher: true, Matcher: "Write||Edit"}, nil, false, false},
		{"not a string", specGroup{HasMatcher: true, Matcher: json.Number("1")}, nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens, all, ok := matcherTokens(tc.group)
			if !reflect.DeepEqual(tokens, tc.tokens) || all != tc.all || ok != tc.ok {
				t.Fatalf("matcherTokens = %v, %v, %v; want %v, %v, %v", tokens, all, ok, tc.tokens, tc.all, tc.ok)
			}
		})
	}
}

// claudeToolNames are the Claude Code built-in tool names a matcher can
// name. A tool table key outside this set is a typo.
var claudeToolNames = map[string]bool{
	"Agent": true, "Bash": true, "Edit": true, "Glob": true, "Grep": true,
	"LS": true, "MultiEdit": true, "NotebookEdit": true, "NotebookRead": true,
	"Read": true, "Task": true, "TodoWrite": true, "WebFetch": true,
	"WebSearch": true, "Write": true,
}

func TestToolTables_KeysAreClaudeTools(t *testing.T) {
	seen := map[string]string{}
	for claude, id := range openCodeTools {
		if !claudeToolNames[claude] {
			t.Errorf("openCodeTools key %q is not a Claude Code tool", claude)
		}
		if prev, dup := seen[id]; dup {
			t.Errorf("OpenCode tool %q is mapped from %q and %q; the reverse map would be ambiguous", id, prev, claude)
		}
		seen[id] = claude
	}
	for tool := range openCodeInputKeys {
		if _, ok := seen[tool]; !ok {
			t.Errorf("openCodeInputKeys names %q, which is not a mapped OpenCode tool", tool)
		}
	}
}

const openCodeScriptDir = "/home/u/.gridctl/hooks/block-force-push@opencode"

func renderOpenCodeRaw(t *testing.T, raw, name string) rendered {
	t.Helper()
	target, ok := FindTarget("opencode")
	if !ok {
		t.Fatal("opencode is not a target")
	}
	r, err := renderFor(target, []byte(raw), name, openCodeScriptDir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func assertHookGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("GRIDCTL_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (regenerate with GRIDCTL_UPDATE_GOLDEN=1): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the render:\n%s", path, got)
	}
}

// pluginRules extracts the JSON rule table from a rendered plugin.
func pluginRules(t *testing.T, plugin []byte) []openCodeRule {
	t.Helper()
	text := string(plugin)
	start := strings.Index(text, "const RULES: Rule[] = ")
	if start < 0 {
		t.Fatalf("plugin has no rule table:\n%s", plugin)
	}
	start += len("const RULES: Rule[] = ")
	dec := json.NewDecoder(strings.NewReader(text[start:]))
	var rules []openCodeRule
	if err := dec.Decode(&rules); err != nil {
		t.Fatalf("decoding rule table: %v", err)
	}
	return rules
}

func TestRenderOpenCode_Golden(t *testing.T) {
	cases := map[string]string{
		"block-force-push": identityFixtures["example"],
		"every-key":        strings.Replace(identityFixtures["every-key"], `"name":"keys"`, `"name":"every-key"`, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r := renderOpenCodeRaw(t, raw, name)
			if len(r.Bytes) == 0 {
				t.Fatalf("no plugin rendered: %+v", r)
			}
			assertHookGolden(t, filepath.Join("testdata", "opencode", name+".ts"), r.Bytes)
			rules := pluginRules(t, r.Bytes)
			if len(rules) == 0 {
				t.Fatal("rule table is empty")
			}
			for _, rule := range rules {
				for _, h := range rule.Handlers {
					if h.TimeoutMs <= 0 {
						t.Errorf("rule %s handler %s has timeoutMs %d", rule.Event, h.Script, h.TimeoutMs)
					}
					if !filepath.IsAbs(h.Script) || !strings.HasPrefix(h.Script, openCodeScriptDir+string(filepath.Separator)) {
						t.Errorf("handler script %q is not inside the script copy", h.Script)
					}
					if h.Args == nil {
						t.Errorf("handler %s has null args", h.Script)
					}
				}
			}
		})
	}
}

func TestRenderOpenCode_RuleTable(t *testing.T) {
	r := renderOpenCodeRaw(t, identityFixtures["example"], "block-force-push")
	rules := pluginRules(t, r.Bytes)
	want := []openCodeRule{{
		Event: "PreToolUse",
		Tools: []string{"shell"},
		Handlers: []openCodeHandler{{
			Script:    filepath.Join(openCodeScriptDir, "scripts", "block-force-push.sh"),
			Args:      []string{},
			TimeoutMs: 600000,
		}},
	}}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %+v, want %+v", rules, want)
	}
	if r.detail() != `dropped if "Bash(git push *)": the handler runs on every matched call` {
		t.Fatalf("detail = %q", r.detail())
	}

	every := renderOpenCodeRaw(t, strings.Replace(identityFixtures["every-key"], `"name":"keys"`, `"name":"every-key"`, 1), "every-key")
	rules = pluginRules(t, every.Bytes)
	if len(rules) != 2 {
		t.Fatalf("rules = %+v", rules)
	}
	post, pre := rules[0], rules[1]
	if post.Event != "PostToolUse" || !reflect.DeepEqual(post.Tools, []string{"write", "edit"}) {
		t.Fatalf("post rule = %+v", post)
	}
	if post.Handlers[0].TimeoutMs != 30000 || !reflect.DeepEqual(post.Handlers[0].Args, []string{"--check", "a b"}) {
		t.Fatalf("post handler = %+v", post.Handlers[0])
	}
	if pre.Event != "PreToolUse" || pre.Tools != nil || len(pre.Handlers) != 2 {
		t.Fatalf("pre rule = %+v", pre)
	}
	if pre.Handlers[0].TimeoutMs != openCodeDefaultTimeoutMs || pre.Handlers[1].TimeoutMs != 1500 {
		t.Fatalf("pre timeouts = %+v", pre.Handlers)
	}
	if !strings.Contains(string(every.Bytes), `"tools": null`) {
		t.Fatal("an every-tool rule must carry tools: null")
	}
	wantDetail := `dropped groups: Stop; lossy opencode render: dropped handler keys async, asyncRewake, custom, onFailure, statusMessage`
	if every.detail() != wantDetail {
		t.Fatalf("detail = %q, want %q", every.detail(), wantDetail)
	}
}

func TestRenderOpenCode_TimeoutCapped(t *testing.T) {
	raw := `{"name":"slow","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./a.sh","timeout":99999999}]}]}}`
	rules := pluginRules(t, renderOpenCodeRaw(t, raw, "slow").Bytes)
	if got := rules[0].Handlers[0].TimeoutMs; got != openCodeMaxTimeoutMs {
		t.Fatalf("timeoutMs = %d, want the setTimeout cap", got)
	}
}

func TestRenderOpenCode_Deterministic(t *testing.T) {
	for name, raw := range identityFixtures {
		first := renderOpenCodeRaw(t, raw, "block-force-push")
		for range 5 {
			again := renderOpenCodeRaw(t, raw, "block-force-push")
			if !bytes.Equal(first.Bytes, again.Bytes) || first.detail() != again.detail() {
				t.Fatalf("%s: render is not deterministic", name)
			}
		}
	}
}

var jsStringLiteral = regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`)

func TestRenderOpenCode_NameOnlyInStringLiteralsAndHeader(t *testing.T) {
	name := "zqx-vbnk"
	raw := strings.Replace(identityFixtures["every-key"], `"name":"keys"`, `"name":"`+name+`"`, 1)
	r := renderOpenCodeRaw(t, raw, name)
	text := string(r.Bytes)
	header, body, ok := strings.Cut(text, "\n")
	if !ok || !strings.Contains(header, `for hook "`+name+`"`) {
		t.Fatalf("header = %q", header)
	}
	if !strings.HasPrefix(header, "// gridctl hook plugin v1 for hook ") {
		t.Fatalf("header = %q", header)
	}
	if strings.Contains(body, "'") || strings.Contains(body, "`") {
		t.Fatal("template must use double-quoted string literals only")
	}
	stripped := jsStringLiteral.ReplaceAllString(body, `""`)
	for _, part := range strings.Split(name, "-") {
		if strings.Contains(stripped, part) {
			t.Fatalf("hook name fragment %q appears outside a string literal:\n%s", part, stripped)
		}
	}
	if !strings.Contains(body, `"`+name+`"`) {
		t.Fatal("hook name is not carried in a string literal")
	}
}

func TestRenderOpenCode_NoDynamicCode(t *testing.T) {
	r := renderOpenCodeRaw(t, identityFixtures["every-key"], "block-force-push")
	text := string(r.Bytes)
	for _, banned := range []string{"eval(", "new Function", "Function(", "fetch(", "http:", "https:", "require(", "import(", "spawnSync", "execSync", "exec(", "shell: true", "process.env"} {
		if strings.Contains(text, banned) {
			t.Errorf("plugin contains %q", banned)
		}
	}
	for _, want := range []string{
		`import { Plugin } from "@opencode/plugin"`,
		`import { spawn } from "node:child_process"`,
		`export default Plugin.define({`,
		`ctx.tool.hook("execute.before"`,
		`ctx.tool.hook("execute.after"`,
		`shell: false`,
		`child.kill("SIGKILL")`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("plugin is missing %q", want)
		}
	}
}

func TestRenderOpenCode_Unexpressible(t *testing.T) {
	cases := []struct {
		name       string
		hooks      string
		bytes      bool
		reasons    string
		dropped    string
		dropDetail string
	}{
		{
			name:    "stop only",
			hooks:   `{"Stop":[{"hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `event "Stop" has no opencode mapping`,
		},
		{
			name:    "task matcher",
			hooks:   `{"PreToolUse":[{"matcher":"Task","hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `matcher token "Task" has no opencode tool`,
		},
		{
			name:    "one unmapped token in an alternation",
			hooks:   `{"PreToolUse":[{"matcher":"Bash|Task","hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `matcher token "Task" has no opencode tool`,
		},
		{
			name:    "regex",
			hooks:   `{"PreToolUse":[{"matcher":"Notebook.*","hooks":[{"type":"command","command":"./a.sh"}]}]}`,
			reasons: `matcher "Notebook.*" is not a plain tool alternation`,
		},
		{
			name:       "mixed",
			hooks:      `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./a.sh"}]},{"matcher":"Task","hooks":[{"type":"command","command":"./a.sh","if":"Task(x)"}]}],"Stop":[{"hooks":[{"type":"command","command":"./a.sh","statusMessage":"s"}]}]}`,
			bytes:      true,
			reasons:    `matcher token "Task" has no opencode tool; event "Stop" has no opencode mapping`,
			dropped:    `matcher "Task"; Stop`,
			dropDetail: `dropped groups: matcher "Task"; Stop`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"name":"h","description":"d","dialect":"claude-code","hooks":` + tc.hooks + `}`
			r := renderOpenCodeRaw(t, raw, "h")
			if (len(r.Bytes) > 0) != tc.bytes || r.expressible() != tc.bytes {
				t.Fatalf("bytes = %d, want rendered=%v", len(r.Bytes), tc.bytes)
			}
			if got := strings.Join(r.Unexpressible, "; "); got != tc.reasons {
				t.Fatalf("reasons = %q, want %q", got, tc.reasons)
			}
			if tc.bytes {
				if got := strings.Join(r.DroppedGroups, "; "); got != tc.dropped {
					t.Fatalf("dropped = %q, want %q", got, tc.dropped)
				}
				// Keys and if rules of a dropped group are not reported.
				if r.detail() != tc.dropDetail {
					t.Fatalf("detail = %q, want %q", r.detail(), tc.dropDetail)
				}
			}
		})
	}
}

func TestRenderOpenCode_RefusesInvalidName(t *testing.T) {
	target, _ := FindTarget("opencode")
	_, err := renderFor(target, []byte(identityFixtures["example"]), `x"; process.exit(1); "`, openCodeScriptDir)
	if err == nil {
		t.Fatal("an invalid hook name must not render")
	}
}
