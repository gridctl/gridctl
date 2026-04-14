package main

import (
	"os"
	"os/exec"
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
