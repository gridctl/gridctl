package controller

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDaemonArgs_PassphraseStdinFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		pass string
		want bool
	}{
		{name: "without passphrase", pass: "", want: false},
		{name: "with passphrase", pass: testPass, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dm := NewDaemonManager(Config{StackPath: "/tmp/stack.yaml", Port: 8180, BasePort: 9000, Passphrase: tc.pass})
			for name, args := range map[string][]string{"apply": dm.forkArgs(), "serve": dm.stacklessArgs()} {
				if got := slices.Contains(args, "--passphrase-stdin"); got != tc.want {
					t.Fatalf("%s argv has --passphrase-stdin = %v, want %v: %v", name, got, tc.want, args)
				}
				for _, a := range args {
					if tc.pass != "" && strings.Contains(a, tc.pass) {
						t.Fatalf("%s argv contains the passphrase: %v", name, args)
					}
				}
			}
		})
	}
}

func TestNewDaemonCmd_EnvironmentHasNoPassphrase(t *testing.T) {
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	if err := os.Unsetenv("GRIDCTL_VAULT_PASSPHRASE"); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	dm := NewDaemonManager(Config{StackPath: "/tmp/stack.yaml", Passphrase: testPass})
	cmd := newDaemonCmd("/bin/true", dm.forkArgs(), logFile)
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "GRIDCTL_VAULT_PASSPHRASE=") || strings.Contains(kv, testPass) {
			t.Fatalf("child environment carries the passphrase: %q", kv)
		}
	}
	if cmd.Stdin != nil {
		t.Fatal("stdin wired before startDaemon")
	}
}

// helperCmd re-executes this test binary as a child that checks its first
// stdin line against want.
func helperCmd(t *testing.T, want string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // G702: re-executes this test binary
	cmd.Env = []string{"GRIDCTL_HELPER_STDIN_WANT=" + want, "PATH=" + os.Getenv("PATH")}
	return cmd
}

func TestStartDaemon_ForwardsPassphraseOverStdin(t *testing.T) {
	logFile, err := os.Create(filepath.Join(t.TempDir(), "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := helperCmd(t, testPass)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	handle, err := startDaemon(cmd, logFile, testPass)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-handle.Exited:
		if err != nil {
			t.Fatalf("child did not receive the passphrase line: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not exit")
	}
	if slices.ContainsFunc(cmd.Args, func(a string) bool { return strings.Contains(a, testPass) }) {
		t.Fatal("argv contains the passphrase")
	}
}

func TestStartDaemon_NoPassphraseLeavesStdinEmpty(t *testing.T) {
	logFile, err := os.Create(filepath.Join(t.TempDir(), "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := helperCmd(t, "")
	handle, err := startDaemon(cmd, logFile, "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Stdin != nil {
		t.Fatal("stdin wired without a passphrase")
	}
	select {
	case err := <-handle.Exited:
		// A nil stdin is /dev/null: the helper reads an empty line.
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				t.Fatalf("helper exit = %d, want 0", exitErr.ExitCode())
			}
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not exit")
	}
}
