package contexts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// legacyContextLockPath is where pre-unification gridctl kept the
// context lockfile.
func legacyContextLockPath(m *Manager) string {
	return filepath.Join(m.Dir(), "context.lock.yaml")
}

// downgradeToLegacyContext rewrites the manager's unified lockfile as a
// version-1 context.lock.yaml, simulating a home last written by a
// pre-unification gridctl.
func downgradeToLegacyContext(t *testing.T, m *Manager) {
	t.Helper()
	lf, err := m.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clients := map[string]any{}
	for slug, e := range lf.Clients {
		clients[slug] = map[string]any{
			"strategy":       e.Strategy,
			"target":         e.Target,
			"installed_hash": e.InstalledHash,
			"canonical_hash": e.CanonicalHash,
			"created_file":   e.CreatedFile,
			"synced_at":      e.SyncedAt,
		}
	}
	data, err := yaml.Marshal(map[string]any{"version": 1, "scope": "global", "clients": clients})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyContextLockPath(m), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(m.lockPath()); err != nil {
		t.Fatal(err)
	}
}

// TestUpgradeMigratesLegacyContextLockOnFirstSync mirrors the skillsync
// end-to-end upgrade test for the context kind: identical reads before
// migration, migration on the first mutating operation, identical
// behavior after.
func TestUpgradeMigratesLegacyContextLockOnFirstSync(t *testing.T) {
	m := newTestManager(t, ".claude", ".gemini")
	initCanonical(t, m, "# Rules\n")
	ctx := context.Background()
	for _, slug := range []string{"claude-code", "gemini"} {
		if _, err := m.SyncClient(ctx, slug, SyncOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	downgradeToLegacyContext(t, m)

	// Pre-migration reads see the legacy state unchanged.
	for _, slug := range []string{"claude-code", "gemini"} {
		if got := statusOf(t, m, slug).State; got != StateInSync {
			t.Fatalf("pre-migration %s state = %q, want in-sync", slug, got)
		}
	}
	if _, err := os.Stat(m.lockPath()); !os.IsNotExist(err) {
		t.Fatal("a read must not trigger migration")
	}

	// The first mutating operation migrates, then behaves as before.
	results, err := m.SyncAll(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if (r.Slug == "claude-code" || r.Slug == "gemini") && r.Action != ActionUnchanged {
			t.Errorf("post-migration %s action = %q, want unchanged", r.Slug, r.Action)
		}
	}
	if _, err := os.Stat(m.lockPath()); err != nil {
		t.Fatalf("unified lockfile missing after migration: %v", err)
	}
	legacy, err := os.ReadFile(legacyContextLockPath(m))
	if err != nil {
		t.Fatalf("legacy path must hold a tombstone: %v", err)
	}
	if !strings.Contains(string(legacy), "version: 2") {
		t.Errorf("legacy file is not a tombstone:\n%s", legacy)
	}

	// Post-migration state is fully operational.
	for _, slug := range []string{"claude-code", "gemini"} {
		if got := statusOf(t, m, slug).State; got != StateInSync {
			t.Errorf("post-migration %s state = %q, want in-sync", slug, got)
		}
	}
}
