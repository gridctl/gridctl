package reload

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// startDirWatcher launches w.Watch in the background and waits briefly for it
// to arm, returning a cancel func the caller should defer.
func startDirWatcher(t *testing.T, w *DirWatcher) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Watch(ctx) }()
	time.Sleep(100 * time.Millisecond)
	return cancel
}

func TestDirWatcher_DetectsNewSkillFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w := NewDirWatcher(root, func() error { calls.Add(1); return nil })
	w.SetDebounce(50 * time.Millisecond)
	defer startDirWatcher(t, w)()

	// Create a new skill directory with a SKILL.md inside it — the common
	// "added out-of-band" case. The watcher must add the new directory to its
	// watch set and fire onChange.
	skillDir := filepath.Join(root, "node-state-snapshot")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(250 * time.Millisecond)
	if calls.Load() == 0 {
		t.Fatal("expected onChange to fire after a new skill file was written")
	}
}

func TestDirWatcher_DebouncesBurst(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w := NewDirWatcher(root, func() error { calls.Add(1); return nil })
	w.SetDebounce(120 * time.Millisecond)
	defer startDirWatcher(t, w)()

	// A single skill is multiple files; writing them in quick succession must
	// collapse to one refresh.
	skillDir := filepath.Join(root, "multi")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"SKILL.md", "scripts/a.sh", "scripts/b.sh", "reference.md"} {
		if err := os.WriteFile(filepath.Join(skillDir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected burst to debounce to 1 refresh, got %d", got)
	}
}

func TestDirWatcher_DetectsLateCreatedRoot(t *testing.T) {
	// The registry skills directory may not exist yet at startup. The watcher
	// must not create it (it is write-free), but must observe it being created
	// later by watching the nearest existing ancestor, then pick up writes.
	root := filepath.Join(t.TempDir(), "registry", "skills")
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("precondition: root should not exist yet")
	}

	var calls atomic.Int32
	w := NewDirWatcher(root, func() error { calls.Add(1); return nil })
	w.SetDebounce(50 * time.Millisecond)
	defer startDirWatcher(t, w)()

	// The watcher must not have created the directory.
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("watcher should not create the root directory")
	}

	// Create the root and a skill inside it after the watcher started.
	skillDir := filepath.Join(root, "late")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# late\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)
	if calls.Load() == 0 {
		t.Fatal("expected onChange to fire for a skill added after startup")
	}
}

func TestDirWatcher_StopsOnContextCancel(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	w := NewDirWatcher(root, func() error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Watch(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}
