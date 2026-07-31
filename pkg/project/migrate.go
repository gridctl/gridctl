package project

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// legacyTombstoneVersion marks a migrated legacy lockfile. Both legacy
// readers rejected Version > 1 with "written by a newer gridctl
// version", so a downgraded binary fails loudly on every projection
// operation instead of silently diverging from the unified file. No
// real legacy version 2 ever existed; the value is reserved for this.
const legacyTombstoneVersion = 2

// contextScope is the Source recorded for context projections,
// mirroring the legacy lockfile's scope field.
const contextScope = "global"

// legacySkillLockName and legacyContextLockPath locate the two
// pre-unification lockfiles relative to home.
const legacySkillLockName = "skillsync.lock.yaml"

func (s *Store) legacySkillPath() string {
	return filepath.Join(s.home, ".gridctl", legacySkillLockName)
}

func (s *Store) legacyContextPath() string {
	return filepath.Join(s.home, ".gridctl", "context", "context.lock.yaml")
}

// migrationBackupRoot holds pristine copies of the legacy files taken
// immediately before migration.
func (s *Store) migrationBackupRoot() string {
	return filepath.Join(s.home, ".gridctl", "project-migration-backup")
}

// legacySkillLock is the frozen pkg/skillsync lockfile schema. Extra
// maps preserve fields this binary does not understand (Article XVII).
type legacySkillLock struct {
	Version     int                                     `yaml:"version"`
	Projections map[string]map[string]*legacySkillEntry `yaml:"projections"`
	Extra       map[string]any                          `yaml:",inline"`
}

type legacySkillEntry struct {
	Channel          string         `yaml:"channel"`
	Target           string         `yaml:"target"`
	CreatedByGridctl bool           `yaml:"created_by_gridctl"`
	TreeHash         string         `yaml:"tree_hash,omitempty"`
	SyncedAt         time.Time      `yaml:"synced_at"`
	Extra            map[string]any `yaml:",inline"`
}

// legacyContextLock is the frozen pkg/contexts lockfile schema.
type legacyContextLock struct {
	Version int                            `yaml:"version"`
	Scope   string                         `yaml:"scope"`
	Clients map[string]*legacyContextEntry `yaml:"clients"`
	Extra   map[string]any                 `yaml:",inline"`
}

type legacyContextEntry struct {
	Strategy      string         `yaml:"strategy"`
	Target        string         `yaml:"target"`
	InstalledHash string         `yaml:"installed_hash"`
	CanonicalHash string         `yaml:"canonical_hash"`
	CreatedFile   bool           `yaml:"created_file"`
	SyncedAt      time.Time      `yaml:"synced_at"`
	Extra         map[string]any `yaml:",inline"`
}

// Known unified-schema YAML keys at the file and entry level. Legacy
// unknown fields whose names collide with them cannot ride along as
// inline-map entries: yaml.v3 panics on an inline key that shadows a
// struct field, so colliding keys are dropped with a warning instead
// (the pre-migration backup still holds them).
var (
	unifiedFileKeys = map[string]bool{
		"version": true, "revision": true, "projections": true,
	}
	unifiedEntryKeys = map[string]bool{
		"kind": true, "client": true, "source": true, "path": true,
		"channel": true, "created_by_gridctl": true, "tree_hash": true,
		"strategy": true, "installed_hash": true, "canonical_hash": true,
		"created_file": true, "synced_at": true,
	}
)

// sanitizeExtra strips keys that collide with the unified schema from a
// legacy unknown-field map, warning about each drop.
func sanitizeExtra(extra map[string]any, known map[string]bool, where string) map[string]any {
	var out map[string]any
	for k, v := range extra {
		if known[k] {
			slog.Warn("dropping legacy lockfile field that collides with the unified schema",
				"field", k, "from", where)
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		out[k] = v
	}
	return out
}

// legacyVersionOf peeks at a legacy file's version field.
func legacyVersionOf(data []byte) (int, error) {
	var head struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return 0, err
	}
	return head.Version, nil
}

