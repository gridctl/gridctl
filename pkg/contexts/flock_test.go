package contexts

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrentManagersSerializeOnEngineLock models the CLI and the
// API server mutating context projections at once: two Managers over
// one home have distinct in-process mutexes, so serialization rides on
// the engine's cross-process lock alone (new with the pkg/project
// extraction; the legacy contexts lockfile had no flock).
func TestConcurrentManagersSerializeOnEngineLock(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{".claude", ".gemini"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cli := NewManagerWithHome(home)
	api := NewManagerWithHome(home)
	if err := cli.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := cli.SyncAll(ctx, SyncOptions{}); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := api.SyncClient(ctx, "gemini", SyncOptions{}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent operation failed: %v", err)
	}

	lf, err := cli.loadView(ctx)
	if err != nil {
		t.Fatalf("lockfile corrupt after concurrent access: %v", err)
	}
	for _, slug := range []string{"claude-code", "gemini"} {
		if lf.Clients[slug] == nil {
			t.Errorf("lockfile lost the %s entry", slug)
		}
	}
}
