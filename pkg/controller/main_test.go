package controller

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

// TestMain sandboxes HOME for the whole package so no test (or a
// goroutine leaked past a test) can read or write the real home
// directory. Per-test t.Setenv("HOME", t.TempDir()) calls remain valid
// and simply narrow the sandbox further.
func TestMain(m *testing.M) {
	// A reap test starts this binary as a short-lived child. Exit before
	// the HOME sandbox so the helper does not create a directory.
	if os.Getenv("GRIDCTL_HELPER_PROCESS") == "1" {
		code, err := strconv.Atoi(os.Getenv("GRIDCTL_HELPER_EXIT"))
		if err != nil {
			code = 1
		}
		os.Exit(code)
	}
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
