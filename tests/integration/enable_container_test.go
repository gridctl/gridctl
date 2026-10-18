//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
)

// TestEnableDisable_ContainerRestart leaves a container running, disables its
// server, and restarts the daemon. The child must not register the server, and
// the leftover container is logged.
func TestEnableDisable_ContainerRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rt, err := runtime.New()
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)

	stackPath := filepath.Join(dir, "stack.yaml")
	body := `version: "1"
name: enable-container
network:
  name: enable-container-net
mcp-servers:
  - name: sleeper
    image: alpine:3.22
    transport: stdio
    command: ["sleep", "3600"]
`
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "enable-container")
	})
	out, err := runGridctl(ctx, t, bin, home, "apply", stackPath, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	st, err := state.Load("enable-container")
	if err != nil {
		t.Fatal(err)
	}
	if st.PID > 0 {
		_ = exec.Command("kill", "-9", strconv.Itoa(st.PID)).Run()
	}
	_ = state.Delete("enable-container")
	disabled := strings.Replace(body, "command: [\"sleep\", \"3600\"]\n", "command: [\"sleep\", \"3600\"]\n    enabled: false\n", 1)
	if err := os.WriteFile(stackPath, []byte(disabled), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err = runGridctl(ctx, t, bin, home, "apply", stackPath, "--port", strconv.Itoa(port)); err != nil {
		t.Fatalf("re-apply: %v\n%s", err, out)
	}
	logPath := filepath.Join(home, ".gridctl", "logs", "enable-container.log")
	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBody), "skipping container for disabled MCP server") {
		t.Fatalf("leftover container was not logged:\n%s", logBody)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	if containsTool(enableToolNames(t, base), "sleeper__") {
		t.Fatal("disabled container server was registered after restart")
	}
}
