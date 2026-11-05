//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDaemonParent_ChildExitSurfaced is sub-mode A: an unreadable pin file
// makes the daemon child exit immediately. The parent must report that exit
// and the log tail without waiting out the readiness budget, and must not
// leave the child running. No container runtime is required.
func TestDaemonParent_ChildExitSurfaced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chmod 000 does not deny root read access")
	}
	buildCtx, buildCancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer buildCancel()

	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, buildCtx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mock := buildRaceBinary(t, buildCtx, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), filepath.Join(dir, "mock"))

	const stackName = "parent-exit"
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	stack := fmt.Sprintf("version: \"1\"\nname: %s\nmcp-servers:\n  - name: echo\n    command: [%q]\n", stackName, mock)
	if err := os.WriteFile(stackPath, []byte(stack), 0o644); err != nil {
		t.Fatal(err)
	}

	pinPath := filepath.Join(home, ".gridctl", "pins", stackName+".json")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatal(err)
	}
	pinBody := fmt.Sprintf("{\"version\":\"2\",\"stack\":%q,\"created_at\":\"2026-01-01T00:00:00Z\",\"servers\":{}}\n", stackName)
	if err := os.WriteFile(pinPath, []byte(pinBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(pinPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pinPath, 0o600) })

	port := freePort(t)
	applyCtx, applyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer applyCancel()
	cmd := exec.CommandContext(applyCtx, bin, "apply", stackPath, "--port", strconv.Itoa(port), "--quiet")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GRIDCTL_HOME=" + home,
		"NO_COLOR=1",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	errText := stderr.String()
	if elapsed >= 15*time.Second {
		t.Fatalf("apply took %s, want under 15s\nstdout:\n%s\nstderr:\n%s", elapsed, stdout.String(), errText)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("exit = %v, want 1\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), errText)
	}
	for _, want := range []string{
		"exited during startup",
		"exit status",
		"permission denied",
		filepath.Base(pinPath),
		"gridctl logs",
	} {
		if !strings.Contains(errText, want) {
			t.Errorf("stderr missing %q\n%s", want, errText)
		}
	}
	statePath := filepath.Join(home, ".gridctl", "state", stackName+".json")
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("state file %s still present (%v)", statePath, statErr)
	}
	cmdline, scanErr := daemonChildCommand(stackPath)
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if cmdline != "" {
		t.Fatalf("daemon child still running: %s", cmdline)
	}
}

// TestDaemonParent_ProcessListCanRun checks the non-Linux listing itself.
// Linux uses /proc in the assertion above; this still runs ps so a broken
// fallback command fails here instead of only on Darwin.
func TestDaemonParent_ProcessListCanRun(t *testing.T) {
	cmdline, err := daemonChildCommandPS(filepath.Join(t.TempDir(), "stack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cmdline != "" {
		t.Fatalf("ps listing matched a child that was never started: %s", cmdline)
	}
}

// daemonChildCommand returns the command line of a process started as this
// stack's daemon child, or empty when none remains. Linux reads /proc, which
// is the command line the kernel recorded. Other Unix systems have no /proc
// (Darwin in particular, and this file's !windows tag includes them), so they
// list processes with ps. A failure to list is an error: the "no child"
// assertion must not pass because the scan could not look.
func daemonChildCommand(stackPath string) (string, error) {
	if runtime.GOOS == "linux" {
		return daemonChildCommandProc(stackPath)
	}
	return daemonChildCommandPS(stackPath)
}

func daemonChildCommandProc(stackPath string) (string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", fmt.Errorf("reading /proc: %w", err)
	}
	for _, ent := range entries {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", ent.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		text := strings.ReplaceAll(string(raw), "\x00", " ")
		if commandLineIsDaemonChild(text, stackPath) {
			return text, nil
		}
	}
	return "", nil
}

func daemonChildCommandPS(stackPath string) (string, error) {
	// -ww disables the terminal-width cap. Darwin ps otherwise truncates
	// args, which can drop a long temp stack path and hide a live child.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axww", "-o", "args=").Output()
	if err != nil {
		return "", fmt.Errorf("listing processes: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if commandLineIsDaemonChild(line, stackPath) {
			return line, nil
		}
	}
	return "", nil
}

func commandLineIsDaemonChild(text, stackPath string) bool {
	return strings.Contains(text, "--daemon-child") && strings.Contains(text, stackPath)
}
