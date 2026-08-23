package project

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LockVersion is the breaking-change tier of the lockfile version:
// readers reject a newer version with ErrNewerLockVersion instead of
// silently clobbering state written by a newer gridctl.
const LockVersion = 1

// lockRevision is the additive tier: a newer gridctl may raise it while
// staying readable by any binary of the same LockVersion. Unknown
// fields written under a higher revision are preserved when this binary
// rewrites the file (entries it actively re-records are written fresh).
// Revision 2 added the models kind and its entry attributes (acked_hash,
// include_ref, include_mode, include_original).
const lockRevision = 2

// LockFile is the on-disk shape of the unified projection lockfile at
// ~/.gridctl/project.lock.yaml. Absence of an entry means "unknown, do
// not touch," never "remove": removal of a projection happens only as
// an explicit engine-driven delete under the cross-process lock
// (Article XVI).
type LockFile struct {
	Version     int      `yaml:"version"`
	Revision    int      `yaml:"revision"`
	Projections []*Entry `yaml:"projections"`

	Extra map[string]any `yaml:",inline"`
}

// newLockFile returns an empty current-version lockfile.
func newLockFile() *LockFile {
	return &LockFile{Version: LockVersion, Revision: lockRevision}
}

// ReadLockFile loads the unified lockfile from path. A missing file is
// the normal nothing-projected state and yields an empty lock.
func ReadLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed name under the store's home
	if err != nil {
		if os.IsNotExist(err) {
			return newLockFile(), nil
		}
		return nil, fmt.Errorf("reading project lockfile: %w", err)
	}
	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing project lockfile %s: %w", path, err)
	}
	if lf.Version > LockVersion {
		return nil, fmt.Errorf("%w (%s is version %d, this gridctl supports %d; upgrade gridctl, or restore the file from ~/.gridctl/project-migration-backup)",
			ErrNewerLockVersion, path, lf.Version, LockVersion)
	}
	return &lf, nil
}

// writeLockFile persists the lockfile atomically. The version is
// stamped current; the revision keeps the highest value seen so a
// higher-revision file rewritten by this binary never reads as
// downgraded (its unknown fields ride along in Extra).
func writeLockFile(path string, lf *LockFile) error {
	lf.Version = LockVersion
	if lf.Revision < lockRevision {
		lf.Revision = lockRevision
	}
	sortEntries(lf.Projections)
	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling project lockfile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating lockfile directory: %w", err)
	}
	return AtomicWriteFile(path, data)
}

// verifyOwnership checks the one-owner invariant across the whole file:
// no two projections may claim the same (client, destination path).
func verifyOwnership(entries []*Entry) error {
	seen := map[string]*Entry{}
	for _, e := range entries {
		k := e.Client + "\x00" + e.Path
		if prev, ok := seen[k]; ok {
			return fmt.Errorf("%w: %s is recorded for both %s %q and %s %q (client %s)",
				ErrPathConflict, e.Path, prev.Kind, prev.Source, e.Kind, e.Source, e.Client)
		}
		seen[k] = e
	}
	return nil
}
