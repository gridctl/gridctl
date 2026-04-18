//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestReplicas_StacklessMode verifies that the gateway — when running without
// an orchestrator (stackless mode) — accepts a multi-replica registration,
// initializes every replica, routes tool calls across them, and surfaces
// per-replica state through ReplicaStatuses.
//
// Stackless mode is the cold-load path used by `gridctl serve` and the
// /api/stack/initialize endpoint. The defining characteristic is that
// registration happens outside any container lifecycle: each replica carries
// its own runtime handle (endpoint for HTTP, container id for stdio). This
// test exercises that contract with HTTP replicas so Docker is not required.
func TestReplicas_StacklessMode(t *testing.T) {
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
	for _, p := range ports {
		startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", p))
	}
	for _, p := range ports {
		waitForPort(t, ctx, p)
	}

	gw := mcp.NewGateway()
	defer gw.Close()

	// Materialize one MCPServerConfig per replica — the shape that
	// server_registrar produces when Replicas > 1 in stack.yaml.
	cfgs := make([]mcp.MCPServerConfig, len(ports))
	endpoints := make(map[string]bool, len(ports))
	for i, p := range ports {
		endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", p)
		cfgs[i] = mcp.MCPServerConfig{
			Name:      serverName,
			Transport: mcp.TransportHTTP,
			Endpoint:  endpoint,
		}
		endpoints[endpoint] = true
	}

	if err := gw.RegisterMCPReplicaSet(ctx, serverName, mcp.ReplicaPolicyLeastConnections, cfgs); err != nil {
		t.Fatalf("RegisterMCPReplicaSet: %v", err)
	}

	statuses := gw.ReplicaStatuses(serverName)
	if len(statuses) != len(ports) {
		t.Fatalf("expected %d replica statuses, got %d", len(ports), len(statuses))
	}
	for i, rs := range statuses {
		if rs.ReplicaID != i {
			t.Errorf("replica %d: expected ReplicaID=%d, got %d", i, i, rs.ReplicaID)
		}
		if !rs.Healthy {
			t.Errorf("replica %d: expected Healthy=true right after registration", i)
		}
	}

	// Gateway Status should surface the replicas on the single logical server.
	gwStatuses := gw.Status()
	if len(gwStatuses) != 1 {
		t.Fatalf("expected 1 server in gateway Status, got %d", len(gwStatuses))
	}
	if gwStatuses[0].Name != serverName {
		t.Errorf("expected Status name %q, got %q", serverName, gwStatuses[0].Name)
	}
	if len(gwStatuses[0].Replicas) != len(ports) {
		t.Errorf("expected Status.Replicas len=%d, got %d", len(ports), len(gwStatuses[0].Replicas))
	}

	// A single server name — tool list must not leak replica ids into the
	// prefix. Every tool is exposed as "<serverName>__<tool>".
	toolsRes, err := gw.HandleToolsList()
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	prefix := serverName + mcp.ToolNameDelimiter
	if len(toolsRes.Tools) == 0 {
		t.Fatal("expected aggregated tools from replica set, got none")
	}
	for _, tool := range toolsRes.Tools {
		if len(tool.Name) <= len(prefix) || tool.Name[:len(prefix)] != prefix {
			t.Errorf("tool %q lacks expected prefix %q", tool.Name, prefix)
		}
	}

	// Tool call should succeed; least-connections routes to the lowest-id
	// idle replica for the first call.
	result, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      mcp.PrefixTool(serverName, "echo"),
		Arguments: map[string]any{"message": "stackless"},
	})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected successful tool call, got IsError=true: %v", result.Content)
	}

	// Fast-health check should not flip any replica to unhealthy — every
	// backend is alive. Run a short monitor window to exercise the per-replica
	// pings via gateway.ReplicaStatuses.
	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)
	time.Sleep(400 * time.Millisecond)

	for i, rs := range gw.ReplicaStatuses(serverName) {
		if !rs.Healthy {
			t.Errorf("replica %d flipped unhealthy during health monitor; lastError=%q", i, rs.LastError)
		}
	}
}
