//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/mcp"
	runtimedetect "github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker"
)

func TestMCPRuntimePython(t *testing.T) {
	if testing.Short() {
		if requiredRuntime() {
			t.Fatal("short mode is not allowed for required MCP runtime acceptance")
		}
		t.Skip("skipping integration test in short mode")
	}
	info, err := runtimedetect.DetectRuntime(runtimedetect.DetectOptions{})
	if err != nil {
		if requiredRuntime() {
			t.Fatalf("container runtime required: %v", err)
		}
		t.Skipf("container runtime not available: %v", err)
	}
	orchestrator, err := runtimedetect.NewWithInfo(info)
	if err != nil {
		if requiredRuntime() {
			t.Fatalf("container runtime required: %v", err)
		}
		t.Skipf("container runtime not available: %v", err)
	}
	defer orchestrator.Close() //nolint:errcheck

	if requiredRuntime() && !nativeArch(runtime.GOARCH) {
		t.Fatalf("hosted acceptance requires native %s", runtime.GOARCH)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	root := repoRoot(t)
	cli := string(info.Type)
	baseTag := "gridctl-mcp-runtime-python:local"
	buildImage(t, ctx, cli, root, baseTag, filepath.Join(root, "images/mcp-runtime-python"), "Dockerfile")
	assertRootOwned(t, ctx, cli, baseTag, "/app")
	assertRootOwned(t, ctx, cli, baseTag, "/usr/local")
	assertNoUV(t, ctx, cli, baseTag)
	assertNumericUser(t, ctx, cli, baseTag)

	uid, gid := uint32(10001), uint32(10001)
	executionCfg := &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid, Network: "none"}
	for _, derivative := range []struct{ name, dockerfile string }{
		{name: "locked", dockerfile: "Dockerfile.locked"},
		{name: "hashed", dockerfile: "Dockerfile.hashed"},
	} {
		t.Run(derivative.name, func(t *testing.T) {
			stackName := fmt.Sprintf("inttest-mcp-runtime-%s-%d", derivative.name, time.Now().UnixNano())
			server := config.MCPServer{
				Name:      "echo",
				Transport: "stdio",
				Command:   []string{"mcp-echo-fixture"},
				Source: &config.Source{
					Type:       "local",
					Path:       filepath.Join(root, "images/mcp-runtime-python/fixtures/echo-server"),
					Dockerfile: derivative.dockerfile,
				},
				BuildArgs: map[string]string{"RUNTIME_BASE": baseTag},
				Execution: executionCfg,
			}
			stack := pythonStack(stackName, server)
			cleanupStack(t, ctx, orchestrator, stackName)
			result, err := orchestrator.Up(ctx, stack, runtimedetect.UpOptions{})
			if err != nil {
				t.Fatalf("Up(%s): %v", derivative.name, err)
			}
			workload := onlyServer(t, result)
			if len(workload.Replicas) == 0 {
				t.Fatal("no replicas")
			}
			replica := string(workload.Replicas[0].WorkloadID)
			status, err := orchestrator.Runtime().Status(ctx, workload.Replicas[0].WorkloadID)
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if status.Execution == nil || !status.Execution.Eligible {
				t.Fatalf("required execution evidence missing: %+v", status.Execution)
			}
			assertControl(t, status.Execution, "uid_gid", "10001:10001")
			assertControl(t, status.Execution, "uid", "nonzero numeric container identity")
			assertControl(t, status.Execution, "gid", "nonzero numeric container identity")
			t.Logf("runtime=%s daemon_rootless=%s user_namespace=%s goarch=%s uname=%s native=%v",
				status.Execution.Runtime, status.Execution.DaemonRootless, status.Execution.UserNamespace,
				runtime.GOARCH, unameM(t), nativeArch(runtime.GOARCH))
			assertTini(t, ctx, cli, replica)
			assertRootOwned(t, ctx, cli, status.Image, "/app/.venv")
			assertRootOwned(t, ctx, cli, status.Image, "/app/.venv/bin/mcp-echo-fixture")
			assertProtocolStdout(t, ctx, cli, status.Image)
			client := connectStdio(t, ctx, orchestrator, workload.Replicas[0].WorkloadID)
			assertEchoCall(t, ctx, client, derivative.name+"-hello")
			assertProbeCall(t, ctx, client)
			assertGracefulStop(t, ctx, cli, replica)
		})
	}

	t.Run("command override keeps tini", func(t *testing.T) {
		stackName := fmt.Sprintf("inttest-mcp-runtime-override-%d", time.Now().UnixNano())
		image := buildDerivative(t, ctx, cli, root, baseTag, "Dockerfile.locked", "gridctl-mcp-runtime-override:local")
		server := config.MCPServer{
			Name:      "echo",
			Image:     image,
			Transport: "stdio",
			Command:   []string{"/app/.venv/bin/python", "-c", "from mcp_echo_fixture.server import main; main()"},
			Execution: executionCfg,
		}
		stack := pythonStack(stackName, server)
		cleanupStack(t, ctx, orchestrator, stackName)
		result, err := orchestrator.Up(ctx, stack, runtimedetect.UpOptions{})
		if err != nil {
			t.Fatalf("Up(override): %v", err)
		}
		workload := onlyServer(t, result)
		replica := string(workload.Replicas[0].WorkloadID)
		assertTini(t, ctx, cli, replica)
		client := connectStdio(t, ctx, orchestrator, workload.Replicas[0].WorkloadID)
		assertEchoCall(t, ctx, client, "override")
		assertProbeCall(t, ctx, client)
		assertGracefulStop(t, ctx, cli, replica)
	})
}

