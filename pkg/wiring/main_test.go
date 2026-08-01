package wiring

import (
	"fmt"
	"os"
	"testing"
)

// TestMain sandboxes HOME for the whole run: nothing in this package may
// resolve the real home directory (PR #1024 discipline).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "wiring-test-home-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating sandbox home:", err)
		os.Exit(1)
	}
	os.Setenv("HOME", tmp)
	os.Setenv("USERPROFILE", tmp)
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
