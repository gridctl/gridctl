//go:build unix

package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// flockTimeout bounds how long a mutating operation waits for the
// cross-process lock before reporting contention.
const flockTimeout = 5 * time.Second

// withFlock runs fn while holding an exclusive flock on the lockfile's
// ".flock" sibling. The kind managers' in-process mutexes are not
// enough: the CLI, the daemon reconcile, and the API server mutate the
// same lockfile from different processes. Follows the pkg/state.WithLock
// pattern (and skillsync's withFileLock before it).
func (s *Store) withFlock(ctx context.Context, fn func() error) error {
	lockPath := s.flockPath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644) // #nosec G304 -- fixed name under the store's home
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
			return fmt.Errorf("timeout acquiring projection lock (another gridctl operation may be in progress)")
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()
	return fn()
}
