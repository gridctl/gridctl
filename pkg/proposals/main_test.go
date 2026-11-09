package proposals

import (
	"fmt"
	"os"
	"testing"
)

// TestMain sandboxes HOME and GRIDCTL_HOME for the whole package so no
// test (or a goroutine leaked past a test) can read or write the real
// home directory. Per-test t.Setenv calls narrow the sandbox further.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gridctl-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating sandbox home:", err)
		os.Exit(1)
	}
	for _, key := range []string{"HOME", "GRIDCTL_HOME"} {
		if err := os.Setenv(key, home); err != nil {
			fmt.Fprintln(os.Stderr, "sandboxing", key+":", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	os.RemoveAll(home) //nolint:errcheck // best-effort cleanup on exit
	os.Exit(code)
}
