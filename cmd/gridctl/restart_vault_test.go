package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

const restartPass = "restart-passphrase-long" // #nosec G101 -- test fixture

// varStatusServer serves /api/var/status with the given body and returns
// its port.
func varStatusServer(t *testing.T, body string) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/var/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// fakeTarget writes a target binary that logs each argv (and whether its
// stdin carried want) and answers "apply --help" with help, which includes
// --passphrase-stdin when capable.
func fakeTarget(t *testing.T, capable bool, want string) (exe, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "args")
	help := "Flags:\n  --port int\n"
	if capable {
		help += "  --passphrase-stdin   Read the store passphrase from the first line of stdin\n"
	}
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = \"apply\" ] && [ \"$2\" = \"--help\" ]; then printf '%s' " + shellQuote(help) + "; exit 0; fi\n" +
		"for a in \"$@\"; do if [ \"$a\" = \"--passphrase-stdin\" ]; then IFS= read -r line; if [ \"$line\" = " + shellQuote(want) + " ]; then echo stdin-ok >> " + shellQuote(logPath) + "; else echo stdin-bad >> " + shellQuote(logPath) + "; fi; fi; done\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n"
	exe = filepath.Join(dir, "gridctl")
	if err := os.WriteFile(exe, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe, logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func plainDaemonPlan(name string, port int) restartPlan {
	st := state.DaemonState{StackName: name, StackFile: "/tmp/" + name + ".yaml", Port: port}
	plan := buildRestartPlan(st, nil, "", false, "linux")
	plan.daemon = &st
	return plan
}

func stubRestartPrompt(t *testing.T, fn func(context.Context, *vault.Store) (string, error)) *int {
	t.Helper()
	calls := 0
	prev := restartPrompt
	restartPrompt = func(ctx context.Context, store *vault.Store, _ io.Writer) (string, error) {
		calls++
		return fn(ctx, store)
	}
	t.Cleanup(func() { restartPrompt = prev })
	return &calls
}

func restartEnv(t *testing.T) {
	t.Helper()
	lockedVarStore(t, restartPass)
}

func TestExecuteRestart_UnlockedDaemonRefusedWithoutTerminal(t *testing.T) {
	restartEnv(t)
	unlocked := varStatusServer(t, `{"encrypted":true,"locked":false,"unlock_source":"stdin"}`)
	plaintext := varStatusServer(t, `{"encrypted":false,"locked":false}`)
	exe, logPath := fakeTarget(t, true, restartPass)
	calls := stubRestartPrompt(t, func(context.Context, *vault.Store) (string, error) { return "", errors.New("unexpected prompt") })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return false }}
	plans := []restartPlan{plainDaemonPlan("daily", unlocked), plainDaemonPlan("other", plaintext)}
	err := u.executeRestart(t.Context(), exe, plans)
	if err == nil {
		t.Fatal("expected an error for the refused daemon")
	}
	want := "Not restarted: daily (store is unlocked in the daemon and no passphrase is available here; set GRIDCTL_VAULT_PASSPHRASE_FILE or run in a terminal)"
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	logged := readLog(t, logPath)
	if strings.Contains(logged, "daily") {
		t.Fatalf("refused daemon was touched:\n%s", logged)
	}
	if !strings.Contains(logged, "destroy other") || !strings.Contains(logged, "apply /tmp/other.yaml") {
		t.Fatalf("other plan did not run:\n%s", logged)
	}
	if strings.Contains(logged, "--passphrase-stdin") {
		t.Fatalf("plaintext daemon got --passphrase-stdin:\n%s", logged)
	}
	if *calls != 0 {
		t.Fatal("prompted without a terminal")
	}
}

func TestExecuteRestart_TargetWithoutCapabilityRefused(t *testing.T) {
	restartEnv(t)
	port := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	exe, logPath := fakeTarget(t, false, restartPass)
	calls := stubRestartPrompt(t, func(context.Context, *vault.Store) (string, error) { return restartPass, nil })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return true }}
	err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("daily", port)})
	if err == nil {
		t.Fatal("expected an error")
	}
	want := "Not restarted: daily (target gridctl does not accept --passphrase-stdin; set GRIDCTL_VAULT_PASSPHRASE_FILE to restart it)"
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if logged := readLog(t, logPath); strings.Contains(logged, "destroy") {
		t.Fatalf("destroy ran:\n%s", logged)
	}
	if *calls != 0 {
		t.Fatal("prompted for a target that cannot take the passphrase")
	}
}

func TestExecuteRestart_PromptsOnceAndHandsOffOverStdin(t *testing.T) {
	restartEnv(t)
	a := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	b := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	exe, logPath := fakeTarget(t, true, restartPass)
	calls := stubRestartPrompt(t, func(_ context.Context, store *vault.Store) (string, error) {
		if logged := readLog(t, logPath); strings.Contains(logged, "destroy") {
			t.Errorf("prompt ran after a destroy:\n%s", logged)
		}
		// Mirror PromptPassphrase: the value is verified locally.
		if err := store.Unlock(restartPass); err != nil {
			return "", err
		}
		return restartPass, nil
	})

	stackless := state.DaemonState{StackName: "gridctl", Port: b}
	slPlan := buildRestartPlan(stackless, nil, "", false, "linux")
	slPlan.daemon = &stackless

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return true }}
	if err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("daily", a), slPlan}); err != nil {
		t.Fatalf("executeRestart: %v (stderr %q)", err, stderr.String())
	}
	if *calls != 1 {
		t.Fatalf("prompted %d times, want 1", *calls)
	}
	logged := readLog(t, logPath)
	for _, want := range []string{
		"destroy daily",
		fmt.Sprintf("apply /tmp/daily.yaml --port %d --passphrase-stdin", a),
		"destroy gridctl",
		fmt.Sprintf("serve --port %d --passphrase-stdin", b),
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q:\n%s", want, logged)
		}
	}
	if strings.Count(logged, "stdin-ok") != 2 || strings.Contains(logged, "stdin-bad") {
		t.Fatalf("stdin hand-off:\n%s", logged)
	}
	if strings.Contains(logged, restartPass) || strings.Contains(stdout.String(), restartPass) || strings.Contains(stderr.String(), restartPass) {
		t.Fatal("passphrase leaked into argv or output")
	}
}

