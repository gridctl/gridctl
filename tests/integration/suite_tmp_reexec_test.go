//go:build integration && !windows

package integration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const suiteTmpProbeEnv = "GRIDCTL_SUITE_TMP_PROBE"

// TestSuiteTmp_Probe is the child half of TestSuiteTmp_RunLeavesNoTempDirs.
// It skips unless the parent sets the probe variable.
func TestSuiteTmp_Probe(t *testing.T) {
	mode := os.Getenv(suiteTmpProbeEnv)
	if mode == "" {
		t.Skip("probe runs only as a child of TestSuiteTmp_RunLeavesNoTempDirs")
	}
	dir, err := sharedTempDir("probe")
	if err != nil {
		t.Fatalf("sharedTempDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gridctl"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("suite tmp probe dir: %s", dir)
	if mode == "fail" {
		t.Fatal("probe asked to fail")
	}
}

// TestSuiteTmp_RunLeavesNoTempDirs re-executes the test binary with a fresh
// TMPDIR so the child runs the real TestMain, mock server builds included,
// and checks that a passing and a failing run both leave TMPDIR empty.
func TestSuiteTmp_RunLeavesNoTempDirs(t *testing.T) {
	if os.Getenv(suiteTmpProbeEnv) != "" {
		t.Skip("already running as the probe child")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probeLine := regexp.MustCompile(`suite tmp probe dir: (\S+)`)

	for _, mode := range []string{"pass", "fail"} {
		t.Run(mode, func(t *testing.T) {
			tmp := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			cmd := exec.CommandContext(ctx, self, "-test.run", "^TestSuiteTmp_Probe$", "-test.count=1", "-test.v")
			cmd.Dir = cwd
			env := make([]string, 0, len(os.Environ())+2)
			for _, kv := range os.Environ() {
				if strings.HasPrefix(kv, "TMPDIR=") || strings.HasPrefix(kv, suiteTmpProbeEnv+"=") {
					continue
				}
				env = append(env, kv)
			}
			cmd.Env = append(env, "TMPDIR="+tmp, suiteTmpProbeEnv+"="+mode)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("probe child timed out: %v\n%s", ctx.Err(), out)
			}

			var exitErr *exec.ExitError
			switch mode {
			case "pass":
				if err != nil {
					t.Fatalf("passing probe child exited with %v\n%s", err, out)
				}
			case "fail":
				if !errors.As(err, &exitErr) {
					t.Fatalf("failing probe child did not exit nonzero: %v\n%s", err, out)
				}
			}

			m := probeLine.FindSubmatch(out)
			if m == nil {
				t.Fatalf("probe child did not report its shared dir\n%s", out)
			}
			if rel, err := filepath.Rel(tmp, string(m[1])); err != nil || strings.HasPrefix(rel, "..") {
				t.Fatalf("probe shared dir %s is outside the child TMPDIR %s", m[1], tmp)
			}

			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			var left []string
			for _, e := range entries {
				left = append(left, e.Name())
			}
			if len(left) > 0 {
				t.Fatalf("probe child (%s) left entries in TMPDIR: %v\n%s", mode, left, out)
			}
		})
	}
}