func requiredRuntime() bool {
	return os.Getenv("GRIDCTL_REQUIRE_MCP_RUNTIME") == "1"
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

func dockerfilePath(contextDir, dockerfile string) string {
	if filepath.IsAbs(dockerfile) {
		return dockerfile
	}
	return filepath.Join(contextDir, dockerfile)
}

func buildImage(t *testing.T, ctx context.Context, cli, root, tag, contextDir, dockerfile string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, cli, "build", "-t", tag, "-f", dockerfilePath(contextDir, dockerfile), contextDir)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", tag, err, out)
	}
}

func buildDerivative(t *testing.T, ctx context.Context, cli, root, base, dockerfile, tag string) string {
	t.Helper()
	contextDir := filepath.Join(root, "images/mcp-runtime-python/fixtures/echo-server")
	cmd := exec.CommandContext(ctx, cli, "build", "-t", tag, "-f", dockerfilePath(contextDir, dockerfile), "--build-arg", "RUNTIME_BASE="+base, contextDir)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", tag, err, out)
	}
	return tag
}

func runImage(t *testing.T, ctx context.Context, cli, image string, dockerArgs []string, cmdArgs ...string) string {
	t.Helper()
	args := append([]string{"run", "--rm"}, dockerArgs...)
	args = append(args, image)
	args = append(args, cmdArgs...)
	cmd := exec.CommandContext(ctx, cli, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s run: %v\n%s", image, err, out)
	}
	return strings.TrimSpace(string(out))
}

func assertRootOwned(t *testing.T, ctx context.Context, cli, image, path string) {
	t.Helper()
	got := runImage(t, ctx, cli, image, []string{"--user", "0", "--entrypoint", "stat"}, "-c", "%u:%g", path)
	if got != "0:0" {
		t.Fatalf("%s %s owner = %q, want 0:0", image, path, got)
	}
}

func assertNoUV(t *testing.T, ctx context.Context, cli, image string) {
	t.Helper()
	runImage(t, ctx, cli, image, []string{"--entrypoint", "python"}, "-c", "import shutil,sys; sys.exit(0 if shutil.which('uv') is None else 1)")
}