// readLegacyFile reads one legacy lockfile into out. Returns
// exists=false for a missing file and tombstone=true for a migrated
// one. A version above the tombstone marker means a newer gridctl wrote
// a genuinely newer legacy format, which does not exist today; refuse.
func readLegacyFile(path string, out any) (exists, tombstone bool, err error) {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed names under the store's home
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("reading legacy lockfile %s: %w", path, err)
	}
	version, err := legacyVersionOf(data)
	if err != nil {
		return true, false, fmt.Errorf("parsing legacy lockfile %s: %w", path, err)
	}
	if version == legacyTombstoneVersion {
		return true, true, nil
	}
	if version > legacyTombstoneVersion {
		return true, false, fmt.Errorf("%w (%s is version %d)", ErrNewerLockVersion, path, version)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return true, false, fmt.Errorf("parsing legacy lockfile %s: %w", path, err)
	}
	return true, false, nil
}

// mergeLegacy builds the unified in-memory lockfile from whatever
// legacy files exist. It never writes. A tombstone with no unified file
// is corrupt state (the unified file was removed after migration) and
// is refused with the recovery path named.
func (s *Store) mergeLegacy() (*LockFile, error) {
	var skillLock legacySkillLock
	skillExists, skillTombstone, err := readLegacyFile(s.legacySkillPath(), &skillLock)
	if err != nil {
		return nil, err
	}
	var ctxLock legacyContextLock
	ctxExists, ctxTombstone, err := readLegacyFile(s.legacyContextPath(), &ctxLock)
	if err != nil {
		return nil, err
	}
	if skillTombstone || ctxTombstone {
		return nil, fmt.Errorf("found a migration tombstone but %s is missing; restore it from %s or re-sync from scratch after removing the tombstones",
			s.Path(), s.migrationBackupRoot())
	}

	lf := newLockFile()
	// File-level fields neither legacy schema models ride into the
	// unified file rather than being dropped (Article XVII). The two
	// legacy namespaces never collided with each other, so a plain merge
	// is safe; keys colliding with the unified schema are sanitized.
	for _, legacy := range []struct {
		extra map[string]any
		path  string
	}{
		{skillLock.Extra, s.legacySkillPath()},
		{ctxLock.Extra, s.legacyContextPath()},
	} {
		for k, v := range sanitizeExtra(legacy.extra, unifiedFileKeys, legacy.path) {
			if lf.Extra == nil {
				lf.Extra = map[string]any{}
			}
			lf.Extra[k] = v
		}
	}
	if skillExists {
		for skill, clients := range skillLock.Projections {
			for client, e := range clients {
				lf.Projections = append(lf.Projections, &Entry{
					Kind:             KindSkill,
					Client:           client,
					Source:           skill,
					Path:             e.Target,
					Channel:          e.Channel,
					CreatedByGridctl: e.CreatedByGridctl,
					TreeHash:         e.TreeHash,
					SyncedAt:         e.SyncedAt,
					Extra:            sanitizeExtra(e.Extra, unifiedEntryKeys, s.legacySkillPath()),
				})
			}
		}
	}
	if ctxExists {
		scope := ctxLock.Scope
		if scope == "" {
			scope = contextScope
		}
		for client, e := range ctxLock.Clients {
			lf.Projections = append(lf.Projections, &Entry{
				Kind:          KindContext,
				Client:        client,
				Source:        scope,
				Path:          e.Target,
				Strategy:      e.Strategy,
				InstalledHash: e.InstalledHash,
				CanonicalHash: e.CanonicalHash,
				CreatedFile:   e.CreatedFile,
				SyncedAt:      e.SyncedAt,
				Extra:         sanitizeExtra(e.Extra, unifiedEntryKeys, s.legacyContextPath()),
			})
		}
	}
	sortEntries(lf.Projections)
	if err := verifyOwnership(lf.Projections); err != nil {
		return nil, fmt.Errorf("refusing to merge legacy lockfiles %s and %s: %w",
			s.legacySkillPath(), s.legacyContextPath(), err)
	}
	return lf, nil
}

