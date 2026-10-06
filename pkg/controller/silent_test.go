package controller

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestCheckState_SilentSkipsReplacePrints(t *testing.T) {
	setTempHome(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	name := "silent-stack"
	if err := state.Save(&state.DaemonState{
		StackName: name,
		StackFile: "/tmp/silent-stack.yaml",
		PID:       cmd.Process.Pid,
		Port:      8180,
	}); err != nil {
		t.Fatal(err)
	}
	stack := &config.Stack{Name: name}

	stdout := captureStdout(t)
	sc := New(Config{Replace: true, Silent: true, StackPath: "/tmp/silent-stack.yaml"})
	if err := sc.checkState(stack); err != nil {
		t.Fatal(err)
	}
	if got := stdout(); strings.Contains(got, "Stopping running stack") || strings.Contains(got, "Cleaned up stale") {
		t.Fatalf("silent stdout = %q", got)
	}
}

func TestCheckState_QuietStillPrintsReplace(t *testing.T) {
	setTempHome(t)
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	name := "quiet-stack"
	if err := state.Save(&state.DaemonState{
		StackName: name,
		StackFile: "/tmp/quiet-stack.yaml",
		PID:       cmd.Process.Pid,
		Port:      8180,
	}); err != nil {
		t.Fatal(err)
	}
	stdout := captureStdout(t)
	sc := New(Config{Replace: true, Quiet: true, StackPath: "/tmp/quiet-stack.yaml"})
	if err := sc.checkState(&config.Stack{Name: name}); err != nil {
		t.Fatal(err)
	}
	if got := stdout(); !strings.Contains(got, "Stopping running stack 'quiet-stack'") {
		t.Fatalf("quiet stdout = %q", got)
	}
}

func captureStdout(t *testing.T) func() string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })
	return func() string {
		_ = w.Close()
		os.Stdout = orig
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		_ = r.Close()
		return buf.String()
	}
}
