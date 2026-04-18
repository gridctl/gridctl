//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestReplicas_MixedCounts verifies that a gateway hosting servers with
// different replica counts (one single-replica, one multi-replica) routes
// tool calls to the correct set and exposes accurate per-server status.
// Round-robin on the multi-replica server should rotate through every
// replica across successive calls.
func TestReplicas_MixedCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockHTTPServerBin == "" {
		t.Skip("mock server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const singleName = "github"
	const tripleName = "junos"

	singlePort := freePort(t)
	triplePorts := []int{freePort(t), freePort(t), freePort(t)}

	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", singlePort))
	for _, p := range triplePorts {
		startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", p))
	}
	waitForPort(t, ctx, singlePort)
	for _, p := range triplePorts {
		waitForPort(t, ctx, p)
	}

	gw := mcp.NewGateway()
	defer gw.Close()

	singleCfg := mcp.MCPServerConfig{
		Name:      singleName,
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", singlePort),
	}
	if err := gw.RegisterMCPReplicaSet(ctx, singleName, mcp.ReplicaPolicyRoundRobin, []mcp.MCPServerConfig{singleCfg}); err != nil {
		t.Fatalf("register %s: %v", singleName, err)
	}

	tripleCfgs := make([]mcp.MCPServerConfig, len(triplePorts))
	for i, p := range triplePorts {
		tripleCfgs[i] = mcp.MCPServerConfig{
			Name:      tripleName,
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", p),
		}
	}
	if err := gw.RegisterMCPReplicaSet(ctx, tripleName, mcp.ReplicaPolicyRoundRobin, tripleCfgs); err != nil {
		t.Fatalf("register %s: %v", tripleName, err)
	}

	if got := len(gw.ReplicaStatuses(singleName)); got != 1 {
		t.Errorf("expected 1 replica for %s, got %d", singleName, got)
	}
	if got := len(gw.ReplicaStatuses(tripleName)); got != 3 {
		t.Errorf("expected 3 replicas for %s, got %d", tripleName, got)
	}

	// Both servers should appear in gateway Status with the right Replicas shape.
	gwStatuses := gw.Status()
	if len(gwStatuses) != 2 {
		t.Fatalf("expected 2 servers in gateway Status, got %d", len(gwStatuses))
	}
	byName := map[string]mcp.MCPServerStatus{}
	for _, s := range gwStatuses {
		byName[s.Name] = s
	}
	if got := len(byName[singleName].Replicas); got != 1 {
		t.Errorf("%s Status: expected 1 Replica entry, got %d", singleName, got)
	}
	if got := len(byName[tripleName].Replicas); got != 3 {
		t.Errorf("%s Status: expected 3 Replica entries, got %d", tripleName, got)
	}

	// Tool calls on the single-replica server succeed.
	single, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      mcp.PrefixTool(singleName, "echo"),
		Arguments: map[string]any{"message": "single"},
	})
	if err != nil || single.IsError {
		t.Fatalf("tool call on %s: err=%v isError=%v", singleName, err, single.IsError)
	}

	// Round-robin on the triple-replica server: fire 6 calls and count how
	// many times each replica observed an in-flight increment. Because
	// IncInFlight/DecInFlight wrap each call, in-flight always reads back to
	// zero afterwards — so we record ids by snapshotting replicas between
	// calls via a tiny probe: inject pick order through ReplicaStatuses'
	// restart attempts would be invasive, so instead we validate uniqueness
	// by observing that every call succeeds (if RR skipped replicas, a dead
	// backend would surface). Here we only assert all calls succeed.
	for i := 0; i < 6; i++ {
		result, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name:      mcp.PrefixTool(tripleName, "echo"),
			Arguments: map[string]any{"message": fmt.Sprintf("triple-%d", i)},
		})
		if err != nil {
			t.Fatalf("tool call %d on %s: %v", i, tripleName, err)
		}
		if result.IsError {
			t.Fatalf("tool call %d on %s returned IsError: %v", i, tripleName, result.Content)
		}
	}

	// Round-robin dispatch should have touched every replica across 6 calls.
	// Use Pick() directly on the router's replica set to observe id rotation.
	set := gw.Router().GetReplicaSet(tripleName)
	if set == nil {
		t.Fatal("expected triple replica set on router")
	}
	seen := map[int]bool{}
	for i := 0; i < 2*set.Size(); i++ {
		r, err := set.Pick()
		if err != nil {
			t.Fatalf("Pick %d: %v", i, err)
		}
		seen[r.ID()] = true
	}
	if len(seen) != set.Size() {
		t.Errorf("round-robin did not touch every replica across %d picks; saw ids=%v", 2*set.Size(), seen)
	}

	// A short health-monitor burst should not flip any replica unhealthy.
	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)
	time.Sleep(400 * time.Millisecond)

	for name := range byName {
		for i, rs := range gw.ReplicaStatuses(name) {
			if !rs.Healthy {
				t.Errorf("%s replica %d unexpectedly unhealthy during monitor: %q", name, i, rs.LastError)
			}
		}
	}
}
