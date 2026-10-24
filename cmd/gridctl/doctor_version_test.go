package main

import (
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/state"
)

func TestCheckDaemonVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(state.HomeEnv, dir)

	var checks []doctorCheck
	checkDaemonVersion(t.Context(), &checks)
	if len(checks) != 1 || checks[0].ID != "daemon.version" || checks[0].Status != doctorStatusOK {
		t.Fatalf("no daemons: %+v", checks)
	}
	if !strings.Contains(checks[0].Message, "no running daemons") {
		t.Fatalf("message = %q", checks[0].Message)
	}

	if err := state.Save(&state.DaemonState{
		StackName:     "netops",
		PID:           os.Getpid(),
		Port:          8181,
		BinaryVersion: "v1.0.0",
	}); err != nil {
		t.Fatal(err)
	}
	checks = nil
	checkDaemonVersion(t.Context(), &checks)
	if checks[0].Status != doctorStatusWarn || checks[0].ID != "daemon.version" {
		t.Fatalf("mismatch: %+v", checks)
	}
	if !strings.Contains(checks[0].Message, "netops runs v1.0.0") || !strings.Contains(checks[0].Message, "CLI is "+version) {
		t.Fatalf("message = %q", checks[0].Message)
	}
	if !strings.Contains(checks[0].Message, "gridctl upgrade --restart") {
		t.Fatalf("message = %q", checks[0].Message)
	}

	// A stopped daemon with a different version is not a mismatch.
	if err := state.Save(&state.DaemonState{
		StackName:     "netops",
		PID:           1<<30 - 1,
		BinaryVersion: "v0.0.1",
	}); err != nil {
		t.Fatal(err)
	}
	checks = nil
	checkDaemonVersion(t.Context(), &checks)
	if checks[0].Status != doctorStatusOK {
		t.Fatalf("stopped daemon warned: %+v", checks)
	}
}
