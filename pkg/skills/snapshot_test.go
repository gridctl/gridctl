package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotWorktree_SkipsSymlinkAndCopiesFiles(t *testing.T) {
	src := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "stack.yaml"), []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "tool.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(src, "leak")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "config"), []byte("git"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "checkout")
	res, err := SnapshotWorktree(context.Background(), src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reused || res.CheckoutDir != dest {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dest, "leak")); !os.IsNotExist(err) {
		t.Fatalf("symlink was copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git was copied: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "stack.yaml"))
	if err != nil || string(got) != "name: demo\n" {
		t.Fatalf("stack.yaml = %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dest, "sub", "tool.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("executable mode = %o", info.Mode().Perm())
	}
	stackInfo, err := os.Stat(filepath.Join(dest, "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if stackInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", stackInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}

	again, err := SnapshotWorktree(context.Background(), src, dest)
	if err != nil || !again.Reused {
		t.Fatalf("reuse = %+v, %v", again, err)
	}
}

func TestSnapshotWorktree_RejectsEscapingPath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := rejectOutsideRoot(root, outside); err == nil || !strings.Contains(err.Error(), outside) {
		t.Fatalf("escape error = %v", err)
	}
	inside := filepath.Join(root, "stack.yaml")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rejectOutsideRoot(root, inside); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotWorktree_Caps(t *testing.T) {
	origFile, origCount, origTotal := snapshotMaxFileSize, snapshotMaxFiles, snapshotMaxTotalSize
	t.Cleanup(func() {
		snapshotMaxFileSize, snapshotMaxFiles, snapshotMaxTotalSize = origFile, origCount, origTotal
	})

	t.Run("file", func(t *testing.T) {
		snapshotMaxFileSize = 4
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "big"), []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "out")
		_, err := SnapshotWorktree(context.Background(), src, dest)
		if err == nil || !strings.Contains(err.Error(), "5 MiB") {
			t.Fatalf("error = %v", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Fatal("partial checkout survived")
		}
	})
	t.Run("count", func(t *testing.T) {
		snapshotMaxFiles = 1
		src := t.TempDir()
		for _, name := range []string{"a", "b"} {
			if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		dest := filepath.Join(t.TempDir(), "out")
		_, err := SnapshotWorktree(context.Background(), src, dest)
		if err == nil || !strings.Contains(err.Error(), "5000") {
			t.Fatalf("error = %v", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Fatal("partial checkout survived")
		}
	})
	t.Run("total", func(t *testing.T) {
		snapshotMaxFileSize = 8
		snapshotMaxTotalSize = 4
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "a"), []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "out")
		_, err := SnapshotWorktree(context.Background(), src, dest)
		if err == nil || !strings.Contains(err.Error(), "64 MiB") {
			t.Fatalf("error = %v", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Fatal("partial checkout survived")
		}
	})
}
