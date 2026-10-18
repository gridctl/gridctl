package reload

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_EventKindsAndIgnoredBasename(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	overridePath := filepath.Join(dir, "stack.override.yaml")
	if err := os.WriteFile(stackPath, []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("mcp-servers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	watcher := NewWatcher(stackPath, func() error {
		calls.Add(1)
		return nil
	})
	watcher.AddBasename("stack.override.yaml")
	watcher.SetDebounce(40 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- watcher.Watch(ctx) }()
	time.Sleep(80 * time.Millisecond)

	wait := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if calls.Load() >= want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("calls = %d, want at least %d", calls.Load(), want)
	}

	if err := os.WriteFile(stackPath, []byte("name: write\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wait(1)

	if err := os.WriteFile(filepath.Join(dir, "other.yaml"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("non-member basename triggered reload: %d", calls.Load())
	}

	if err := os.Remove(overridePath); err != nil {
		t.Fatal(err)
	}
	wait(2)

	if err := os.WriteFile(overridePath, []byte("mcp-servers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wait(3)

	if err := os.Rename(overridePath, filepath.Join(dir, "moved.yaml")); err != nil {
		t.Fatal(err)
	}
	wait(4)

	cancel()
	<-errCh
}
