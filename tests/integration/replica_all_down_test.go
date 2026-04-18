//go:build integration

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestReplicas_AllReplicasDown verifies that when every replica of a server is
// unhealthy, tool calls fail fast with an error that names the server.
func TestReplicas_AllReplicasDown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockHTTPServerBin == "" {
		t.Skip("mock server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const serverName = "junos"
	ports := []int{freePort(t), freePort(t), freePort(t)}
	cmds := make([]*exec.Cmd, len(ports))
	for i, p := range ports {
		cmd := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", p))
		if err := cmd.Start(); err != nil {
			t.Fatalf("start mock replica-%d: %v", i, err)
		}
		cmds[i] = cmd
	}
	t.Cleanup(func() {
		for _, cmd := range cmds {
			if cmd != nil && cmd.Process != nil {
				cmd.Process.Kill() //nolint:errcheck
				cmd.Wait()         //nolint:errcheck
			}
		}
	})
	for _, p := range ports {
		waitForPort(t, ctx, p)
	}

	gw := mcp.NewGateway()
	defer gw.Close()

	cfgs := make([]mcp.MCPServerConfig, len(ports))
	for i, p := range ports {
		cfgs[i] = mcp.MCPServerConfig{
			Name:      serverName,
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", p),
		}
	}
	if err := gw.RegisterMCPReplicaSet(ctx, serverName, mcp.ReplicaPolicyRoundRobin, cfgs); err != nil {
		t.Fatalf("RegisterMCPReplicaSet: %v", err)
	}

	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)

	// Kill every replica.
	for i, cmd := range cmds {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("kill replica-%d: %v", i, err)
		}
		cmd.Wait() //nolint:errcheck
	}

	// Poll until all three replicas are marked unhealthy (up to 5s).
	deadline := time.Now().Add(5 * time.Second)
	var statuses []mcp.ReplicaStatus
	for time.Now().Before(deadline) {
		statuses = gw.ReplicaStatuses(serverName)
		allDown := len(statuses) == len(ports)
		for _, rs := range statuses {
			if rs.Healthy {
				allDown = false
				break
			}
		}
		if allDown {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for i, rs := range statuses {
		if rs.Healthy {
			t.Fatalf("expected replica-%d to be unhealthy after process kill", i)
		}
	}

	// Rollup health should also reflect the outage.
	hs := gw.GetHealthStatus(serverName)
	if hs == nil {
		t.Fatal("expected rollup health status, got nil")
	}
	if hs.Healthy {
		t.Error("expected rollup Healthy=false when all replicas are down")
	}

	// Tool call should return an error result naming the server and the
	// no-healthy-replicas failure reason.
	result, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      mcp.PrefixTool(serverName, "echo"),
		Arguments: map[string]any{"message": "nobody home"},
	})
	if err != nil {
		t.Fatalf("HandleToolsCall returned Go error (expected structured IsError): %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool call result to have IsError=true when all replicas are down")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected tool call error to include Content")
	}
	text := result.Content[0].Text
	if !strings.Contains(text, serverName) {
		t.Errorf("expected error to name server %q, got: %q", serverName, text)
	}
	if !strings.Contains(text, mcp.ErrNoHealthyReplicas.Error()) {
		t.Errorf("expected error to mention %q, got: %q", mcp.ErrNoHealthyReplicas.Error(), text)
	}
}
