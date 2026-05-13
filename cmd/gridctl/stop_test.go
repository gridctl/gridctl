package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

func setTempHomeStop(t *testing.T) {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	os.Setenv("HOME", t.TempDir())
}

func TestRunStop_NoDaemonRunning(t *testing.T) {
	setTempHomeStop(t)
	// Stub out the orphan scan: the developer's machine may have a
	// real gridctl daemon running outside the test sandbox, and we
	// don't want findOrphan to see it.
	stubFindOrphan(t, func(int) (int, bool, error) { return 0, false, nil })

	err := runStop()
	if err == nil {
		t.Fatal("expected error when no daemon is running")
	}
	if err.Error() != "no stackless daemon is running" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunStop_StaleState(t *testing.T) {
	setTempHomeStop(t)

	// Save state with a dead PID — runStop should clean it up.
	st := &state.DaemonState{
		StackName: "gridctl",
		PID:       999999,
		Port:      8180,
		StartedAt: time.Now(),
	}
	if err := state.Save(st); err != nil {
		t.Fatalf("saving state: %v", err)
	}

	err := runStop()
	if err == nil {
		t.Fatal("expected error for stale (non-running) daemon")
	}
}

func TestRunStop_RunningDaemon(t *testing.T) {
	setTempHomeStop(t)

	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting dummy process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	st := &state.DaemonState{
		StackName: "gridctl",
		PID:       cmd.Process.Pid,
		Port:      8180,
		StartedAt: time.Now(),
	}
	if err := state.Save(st); err != nil {
		t.Fatalf("saving state: %v", err)
	}

	if err := runStop(); err != nil {
		t.Fatalf("expected runStop to succeed, got: %v", err)
	}

	// State file should be gone.
	if _, err := state.Load("gridctl"); err == nil {
		t.Error("expected state file to be deleted after stop")
	}
}

// stubFindOrphan installs a test seam for findOrphan and restores the
// real implementation on test cleanup.
func stubFindOrphan(t *testing.T, fn func(int) (int, bool, error)) {
	t.Helper()
	orig := findOrphan
	findOrphan = fn
	t.Cleanup(func() { findOrphan = orig })
}

func TestRunStop_OrphanDaemon_NoForce(t *testing.T) {
	setTempHomeStop(t)
	stubFindOrphan(t, func(int) (int, bool, error) { return 4242, true, nil })

	err := runStop()
	if err == nil {
		t.Fatal("expected error pointing at the orphan, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pid 4242") || !strings.Contains(msg, ":"+strconv.Itoa(stopDefaultPort)) {
		t.Errorf("expected error to mention pid and port; got: %q", msg)
	}
	if !strings.Contains(msg, "--force") {
		t.Errorf("expected error to suggest --force; got: %q", msg)
	}
}

func TestRunStop_OrphanDaemon_WithForce(t *testing.T) {
	setTempHomeStop(t)

	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting dummy orphan: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	stubFindOrphan(t, func(int) (int, bool, error) { return cmd.Process.Pid, true, nil })

	stopForce = true
	t.Cleanup(func() { stopForce = false })

	if err := runStop(); err != nil {
		t.Fatalf("expected runStop --force to succeed, got: %v", err)
	}
	// Reap the zombie so VerifyPID's signal-0 check is meaningful;
	// without this, a freshly SIGKILLed-but-unreaped child still
	// answers to signal 0 on macOS and the assertion flakes.
	_ = cmd.Wait()
	if state.VerifyPID(cmd.Process.Pid) {
		t.Error("expected orphan process to be terminated")
	}
	// No state file should have been written.
	if _, err := os.Stat(state.StatePath("gridctl")); !os.IsNotExist(err) {
		t.Errorf("expected no state file, got err=%v", err)
	}
}

func TestRunStop_OrphanDaemon_Ambiguous(t *testing.T) {
	setTempHomeStop(t)
	stubFindOrphan(t, func(int) (int, bool, error) { return 0, false, nil })

	err := runStop()
	if err == nil || err.Error() != "no stackless daemon is running" {
		t.Errorf("expected legacy no-daemon error, got: %v", err)
	}
}
