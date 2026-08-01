package agentsync

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
)

const testAgentMD = `---
name: reviewer
description: Reviews pull requests for style and correctness
tools: Read, Grep, Bash
model: sonnet
hooks:
  PreToolUse: echo hi
color: blue
---

Review the diff carefully.

Focus on correctness first.
`

func parseTestAgent(t *testing.T) *skills.AgentDefinition {
	t.Helper()
	def, err := skills.ParseAgentMD([]byte(testAgentMD))
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func TestRenderOpenCode_Golden(t *testing.T) {
	def := parseTestAgent(t)
	got, err := renderOpenCode(def)
	if err != nil {
		t.Fatal(err)
	}

	want := `---
description: Reviews pull requests for style and correctness
mode: subagent
---

Review the diff carefully.

Focus on correctness first.
`
	if string(got.Bytes) != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", got.Bytes, want)
	}
	// OpenCode has no name key and no tools mapping; everything beyond
	// description drops, sorted.
	wantDropped := []string{"color", "hooks", "model", "tools"}
	if strings.Join(got.Dropped, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("dropped = %v, want %v", got.Dropped, wantDropped)
	}
}

func TestRenderCopilot_Golden(t *testing.T) {
	def := parseTestAgent(t)
	got, err := renderCopilot(def)
	if err != nil {
		t.Fatal(err)
	}

	want := `---
name: reviewer
description: Reviews pull requests for style and correctness
tools: [Read, Grep, Bash]
---

Review the diff carefully.

Focus on correctness first.
`
	if string(got.Bytes) != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", got.Bytes, want)
	}
	wantDropped := []string{"color", "hooks", "model"}
	if strings.Join(got.Dropped, ",") != strings.Join(wantDropped, ",") {
		t.Errorf("dropped = %v, want %v", got.Dropped, wantDropped)
	}
}

func TestRenderGemini_ToolsArrayInput(t *testing.T) {
	// A canonical file whose tools key is already a YAML sequence
	// converts the same way as the comma string.
	src := strings.Replace(testAgentMD, "tools: Read, Grep, Bash", "tools: [Read, Grep]", 1)
	def, err := skills.ParseAgentMD([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderGemini(def)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.Bytes), "tools: [Read, Grep]\n") {
		t.Errorf("sequence tools not carried:\n%s", got.Bytes)
	}
}

func TestRender_Deterministic(t *testing.T) {
	def := parseTestAgent(t)
	for _, tt := range Targets() {
		if tt.Render == nil {
			continue
		}
		a, err := tt.Render(def)
		if err != nil {
			t.Fatalf("%s: %v", tt.Slug, err)
		}
		b, err := tt.Render(def)
		if err != nil {
			t.Fatalf("%s: %v", tt.Slug, err)
		}
		if !bytes.Equal(a.Bytes, b.Bytes) {
			t.Errorf("%s render is not deterministic", tt.Slug)
		}
	}
}

func TestRender_QuotesUnsafeScalars(t *testing.T) {
	src := strings.Replace(testAgentMD,
		"description: Reviews pull requests for style and correctness",
		`description: "reviews: everything #carefully"`, 1)
	def, err := skills.ParseAgentMD([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderCopilot(def)
	if err != nil {
		t.Fatal(err)
	}
	// The rendered frontmatter must round-trip: parse it back and check
	// the description survived exactly.
	back, err := skills.ParseAgentMD(got.Bytes)
	if err != nil {
		t.Fatalf("rendered output does not parse: %v\n%s", err, got.Bytes)
	}
	if back.Description != "reviews: everything #carefully" {
		t.Errorf("description mangled: %q", back.Description)
	}
}

func TestRender_NoToolsKey(t *testing.T) {
	src := strings.Replace(testAgentMD, "tools: Read, Grep, Bash\n", "", 1)
	def, err := skills.ParseAgentMD([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderCopilot(def)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got.Bytes), "tools:") {
		t.Errorf("tools emitted despite absent key:\n%s", got.Bytes)
	}
}

func TestTargetFileNames(t *testing.T) {
	for _, tt := range Targets() {
		got := tt.fileName("reviewer")
		want := "reviewer.md"
		if tt.Slug == "copilot" {
			want = "reviewer.agent.md"
		}
		if got != want {
			t.Errorf("%s fileName = %q, want %q", tt.Slug, got, want)
		}
	}
}

func TestLossyDetail(t *testing.T) {
	if d := lossyDetail("opencode", nil); d != "" {
		t.Errorf("empty dropped must yield empty detail, got %q", d)
	}
	d := lossyDetail("opencode", []string{"model", "tools"})
	if !strings.Contains(d, "model, tools") || !strings.Contains(d, "opencode") {
		t.Errorf("detail = %q", d)
	}
}
