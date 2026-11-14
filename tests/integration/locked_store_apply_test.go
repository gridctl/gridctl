//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLockedStore_ApplyReportsLockedStore drives a race-built CLI against a
// locked variable store. Without a passphrase in the environment, apply must
// name the locked store rather than a missing variable; with the passphrase,
// the stored value must reach a loopback MCP server.
func TestLockedStore_ApplyReportsLockedStore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mockBin := buildRaceBinary(t, ctx, filepath.Join(root, "examples/_mock-servers/mock-mcp-server"), filepath.Join(dir, "mock"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}

	const (
		token = "locked-store-token-7c1e"
		pass  = "locked-store-passphrase-long-enough"
	)

	mockPort := freePort(t)
	mock := exec.CommandContext(ctx, mockBin, "-port", strconv.Itoa(mockPort), "-require-header", "X-Demo-Token="+token)
	if err := mock.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mock.Process.Kill(); _ = mock.Wait() })
	waitForPort(t, ctx, mockPort)

	stackPath := filepath.Join(dir, "stack.yaml")
	body := fmt.Sprintf(`version: "1"
name: locked-demo
network:
  name: locked-demo-net
mcp-servers:
  - name: demo
    url: http://127.0.0.1:%d/mcp
    headers:
      X-Demo-Token: "${var:DEMO_TOKEN}"
`, mockPort)
	if err := os.WriteFile(stackPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	env := lockedStoreChildEnv(home)
	withPass := append(append([]string(nil), env...), "GRIDCTL_VAULT_PASSPHRASE="+pass)

	if out, err := runGridctlEnv(ctx, t, bin, env, "var", "set", "DEMO_TOKEN", "--value", token); err != nil {
		t.Fatalf("var set: %v\n%s", err, out)
	}
	if out, err := runGridctlEnv(ctx, t, bin, withPass, "var", "lock"); err != nil {
		t.Fatalf("var lock: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "vault", "secrets.enc")); err != nil {
		t.Fatalf("store not encrypted: %v", err)
	}

	out, err := runGridctlEnv(ctx, t, bin, env, "apply", stackPath, "-f", "--port", strconv.Itoa(freePort(t)))
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("locked apply: err=%v, want exit 1\n%s", err, out)
	}
	for _, want := range []string{"variable store is locked", "DEMO_TOKEN", "GRIDCTL_VAULT_PASSPHRASE", "GRIDCTL_VAULT_PASSPHRASE_FILE"} {
		if !strings.Contains(out, want) {
			t.Errorf("locked apply output missing %q:\n%s", want, out)
		}
	}
	for _, reject := range []string{"missing variable(s)", "gridctl var set", token, pass} {
		if strings.Contains(out, reject) {
			t.Errorf("locked apply output contains %q:\n%s", reject, out)
		}
	}

	port := freePort(t)
	apply := exec.CommandContext(ctx, bin, "apply", stackPath, "-f", "--port", strconv.Itoa(port))
	apply.Env = withPass
	var applyOut strings.Builder
	apply.Stdout = &applyOut
	apply.Stderr = &applyOut
	if err := apply.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = apply.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = apply.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = apply.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	names := toolNamesAt(t, base+"/mcp")
	found := false
	for _, name := range names {
		if strings.HasPrefix(name, "demo__") {
			found = true
			break
		}
	}
	if !found {
		stop()
		t.Fatalf("unlocked apply did not register the demo server: tools=%v\n%s", names, applyOut.String())
	}
	stop()
	if strings.Contains(applyOut.String(), token) || strings.Contains(applyOut.String(), pass) {
		t.Fatalf("unlocked apply printed a value:\n%s", applyOut.String())
	}
}

// lockedStoreChildEnv starts from the test environment, drops every
// GRIDCTL_VAULT_PASSPHRASE* variable and any ambient DEMO_TOKEN, and points
// the child at an isolated home.
func lockedStoreChildEnv(home string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GRIDCTL_VAULT_PASSPHRASE") ||
			strings.HasPrefix(kv, "DEMO_TOKEN=") ||
			strings.HasPrefix(kv, "GRIDCTL_HOME=") ||
			strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GRIDCTL_HOME="+home, "HOME="+home)
}
