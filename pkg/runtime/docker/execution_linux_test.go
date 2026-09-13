//go:build linux

package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecution_KernelIdentityMapping(t *testing.T) {
	for _, tc := range []struct {
		mapping         string
		inside, outside uint64
		want            bool
	}{
		{"0 0 4294967295", 65534, 65534, true},
		{"0 1000 1\n1 100000 65536", 65534, 165533, true},
		{"0 1000 1\n1 100000 65536", 65534, 65534, false},
		{"0 1000 1", 1, 1001, false},
		{"malformed\n0 -1 1", 0, 0, false},
		{"0 0 4294967296", 1, 1, false},
	} {
		if got := mappedExecutionID(tc.mapping, tc.inside, tc.outside); got != tc.want {
			t.Fatalf("mapping outcome %v, want %v", got, tc.want)
		}
	}
}

func TestExecution_KernelSizeBounds(t *testing.T) {
	for input, want := range map[string]int64{"65536": 65536, "64k": 65536, "64m": 67108864, "2g": 2147483648, "-1": -1, "unlimited": -1, "64G": -1, "18446744073709551616": -1, "1125899906842625": -1} {
		if got := executionKernelSize(input); got != want {
			t.Fatalf("%q: %d, want %d", input, got, want)
		}
	}
}

func TestExecution_KernelReadBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "limit"), []byte("268435456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oversized"), []byte(strings.Repeat("x", 65537)), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("synthetic-private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if value, err := executionRead(t.Context(), root, "limit"); err != nil || value != "268435456\n" {
		t.Fatalf("bounded read: %q %v", value, err)
	}
	for _, name := range []string{"missing", "oversized", "escape", "../private"} {
		value, err := executionRead(t.Context(), root, name)
		if err == nil || value != "" || strings.Contains(err.Error(), outside) {
			t.Fatalf("unsafe observation boundary for %s", name)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := executionRead(ctx, root, "limit"); !errors.Is(err, context.Canceled) {
		t.Fatal("kernel read ignored cancellation")
	}
}

func TestExecution_RejectsUnboundKernelObservation(t *testing.T) {
	contract := executionTestContract(t)
	if _, err := observeExecution(t.Context(), "label-only", os.Getpid(), contract); err == nil {
		t.Fatal("label became instance identity")
	}
	if _, err := observeExecution(t.Context(), strings.Repeat("a", 64), os.Getpid(), contract); err == nil {
		t.Fatal("unrelated local process became daemon evidence")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := observeExecution(ctx, strings.Repeat("a", 64), os.Getpid(), contract); !errors.Is(err, context.Canceled) {
		t.Fatal("observation ignored cancellation")
	}
}
