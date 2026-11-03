package hookpins

import (
	"fmt"
	"os"
	"testing"
)

// TestMain sandboxes HOME so a store that resolves the real pin path cannot
// touch the user's home. Per-test t.Setenv calls narrow the sandbox further.
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
