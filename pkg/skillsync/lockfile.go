package skillsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// lockVersion is the current lockfile schema version. Readers reject
// newer versions with ErrNewerLockVersion instead of silently clobbering
// state written by a newer gridctl (the pkg/pins lesson).
const lockVersion = 1

// flockTimeout bounds how long a mutating operation waits for the
// cross-process lock before reporting contention.
const flockTimeout = 5 * time.Second

// ErrNewerLockVersion signals a lockfile written by a newer gridctl.
var ErrNewerLockVersion = errors.New("skill projection lockfile was written by a newer gridctl version")

// LockFile records, per skill and client, what gridctl last projected.
// Ownership (CreatedByGridctl) is what lets sync refuse to clobber
// foreign paths and lets unsync remove only gridctl's own artifacts.
type LockFile struct {
	Version int `yaml:"version"`
	// Projections maps skill name → client slug → entry.
	Projections map[string]map[string]*Entry `yaml:"projections"`
}

// Entry is one (skill, client) projection record.
type Entry struct {
	// Channel is "symlink" or "copy".
	Channel Channel `yaml:"channel"`
	// Target is the absolute path gridctl created (the symlink itself or
	// the copied directory).
	Target string `yaml:"target"`
	// CreatedByGridctl marks the path as gridctl-owned. Always true for
	// recorded entries; present in the schema so a future adopt flow can
	// track foreign paths without a format break.
	CreatedByGridctl bool `yaml:"created_by_gridctl"`
	// TreeHash is the copied directory's tree hash at sync time (empty
	// for symlinks, whose content lives in the registry).
	TreeHash string    `yaml:"tree_hash,omitempty"`
	SyncedAt time.Time `yaml:"synced_at"`
}

// newLockFile returns an empty lockfile.
func newLockFile() *LockFile {
	return &LockFile{Version: lockVersion, Projections: map[string]map[string]*Entry{}}
}

// entry returns the record for (skill, client), or nil.
func (lf *LockFile) entry(skill, client string) *Entry {
	return lf.Projections[skill][client]
}

// set records an entry for (skill, client).
func (lf *LockFile) set(skill, client string, e *Entry) {
	if lf.Projections[skill] == nil {
		lf.Projections[skill] = map[string]*Entry{}
	}
	lf.Projections[skill][client] = e
}

// remove deletes the record for (skill, client), dropping the skill key
// when its last client entry goes.
func (lf *LockFile) remove(skill, client string) {
	delete(lf.Projections[skill], client)
	if len(lf.Projections[skill]) == 0 {
		delete(lf.Projections, skill)
	}
}

// readLockFile loads the lockfile from path. A missing file is the
// normal nothing-projected state and yields an empty lock.
func readLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed name under the manager's home
	if err != nil {
		if os.IsNotExist(err) {
			return newLockFile(), nil
		}
		return nil, fmt.Errorf("reading skill projection lockfile: %w", err)
	}
	var lf LockFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing skill projection lockfile: %w", err)
	}
	if lf.Version > lockVersion {
		return nil, fmt.Errorf("%w (file version %d, supported %d)", ErrNewerLockVersion, lf.Version, lockVersion)
	}
	if lf.Projections == nil {
		lf.Projections = map[string]map[string]*Entry{}
	}
	return &lf, nil
}

// writeLockFile persists the lockfile atomically (temp + rename).
func writeLockFile(path string, lf *LockFile) error {
	lf.Version = lockVersion
	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling skill projection lockfile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating lockfile directory: %w", err)
	}
	return atomicWriteFile(path, data)
}

// withFileLock runs fn while holding an exclusive flock on a sibling of
// the lockfile. The in-process mutex alone is not enough: the CLI and
// the daemon reconcile mutate the same lockfile from different
// processes. Follows the pkg/state.WithLock pattern.
func (m *Manager) withFileLock(ctx context.Context, fn func() error) error {
	lockPath := m.LockPath() + ".flock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644) // #nosec G304 -- fixed name under the manager's home
	if err != nil {
		return fmt.Errorf("opening projection lock: %w", err)
	}
	defer f.Close() //nolint:errcheck // closed after unlock; nothing was written

	deadline := time.Now().Add(flockTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring skill projection lock (another gridctl operation may be in progress)")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()
	return fn()
}
