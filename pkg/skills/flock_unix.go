//go:build unix

package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// importLockTimeout bounds how long a mutating operation waits for the
// cross-process import lock before reporting contention.
const importLockTimeout = 5 * time.Second

// withLockFileFlock runs fn while holding an exclusive flock on the
// import lockfile's ".flock" sibling. The Importer's in-process mutex is
// not enough: the CLI, pack operations, and the API server all
// read-modify-write skills.lock.yaml, each from its own process (and the
// API server builds a fresh Importer per request), so without a
// cross-process lock two concurrent operations silently lose one side's
// update. Follows the pkg/project withFlock pattern.
func withLockFileFlock(ctx context.Context, lockFilePath string, fn func() error) error {
	flockPath := lockFilePath + ".flock"
	if err := os.MkdirAll(filepath.Dir(flockPath), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	f, err := os.OpenFile(flockPath, os.O_CREATE|os.O_RDWR, 0o644) // #nosec G304 -- sibling of the caller's lockfile path
	if err != nil {
		return fmt.Errorf("opening import lock: %w", err)
	}
	defer f.Close() //nolint:errcheck // closed after unlock; nothing was written

	deadline := time.Now().Add(importLockTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (another gridctl operation may be in progress)", ErrImportLockBusy)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()
	return fn()
}
