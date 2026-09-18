package state

import (
	"context"
	"os"
	"time"
)

// WithLockContext executes fn under the cross-process state lock, honoring
// cancellation while waiting. The callback is responsible for its own I/O.
func WithLockContext(ctx context.Context, name string, fn func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path, err := LockPath(name)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		unlock, err := tryFileLock(file)
		if err == nil {
			defer func() { _ = unlock() }()
			if err := ctx.Err(); err != nil {
				return err
			}
			return fn(ctx)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
