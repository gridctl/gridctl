package skillsync

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// snapshotTree records every path under root with its content (files),
// link target (symlinks), or presence (dirs), so a before/after compare
// catches any write, creation, or removal inside the tree.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		switch {
		case d.IsDir():
			snap[rel] = "dir"
		case d.Type()&fs.ModeSymlink != 0:
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			snap[rel] = "link:" + link
		default:
			data, rerr := os.ReadFile(path) // #nosec G304 -- walking a test fixture tree
			if rerr != nil {
				return rerr
			}
			snap[rel] = "file:" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// TestReconcileNeverWritesInsideWatchedRegistry is the regression test
// for the write-free-watcher invariant (pkg/controller arms a disk
// watcher on <registry>/skills and reconciles on every change; a
// reconcile that wrote into the watched tree would feed back on
// itself). A reconcile that repairs, refreshes, and skips drift must
// leave the registry tree byte-identical.
func TestReconcileNeverWritesInsideWatchedRegistry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// One symlink projection, two copies: one to drift, one to go stale.
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code", "antigravity"}})
	f.mustSync(t, []string{"beta"}, SyncOptions{Clients: []string{"antigravity"}})

	// Drift alpha's copy, break the symlink, and edit beta in the
	// registry so the reconcile exercises skip, repair, and refresh.
	if err := os.WriteFile(filepath.Join(f.dest(t, "antigravity", "alpha"), "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.dest(t, "claude-code", "alpha")); err != nil {
		t.Fatal(err)
	}
	f.writeSkill(t, "beta", "active", "Beta body, revised.")
	f.reload(t)

	watched := filepath.Join(f.regDir, "skills")
	before := snapshotTree(t, watched)

	results, err := f.mgr.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("reconcile should have acted on the recorded projections")
	}

	after := snapshotTree(t, watched)
	if len(before) != len(after) {
		t.Fatalf("reconcile changed the watched registry tree: %d entries before, %d after", len(before), len(after))
	}
	for rel, want := range before {
		if got, ok := after[rel]; !ok || got != want {
			t.Errorf("reconcile modified %s inside the watched registry tree", rel)
		}
	}
}
