//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
	"github.com/gridctl/gridctl/pkg/state"
)

const crashDiagnosticsScript = `import json, os, sys, time

marker = "/tmp/crash-again"
again = os.path.exists(marker)
if not again:
    open(marker, "w").close()

def reply(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    request = json.loads(line)
    if "id" not in request:
        continue
    method = request.get("method")
    if method == "initialize":
        version = request.get("params", {}).get("protocolVersion", "2025-06-18")
        reply({"jsonrpc": "2.0", "id": request["id"], "result": {"protocolVersion": version, "capabilities": {"tools": {}}, "serverInfo": {"name": "crash", "version": "1"}}})
    elif method == "tools/list":
        reply({"jsonrpc": "2.0", "id": request["id"], "result": {"tools": []}})
        break
    else:
        reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})

sys.stderr.write("fatal: refusing to continue\n")
sys.stderr.flush()
if again:
    raise SystemExit(3)
time.sleep(2)
raise SystemExit(3)
`

func TestContainerCrashDiagnostics(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := dockerruntime.NewWithInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := rt.EnsureImage(ctx, "python:3.13-alpine"); err != nil {
		t.Fatal(err)
	}
	stack := fmt.Sprintf("crash-diag-%d", time.Now().UnixNano())
	networkName := stack + "-net"
	if err := rt.EnsureNetwork(ctx, networkName, runtime.NetworkOptions{Driver: "bridge", Stack: stack}); err != nil {
		t.Fatal(err)
	}
	cfg := runtime.WorkloadConfig{
		Name:        "crash",
		Stack:       stack,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "python:3.13-alpine",
		Command:     []string{"python", "-u", "-c", crashDiagnosticsScript},
		Transport:   "stdio",
		NetworkName: networkName,
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		exists, id, err := rt.Exists(cleanupCtx, dockerruntime.ContainerName(cfg.Stack, cfg.Name))
		if err == nil && exists {
			_ = rt.Remove(cleanupCtx, id)
		}
		_ = rt.RemoveNetwork(cleanupCtx, networkName)
	})
	started, err := rt.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	logBuffer := logging.NewLogBuffer(100)
	gateway := mcp.NewGateway()
	t.Cleanup(func() { gateway.Close() })
	gateway.SetLogger(slog.New(logging.NewBufferHandler(logBuffer, nil)))
	gateway.SetDockerClient(rt.Client())
	if err := gateway.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "crash",
		Transport:          mcp.TransportStdio,
		ContainerID:        string(started.ID),
		ProtocolGeneration: "handshake",
		PingTimeout:        time.Second,
	}); err != nil {
		t.Fatal(err)
	}

	waitForContainerExit(t, ctx, rt, string(started.ID))
	waitForStderr(t, logBuffer, "crash")
	gateway.StartHealthMonitor(ctx, 200*time.Millisecond)

	var replica mcp.ReplicaStatus
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		replicas := gateway.ReplicaStatuses("crash")
		if len(replicas) == 1 && replicas[0].Exit != nil && replicas[0].Exit.Code == 3 && replicas[0].ContainerRestarts >= 1 {
			replica = replicas[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if replica.Exit == nil || replica.Exit.Code != 3 || replica.ContainerRestarts < 1 {
		t.Fatalf("replica = %#v", gateway.ReplicaStatuses("crash"))
	}
	if replica.Exit.Status == "" {
		t.Fatal("runtime status string is empty")
	}
	t.Logf("runtime status %q", replica.Exit.Status)
	sawExit := false
	stableUntil := time.Now().Add(2 * time.Second)
	for time.Now().Before(stableUntil) {
		again := gateway.ReplicaStatuses("crash")
		if len(again) != 1 {
			t.Fatalf("replica set changed: %#v", again)
		}
		if again[0].Exit != nil {
			if again[0].Exit.Code != 3 {
				t.Fatalf("exit evidence replaced: %#v", again)
			}
			sawExit = true
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !sawExit {
		t.Fatal("exit evidence was not re-recorded across restart cycles")
	}

	apiServer := api.NewServer(gateway, nil)
	apiServer.SetLogBuffer(logBuffer)
	ln := httptest.NewServer(apiServer.Handler())
	t.Cleanup(ln.Close)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gridctl", "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.HomeEnv, home)
	if err := state.Save(&state.DaemonState{StackName: stack, PID: os.Getpid(), Port: mustPort(ln.URL), Home: home, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	bin := crashDiagnosticsBinary(t, ctx)
	statusDeadline := time.Now().Add(20 * time.Second)
	var out []byte
	var found bool
	for time.Now().Before(statusDeadline) && !found {
		cmd := exec.CommandContext(ctx, bin, "status", "--json")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1", "DOCKER_HOST=" + os.Getenv("DOCKER_HOST"), "GRIDCTL_RUNTIME=" + os.Getenv("GRIDCTL_RUNTIME")}
		out, err = cmd.Output()
		if err != nil {
			t.Fatalf("status --json: %v", err)
		}
		var report struct {
			MCPServers []struct {
				Name     string `json:"name"`
				Replicas []struct {
					LastError string `json:"lastError"`
					Exit      *struct {
						Code int `json:"code"`
					} `json:"exit"`
				} `json:"replicas"`
			} `json:"mcp_servers"`
		}
		if err := json.Unmarshal(out, &report); err != nil {
			t.Fatalf("decode status: %v\n%s", err, out)
		}
		for _, server := range report.MCPServers {
			if server.Name != "crash" || len(server.Replicas) == 0 {
				continue
			}
			got := server.Replicas[0]
			if got.LastError != "" && !strings.Contains(got.LastError, "execution.ping") && got.Exit != nil && got.Exit.Code == 3 {
				found = true
			}
		}
		if !found {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if !found {
		t.Fatalf("status json missing crash exit: %s", out)
	}
}

func waitForContainerExit(t *testing.T, ctx context.Context, rt *dockerruntime.DockerRuntime, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		info, err := rt.Client().ContainerInspect(ctx, id)
		if err == nil && info.State != nil && !info.State.Running && info.State.ExitCode == 3 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("container did not exit 3")
}

func waitForStderr(t *testing.T, buffer *logging.LogBuffer, server string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range buffer.GetRecent(100) {
			if entry.Level == "WARN" && entry.Message == "server stderr" && entry.Attrs["server"] == server && entry.Attrs["output"] == "fatal: refusing to continue" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("stderr ring = %#v", buffer.GetRecent(20))
}

func crashDiagnosticsBinary(t *testing.T, ctx context.Context) string {
	t.Helper()
	if bin := os.Getenv("GRIDCTL_A2A_FIXTURE_BINARY"); bin != "" {
		return bin
	}
	bin := filepath.Join(t.TempDir(), "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/gridctl")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gridctl: %v\n%s", err, out)
	}
	return bin
}
