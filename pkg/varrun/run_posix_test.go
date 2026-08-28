//go:build !windows

package varrun

import (
	"context"
	"errors"
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

func TestRun_ForwardsSignalsUntilChildExits(t *testing.T) {
	if os.Getenv("VAR_RUN_SIGNAL_HELPER") == "1" {
		signals := make(chan os.Signal, 2)
		signal.Notify(signals, syscall.SIGTERM)
		defer signal.Stop(signals)
		<-signals
		<-signals
		os.Exit(7)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	result, err := Run(context.Background(), Options{
		Command:     []string{os.Args[0], "-test.run=TestRun_ForwardsSignalsUntilChildExits"},
		Environment: append(os.Environ(), "VAR_RUN_SIGNAL_HELPER=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit = %d, want 7", result.ExitCode)
	}
}

func TestRun_DecodesForwardedSignalExit(t *testing.T) {
	if os.Getenv("VAR_RUN_TERMINATE_HELPER") == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	result, err := Run(context.Background(), Options{
		Command:     []string{os.Args[0], "-test.run=TestRun_DecodesForwardedSignalExit"},
		Environment: append(os.Environ(), "VAR_RUN_TERMINATE_HELPER=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 143 {
		t.Fatalf("exit = %d, want 143", result.ExitCode)
	}
}

func TestRun_ForwardsSignalToProcessGroup(t *testing.T) {
	if pidFile := os.Getenv("VAR_RUN_GROUP_HELPER"); pidFile != "" {
		child := exec.Command("sleep", "30")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			os.Exit(2)
		}
		_ = child.Wait()
		os.Exit(0)
	}
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	go func() {
		for range 100 {
			if _, err := os.Stat(pidFile); err == nil {
				_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	result, err := Run(context.Background(), Options{
		Command:     []string{os.Args[0], "-test.run=TestRun_ForwardsSignalToProcessGroup"},
		Environment: append(os.Environ(), "VAR_RUN_GROUP_HELPER="+pidFile),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 143 {
		t.Fatalf("exit = %d, want 143", result.ExitCode)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild process %d survived group signal", pid)
}
