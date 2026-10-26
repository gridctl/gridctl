//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadyGate_DeclaredServerCallable polls /ready with no sleep and calls
// the declared tool on the first 200. Before the registration gate, that
// call returned Error: unknown tool.
func TestReadyGate_DeclaredServerCallable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mock := buildRaceBinary(t, ctx, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), filepath.Join(dir, "mock"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	stack := fmt.Sprintf(`version: "1"
name: ready-gate
network:
  name: ready-gate-net
mcp-servers:
  - name: echo
    command: [%q]
`, mock)
	if err := os.WriteFile(stackPath, []byte(stack), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	daemon := exec.CommandContext(ctx, bin, "apply", stackPath, "--foreground", "--port", fmt.Sprint(port))
	daemon.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
	daemon.Stdout = logFile
	daemon.Stderr = logFile
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if daemon.Process != nil {
			_ = daemon.Process.Signal(syscall.SIGTERM)
			_ = daemon.Wait()
		}
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("daemon:\n%s", data)
		}
	}()

	sawEchoNotReady := waitReadyNoSleep(t, ctx, port)
	t.Logf("observed 503 naming echo before 200: %v", sawEchoNotReady)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	session := otlpInitialize(t, ctx, base)
	otlpCall(t, ctx, base, session, "echo__echo", map[string]any{"message": "hi"}, true)

	_ = daemon.Process.Signal(syscall.SIGTERM)
	_ = daemon.Wait()
	daemon.Process = nil
}

// waitReadyNoSleep polls /ready with no delay between attempts. A 200 ms
// poll hides the registration race roughly half the time.
func waitReadyNoSleep(t *testing.T, ctx context.Context, port int) bool {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/ready", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(45 * time.Second)
	sawEcho := false
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusServiceUnavailable && bytes.Contains(body, []byte("echo")) {
			sawEcho = true
		}
		if resp.StatusCode == http.StatusOK {
			return sawEcho
		}
	}
	t.Fatal("daemon did not become ready")
	return false
}
