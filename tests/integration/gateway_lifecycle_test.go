//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestGatewayRegisterHTTPServer verifies that the gateway can register a real
// HTTP MCP server, report it in Status(), and aggregate its tools.
func TestGatewayRegisterHTTPServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	defer gw.Close()

	cfg := mcp.MCPServerConfig{
		Name:      "test-http",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	statuses := gw.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 registered server, got %d", len(statuses))
	}
	if statuses[0].Name != "test-http" {
		t.Errorf("expected server name 'test-http', got %q", statuses[0].Name)
	}

	result, err := gw.HandleToolsListUnscoped()
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("expected tools from registered server, got none")
	}

	var hasEcho bool
	for _, tool := range result.Tools {
		if strings.Contains(tool.Name, "echo") {
			hasEcho = true
			break
		}
	}
	if !hasEcho {
		t.Errorf("expected 'echo' tool in aggregated list, got: %v", toolNames(result.Tools))
	}
}

// TestGatewayUnregisterServer verifies that unregistering an MCP server removes
// it from Status() and clears its tools from HandleToolsList().
func TestGatewayUnregisterServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	defer gw.Close()

	cfg := mcp.MCPServerConfig{
		Name:      "test-unregister",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	// Confirm tools visible before unregister.
	before, err := gw.HandleToolsListUnscoped()
	if err != nil {
		t.Fatalf("HandleToolsList (before unregister): %v", err)
	}
	if len(before.Tools) == 0 {
		t.Fatal("expected tools before unregister, got none")
	}

	gw.UnregisterMCPServer("test-unregister")

	if statuses := gw.Status(); len(statuses) != 0 {
		t.Errorf("expected empty Status() after unregister, got %d entries", len(statuses))
	}

	after, err := gw.HandleToolsListUnscoped()
	if err != nil {
		t.Fatalf("HandleToolsList (after unregister): %v", err)
	}
	if len(after.Tools) != 0 {
		t.Errorf("expected no tools after unregister, got %d: %v", len(after.Tools), toolNames(after.Tools))
	}
}

// TestGatewayHealthMonitor verifies that the gateway health monitor detects an
// unhealthy server when its subprocess is killed.
func TestGatewayHealthMonitor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)

	// Start mock server manually so we can kill it during the test.
	cmd := exec.Command(mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	})

	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	defer gw.Close()

	cfg := mcp.MCPServerConfig{
		Name:      "test-health",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	// Start health monitor with a short interval so the test doesn't need to
	// wait for DefaultHealthCheckInterval (30s).
	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()
	gw.StartHealthMonitor(healthCtx, 100*time.Millisecond)

	// Kill the backend subprocess.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill mock server: %v", err)
	}
	cmd.Wait() //nolint:errcheck

	// Poll until the health monitor detects the failure (up to 5 seconds).
	deadline := time.Now().Add(5 * time.Second)
	var hs *mcp.HealthStatus
	for time.Now().Before(deadline) {
		hs = gw.GetHealthStatus("test-health")
		if hs != nil && !hs.Healthy {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if hs == nil {
		t.Fatal("expected health status to be recorded, got nil")
	}
	if hs.Healthy {
		t.Error("expected server to be marked unhealthy after subprocess kill")
	}
}

// TestGatewayGracefulShutdown verifies that the gateway HTTP server responds to
// requests and stops accepting connections after shutdown.
func TestGatewayGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	backendPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", backendPort))
	waitForPort(t, ctx, backendPort)

	gw := mcp.NewGateway()

	cfg := mcp.MCPServerConfig{
		Name:      "test-shutdown",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", backendPort),
	}
	if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	// Start the gateway HTTP server on a free port.
	gatewayPort := freePort(t)
	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", gatewayPort),
		Handler: mcp.NewHandler(gw),
	}

	srvDone := make(chan error, 1)
	go func() {
		srvDone <- srv.ListenAndServe()
	}()

	waitForPort(t, ctx, gatewayPort)

	// Verify the gateway responds to a tools/list JSON-RPC call.
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/mcp", gatewayPort),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST to gateway: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK from gateway, got %d", resp.StatusCode)
	}

	// Graceful shutdown.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("gateway Shutdown: %v", err)
	}
	gw.Close()

	// Verify the port is released — a new connection should fail.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", gatewayPort), 500*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Error("expected connection to fail after gateway shutdown, but it succeeded")
	}
}
