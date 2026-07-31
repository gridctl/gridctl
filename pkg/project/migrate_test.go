package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Golden legacy fixtures: real pre-unification lockfile shapes,
// including a field no reader today understands, so migration proves
// itself loss-free (Article XVII).
const goldenSkillLock = `version: 1
projections:
    alpha:
        claude-code:
            channel: symlink
            target: /home/u/.claude/skills/alpha
            created_by_gridctl: true
            synced_at: 2026-07-01T10:00:00Z
        antigravity:
            channel: copy
            target: /home/u/.gemini/config/skills/alpha
            created_by_gridctl: true
            tree_hash: sha256:aaaa
            synced_at: 2026-07-01T10:00:01Z
            future_field: preserved
    beta:
        agents:
            channel: symlink
            target: /home/u/.agents/skills/beta
            created_by_gridctl: true
            synced_at: 2026-07-02T09:30:00Z
`

const goldenContextLock = `version: 1
scope: global
clients:
    claude-code:
        strategy: dedicated-file
        target: /home/u/.claude/rules/gridctl.md
        installed_hash: sha256:bbbb
        canonical_hash: sha256:cccc
        created_file: true
        synced_at: 2026-07-03T08:00:00Z
    gemini:
        strategy: import-shim
        target: /home/u/.gemini/GEMINI.md
        installed_hash: sha256:dddd
        canonical_hash: sha256:eeee
        created_file: false
        synced_at: 2026-07-03T08:00:01Z
`

