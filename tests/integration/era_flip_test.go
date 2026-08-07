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

// TestGatewayHTTPEraFlipRecovery covers the redeploy scenario behind
// #1088: an external HTTP server registered on the handshake generation
// comes back stateless-only on the same port. The health monitor must
// detect the flip (the old bare-reachability check read the flipped
// server as healthy forever), reconnect the live client, re-resolve the
// generation, and serve tool calls again without a gateway restart.
func TestGatewayHTTPEraFlipRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := freePort(t)

	// Start the legacy-generation mock manually so it can be killed
	// mid-test to simulate the redeploy.
	legacy := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	if err := legacy.Start(); err != nil {
		t.Fatalf("start legacy mock: %v", err)
	}
	t.Cleanup(func() {
		legacy.Process.Kill() //nolint:errcheck
		legacy.Wait()         //nolint:errcheck
	})
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	defer gw.Close()

	cfg := mcp.MCPServerConfig{
		Name:         "flip-era",
		Transport:    mcp.TransportHTTP,
		Endpoint:     fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:     true,
		ReadyTimeout: 10 * time.Second,
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	statuses := gw.Status()
	if len(statuses) != 1 || statuses[0].ProtocolGeneration != "handshake" {
		t.Fatalf("expected handshake generation at registration, got %+v", statuses)
	}

	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)

	// Redeploy: kill the legacy process and bring the server back on the
	// same port speaking only the stateless generation.
	if err := legacy.Process.Kill(); err != nil {
		t.Fatalf("kill legacy mock: %v", err)
	}
	legacy.Wait() //nolint:errcheck

	modern := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-protocol", statelessVersion)
	if err := modern.Start(); err != nil {
		t.Fatalf("start modern mock: %v", err)
	}
	t.Cleanup(func() {
		modern.Process.Kill() //nolint:errcheck
		modern.Wait()         //nolint:errcheck
	})
	waitForPort(t, ctx, port)

	// Reconnect attempts back off starting at one second, so allow a
	// generous window for the monitor to converge on the flipped server.
	deadline := time.Now().Add(20 * time.Second)
	converged := false
	for time.Now().Before(deadline) {
		hs := gw.GetHealthStatus("flip-era")
		st := gw.Status()
		if hs != nil && hs.Healthy && len(st) == 1 && st[0].ProtocolGeneration == "stateless" {
			converged = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !converged {
		t.Fatalf("gateway did not converge on the flipped server: health=%+v status=%+v",
			gw.GetHealthStatus("flip-era"), gw.Status())
	}

	// The recovered client must serve tool calls on the new generation.
	res, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name:      "flip-era__echo",
		Arguments: map[string]any{"message": "post-flip"},
	})
	if err != nil {
		t.Fatalf("HandleToolsCall after flip recovery: %v", err)
	}
	if res.IsError {
		t.Fatalf("echo after flip recovery returned tool error: %+v", res.Content)
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "post-flip") {
		t.Errorf("echo result = %+v, want the echoed message", res.Content)
	}
}
