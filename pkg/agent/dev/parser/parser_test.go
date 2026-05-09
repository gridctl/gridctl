package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoFileExtractsPrimitives(t *testing.T) {
	dir := t.TempDir()
	src := `package skill

import "context"

func Run(ctx context.Context) {
	out := tool("github__list_issues", map[string]any{"repo": "x"})
	style := agent.LLM("claude-sonnet-4-6")
	parallel(out, style)
	handoff("summarize", out)
	approval("ship it")
}
`
	path := filepath.Join(dir, "skill.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g, err := ParseFile("hello", path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if g.Lang != LangGo {
		t.Fatalf("Lang = %q, want %q", g.Lang, LangGo)
	}
	if g.ParseError != "" {
		t.Fatalf("unexpected parse error: %s", g.ParseError)
	}
	if got := len(g.Nodes); got != 5 {
		t.Fatalf("nodes = %d, want 5: %+v", got, g.Nodes)
	}
	wantKinds := []NodeKind{KindTool, KindLLM, KindParallel, KindHandoff, KindApproval}
	for i, want := range wantKinds {
		if g.Nodes[i].Kind != want {
			t.Errorf("node[%d].Kind = %q, want %q", i, g.Nodes[i].Kind, want)
		}
	}
	if g.Nodes[0].Label != "github__list_issues" {
		t.Errorf("first label = %q, want literal", g.Nodes[0].Label)
	}
}

func TestParseTSFileExtractsPrimitives(t *testing.T) {
	dir := t.TempDir()
	src := `import { tool, llm } from "@gridctl/agent";

export default async function run(input) {
  const a = await tool("gridctl__greeting", { name: input.name });
  // a comment that mentions tool() shouldn't count
  const b = await llm({ model: "claude-sonnet-4-6" });
  /* block comment with parallel() and handoff() also doesn't count */
  await parallel(a, b);
  await handoff("summarize", a);
  await approval("ship it");
}
`
	path := filepath.Join(dir, "skill.ts")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g, err := ParseFile("hello", path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if g.Lang != LangTS {
		t.Fatalf("Lang = %q, want %q", g.Lang, LangTS)
	}
	if got := len(g.Nodes); got != 5 {
		t.Fatalf("nodes = %d, want 5: %+v", got, g.Nodes)
	}
	wantKinds := []NodeKind{KindTool, KindLLM, KindParallel, KindHandoff, KindApproval}
	for i, want := range wantKinds {
		if g.Nodes[i].Kind != want {
			t.Errorf("node[%d].Kind = %q, want %q", i, g.Nodes[i].Kind, want)
		}
	}
	if g.Nodes[0].Label != "gridctl__greeting" {
		t.Errorf("first label = %q, want literal string", g.Nodes[0].Label)
	}
}

func TestParseTSIgnoresFunctionDeclaration(t *testing.T) {
	dir := t.TempDir()
	src := `function tool(name) { return name; }
function llm() {}
const x = tool("real_call");
`
	path := filepath.Join(dir, "skill.ts")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g, err := ParseFile("hello", path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Only the third line counts; the first two are declarations.
	if got := len(g.Nodes); got != 1 {
		t.Fatalf("nodes = %d, want 1: %+v", got, g.Nodes)
	}
	if g.Nodes[0].Kind != KindTool || g.Nodes[0].Label != "real_call" {
		t.Errorf("got %+v, want tool real_call", g.Nodes[0])
	}
}

func TestParseFileRejectsUnknownExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.py")
	if err := os.WriteFile(path, []byte("# ignored"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ParseFile("hello", path); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestParseSkillPrefersGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.go"),
		[]byte("package x\nfunc Run(){ tool(\"go-side\") }\n"), 0o644); err != nil {
		t.Fatalf("write go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.ts"),
		[]byte("await tool(\"ts-side\");\n"), 0o644); err != nil {
		t.Fatalf("write ts: %v", err)
	}
	g, err := ParseSkill("hello", dir)
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if g.Lang != LangGo {
		t.Fatalf("Lang = %q, want %q", g.Lang, LangGo)
	}
	if g.Nodes[0].Label != "go-side" {
		t.Errorf("label = %q, want go-side", g.Nodes[0].Label)
	}
}

func TestParseSkillFallsBackToTS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.ts"),
		[]byte("await tool(\"ts-side\");\n"), 0o644); err != nil {
		t.Fatalf("write ts: %v", err)
	}
	g, err := ParseSkill("hello", dir)
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if g.Lang != LangTS {
		t.Fatalf("Lang = %q, want %q", g.Lang, LangTS)
	}
}

func TestParseSkillReportsMissingHandler(t *testing.T) {
	dir := t.TempDir()
	g, err := ParseSkill("hello", dir)
	if err != nil {
		t.Fatalf("ParseSkill: %v", err)
	}
	if g.ParseError == "" {
		t.Fatal("expected ParseError for missing handler")
	}
}

func TestParseGoFileSurvivesSyntaxError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skill.go")
	src := "package x\nfunc Run() { tool(\"a\"\n" // missing closing paren
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	g, err := ParseFile("hello", path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if g.ParseError == "" {
		t.Error("expected non-empty ParseError")
	}
}
