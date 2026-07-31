package skillsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// legacySkillLockPath is where pre-unification gridctl kept the skill
// projection lockfile.
func legacySkillLockPath(home string) string {
	return filepath.Join(home, ".gridctl", "skillsync.lock.yaml")
}

// downgradeToLegacy rewrites the manager's unified lockfile as a
// version-1 skillsync.lock.yaml, simulating a home last written by a
// pre-unification gridctl.
func downgradeToLegacy(t *testing.T, f *fixture) {
	t.Helper()
	lf, err := f.mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{"version": 1, "projections": map[string]any{}}
	projections := legacy["projections"].(map[string]any)
	for skill, clients := range lf.Projections {
		row := map[string]any{}
		for client, e := range clients {
			row[client] = map[string]any{
				"channel":            string(e.Channel),
				"target":             e.Target,
				"created_by_gridctl": e.CreatedByGridctl,
				"tree_hash":          e.TreeHash,
				"synced_at":          e.SyncedAt,
			}
		}
		projections[skill] = row
	}
	data, err := yaml.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySkillLockPath(f.home), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.mgr.LockPath()); err != nil {
		t.Fatal(err)
	}
}

// TestUpgradeMigratesLegacyLockfileOnFirstSync is the end-to-end
// upgrade path: a home written by a pre-unification gridctl reads
// identically before migration, migrates on the first mutating
// operation (backup, unified file, tombstone), and behaves identically
// after. Re-running is a no-op.
func TestUpgradeMigratesLegacyLockfileOnFirstSync(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code", "antigravity"}})
	downgradeToLegacy(t, f)

	// Pre-migration reads see the legacy state unchanged.
	statuses, err := f.mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || stateOf(t, statuses, "alpha", "claude-code") != StateInSync ||
		stateOf(t, statuses, "alpha", "antigravity") != StateInSync {
		t.Fatalf("pre-migration statuses = %+v", statuses)
	}
	if _, err := os.Stat(f.mgr.LockPath()); !os.IsNotExist(err) {
		t.Fatal("a read must not trigger migration")
	}

	// The first mutating operation migrates, then behaves as before.
	results := f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionUnchanged {
		t.Errorf("post-migration reconcile action = %s, want unchanged", got)
	}
	if _, err := os.Stat(f.mgr.LockPath()); err != nil {
		t.Fatalf("unified lockfile missing after migration: %v", err)
	}
	legacy, err := os.ReadFile(legacySkillLockPath(f.home))
	if err != nil {
		t.Fatalf("legacy path must hold a tombstone: %v", err)
	}
	if !strings.Contains(string(legacy), "version: 2") {
		t.Errorf("legacy file is not a tombstone:\n%s", legacy)
	}
	backups, err := os.ReadDir(filepath.Join(f.home, ".gridctl", "project-migration-backup"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one migration backup: %v", err)
	}

	// Post-migration state is fully operational and re-syncs are no-ops.
	statuses, err = f.mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stateOf(t, statuses, "alpha", "antigravity") != StateInSync {
		t.Errorf("post-migration status = %+v", statuses)
	}
	results = f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionUnchanged {
		t.Errorf("second reconcile action = %s, want unchanged", got)
	}
	if backups, _ := os.ReadDir(filepath.Join(f.home, ".gridctl", "project-migration-backup")); len(backups) != 1 {
		t.Errorf("re-running must not add migration backups, got %d", len(backups))
	}
}