func assertNumericUser(t *testing.T, ctx context.Context, cli, image string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, cli, "image", "inspect", "--format", "{{.Config.User}}", image)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect user: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "10001:10001" {
		t.Fatalf("image USER = %q, want 10001:10001", out)
	}
}

func assertTini(t *testing.T, ctx context.Context, cli, id string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, cli, "inspect", "--format", "{{.Path}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect path: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "/usr/bin/tini" {
		t.Fatalf("container Path = %q, want /usr/bin/tini", out)
	}
}

func assertProtocolStdout(t *testing.T, ctx context.Context, cli, image string) {
	t.Helper()
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"gridctl-test","version":"1"}}}`
	cmd := exec.CommandContext(ctx, cli, "run", "--rm", "-i", image)
	cmd.Stdin = strings.NewReader(request + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("protocol stdout run: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "mcp-echo-fixture starting") {
		t.Fatalf("expected fixture banner on stderr, got %q", stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatalf("non-JSON protocol stdout %q: %v", line, err)
		}
		if message["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line is not JSON-RPC: %s", line)
		}
	}
	if stdout.Len() == 0 {
		t.Fatal("expected JSON-RPC on stdout")
	}
}

func assertProbeCall(t *testing.T, ctx context.Context, client *mcp.StdioClient) {
	t.Helper()
	if !hasTool(client.Tools(), "probe") {
		t.Fatalf("tools = %v, want probe", toolNames(client.Tools()))
	}
	result, err := client.CallTool(ctx, "probe", map[string]any{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("probe result = %+v", result)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("probe json: %v", err)
	}
	if payload["sqlite"] != true || payload["scratch"] != true || payload["app_readonly"] != true {
		t.Fatalf("probe = %+v, want sqlite/scratch/app_readonly", payload)
	}
	if payload["uid"] != float64(10001) || payload["gid"] != float64(10001) {
		t.Fatalf("probe identity = %+v, want 10001", payload)
	}
}

func assertGracefulStop(t *testing.T, ctx context.Context, cli, id string) {
	t.Helper()
	start := time.Now()
	cmd := exec.CommandContext(ctx, cli, "stop", "-t", "8", id)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("stop %s: %v\n%s", id, err, out)
	}
	if elapsed >= 8*time.Second {
		t.Fatalf("stop used the SIGKILL timeout (%s); tini did not deliver a bounded shutdown", elapsed)
	}
	inspect := exec.CommandContext(ctx, cli, "inspect", "--format", "{{json .State}}", id)
	stateOut, err := inspect.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect state: %v\n%s", err, stateOut)
	}
	var state struct {
		OOMKilled bool
		Error     string
		Status    string
	}
	if err := json.Unmarshal(stateOut, &state); err != nil {
		t.Fatalf("decode state: %v\n%s", err, stateOut)
	}
	if state.OOMKilled {
		t.Fatal("container was OOM-killed")
	}
	if state.Error != "" {
		t.Fatalf("container stop error: %s", state.Error)
	}
	if state.Status != "exited" && state.Status != "dead" {
		t.Fatalf("container status after stop = %q, want exited", state.Status)
	}
}

func assertControl(t *testing.T, report *execution.Report, field, requested string) {
	t.Helper()
	for _, control := range report.Controls {
		if control.Field == field && control.Requested == requested && control.Outcome == "observed" {
			return
		}
	}
	t.Fatalf("missing observed control %s=%s in %+v", field, requested, report.Controls)
}

func unameM(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func nativeArch(goarch string) bool {
	machine := ""
	out, err := exec.Command("uname", "-m").Output()
	if err == nil {
		machine = strings.TrimSpace(string(out))
	}
	switch goarch {
	case "amd64":
		return machine == "x86_64"
	case "arm64":
		return machine == "aarch64" || machine == "arm64"
	default:
		return false
	}
}