func writeLegacyFixtures(t *testing.T, s *Store, skill, context string) {
	t.Helper()
	if skill != "" {
		if err := os.MkdirAll(filepath.Dir(s.legacySkillPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.legacySkillPath(), []byte(skill), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if context != "" {
		if err := os.MkdirAll(filepath.Dir(s.legacyContextPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.legacyContextPath(), []byte(context), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// assertGoldenMerge checks the merged view against the fixtures,
// whether it came from an in-memory merge or the migrated file.
func assertGoldenMerge(t *testing.T, l *Lock) {
	t.Helper()
	skills := l.Entries(KindSkill)
	if len(skills) != 3 {
		t.Fatalf("skill entries = %d, want 3: %+v", len(skills), skills)
	}
	anti := l.Get(KindSkill, "antigravity", "alpha")
	if anti == nil || anti.Channel != "copy" || anti.TreeHash != "sha256:aaaa" || !anti.CreatedByGridctl {
		t.Fatalf("antigravity alpha entry = %+v", anti)
	}
	if anti.Extra["future_field"] != "preserved" {
		t.Errorf("unknown legacy field dropped: %+v", anti.Extra)
	}
	contexts := l.Entries(KindContext)
	if len(contexts) != 2 {
		t.Fatalf("context entries = %d, want 2", len(contexts))
	}
	cc := l.Get(KindContext, "claude-code", "global")
	if cc == nil || cc.Strategy != "dedicated-file" || cc.InstalledHash != "sha256:bbbb" ||
		cc.CanonicalHash != "sha256:cccc" || !cc.CreatedFile {
		t.Fatalf("claude-code context entry = %+v", cc)
	}
}

func TestLoadMergesLegacyInMemoryWithoutWriting(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, goldenSkillLock, goldenContextLock)

	l, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenMerge(t, l)

	if fileExists(s.Path()) {
		t.Error("Load must never write the unified file")
	}
	data, err := os.ReadFile(s.legacySkillPath())
	if err != nil || string(data) != goldenSkillLock {
		t.Error("Load must leave the legacy skill lockfile untouched")
	}
}

func TestMigrateOnFirstMutatingOperation(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, goldenSkillLock, goldenContextLock)

	mustMutate(t, s, func(l *Lock) error {
		assertGoldenMerge(t, l)
		return nil
	})

	// The unified file exists and holds everything the fixtures held.
	l, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenMerge(t, l)

	// Both legacy files are version-2 tombstones naming the unified path.
	for _, legacy := range []string{s.legacySkillPath(), s.legacyContextPath()} {
		data, err := os.ReadFile(legacy)
		if err != nil {
			t.Fatalf("legacy file missing after migration: %v", err)
		}
		if !strings.Contains(string(data), "version: 2") || !strings.Contains(string(data), s.Path()) {
			t.Errorf("tombstone at %s malformed:\n%s", legacy, data)
		}
	}

	// Pristine backups exist.
	backups, err := os.ReadDir(s.migrationBackupRoot())
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one migration backup dir: %v", err)
	}
	backed, err := os.ReadFile(filepath.Join(s.migrationBackupRoot(), backups[0].Name(), legacySkillLockName))
	if err != nil || string(backed) != goldenSkillLock {
		t.Errorf("backup must preserve the legacy file byte-for-byte: %v", err)
	}

	// Re-running is a no-op: no second backup, unified content stable.
	unifiedBefore, _ := os.ReadFile(s.Path())
	mustMutate(t, s, func(l *Lock) error { return nil })
	unifiedAfter, _ := os.ReadFile(s.Path())
	if string(unifiedBefore) != string(unifiedAfter) {
		t.Error("re-running migration must not rewrite the unified file")
	}
	backups, _ = os.ReadDir(s.migrationBackupRoot())
	if len(backups) != 1 {
		t.Errorf("re-running migration must not add backups, got %d", len(backups))
	}

	// A subsequent write drops nothing (Article XVII).
	mustMutate(t, s, func(l *Lock) error {
		if err := l.Set(testEntry(KindSkill, "claude-code", "gamma", "/dest/gamma")); err != nil {
			return err
		}
		return l.Save()
	})
	l, err = s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if anti := l.Get(KindSkill, "antigravity", "alpha"); anti == nil || anti.Extra["future_field"] != "preserved" {
		t.Errorf("migrated entry or its unknown field lost after a write: %+v", anti)
	}
	if l.Get(KindContext, "gemini", "global") == nil {
		t.Error("migrated context entry lost after a write")
	}
	if l.Get(KindSkill, "claude-code", "gamma") == nil {
		t.Error("new entry missing after write")
	}
}

func TestDryRunMutateDoesNotMigrate(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, goldenSkillLock, goldenContextLock)

	err := s.Mutate(context.Background(), true, func(l *Lock) error {
		assertGoldenMerge(t, l)
		if err := l.Save(); err == nil {
			t.Error("Save must be refused inside a dry-run Mutate")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(s.Path()) {
		t.Error("dry-run must not write the unified file")
	}
	data, _ := os.ReadFile(s.legacySkillPath())
	if string(data) != goldenSkillLock {
		t.Error("dry-run must not tombstone the legacy files")
	}
}

func TestMigrationOnlySkillOrOnlyContext(t *testing.T) {
	for name, fixture := range map[string]struct{ skill, context string }{
		"skill-only":   {skill: goldenSkillLock},
		"context-only": {context: goldenContextLock},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewStore(t.TempDir())
			writeLegacyFixtures(t, s, fixture.skill, fixture.context)
			mustMutate(t, s, func(l *Lock) error { return nil })
			if !fileExists(s.Path()) {
				t.Fatal("unified file missing after migration")
			}
			l, err := s.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if fixture.skill != "" && len(l.Entries(KindSkill)) != 3 {
				t.Errorf("skill entries = %d", len(l.Entries(KindSkill)))
			}
			if fixture.context != "" && len(l.Entries(KindContext)) != 2 {
				t.Errorf("context entries = %d", len(l.Entries(KindContext)))
			}
		})
	}
}

// TestInterruptedMigrationIsRepaired covers the crash window between
// the unified write and the tombstone write: a live legacy file beside
// an existing unified file is re-tombstoned on the next mutating
// operation, so a downgraded binary can never silently work from it.
func TestInterruptedMigrationIsRepaired(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, goldenSkillLock, goldenContextLock)
	mustMutate(t, s, func(l *Lock) error { return nil })

	// Simulate the crash aftermath: restore one legacy file to its live
	// version-1 form.
	if err := os.WriteFile(s.legacySkillPath(), []byte(goldenSkillLock), 0o644); err != nil {
		t.Fatal(err)
	}
	unifiedBefore, _ := os.ReadFile(s.Path())

	mustMutate(t, s, func(l *Lock) error { return nil })

	data, err := os.ReadFile(s.legacySkillPath())
	if err != nil || !strings.Contains(string(data), "version: 2") {
		t.Errorf("live legacy file must be re-tombstoned: %v\n%s", err, data)
	}
	unifiedAfter, _ := os.ReadFile(s.Path())
	if string(unifiedBefore) != string(unifiedAfter) {
		t.Error("repair must not rewrite the unified file")
	}
	backed, err := os.ReadDir(s.migrationBackupRoot())
	if err != nil || len(backed) < 2 {
		t.Errorf("repair must back up the live legacy file before tombstoning: %v", err)
	}
}

func TestFreshInstallSkipsMigration(t *testing.T) {
	s := NewStore(t.TempDir())
	mustMutate(t, s, func(l *Lock) error {
		if err := l.Set(testEntry(KindSkill, "claude-code", "alpha", "/dest/a")); err != nil {
			return err
		}
		return l.Save()
	})
	if !fileExists(s.Path()) {
		t.Fatal("unified file must appear on first save")
	}
	if fileExists(s.legacySkillPath()) || fileExists(s.legacyContextPath()) {
		t.Error("fresh install must not create legacy files or tombstones")
	}
	if _, err := os.Stat(s.migrationBackupRoot()); !os.IsNotExist(err) {
		t.Error("fresh install must not create a migration backup dir")
	}
}

func TestTombstoneWithoutUnifiedFileIsRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, "version: 2\nnote: migrated\n", "")

	_, err := s.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tombstone") || !strings.Contains(err.Error(), s.Path()) {
		t.Fatalf("expected tombstone-without-unified refusal, got %v", err)
	}
	merr := s.Mutate(context.Background(), false, func(l *Lock) error { return nil })
	if merr == nil || !strings.Contains(merr.Error(), "tombstone") {
		t.Fatalf("Mutate must refuse too, got %v", merr)
	}
}

func TestLegacyConflictOnSameDestinationRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	// Should be impossible today (different path namespaces), but if the
	// two legacy files ever claim one destination, refuse over guessing.
	skill := `version: 1
projections:
    alpha:
        claude-code:
            channel: copy
            target: /home/u/.claude/rules/gridctl.md
            created_by_gridctl: true
            synced_at: 2026-07-01T10:00:00Z
`
	ctx := `version: 1
scope: global
clients:
    claude-code:
        strategy: dedicated-file
        target: /home/u/.claude/rules/gridctl.md
        installed_hash: sha256:bbbb
        canonical_hash: sha256:cccc
        created_file: true
        synced_at: 2026-07-03T08:00:00Z
`
	writeLegacyFixtures(t, s, skill, ctx)
	_, err := s.Load(context.Background())
	if !errors.Is(err, ErrPathConflict) {
		t.Fatalf("expected ErrPathConflict, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), s.legacySkillPath()) || !strings.Contains(err.Error(), s.legacyContextPath()) {
		t.Errorf("conflict error must name both legacy files: %v", err)
	}
}

// TestMigrationSanitizesCollidingLegacyKeys guards against the yaml
// inline-map panic: a legacy unknown field whose name collides with a
// unified-schema key (possible because the legacy schemas are narrower)
// is dropped with a warning instead of panicking the marshal.
func TestMigrationSanitizesCollidingLegacyKeys(t *testing.T) {
	s := NewStore(t.TempDir())
	skill := `version: 1
strategy: file-level-collision-is-fine
projections:
    alpha:
        claude-code:
            channel: symlink
            target: /home/u/.claude/skills/alpha
            created_by_gridctl: true
            synced_at: 2026-07-01T10:00:00Z
            installed_hash: collides-with-context-key
            harmless: survives
`
	ctxLock := `version: 1
scope: global
revision: 9
clients:
    gemini:
        strategy: import-shim
        target: /home/u/.gemini/GEMINI.md
        installed_hash: sha256:dddd
        canonical_hash: sha256:eeee
        created_file: false
        synced_at: 2026-07-03T08:00:01Z
        tree_hash: collides-with-skill-key
`
	writeLegacyFixtures(t, s, skill, ctxLock)
	mustMutate(t, s, func(l *Lock) error { return nil })

	l, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(KindSkill, "claude-code", "alpha")
	if e == nil || e.Extra["harmless"] != "survives" {
		t.Fatalf("non-colliding unknown field lost: %+v", e)
	}
	if e.InstalledHash != "" || e.Extra["installed_hash"] != nil {
		t.Errorf("colliding entry key must be dropped, not adopted: %+v", e)
	}
	g := l.Get(KindContext, "gemini", "global")
	if g == nil || g.TreeHash != "" || g.Extra["tree_hash"] != nil {
		t.Errorf("colliding context entry key must be dropped: %+v", g)
	}
	unified, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unified), "collides-with") {
		t.Errorf("colliding values leaked into the unified file:\n%s", unified)
	}
}

func TestNewerLegacyVersionRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, "version: 99\nprojections: {}\n", "")
	_, err := s.Load(context.Background())
	if !errors.Is(err, ErrNewerLockVersion) {
		t.Fatalf("expected ErrNewerLockVersion for legacy version 99, got %v", err)
	}
}

// TestDowngradedReaderFailsLoudlyOnTombstone proves the downgrade story
// by construction: the tombstone the migration writes trips the exact
// version guard every legacy reader shipped with (reject Version > 1
// with "written by a newer gridctl version"), so an older binary fails
// loudly on every projection operation instead of silently diverging.
func TestDowngradedReaderFailsLoudlyOnTombstone(t *testing.T) {
	s := NewStore(t.TempDir())
	writeLegacyFixtures(t, s, goldenSkillLock, goldenContextLock)
	mustMutate(t, s, func(l *Lock) error { return nil })

	// legacyGuard replicates the frozen v1 readers' guard verbatim
	// (pkg/skillsync/lockfile.go and pkg/contexts/lockfile.go at the
	// last release that shipped them).
	legacyGuard := func(path string) error {
		data, err := os.ReadFile(path) // #nosec G304 -- test fixture
		if err != nil {
			return err
		}
		version, err := legacyVersionOf(data)
		if err != nil {
			return err
		}
		const legacyLockVersion = 1
		if version > legacyLockVersion {
			return fmt.Errorf("lockfile was written by a newer gridctl version (file version %d, supported %d)", version, legacyLockVersion)
		}
		return nil
	}
	for _, legacy := range []string{s.legacySkillPath(), s.legacyContextPath()} {
		err := legacyGuard(legacy)
		if err == nil || !strings.Contains(err.Error(), "newer gridctl version") {
			t.Errorf("old reader must reject the tombstone at %s loudly, got %v", legacy, err)
		}
	}
}
