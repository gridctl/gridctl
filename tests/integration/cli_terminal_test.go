//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
