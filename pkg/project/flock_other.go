//go:build !unix

package project

import (
	"context"
	"sync"
)

// Non-Unix platforms have no syscall.Flock, so mutating operations fall
// back to an in-process mutex keyed by lockfile path. This is an
// accepted gap: two gridctl processes on Windows can interleave
// lockfile read-modify-write cycles, exactly as the pre-engine
// pkg/contexts did on every platform. The atomic rename in
// AtomicWriteFile still prevents torn files.
var (
	fallbackMu    sync.Mutex
	fallbackLocks = map[string]*sync.Mutex{}
)

func (s *Store) withFlock(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fallbackMu.Lock()
	mu, ok := fallbackLocks[s.flockPath()]
	if !ok {
		mu = &sync.Mutex{}
		fallbackLocks[s.flockPath()] = mu
	}
	fallbackMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
