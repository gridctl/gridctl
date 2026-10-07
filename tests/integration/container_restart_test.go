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

const containerRestartScript = `import json, os, sys

marker = "/tmp/restarted"
restarted = os.path.exists(marker)
if not restarted:
    open(marker, "w").close()

def reply(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    request = json.loads(line)
    if "id" not in request:
        continue
    method = request.get("method")
    req_id = request["id"]
    if method == "initialize":
        version = request.get("params", {}).get("protocolVersion", "2025-06-18")
        reply({"jsonrpc": "2.0", "id": req_id, "result": {"protocolVersion": version, "capabilities": {"tools": {}}, "serverInfo": {"name": "revive", "version": "1"}}})
    elif method == "tools/list":
        tools = []
        if restarted:
            tools = [{"name": "revived", "description": "after restart", "inputSchema": {"type": "object", "properties": {}}}]
        reply({"jsonrpc": "2.0", "id": req_id, "result": {"tools": tools}})
        if not restarted:
            break
    elif method == "ping":
        reply({"jsonrpc": "2.0", "id": req_id, "result": {}})
    elif method == "tools/call":
        reply({"jsonrpc": "2.0", "id": req_id, "result": {"content": [{"type": "text", "text": "revived"}]}})
    else:
        reply({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32601, "message": "Method not found"}})

if not restarted:
    raise SystemExit(3)
`

const containerExitScript = `import json, sys

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
        reply({"jsonrpc": "2.0", "id": request["id"], "result": {"protocolVersion": version, "capabilities": {"tools": {}}, "serverInfo": {"name": "exit", "version": "1"}}})
    elif method == "tools/list":
        reply({"jsonrpc": "2.0", "id": request["id"], "result": {"tools": []}})
        break
    else:
        reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})

raise SystemExit(3)
`

