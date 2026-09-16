package runs

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gridctl-runs-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating sandbox home:", err)
		os.Exit(1)
	}
	if err := os.Setenv("HOME", home); err != nil {
		fmt.Fprintln(os.Stderr, "sandboxing HOME:", err)
		os.Exit(1)
	}
	_ = os.Setenv("GRIDCTL_HOME", "")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