// migrateLocked performs the one-time on-disk migration. Caller holds
// the flock. Idempotent: once the unified file exists it is
// authoritative and the migration never runs again. Sequence: back up
// both legacy files, write the unified file, then tombstone each legacy
// file so a downgraded binary fails loudly instead of diverging.
func (s *Store) migrateLocked() error {
	if fileExists(s.Path()) {
		// The unified file is authoritative. A live (version 1) legacy
		// file next to it means a crash between the unified write and
		// the tombstone write, or a hand-restored backup; finish the
		// tombstoning so a downgraded binary cannot silently work from
		// the stale legacy state.
		return s.repairTombstonesLocked()
	}
	skillExists := fileExists(s.legacySkillPath())
	ctxExists := fileExists(s.legacyContextPath())
	if !skillExists && !ctxExists {
		return nil // fresh install: the unified file appears on first save
	}
	merged, err := s.mergeLegacy()
	if err != nil {
		return err
	}

	backupDir := filepath.Join(s.migrationBackupRoot(), time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("creating migration backup directory: %w", err)
	}
	for _, src := range []string{s.legacySkillPath(), s.legacyContextPath()} {
		if !fileExists(src) {
			continue
		}
		data, err := os.ReadFile(src) // #nosec G304 -- fixed names under the store's home
		if err != nil {
			return fmt.Errorf("backing up %s: %w", src, err)
		}
		if err := os.WriteFile(filepath.Join(backupDir, filepath.Base(src)), data, 0o644); err != nil {
			return fmt.Errorf("writing migration backup: %w", err)
		}
	}

	if err := writeLockFile(s.Path(), merged); err != nil {
		return err
	}
	for _, legacy := range []string{s.legacySkillPath(), s.legacyContextPath()} {
		if !fileExists(legacy) {
			continue
		}
		if err := s.writeTombstone(legacy, backupDir); err != nil {
			return err
		}
	}
	slog.Debug("migrated projection lockfiles to unified project lockfile",
		"unified", s.Path(), "backup", backupDir,
		"skill_lock", skillExists, "context_lock", ctxExists)
	return nil
}

// repairTombstonesLocked re-tombstones any live legacy lockfile left
// beside an existing unified file, backing it up first. No-op in the
// steady state (legacy paths absent or already tombstoned).
func (s *Store) repairTombstonesLocked() error {
	var live []string
	for _, legacy := range []string{s.legacySkillPath(), s.legacyContextPath()} {
		if !fileExists(legacy) {
			continue
		}
		data, err := os.ReadFile(legacy) // #nosec G304 -- fixed names under the store's home
		if err != nil {
			return fmt.Errorf("reading legacy lockfile %s: %w", legacy, err)
		}
		if version, err := legacyVersionOf(data); err == nil && version == legacyTombstoneVersion {
			continue
		}
		live = append(live, legacy)
	}
	if len(live) == 0 {
		return nil
	}
	// MkdirTemp keeps the repair backup distinct from the original
	// migration's dir even within the same timestamp second.
	if err := os.MkdirAll(s.migrationBackupRoot(), 0o755); err != nil {
		return fmt.Errorf("creating migration backup root: %w", err)
	}
	backupDir, err := os.MkdirTemp(s.migrationBackupRoot(), time.Now().UTC().Format("20060102-150405")+"-*")
	if err != nil {
		return fmt.Errorf("creating migration backup directory: %w", err)
	}
	for _, legacy := range live {
		data, err := os.ReadFile(legacy) // #nosec G304 -- fixed names under the store's home
		if err != nil {
			return fmt.Errorf("backing up %s: %w", legacy, err)
		}
		if err := os.WriteFile(filepath.Join(backupDir, filepath.Base(legacy)), data, 0o644); err != nil {
			return fmt.Errorf("writing migration backup: %w", err)
		}
		if err := s.writeTombstone(legacy, backupDir); err != nil {
			return err
		}
	}
	slog.Debug("re-tombstoned legacy projection lockfiles found beside the unified lockfile",
		"unified", s.Path(), "backup", backupDir, "paths", live)
	return nil
}

// writeTombstone replaces a legacy lockfile with a version-2 marker.
// Legacy readers reject Version > 1 with "written by a newer gridctl
// version", so this is what makes a downgrade loud. The note names the
// unified file and the backup for a human reading the YAML.
func (s *Store) writeTombstone(path, backupDir string) error {
	tombstone := struct {
		Version int    `yaml:"version"`
		Note    string `yaml:"note"`
	}{
		Version: legacyTombstoneVersion,
		Note: fmt.Sprintf("projection state moved to %s; this tombstone makes older gridctl versions (before the unified project lockfile, v0.1.0-beta.16) fail loudly instead of diverging. The original file is preserved at %s.",
			s.Path(), filepath.Join(backupDir, filepath.Base(path))),
	}
	data, err := yaml.Marshal(tombstone)
	if err != nil {
		return fmt.Errorf("marshaling tombstone: %w", err)
	}
	return AtomicWriteFile(path, data)
}

// fileExists reports whether path exists as a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
