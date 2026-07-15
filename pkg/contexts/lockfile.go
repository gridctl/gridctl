package contexts

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// lockVersion is the current lock file schema version. Readers reject
// newer versions with ErrNewerLockVersion instead of silently clobbering
// state written by a newer gridctl (the pkg/pins lesson).
const lockVersion = 1

// ErrNewerLockVersion signals a lock file written by a newer gridctl.
var ErrNewerLockVersion = errors.New("context lock file was written by a newer gridctl version")

// LockFile records, per client, what gridctl last wrote and from which
// canonical revision. Drift is judged against InstalledHash; staleness
// against CanonicalHash.
type LockFile struct {
	Version int `yaml:"version"`
	// Scope is "global" today. Present so a future project scope can
	// share the schema without a format break.
	Scope   string                  `yaml:"scope"`
	Clients map[string]*ClientEntry `yaml:"clients"`
}

// ClientEntry is one client's sync record.
type ClientEntry struct {
	Strategy string `yaml:"strategy"`
	// Target is the absolute path gridctl wrote to.
	Target string `yaml:"target"`
	// InstalledHash is the managed-region hash exactly as written.
	InstalledHash string `yaml:"installed_hash"`
	// CanonicalHash is the canonical file's hash at sync time.
	CanonicalHash string `yaml:"canonical_hash"`
	// CreatedFile records whether gridctl created the target file itself
	// (unsync then removes the whole file, not just the managed region).
	CreatedFile bool      `yaml:"created_file"`
	SyncedAt    time.Time `yaml:"synced_at"`
}

// newLockFile returns an empty lock for the global scope.
func newLockFile() *LockFile {
	return &LockFile{Version: lockVersion, Scope: "global", Clients: map[string]*ClientEntry{}}
}

// readLockFile loads the lock from path. A missing file is the normal
// never-synced state and yields an empty lock.
func readLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newLockFile(), nil
		}
		return nil, fmt.Errorf("reading context lock file: %w", err)
	}
	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing context lock file: %w", err)
	}
	if lf.Version > lockVersion {
		return nil, fmt.Errorf("%w (file version %d, supported %d)", ErrNewerLockVersion, lf.Version, lockVersion)
	}
	if lf.Clients == nil {
		lf.Clients = map[string]*ClientEntry{}
	}
	if lf.Scope == "" {
		lf.Scope = "global"
	}
	return &lf, nil
}

// writeLockFile persists the lock atomically.
func writeLockFile(path string, lf *LockFile) error {
	lf.Version = lockVersion
	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling context lock file: %w", err)
	}
	return atomicWriteFile(path, data)
}
