//go:build linux

package mcp

import (
	"context"
	"github.com/gridctl/gridctl/pkg/execution"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessSignalHelper(t *testing.T) {
	if os.Getenv("PROCESS_SIGNAL_FIXTURE") == "descendant" {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		child := exec.Command("/bin/sleep", "30")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("PROCESS_CHILD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
			os.Exit(2)
		}
		<-signals
		_ = child.Wait()
		os.Exit(0)
	}
	if os.Getenv("PROCESS_SIGNAL_FIXTURE") != "ignore" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	for {
		time.Sleep(time.Second)
	}
}

func TestProcessClient_EscalatesAndReaps(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	none := []string{}
	client := newProcessClient("ignore-term", []string{bin, "-test.run=^TestProcessSignalHelper$"}, "", map[string]string{"PROCESS_SIGNAL_FIXTURE": "ignore"}, &execution.ExecutionContract{Mode: "local", Lookup: "absolute", Inherit: &none})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if group, err := syscall.Getpgid(client.PID()); err != nil || group != client.PID() {
		t.Fatal("selected local execution did not establish an owned process group")
	}
	defer client.Close()
	// Wait for the fixture's signal disposition, not an arbitrary sleep.
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, readErr := os.ReadFile("/proc/" + strconv.Itoa(client.PID()) + "/status")
		if readErr == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "SigIgn:") {
					bits, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "SigIgn:")), 16, 64)
					if bits&(1<<14) != 0 {
						goto ready
					}
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not install SIGTERM ignore")
		}
		time.Sleep(10 * time.Millisecond)
	}
ready:
	start := time.Now()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < processKillGracePeriod || elapsed > 2*processKillGracePeriod {
		t.Fatalf("unexpected escalation bound: %v", elapsed)
	}
	if client.PID() != 0 || client.cmd.ProcessState == nil {
		t.Fatal("escalated child was not reaped")
	}
}

func TestProcessClient_SignalsOrdinaryDescendants(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	none := []string{}
	client := newProcessClient("descendant", []string{bin, "-test.run=^TestProcessSignalHelper$"}, "", map[string]string{"PROCESS_SIGNAL_FIXTURE": "descendant", "PROCESS_CHILD_PID_FILE": pidFile}, &execution.ExecutionContract{Mode: "local", Lookup: "absolute", Inherit: &none})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var childPID int
	for childPID == 0 {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			childPID, _ = strconv.Atoi(string(data))
		}
		select {
		case <-ctx.Done():
			t.Fatal("descendant fixture did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(childPID, 0); err != syscall.ESRCH {
		t.Fatal("ordinary descendant survived group shutdown")
	}
	if client.PID() != 0 {
		t.Fatal("owned direct child was not reaped")
	}
}
