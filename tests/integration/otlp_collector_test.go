//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOTLPCollector_DebugExporter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI unavailable")
	}
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skip("Docker daemon unavailable")
	}
	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mock := buildRaceBinary(t, ctx, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), filepath.Join(dir, "mock"))
	cfgDir := filepath.Join(dir, "collector")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
exporters:
  debug:
    verbosity: detailed
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      exporters: [debug]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	hostPort := freePort(t)
	name := fmt.Sprintf("gridctl-otlp-%d", hostPort)
	image := "otel/opentelemetry-collector:0.163.1@sha256:f0e23036dce3db3f9cb80d907c75a0d034cd32e93511a1943482d46129b9ae9c"
	run := exec.CommandContext(ctx, "docker", "run", "--rm", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:4318", hostPort),
		"-v", cfgDir+":/etc/otel:ro",
		image, "--config", "/etc/otel/config.yaml")
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
		_ = run.Wait()
	}()
	waitForTCP(t, ctx, hostPort)

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", hostPort)
	stackPath := filepath.Join(dir, "stack.yaml")
	stack := fmt.Sprintf(`version: "1"
name: otel-collector
network:
  name: otel-collector-net
mcp-servers:
  - name: echo
    command: [%q]
gateway:
  tracing:
    export: otlp
    endpoint: %s
  metrics:
    otlp:
      endpoint: %s
      interval: 1s
      timeout: 5s
policy:
  default: allow
  rules:
    - match: echo__add
      action: block
`, mock, endpoint, endpoint)
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
	}()
	waitForReady(t, ctx, port)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	session := otlpInitialize(t, ctx, base)
	otlpCall(t, ctx, base, session, "echo__echo", map[string]any{"message": otlpSecret}, true)
	otlpCall(t, ctx, base, session, "echo__add", map[string]any{"a": 1, "b": 2}, false)
	_ = daemon.Process.Signal(syscall.SIGTERM)
	_ = daemon.Wait()
	daemon.Process = nil

	deadline := time.Now().Add(20 * time.Second)
	var logs string
	for time.Now().Before(deadline) {
		out, _ := exec.Command("docker", "logs", name).CombinedOutput()
		logs = string(out)
		if strings.Contains(logs, "tools/call echo") && strings.Contains(logs, "otel-collector") &&
			strings.Contains(logs, "gridctl.gateway.tool.calls") && strings.Contains(logs, "mcp.server.operation.duration") &&
			strings.Contains(logs, "denied") {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	for _, want := range []string{"tools/call echo", "otel-collector", "gridctl.server.name", "gen_ai.tool.name", "network.transport", "pipe", "mcp.server.operation.duration", "gridctl.gateway.tool.calls", "denied"} {
		if !strings.Contains(logs, want) {
			t.Errorf("collector logs missing %q", want)
		}
	}
	if strings.Contains(logs, otlpSecret) {
		t.Error("collector logs contain the planted secret")
	}
	if t.Failed() {
		t.Logf("collector logs:\n%s", logs)
		data, _ := os.ReadFile(logPath)
		t.Logf("daemon:\n%s", data)
	}
}

func waitForTCP(t *testing.T, ctx context.Context, port int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("collector port did not open")
}
