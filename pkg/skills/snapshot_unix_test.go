//go:build unix

package skills

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/builder"
)

func TestSnapshotWorktree_FlockSerializes(t *testing.T) {
	orig := importLockTimeout
	importLockTimeout = 200 * time.Millisecond
	t.Cleanup(func() { importLockTimeout = orig })

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "stack.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	doneHold := make(chan error, 1)
	go func() {
		doneHold <- withLockFileFlock(context.Background(), repo, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case <-time.After(2 * time.Second):
		t.Fatal("flock was not acquired")
	}

	timed := make(chan error, 1)
	go func() {
		_, err := SnapshotWorktree(context.Background(), repo, filepath.Join(t.TempDir(), "out"))
		timed <- err
	}()
	select {
	case err := <-timed:
		if !errors.Is(err, ErrImportLockBusy) {
			t.Fatalf("snapshot error = %v, want lock timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot did not time out while the flock was held")
	}
	close(release)
	if err := <-doneHold; err != nil {
		t.Fatal(err)
	}
}

func TestCloneShallow_WaitsOnSnapshotFlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRIDCTL_HOME", os.Getenv("HOME"))
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath, err := builder.URLToPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = withLockFileFlock(context.Background(), cachePath, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	done := make(chan struct{})
	go func() {
		_, _ = cloneShallow(repo, "", AuthConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("cloneShallow proceeded while the snapshot flock was held")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cloneShallow did not proceed after the flock was released")
	}
}
