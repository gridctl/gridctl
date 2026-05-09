package scaffold

import (
	"os"
	"path/filepath"
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
