package pins

import (
	"fmt"
	"os"
	"testing"
)

// TestMain sandboxes HOME for the whole package so no test (or a
// goroutine leaked past a test) can read or write the real home
// directory. Per-test t.Setenv("HOME", t.TempDir()) calls remain valid
// and simply narrow the sandbox further.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gridctl-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating sandbox home:", err)
		os.Exit(1)
	}
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintln(os.Stderr, "sandboxing HOME:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(home) //nolint:errcheck // best-effort cleanup on exit
	os.Exit(code)
}
