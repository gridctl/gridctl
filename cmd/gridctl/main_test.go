package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain sandboxes HOME for the whole package so no test (or a
// goroutine leaked past a test) can read or write the real home
// directory. Per-test t.Setenv("HOME", t.TempDir()) calls remain valid
// and simply narrow the sandbox further.
//
// Go's caches are pinned to their real locations first: the serve
// integration test shells out to `go build`, and with an empty HOME
// the go command would re-download the toolchain and module graph into
// the sandbox as read-only files that cleanup cannot remove.
func TestMain(m *testing.M) {
	if out, err := exec.Command("go", "env", "GOPATH", "GOCACHE", "GOMODCACHE").Output(); err == nil {
		vals := strings.Split(strings.TrimSpace(string(out)), "\n")
		for i, key := range []string{"GOPATH", "GOCACHE", "GOMODCACHE"} {
			if i < len(vals) && os.Getenv(key) == "" && strings.TrimSpace(vals[i]) != "" {
				os.Setenv(key, strings.TrimSpace(vals[i])) //nolint:errcheck // best-effort pinning
			}
		}
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
