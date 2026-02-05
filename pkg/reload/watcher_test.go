package reload

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_DirectWrite(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "stack.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nname: test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var callCount atomic.Int32
	watcher := NewWatcher(configPath, func() error {
		callCount.Add(1)
		return nil
	})
	watcher.SetDebounce(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watcher in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Direct write to file
	if err := os.WriteFile(configPath, []byte("version: 1\nname: test-updated\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + processing
	time.Sleep(200 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("expected onChange to be called once, got %d", callCount.Load())
	}

	cancel()
	<-errCh
}

func TestWatcher_AtomicSave(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "stack.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nname: test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var callCount atomic.Int32
	watcher := NewWatcher(configPath, func() error {
		callCount.Add(1)
		return nil
	})
	watcher.SetDebounce(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watcher in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Simulate atomic save (write to temp, rename)
	tmpPath := filepath.Join(tmpDir, "stack.yaml.tmp")
	if err := os.WriteFile(tmpPath, []byte("version: 1\nname: test-atomic\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + processing + re-watch delay
	time.Sleep(500 * time.Millisecond)

	if callCount.Load() < 1 {
		t.Errorf("expected onChange to be called at least once for atomic save, got %d", callCount.Load())
	}

	cancel()
	<-errCh
}

func TestWatcher_MultipleWritesDebounced(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "stack.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nname: test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var callCount atomic.Int32
	watcher := NewWatcher(configPath, func() error {
		callCount.Add(1)
		return nil
	})
	watcher.SetDebounce(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watcher in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Multiple rapid writes should be debounced to one call
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(configPath, []byte("version: 1\nname: test-"+string(rune('a'+i))+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(300 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("expected rapid writes to be debounced to 1 call, got %d", callCount.Load())
	}

	cancel()
	<-errCh
}
