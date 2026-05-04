package telemetry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// wipeLockTimeout bounds how long Wipe will wait for state.WithLock to
// acquire the per-stack flock. The flock serializes wipes against daemon
// startup (which seeds in-memory buffers from disk). Ten seconds is generous
// for a filesystem operation but short enough that a stuck wipe surfaces as
// an explicit error rather than a hang.
const wipeLockTimeout = 10 * time.Second

// Wipe deletes telemetry files matching the requested scope. Empty serverName
// or signal acts as a wildcard. Both the active <sig>.jsonl and any rotated
// lumberjack siblings are removed.
//
// Scope combinations:
//
//	serverName="", signal=""      → everything for this stack
//	serverName="X", signal=""     → all signals for server X
//	serverName="", signal="logs"  → logs across all servers in the stack
//	serverName="X", signal="logs" → logs for server X only
//
// The operation is wrapped in state.WithLock so a wipe and a daemon
// reload/seed cannot interleave on the same stack file. Lumberjack keeps
// its active file open across rotations; on POSIX, removing the file while
// the writer holds it is safe (unlink-on-close), and lumberjack will
// transparently reopen on the next emit.
//
// A missing stack directory is treated as a successful no-op; signal
// validation (against the known {logs, metrics, traces}) is the caller's
// responsibility.
func Wipe(stackName, serverName, signal string) error {
	if stackName == "" {
		return fmt.Errorf("stack name is required")
	}
	return state.WithLock(stackName, wipeLockTimeout, func() error {
		return doWipe(stackName, serverName, signal)
	})
}

func doWipe(stackName, serverName, signal string) error {
	stackDir := filepath.Join(state.TelemetryDir(), stackName)
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read telemetry dir: %w", err)
	}
	// Accumulate per-file failures rather than aborting on the first error,
	// so a half-applied wipe never silently leaves intact files behind a
	// single transient failure (e.g. a stale handle on macOS, a perms gap
	// on one rotated sibling). Callers see the joined error and can retry.
	var errs []error
	for _, srv := range entries {
		if !srv.IsDir() {
			continue
		}
		if serverName != "" && srv.Name() != serverName {
			continue
		}
		if err := wipeServerDir(filepath.Join(stackDir, srv.Name()), signal); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func wipeServerDir(srvDir, signal string) error {
	files, err := os.ReadDir(srvDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read server telemetry dir: %w", err)
	}
	var errs []error
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		sig := matchSignal(f.Name())
		if sig == "" {
			continue
		}
		if signal != "" && sig != signal {
			continue
		}
		fpath := filepath.Join(srvDir, f.Name())
		if err := os.Remove(fpath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", fpath, err))
		}
	}
	return errors.Join(errs...)
}
