package runs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

const wipeLockTimeout = 10 * time.Second

var (
	registryMu sync.Mutex
	registry   = map[string]*Recorder{}

	// lifecycleMu serializes opening and registering a recorder against an
	// in-process wipe, so the wipe never unlinks behind a writer that opened
	// after its registry check.
	lifecycleMu sync.Mutex
)

// NewActiveRecorder creates a recorder and registers it for cfg.StackName
// while holding the lock WipeStackInProcess takes. Use it instead of
// NewRecorder followed by RegisterActive in the daemon that owns the stack.
func NewActiveRecorder(cfg Config, logger *slog.Logger) (*Recorder, error) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	rec, err := NewRecorder(cfg, logger)
	if err != nil {
		return nil, err
	}
	RegisterActive(cfg.StackName, rec)
	return rec, nil
}

// RegisterActive records the live recorder for a stack so wipe can
// coordinate instead of unlinking behind an open writer.
func RegisterActive(stackName string, rec *Recorder) {
	if stackName == "" || rec == nil {
		return
	}
	registryMu.Lock()
	registry[stackName] = rec
	registryMu.Unlock()
}

// UnregisterActive drops the live recorder for a stack.
func UnregisterActive(stackName string) {
	registryMu.Lock()
	delete(registry, stackName)
	registryMu.Unlock()
}

func activeRecorder(stackName string) *Recorder {
	registryMu.Lock()
	defer registryMu.Unlock()
	return registry[stackName]
}

// ErrActiveWriter means an offline wipe refused to unlink behind a live writer.
var ErrActiveWriter = errors.New("runs wipe refused: an active recorder owns this stack; use the live daemon")

// ErrPerServerRuns is returned when a caller asks to wipe runs for one server.
var ErrPerServerRuns = errors.New("runs history is stack-wide; omit --server and wipe the stack, or use gridctl runs wipe")

// WipeStack deletes owned run files for a stack. When a live recorder is
// registered it performs a coordinated barrier. Offline wipe refuses if a
// daemon is running for the stack unless coordinateOffline is true and no
// in-process recorder exists — then it still refuses rather than unlink
// behind a foreign writer.
func WipeStack(ctx context.Context, stackName string) error {
	if stackName == "" {
		return fmt.Errorf("stack name is required")
	}
	if rec := activeRecorder(stackName); rec != nil {
		return rec.Wipe(ctx)
	}
	running, err := daemonRunning(stackName)
	if err != nil {
		return err
	}
	if running {
		return ErrActiveWriter
	}
	dir, err := Dir(stackName)
	if err != nil {
		return err
	}
	return state.WithLock(stackName, wipeLockTimeout, func() error {
		return deleteOwned(dir)
	})
}

// WipeStackInProcess deletes owned run files for a stack from inside the
// process that owns it. Only the daemon serving the stack may call it: when
// no recorder is registered it deletes under the stack lock without the
// daemon-state check, because the live daemon in that file is the caller.
func WipeStackInProcess(ctx context.Context, stackName string) error {
	if stackName == "" {
		return fmt.Errorf("stack name is required")
	}
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if rec := activeRecorder(stackName); rec != nil {
		return rec.Wipe(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := Dir(stackName)
	if err != nil {
		return err
	}
	return state.WithLock(stackName, wipeLockTimeout, func() error {
		return deleteOwned(dir)
	})
}

func daemonRunning(stackName string) (bool, error) {
	st, err := state.Load(stackName)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return state.IsRunning(st), nil
}

func deleteOwned(dir string) error {
	if err := refuseSymlinkParents(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := openDirNoFollow(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		if !isOwnedName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			errs = append(errs, fmt.Errorf("refusing to delete non-regular owned path %s", name))
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
