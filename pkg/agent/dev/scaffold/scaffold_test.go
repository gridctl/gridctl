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

func TestScaffoldGoNotYetImplemented(t *testing.T) {
	dir := t.TempDir()
	_, err := Scaffold(dir, Options{Language: "go", SkillName: "hello-go"})
	if err == nil {
		t.Fatal("expected error for Language=\"go\" in Phase 1")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' in error, got: %v", err)
	}
	// No partial output should land on disk before the error returns.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected empty dir after Go-stub error, got %d entries", len(entries))
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