func TestContainerRestart_RecoversExitedStdio(t *testing.T) {
	rt, ctx, id := startRestartFixture(t, "revive", containerRestartScript)
	gateway := newRestartGateway(t, rt)
	if err := gateway.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "revive",
		Transport:          mcp.TransportStdio,
		ContainerID:        id,
		ProtocolGeneration: "handshake",
		PingTimeout:        time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	waitForContainerExit(t, ctx, rt, id)
	gateway.StartHealthMonitor(ctx, 200*time.Millisecond)

	deadline := time.Now().Add(30 * time.Second)
	var replica mcp.ReplicaStatus
	for time.Now().Before(deadline) {
		replicas := gateway.ReplicaStatuses("revive")
		if len(replicas) == 1 && replicas[0].Healthy && replicas[0].Exit == nil && replicas[0].RestartAttempts == 0 && replicas[0].ContainerRestarts == 1 && replicas[0].LastError == "" {
			replica = replicas[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !replica.Healthy || replica.ContainerRestarts != 1 || replica.Exit != nil || replica.RestartAttempts != 0 {
		t.Fatalf("replica = %#v", gateway.ReplicaStatuses("revive"))
	}
	result, err := gateway.HandleToolsCall(ctx, mcp.ToolCallParams{Name: mcp.PrefixTool("revive", "revived")})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if result.IsError || len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "revived") {
		t.Fatalf("tool result = %#v", result)
	}
	assertStatusHealthy(t, ctx, gateway, "revive")
}

func TestContainerRestart_PolicyNoStaysExited(t *testing.T) {
	rt, ctx, id := startRestartFixture(t, "norestart", containerExitScript)
	gateway := newRestartGateway(t, rt)
	if err := gateway.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "norestart",
		Transport:          mcp.TransportStdio,
		ContainerID:        id,
		ProtocolGeneration: "handshake",
		PingTimeout:        time.Second,
		RestartPolicy:      "no",
	}); err != nil {
		t.Fatal(err)
	}
	waitForContainerExit(t, ctx, rt, id)
	gateway.StartHealthMonitor(ctx, 200*time.Millisecond)

	deadline := time.Now().Add(30 * time.Second)
	var replica mcp.ReplicaStatus
	for time.Now().Before(deadline) {
		replicas := gateway.ReplicaStatuses("norestart")
		if len(replicas) == 1 && replicas[0].RestartExhausted {
			replica = replicas[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !replica.RestartExhausted || replica.State != "unhealthy" || replica.ContainerRestarts != 0 {
		t.Fatalf("replica = %#v", gateway.ReplicaStatuses("norestart"))
	}
	info, err := rt.Client().ContainerInspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if info.State == nil || info.State.Running || info.State.ExitCode != 3 {
		t.Fatalf("container was restarted: %#v", info.State)
	}
}

func TestContainerRestart_RemovedContainerIsTerminal(t *testing.T) {
	rt, ctx, id := startRestartFixture(t, "removed", containerExitScript)
	gateway := newRestartGateway(t, rt)
	if err := gateway.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "removed",
		Transport:          mcp.TransportStdio,
		ContainerID:        id,
		ProtocolGeneration: "handshake",
		PingTimeout:        time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	waitForContainerExit(t, ctx, rt, id)
	if err := rt.Remove(ctx, runtime.WorkloadID(id)); err != nil {
		t.Fatal(err)
	}
	gateway.StartHealthMonitor(ctx, 200*time.Millisecond)

	deadline := time.Now().Add(30 * time.Second)
	var replica mcp.ReplicaStatus
	for time.Now().Before(deadline) {
		replicas := gateway.ReplicaStatuses("removed")
		if len(replicas) == 1 && replicas[0].RestartExhausted && strings.Contains(replicas[0].LastError, "container removed from runtime") {
			replica = replicas[0]
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !replica.RestartExhausted || replica.State != "unhealthy" || replica.RestartAttempts != 0 || !strings.Contains(replica.LastError, "re-apply") {
		t.Fatalf("replica = %#v", gateway.ReplicaStatuses("removed"))
	}
}

func startRestartFixture(t *testing.T, name, script string) (*dockerruntime.DockerRuntime, context.Context, string) {
	t.Helper()
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
	t.Cleanup(cancel)
	if err := rt.EnsureImage(ctx, "python:3.13-alpine"); err != nil {
		t.Fatal(err)
	}
	stack := fmt.Sprintf("restart-%s-%d", name, time.Now().UnixNano())
	networkName := stack + "-net"
	if err := rt.EnsureNetwork(ctx, networkName, runtime.NetworkOptions{Driver: "bridge", Stack: stack}); err != nil {
		t.Fatal(err)
	}
	cfg := runtime.WorkloadConfig{
		Name:        name,
		Stack:       stack,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       "python:3.13-alpine",
		Command:     []string{"python", "-u", "-c", script},
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
	return rt, ctx, string(started.ID)
}

func newRestartGateway(t *testing.T, rt *dockerruntime.DockerRuntime) *mcp.Gateway {
	t.Helper()
	gateway := mcp.NewGateway()
	t.Cleanup(func() { gateway.Close() })
	gateway.SetLogger(slog.New(logging.NewBufferHandler(logging.NewLogBuffer(50), nil)))
	gateway.SetDockerClient(rt.Client())
	return gateway
}

func assertStatusHealthy(t *testing.T, ctx context.Context, gateway *mcp.Gateway, name string) {
	t.Helper()
	ln := httptest.NewServer(api.NewServer(gateway, nil).Handler())
	t.Cleanup(ln.Close)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gridctl", "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.HomeEnv, home)
	if err := state.Save(&state.DaemonState{StackName: "restart", PID: os.Getpid(), Port: mustPort(ln.URL), Home: home, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	bin := crashDiagnosticsBinary(t, ctx)
	deadline := time.Now().Add(20 * time.Second)
	var out []byte
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, bin, "status", "--json")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1", "DOCKER_HOST=" + os.Getenv("DOCKER_HOST"), "GRIDCTL_RUNTIME=" + os.Getenv("GRIDCTL_RUNTIME")}
		var err error
		out, err = cmd.Output()
		if err != nil {
			t.Fatalf("status --json: %v", err)
		}
		var report struct {
			MCPServers []struct {
				Name     string `json:"name"`
				Healthy  *bool  `json:"healthy"`
				Replicas []struct {
					State             string `json:"state"`
					ContainerRestarts uint32 `json:"containerRestarts"`
					RestartPolicy     string `json:"restartPolicy"`
				} `json:"replicas"`
			} `json:"mcp_servers"`
		}
		if err := json.Unmarshal(out, &report); err != nil {
			t.Fatalf("decode status: %v\n%s", err, out)
		}
		for _, server := range report.MCPServers {
			if server.Name != name || len(server.Replicas) == 0 {
				continue
			}
			got := server.Replicas[0]
			if server.Healthy != nil && *server.Healthy && got.State == "healthy" && got.ContainerRestarts == 1 && got.RestartPolicy == "always" {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("status json did not show %s healthy: %s", name, out)
}
