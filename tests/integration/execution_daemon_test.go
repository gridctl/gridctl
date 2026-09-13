//go:build integration && !windows

package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
	"gopkg.in/yaml.v3"
)

func TestExecution_RealDaemonReloadAndRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, "./cmd/gridctl")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("race build: %v\n%s", err, output)
	}
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := dockerruntime.NewWithInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if err := rt.EnsureImage(ctx, "python:3.13-alpine"); err != nil {
		t.Fatal(err)
	}
	stack := &config.Stack{Name: "execution-daemon-" + strings.ToLower(rand.Text()[:8]), Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{Type: "bearer", Token: "${GRIDCTL_EXECUTION_TEST_TOKEN}"}}, MCPServers: []config.MCPServer{{Name: "fixture", Image: "python:3.13-alpine", Transport: "stdio", Command: []string{"python", "-u", "-c", pythonFixtureModule + "\nmain()\n"}, Replicas: 2}}}
	stack.SetDefaults()
	stackPath := filepath.Join(dir, "stack.yaml")
	writeStack := func() {
		data, err := yaml.Marshal(stack)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stackPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeStack()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		workloads, err := rt.List(cleanupCtx, runtime.WorkloadFilter{Stack: stack.Name})
		if err != nil {
			t.Error(err)
			return
		}
		for _, workload := range workloads {
			if err := rt.Remove(cleanupCtx, workload.ID); err != nil {
				t.Error(err)
			}
		}
		networks, err := rt.ListNetworks(cleanupCtx, stack.Name)
		if err != nil {
			t.Error(err)
			return
		}
		for _, network := range networks {
			if err := rt.RemoveNetwork(cleanupCtx, network); err != nil {
				t.Error(err)
			}
		}
	}()
	port := freePort(t)
	token := rand.Text()
	cmd := exec.CommandContext(ctx, bin, "apply", stackPath, "--foreground", "--port", strconv.Itoa(port))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GRIDCTL_HOME=" + dir, "XDG_CONFIG_HOME=" + dir, "DOCKER_HOST=" + info.DockerHost(), "GRIDCTL_EXECUTION_TEST_TOKEN=" + token, "GRIDCTL_RUNTIME=" + os.Getenv("GRIDCTL_RUNTIME")}
	logFile, err := os.Create(filepath.Join(dir, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil && ctx.Err() == nil {
				t.Errorf("race daemon exit: %v", err)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("daemon shutdown exceeded bound")
		}
	}()
	waitForPort(t, ctx, port)
	request := func(method, path string, into any) int {
		req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), token) {
			t.Fatal("status disclosed gateway credential")
		}
		if res.StatusCode >= http.StatusBadRequest {
			// This fixture owns the stack and credentials; retain bounded API
			// failure details so reload errors identify the failing control.
			t.Logf("%s %s failed (%d): %s", method, path, res.StatusCode, body[:min(len(body), 4096)])
		}
		if into != nil {
			if err := json.Unmarshal(body, into); err != nil {
				t.Fatalf("invalid response: %v", err)
			}
		}
		return res.StatusCode
	}
	waitReady := func(protected bool) mcp.MCPServerStatus {
		deadline := time.NewTimer(40 * time.Second)
		defer deadline.Stop()
		for {
			var statuses []mcp.MCPServerStatus
			request(http.MethodGet, "/api/mcp-servers", &statuses)
			if len(statuses) == 1 && len(statuses[0].Replicas) == 2 && statuses[0].Initialized {
				good := true
				for _, replica := range statuses[0].Replicas {
					good = good && replica.Healthy && (!protected || (replica.Execution != nil && replica.Execution.Eligible))
				}
				if good {
					return statuses[0]
				}
			}
			select {
			case <-deadline.C:
				logData, _ := os.ReadFile(filepath.Join(dir, "daemon.log"))
				t.Log(strings.ReplaceAll(string(logData), token, "[redacted]"))
				t.Fatalf("required execution readiness (protected=%v): %+v", protected, statuses)
			case <-ctx.Done():
				t.Fatal("daemon did not reach required execution readiness")
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	before := waitReady(false)
	var pinsBefore pins.ServerPins
	if code := request(http.MethodGet, "/api/pins/fixture", &pinsBefore); code != http.StatusOK || pinsBefore.PinnedAt.IsZero() {
		t.Fatalf("initial pins unavailable: %d", code)
	}
	uid, gid := uint32(65534), uint32(65534)
	stack.MCPServers[0].Execution = &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}
	writeStack()
	if code := request(http.MethodPost, "/api/reload", nil); code != http.StatusOK {
		t.Fatalf("execution reload failed: %d", code)
	}
	after := waitReady(true)
	if before.Replicas[0].ContainerID == after.Replicas[0].ContainerID {
		t.Fatal("tightening reused weaker container")
	}
	var pinsAfter pins.ServerPins
	request(http.MethodGet, "/api/pins/fixture", &pinsAfter)
	if !pinsBefore.PinnedAt.Equal(pinsAfter.PinnedAt) || pinsBefore.ServerHash != pinsAfter.ServerHash {
		t.Fatal("execution-only recreation reset schema pins")
	}
	if code := request(http.MethodPost, "/api/mcp-servers/fixture/restart", nil); code != http.StatusOK {
		t.Fatalf("protected replica restart failed: %d", code)
	}
	restarted := waitReady(true)
	if !restarted.Replicas[0].Execution.ObservedAt.After(after.Replicas[0].Execution.ObservedAt) {
		t.Fatal("direct restart reused stale evidence")
	}
	bound := int64(128 * 1024 * 1024)
	stack.MCPServers[0].Execution.MemoryBytes = &bound
	stack.MCPServers[0].Command = []string{"/fixture-missing-command"}
	writeStack()
	request(http.MethodPost, "/api/reload", nil)
	var failed []mcp.MCPServerStatus
	request(http.MethodGet, "/api/mcp-servers", &failed)
	if len(failed) != 1 || !failed[0].RegistrationFailed || len(failed[0].Replicas) != 0 || failed[0].Execution == nil || failed[0].Execution.Eligible || failed[0].Execution.Revision == after.Execution.Revision {
		t.Fatalf("failed tightening lost accepted intent: %+v", failed)
	}
}
