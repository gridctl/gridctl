//go:build !unix

package skills

import (
	"context"
	"sync"
)

// Non-Unix platforms have no syscall.Flock, so mutating operations fall
// back to an in-process mutex keyed by lockfile path (the pkg/project
// fallback shape). Two gridctl processes on Windows can still interleave
// read-modify-write cycles; the atomic rename in atomicWriteBytes
// prevents torn files.
var (
	importFallbackMu    sync.Mutex
	importFallbackLocks = map[string]*sync.Mutex{}
)

func withLockFileFlock(ctx context.Context, lockFilePath string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	importFallbackMu.Lock()
	mu, ok := importFallbackLocks[lockFilePath]
	if !ok {
		mu = &sync.Mutex{}
		importFallbackLocks[lockFilePath] = mu
	}
	importFallbackMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
