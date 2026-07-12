package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

func TestResolveDestroyTargetByFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte("name: filestack\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	name, stack, err := resolveDestroyTarget(path)
	if err != nil {
		t.Fatalf("resolveDestroyTarget(file): %v", err)
	}
	if name != "filestack" {
		t.Errorf("name = %q, want filestack", name)
	}
	if stack == nil {
		t.Error("expected the loaded spec for a file target")
	}
}

func TestResolveDestroyTargetByName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	st := &state.DaemonState{
		StackName: "namedstack",
		StackFile: "/moved/away.yaml",
		PID:       os.Getpid(),
		Port:      8181,
		StartedAt: time.Now(),
	}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}

	name, stack, err := resolveDestroyTarget("namedstack")
	if err != nil {
		t.Fatalf("resolveDestroyTarget(name): %v", err)
	}
	if name != "namedstack" {
		t.Errorf("name = %q, want namedstack", name)
	}
	if stack != nil {
		t.Error("expected nil spec when the recorded stack file is gone")
	}
}

func TestResolveDestroyTargetNameShadowedByDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	st := &state.DaemonState{StackName: "examples", StackFile: "/moved.yaml", PID: os.Getpid(), Port: 8181, StartedAt: time.Now()}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}

	// A directory named like the stack in cwd must not block by-name teardown.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "examples"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	name, _, err := resolveDestroyTarget("examples")
	if err != nil {
		t.Fatalf("resolveDestroyTarget: %v", err)
	}
	if name != "examples" {
		t.Errorf("name = %q, want examples", name)
	}
}

func TestResolveDestroyTargetUnknownListsKnown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	st := &state.DaemonState{StackName: "known", StackFile: "/x.yaml", PID: os.Getpid(), Port: 8181, StartedAt: time.Now()}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveDestroyTarget("nope")
	if err == nil {
		t.Fatal("expected an error for an unknown stack")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "known") {
		t.Errorf("error should mention known stacks, got %q", err)
	}
}
