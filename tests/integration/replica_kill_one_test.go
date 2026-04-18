//go:build integration

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestReplicas_KillOneReplica verifies that killing one replica's backend
// process causes the health monitor to mark it unhealthy, exclude it from
// dispatch, and keep the surviving replicas serving tool calls without error.
func TestReplicas_KillOneReplica(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockHTTPServerBin == "" {
		t.Skip("mock server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start three mock HTTP servers; keep handles so we can kill one mid-test.
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
			Name:      "junos",
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", p),
		}
	}
	if err := gw.RegisterMCPReplicaSet(ctx, "junos", mcp.ReplicaPolicyRoundRobin, cfgs); err != nil {
		t.Fatalf("RegisterMCPReplicaSet: %v", err)
	}

	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)

	// Kill replica-1.
	if err := cmds[1].Process.Kill(); err != nil {
		t.Fatalf("kill replica-1: %v", err)
	}
	cmds[1].Wait() //nolint:errcheck

	// Poll until replica-1 is marked unhealthy (up to 5s).
	deadline := time.Now().Add(5 * time.Second)
	var statuses []mcp.ReplicaStatus
	for time.Now().Before(deadline) {
		statuses = gw.ReplicaStatuses("junos")
		if len(statuses) == 3 && !statuses[1].Healthy {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 replica statuses, got %d", len(statuses))
	}
	if statuses[1].Healthy {
		t.Fatalf("expected replica-1 to be marked unhealthy after process kill")
	}
	if !statuses[0].Healthy || !statuses[2].Healthy {
		t.Errorf("expected replicas 0 and 2 to remain healthy; got r0=%v r2=%v",
			statuses[0].Healthy, statuses[2].Healthy)
	}

	// Surviving replicas should continue serving tool calls. Call echo several
	// times; dispatch should skip replica-1 and every call should succeed.
	for i := 0; i < 6; i++ {
		result, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      mcp.PrefixTool("junos", "echo"),
			Arguments: map[string]any{"message": fmt.Sprintf("call-%d", i)},
		})
		if err != nil {
			t.Fatalf("tool call %d: %v", i, err)
		}
		if result.IsError {
			t.Fatalf("tool call %d returned IsError; content=%v", i, result.Content)
		}
	}
}
