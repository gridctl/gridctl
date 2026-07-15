//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// buildGridctlBinary compiles the CLI once per test into a temp dir. The
// build runs before HOME is redirected so the module cache stays intact.
func buildGridctlBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "gridctl")
	build := exec.Command("go", "build", "-o", binPath, "../../cmd/gridctl")
	var stderr bytes.Buffer
	build.Stderr = &stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building gridctl: %v\n%s", err, stderr.String())
	}
	return binPath
}

// TestDoctorAgainstRealRuntime runs `gridctl doctor --json` against the
// real container runtime (Article IV: no mocks here) and asserts the
// runtime and socket checks pass with a healthy daemon.
func TestDoctorAgainstRealRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildGridctlBinary(t)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(binPath, "doctor", "--json")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	var report struct {
		OK     bool `json:"ok"`
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
		t.Fatalf("doctor --json produced invalid JSON: %v\nstdout: %s\nstderr: %s", jsonErr, stdout.String(), stderr.String())
	}
	if len(report.Checks) == 0 {
		t.Fatal("doctor reported no checks")
	}

	statuses := map[string]string{}
	for _, c := range report.Checks {
		statuses[c.ID] = c.Status
	}
	// CI has a live Docker daemon, so detection and socket must pass; a
	// doctor exit error must then come from somewhere legitimate.
	if statuses["runtime.detect"] != "ok" {
		t.Errorf("runtime.detect = %q, want ok (checks: %v)", statuses["runtime.detect"], statuses)
	}
	if statuses["runtime.socket"] != "ok" {
		t.Errorf("runtime.socket = %q, want ok (checks: %v)", statuses["runtime.socket"], statuses)
	}
	if report.OK && err != nil {
		t.Errorf("doctor exited non-zero (%v) despite ok=true", err)
	}
	if strings.Contains(stdout.String(), "\033") {
		t.Error("doctor --json stdout contains ANSI escapes")
	}
}

// TestLogsAfterServe daemonizes a stackless gateway, then asserts
// `gridctl logs` can read the daemon log it produced and `gridctl stop`
// tears it down.
func TestLogsAfterServe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildGridctlBinary(t)
	t.Setenv("HOME", t.TempDir())

	port := freeTCPPort(t)

	var serveOut bytes.Buffer
	serve := exec.Command(binPath, "serve", "--port", strconv.Itoa(port))
	serve.Stdout = &serveOut
	serve.Stderr = &serveOut
	if err := serve.Run(); err != nil {
		t.Fatalf("gridctl serve: %v\n%s", err, serveOut.String())
	}

	t.Cleanup(func() {
		stop := exec.Command(binPath, "stop")
		_ = stop.Run()
	})

	if !waitForHealthEndpoint(port, 15*time.Second) {
		t.Fatalf("gateway never became healthy on :%d\n%s", port, serveOut.String())
	}

	var logsOut, logsErr bytes.Buffer
	logs := exec.Command(binPath, "logs", "gridctl", "-n", "50")
	logs.Stdout = &logsOut
	logs.Stderr = &logsErr
	if err := logs.Run(); err != nil {
		t.Fatalf("gridctl logs: %v\nstderr: %s", err, logsErr.String())
	}
	if logsOut.Len() == 0 {
		t.Error("gridctl logs printed nothing for a freshly started daemon")
	}

	var stopOut bytes.Buffer
	stop := exec.Command(binPath, "stop")
	stop.Stdout = &stopOut
	stop.Stderr = &stopOut
	if err := stop.Run(); err != nil {
		t.Fatalf("gridctl stop: %v\n%s", err, stopOut.String())
	}
}

// TestInitScaffoldPassesValidate runs the real binary end to end:
// `gridctl init` writes a stack.yaml that `gridctl validate` accepts, a
// re-run without --force refuses, and `gridctl link` on a closed stdin
// fails fast instead of hanging (Article IV: no mocks here).
func TestInitScaffoldPassesValidate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildGridctlBinary(t)
	dir := t.TempDir()

	var out bytes.Buffer
	initCmd := exec.Command(binPath, "init", dir, "--name", "demo")
	initCmd.Stdout = &out
	initCmd.Stderr = &out
	if err := initCmd.Run(); err != nil {
		t.Fatalf("gridctl init: %v\n%s", err, out.String())
	}

	stackPath := filepath.Join(dir, "stack.yaml")
	var valOut bytes.Buffer
	validate := exec.Command(binPath, "validate", stackPath)
	validate.Stdout = &valOut
	validate.Stderr = &valOut
	if err := validate.Run(); err != nil {
		t.Fatalf("scaffold failed gridctl validate: %v\n%s", err, valOut.String())
	}

	rerun := exec.Command(binPath, "init", dir)
	var rerunOut bytes.Buffer
	rerun.Stdout = &rerunOut
	rerun.Stderr = &rerunOut
	if err := rerun.Run(); err == nil {
		t.Fatalf("init over an existing stack.yaml should fail without --force\n%s", rerunOut.String())
	}
	if !strings.Contains(rerunOut.String(), "--force") {
		t.Errorf("overwrite refusal should name --force: %s", rerunOut.String())
	}
}

// TestLinkFailsFastOnNonTTY guards the F14 non-TTY contract with the real
// binary. With a detected client, bare `gridctl link` on a non-terminal
// stdin must exit 1 immediately (naming the non-interactive options)
// rather than block on a prompt; bare `gridctl unlink` with nothing
// linked must stay a fast, successful no-op as before.
func TestLinkFailsFastOnNonTTY(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binPath := buildGridctlBinary(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Claude Code detects via ~/.claude.json on every platform, so this
	// makes exactly one client detectable inside the redirected HOME.
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("seeding fake client config: %v", err)
	}

	run := func(sub string) (string, error) {
		var out bytes.Buffer
		cmd := exec.Command(binPath, sub)
		cmd.Stdin = strings.NewReader("")
		cmd.Stdout = &out
		cmd.Stderr = &out

		done := make(chan error, 1)
		if err := cmd.Start(); err != nil {
			t.Fatalf("starting gridctl %s: %v", sub, err)
		}
		go func() { done <- cmd.Wait() }()

		select {
		case err := <-done:
			return out.String(), err
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("gridctl %s hung on non-TTY stdin", sub)
			return "", nil
		}
	}

	out, err := run("link")
	if err == nil {
		t.Errorf("gridctl link with a detected client on non-TTY stdin should exit non-zero\n%s", out)
	}
	if !strings.Contains(out, "--all") {
		t.Errorf("gridctl link non-TTY error should name --all:\n%s", out)
	}

	// Nothing is linked in the fresh HOME, so unlink keeps its no-op
	// contract for scripts: exit 0, no prompt, no guard error.
	out, err = run("unlink")
	if err != nil {
		t.Errorf("gridctl unlink with nothing linked should stay a successful no-op, got %v\n%s", err, out)
	}
}

// freeTCPPort finds an available TCP port for the test gateway.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitForHealthEndpoint polls the gateway health endpoint until it
// responds or the timeout elapses.
func waitForHealthEndpoint(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