func TestExecuteRestart_PromptCancelledRunsNothing(t *testing.T) {
	restartEnv(t)
	port := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	exe, logPath := fakeTarget(t, true, restartPass)
	stubRestartPrompt(t, func(context.Context, *vault.Store) (string, error) { return "", controller.ErrPassphraseCancelled })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return true }}
	err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("daily", port)})
	if !errors.Is(err, controller.ErrPassphraseCancelled) {
		t.Fatalf("err = %v", err)
	}
	if logged := readLog(t, logPath); strings.Contains(logged, "destroy") {
		t.Fatalf("destroy ran after a cancelled prompt:\n%s", logged)
	}
	if !strings.Contains(stderr.String(), "Not restarted: daily (not attempted)") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestExecuteRestart_UnreadableLocalStoreRefusesOnlyNeedingDaemons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv(vault.PassphraseFileEnv, "")
	dir := filepath.Join(home, ".gridctl", "vault")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlocked := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	plaintext := varStatusServer(t, `{"encrypted":false,"locked":false}`)
	exe, logPath := fakeTarget(t, true, restartPass)
	calls := stubRestartPrompt(t, func(context.Context, *vault.Store) (string, error) { return "", errors.New("unexpected prompt") })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return true }}
	err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("daily", unlocked), plainDaemonPlan("other", plaintext)})
	if err == nil {
		t.Fatal("expected an error for the refused daemon")
	}
	want := "Not restarted: daily (local variable store could not be read; set GRIDCTL_VAULT_PASSPHRASE_FILE to restart it)"
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	logged := readLog(t, logPath)
	if strings.Contains(logged, "daily") {
		t.Fatalf("refused daemon was touched:\n%s", logged)
	}
	if !strings.Contains(logged, "destroy other") || !strings.Contains(logged, "apply /tmp/other.yaml") {
		t.Fatalf("other plan did not run:\n%s", logged)
	}
	if *calls != 0 {
		t.Fatal("prompted with an unreadable store")
	}
}

func TestExecuteRestart_EnvironmentPassphraseUnaffected(t *testing.T) {
	restartEnv(t)
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", restartPass)
	port := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	exe, logPath := fakeTarget(t, false, restartPass)
	calls := stubRestartPrompt(t, func(context.Context, *vault.Store) (string, error) { return "", errors.New("unexpected prompt") })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return false }}
	if err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("daily", port)}); err != nil {
		t.Fatal(err)
	}
	logged := readLog(t, logPath)
	if !strings.Contains(logged, "destroy daily") || strings.Contains(logged, "--passphrase-stdin") {
		t.Fatalf("log:\n%s", logged)
	}
	if *calls != 0 {
		t.Fatal("prompted with the environment set")
	}
}

func TestExecuteRestart_LockedOrUnreadableDaemonRunsAsBefore(t *testing.T) {
	restartEnv(t)
	locked := varStatusServer(t, `{"encrypted":true,"locked":true}`)
	// A closed port: the status read fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unreadable := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	exe, logPath := fakeTarget(t, true, restartPass)
	calls := stubRestartPrompt(t, func(context.Context, *vault.Store) (string, error) { return "", errors.New("unexpected prompt") })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return false }}
	if err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("locked", locked), plainDaemonPlan("gone", unreadable)}); err != nil {
		t.Fatalf("executeRestart: %v", err)
	}
	logged := readLog(t, logPath)
	if !strings.Contains(logged, "destroy locked") || !strings.Contains(logged, "destroy gone") || strings.Contains(logged, "--passphrase-stdin") {
		t.Fatalf("log:\n%s", logged)
	}
	if !strings.Contains(stderr.String(), "note: could not read the variable store status of gone") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if *calls != 0 {
		t.Fatal("prompted")
	}
}

func TestExecuteRestart_TargetWithoutCapabilityRefusedWithoutTerminal(t *testing.T) {
	restartEnv(t)
	port := varStatusServer(t, `{"encrypted":true,"locked":false}`)
	exe, logPath := fakeTarget(t, false, restartPass)

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, isTTY: func() bool { return false }}
	if err := u.executeRestart(t.Context(), exe, []restartPlan{plainDaemonPlan("daily", port)}); err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(stderr.String(), "Not restarted: daily (target gridctl does not accept --passphrase-stdin; set GRIDCTL_VAULT_PASSPHRASE_FILE to restart it)") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if logged := readLog(t, logPath); strings.Contains(logged, "destroy") {
		t.Fatalf("destroy ran:\n%s", logged)
	}
}
