package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherEmitsEventOnTSWrite(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.SetDebounce(50 * time.Millisecond)
	w.SetLogger(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	// Allow the watcher to set up before we touch the directory.
	time.Sleep(80 * time.Millisecond)

	ch, unsub := w.Subscribe()
	defer unsub()

	path := filepath.Join(dir, "skill.ts")
	if err := os.WriteFile(path, []byte("await tool(\"x\");\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Lang != "ts" {
			t.Errorf("Lang = %q, want ts", ev.Lang)
		}
		if filepath.Base(ev.Path) != "skill.ts" {
			t.Errorf("Path = %q, want trailing skill.ts", ev.Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received within timeout")
	}
}

func TestWatcherIgnoresUnknownExt(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.SetDebounce(50 * time.Millisecond)
	w.SetLogger(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	time.Sleep(80 * time.Millisecond)

	ch, unsub := w.Subscribe()
	defer unsub()

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case ev := <-ch:
		t.Errorf("unexpected event: %+v", ev)
	case <-time.After(250 * time.Millisecond):
		// good — no event for unrelated extension
	}
}

func TestNewRejectsMissingDir(t *testing.T) {
	if _, err := New("/no/such/path/here"); err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestNewRejectsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := New(path); err == nil {
		t.Fatal("expected error when root is a file")
	}
}
