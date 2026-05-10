package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldCreatesStarterFiles(t *testing.T) {
	dir := t.TempDir()
	res, err := Scaffold(dir, Options{})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if got := len(res.Created); got != 3 {
		t.Fatalf("Created = %d files, want 3: %v", got, res.Created)
	}
	for _, name := range []string{"SKILL.md", "skill.ts", "agent.json"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s on disk: %v", name, err)
		}
	}
}

func TestScaffoldIsIdempotentWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, err := Scaffold(dir, Options{}); err != nil {
		t.Fatalf("first Scaffold: %v", err)
	}
	res, err := Scaffold(dir, Options{})
	if err != nil {
		t.Fatalf("second Scaffold: %v", err)
	}
	if got := len(res.Created); got != 0 {
		t.Errorf("Created on second pass = %d, want 0: %v", got, res.Created)
	}
	if got := len(res.Skipped); got != 3 {
		t.Errorf("Skipped on second pass = %d, want 3: %v", got, res.Skipped)
	}
}

func TestScaffoldForceOverwritesDifferent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# legacy\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := Scaffold(dir, Options{Force: true})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if got := len(res.Created); got != 3 {
		t.Errorf("Created = %d, want 3: %v", got, res.Created)
	}
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) == "# legacy\n" {
		t.Errorf("Force did not overwrite existing SKILL.md")
	}
}

func TestScaffoldRejectsEmptyRoot(t *testing.T) {
	if _, err := Scaffold("", Options{}); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestScaffoldPromptOnlyWritesOnlySkillMD(t *testing.T) {
	dir := t.TempDir()
	res, err := Scaffold(dir, Options{Language: "prompt", SkillName: "hello-prompt"})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if got := len(res.Created); got != 1 {
		t.Fatalf("Created = %d files, want 1: %v", got, res.Created)
	}
	if res.Created[0] != "SKILL.md" {
		t.Errorf("Created[0] = %q, want SKILL.md", res.Created[0])
	}
	for _, name := range []string{"skill.ts", "skill.go", "agent.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected no %s on disk for prompt-only scaffold, got err=%v", name, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(body), "name: hello-prompt") {
		t.Errorf("expected name: hello-prompt in frontmatter, got: %s", body)
	}
	if !strings.Contains(string(body), "state: active") {
		t.Errorf("expected state: active in frontmatter")
	}
}

func TestScaffoldGoWritesSkillSourceAndTest(t *testing.T) {
	dir := t.TempDir()
	res, err := Scaffold(dir, Options{Language: "go", SkillName: "hello-go"})
	if err != nil {
		t.Fatalf("Scaffold(go): %v", err)
	}
	want := []string{"SKILL.md", "skill.go", "skill_test.go"}
	if got := len(res.Created); got != len(want) {
		t.Fatalf("Created = %d files, want %d: %v", got, len(want), res.Created)
	}
	for _, name := range want {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s on disk: %v", name, err)
		}
	}
	// Go skills don't carry agent.json or skill.ts — verify they
	// don't leak in from the TS branch.
	for _, name := range []string{"agent.json", "skill.ts"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected no %s for go scaffold, got err=%v", name, err)
		}
	}
	src, err := os.ReadFile(filepath.Join(dir, "skill.go"))
	if err != nil {
		t.Fatalf("read skill.go: %v", err)
	}
	srcStr := string(src)
	for _, want := range []string{
		"package main",
		"\"github.com/gridctl/gridctl/pkg/agent/skill\"",
		"\"github.com/gridctl/gridctl/pkg/agent/llm\"",
		"type HelloInput struct",
		"type HelloOutput struct",
		"jsonschema:\"required",
		"func New() *skill.Definition",
		"func RegisterSkill(reg *skill.Registry) error",
		"func main() {}",
		"\"hello-go\"",
	} {
		if !strings.Contains(srcStr, want) {
			t.Errorf("skill.go missing expected substring %q\n--- skill.go ---\n%s", want, srcStr)
		}
	}
}

func TestHelloSkillGoExportsTestChannel(t *testing.T) {
	// Mirrors HelloSkillTS: the regression suite needs a stable
	// channel into the scaffold body so a future scaffold change
	// re-runs compatibility checks without a parallel copy of the
	// source drifting.
	src := HelloSkillGo("hello-go")
	if !strings.Contains(src, "package main") {
		t.Errorf("HelloSkillGo: missing 'package main'")
	}
	if !strings.Contains(src, "func RegisterSkill") {
		t.Errorf("HelloSkillGo: missing RegisterSkill plugin entry")
	}
	tst := HelloSkillGoTest("hello-go")
	if !strings.Contains(tst, "package main") {
		t.Errorf("HelloSkillGoTest: missing 'package main'")
	}
	if !strings.Contains(tst, "TestRun_GreetsTheCaller") {
		t.Errorf("HelloSkillGoTest: missing TestRun_GreetsTheCaller")
	}
}

func TestScaffoldUnknownLanguageRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := Scaffold(dir, Options{Language: "rust"})
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("expected 'unsupported language' in error, got: %v", err)
	}
}

func TestScaffoldDefaultLanguageIsTS(t *testing.T) {
	// Empty Language is back-compat for "ts"; verify three-file output unchanged.
	dir := t.TempDir()
	res, err := Scaffold(dir, Options{})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if got := len(res.Created); got != 3 {
		t.Fatalf("Created = %d files, want 3 (TS default): %v", got, res.Created)
	}
	dirTS := t.TempDir()
	resTS, err := Scaffold(dirTS, Options{Language: "ts"})
	if err != nil {
		t.Fatalf("Scaffold(ts): %v", err)
	}
	if len(resTS.Created) != 3 {
		t.Errorf("Language=\"ts\" expected 3 files, got %v", resTS.Created)
	}
}
