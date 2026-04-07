//go:build integration

package integration

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Package-level paths to compiled mock server binaries.
// Set by TestMain; individual tests skip if empty.
var (
	mockHTTPServerBin string
	mockStdioBin      string
)

// TestMain compiles the mock server binaries once for the entire integration
// test suite and cleans up after all tests complete.
func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

func runIntegrationTests(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "gridctl-transport-*")
	if err != nil {
		log.Printf("warning: failed to create temp dir for mock binaries: %v", err)
		return m.Run()
	}
	defer os.RemoveAll(tmpDir)

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("warning: failed to get working directory: %v", err)
		return m.Run()
	}

	// Build mock HTTP/SSE server.
	httpSrc := filepath.Join(cwd, "..", "..", "examples", "_mock-servers", "mock-mcp-server")
	httpBin := filepath.Join(tmpDir, "mock-mcp-server")
	buildCmd := exec.Command("go", "build", "-o", httpBin, ".")
	buildCmd.Dir = httpSrc
	if out, err := buildCmd.CombinedOutput(); err != nil {
		log.Printf("warning: failed to build mock-mcp-server: %v\n%s", err, out)
	} else {
		mockHTTPServerBin = httpBin
	}

	// Build mock stdio server.
	stdioSrc := filepath.Join(cwd, "..", "..", "examples", "_mock-servers", "local-stdio-server")
	stdioBin := filepath.Join(tmpDir, "mock-stdio-server")
	buildCmd = exec.Command("go", "build", "-o", stdioBin, ".")
	buildCmd.Dir = stdioSrc
	if out, err := buildCmd.CombinedOutput(); err != nil {
		log.Printf("warning: failed to build local-stdio-server: %v\n%s", err, out)
	} else {
		mockStdioBin = stdioBin
	}

	return m.Run()
}

// freePort returns an OS-assigned free TCP port on localhost.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// startMockServer starts a mock server binary as a subprocess and registers
// cleanup. The test is skipped if the binary path is empty.
func startMockServer(t *testing.T, bin string, args ...string) {
	t.Helper()
	if bin == "" {
		t.Skip("mock server binary not available")
	}
	cmd := exec.Command(bin, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mock server: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	})
}

// waitForPort polls until the TCP port is accepting connections or the context
// is cancelled.
func waitForPort(t *testing.T, ctx context.Context, port int) {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for port %d: %v", port, ctx.Err())
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHTTPTransportConnect verifies that the HTTP MCP transport client can
// connect to a real server, initialize, list tools, and call a tool.
func TestHTTPTransportConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	client := mcp.NewClient("test-http", endpoint)

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !client.IsInitialized() {
		t.Error("expected client to be initialized")
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools: %v", err)
	}

	tools := client.Tools()
	if len(tools) == 0 {
		t.Fatal("expected at least one tool, got none")
	}

	// Verify the echo tool is present.
	var hasEcho bool
	for _, tool := range tools {
		if tool.Name == "echo" {
			hasEcho = true
			break
		}
	}
	if !hasEcho {
		t.Errorf("expected 'echo' tool in list, got: %v", toolNames(tools))
	}

	// Call the echo tool and verify the response.
	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool(echo): %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content in tool result")
	}
	if !strings.Contains(result.Content[0].Text, "hello") {
		t.Errorf("expected echo response to contain 'hello', got: %q", result.Content[0].Text)
	}
}

// TestSSETransportConnect verifies the HTTP client transparently handles
// SSE-formatted responses from a server running in SSE mode.
func TestSSETransportConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-sse")
	waitForPort(t, ctx, port)

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	client := mcp.NewClient("test-sse", endpoint)

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize (SSE): %v", err)
	}
	if !client.IsInitialized() {
		t.Error("expected client to be initialized after SSE handshake")
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools (SSE): %v", err)
	}

	tools := client.Tools()
	if len(tools) == 0 {
		t.Fatal("expected tools from SSE server, got none")
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "sse-test"})
	if err != nil {
		t.Fatalf("CallTool(echo) via SSE: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content from SSE tool call")
	}
	if !strings.Contains(result.Content[0].Text, "sse-test") {
		t.Errorf("expected echo response to contain 'sse-test', got: %q", result.Content[0].Text)
	}
}

// TestStdioTransportConnect verifies that the process-based (stdio) MCP
// transport can start a subprocess, initialize, list tools, and call a tool.
func TestStdioTransportConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockStdioBin == "" {
		t.Skip("mock stdio server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The ProcessClient manages the subprocess lifecycle — do not pre-start it.
	client := mcp.NewProcessClient("test-stdio", []string{mockStdioBin}, "", nil)
	t.Cleanup(func() { client.Close() })

	// Initialize calls Connect internally for process clients.
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize (stdio): %v", err)
	}
	if !client.IsInitialized() {
		t.Error("expected stdio client to be initialized")
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools (stdio): %v", err)
	}

	tools := client.Tools()
	if len(tools) == 0 {
		t.Fatal("expected tools from stdio server, got none")
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "stdio-test"})
	if err != nil {
		t.Fatalf("CallTool(echo) via stdio: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content from stdio tool call")
	}
	if !strings.Contains(result.Content[0].Text, "stdio-test") {
		t.Errorf("expected echo response to contain 'stdio-test', got: %q", result.Content[0].Text)
	}
}

// TestTransportConnectError verifies that connecting to a port with nothing
// listening returns an error promptly.
func TestTransportConnectError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Pick a free port and immediately release it — nothing will listen on it.
	port := freePort(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := mcp.NewClient("test-error", fmt.Sprintf("http://127.0.0.1:%d/mcp", port))
	err := client.Initialize(ctx)
	if err == nil {
		t.Fatal("expected error connecting to unused port, got nil")
	}
}

// toolNames returns a slice of tool names for use in test error messages.
func toolNames(tools []mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}
