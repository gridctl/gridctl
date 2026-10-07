//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Package-level paths to compiled mock server binaries.
// Set by TestMain before any test runs; a build failure fails the suite.
var (
	mockHTTPServerBin string
	mockStdioBin      string
)

// TestMain compiles the mock server binaries once for the entire integration
// test suite and cleans up after all tests complete. A failure to prepare the
// binaries fails the whole suite rather than letting dependent tests skip,
// so CI cannot go green while the mock servers do not compile.
func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

func runIntegrationTests(m *testing.M) int {
	tmpDir, err := os.MkdirTemp("", "gridctl-transport-*")
	if err != nil {
		log.Printf("failed to create temp dir for mock binaries: %v", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("failed to get working directory: %v", err)
		return 1
	}

	const fixtureBuildTimeout = 90 * time.Second

	buildFixture := func(src, bin, name string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), fixtureBuildTimeout)
		defer cancel()
		buildCmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
		buildCmd.Dir = src
		if out, err := buildCmd.CombinedOutput(); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				log.Printf("timed out building %s after %s", name, fixtureBuildTimeout)
			}
			log.Printf("failed to build %s: %v\n%s", name, err, out)
			return false
		}
		return true
	}

	// Build mock HTTP/SSE server.
	httpSrc := filepath.Join(cwd, "..", "..", "examples", "_mock-servers", "mock-mcp-server")
	httpBin := filepath.Join(tmpDir, "mock-mcp-server")
	if !buildFixture(httpSrc, httpBin, "mock-mcp-server") {
		return 1
	}
	mockHTTPServerBin = httpBin

	// Build mock stdio server.
	stdioSrc := filepath.Join(cwd, "..", "..", "examples", "_mock-servers", "local-stdio-server")
	stdioBin := filepath.Join(tmpDir, "mock-stdio-server")
	if !buildFixture(stdioSrc, stdioBin, "local-stdio-server") {
		return 1
	}
	mockStdioBin = stdioBin

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
// cleanup.
func startMockServer(t *testing.T, bin string, args ...string) {
	t.Helper()
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

// TestSSETransportServerRequestBeforeResponse verifies that an SSE server
// which emits a ping before tools/call still returns the real result once
// the client answers the ping.
func TestSSETransportServerRequestBeforeResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-sse", "-sse-server-request")
	waitForPort(t, ctx, port)

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	client := mcp.NewClient("test-sse-request", endpoint)

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize (SSE server request): %v", err)
	}
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools (SSE server request): %v", err)
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "pong-test"})
	if err != nil {
		t.Fatalf("CallTool(echo): %v", err)
	}
	if result.IsError {
		t.Fatalf("isError result: %+v", result)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "pong-test") {
		t.Fatalf("content = %+v, want pong-test", result.Content)
	}
}

// TestStdioTransportConnect verifies that the process-based (stdio) MCP
// transport can start a subprocess, initialize, list tools, and call a tool.
func TestStdioTransportConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
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

// TestStdioTransportStateless verifies the stdio server/discover probe
// path against a 2026-07-28-only stdio server: era resolution with no
// handshake, tool listing, and a tool call carrying resultType.
func TestStdioTransportStateless(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := mcp.NewProcessClient("test-stdio-modern", []string{mockStdioBin, "-protocol", "2026-07-28"}, "", nil)
	t.Cleanup(func() { client.Close() })

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize (stdio stateless): %v", err)
	}
	if client.Era() != mcp.EraStateless {
		t.Fatalf("era = %q, want stateless (probe must classify without a handshake)", client.Era())
	}
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools (stdio stateless): %v", err)
	}
	if len(client.Tools()) == 0 {
		t.Fatal("expected tools from the modern stdio server, got none")
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "stdio-modern"})
	if err != nil {
		t.Fatalf("CallTool(echo) via modern stdio: %v", err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "stdio-modern") {
		t.Fatalf("unexpected echo result: %+v", result.Content)
	}
	if result.ResultType != mcp.ResultTypeComplete {
		t.Errorf("resultType = %q, want complete", result.ResultType)
	}
}

// TestStdioTransportMixedContent drives the fixture's mixed_content tool
// through ProcessClient. This is a decode check of the stdio chokepoint,
// not a gateway truncation or aggregation check.
func TestStdioTransportMixedContent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := mcp.NewProcessClient("test-stdio-mixed", []string{mockStdioBin}, "", nil)
	t.Cleanup(func() { client.Close() })

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize (stdio mixed): %v", err)
	}
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools (stdio mixed): %v", err)
	}

	var def mcp.Tool
	var found bool
	for _, tool := range client.Tools() {
		if tool.Name == "mixed_content" {
			def = tool
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mixed_content missing from tools/list: %v", toolNames(client.Tools()))
	}
	if def.Annotations == nil || string(def.Annotations.Extra["x-custom"]) != `"keep"` {
		t.Fatalf("unknown annotation key lost: %+v", def.Annotations)
	}
	assertRawJSON(t, def.Icons, `[{"src":"icon.png","mimeType":"image/png"}]`)
	assertRawJSON(t, def.Execution, `{"taskSupport":"optional"}`)
	assertRawJSON(t, def.Meta, `{"ui":{"resourceUri":"ui://mock/mixed"}}`)

	result, err := client.CallTool(ctx, "mixed_content", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool(mixed_content): %v", err)
	}
	if result.IsError || len(result.Content) != 6 {
		t.Fatalf("unexpected result: %+v", result)
	}

	text := result.Content[0]
	if text.Type != "text" || text.Text != "Screenshot taken" {
		t.Fatalf("text block: %+v", text)
	}
	assertRawJSON(t, text.Annotations, `{"audience":["user"],"priority":0.9}`)
	assertRawJSON(t, text.Meta, `{"k":"v"}`)

	image := result.Content[1]
	if image.Type != "image" || image.Data != "iVBORw0K" || image.MimeType != "image/png" {
		t.Fatalf("image block: %+v", image)
	}
	audio := result.Content[2]
	if audio.Type != "audio" || audio.Data != "UklGRg==" || audio.MimeType != "audio/wav" {
		t.Fatalf("audio block: %+v", audio)
	}
	link := result.Content[3]
	if link.Type != "resource_link" || link.URI != "file:///a.rs" || link.Name != "a.rs" || link.MimeType != "text/x-rust" || link.Size == nil || *link.Size != 42 {
		t.Fatalf("resource_link block: %+v", link)
	}
	assertRawJSON(t, result.Content[4].Resource, `{"uri":"ui://excalidraw/canvas","mimeType":"text/html","text":"<html></html>"}`)
	assertRawJSON(t, result.Content[5].Resource, `{"uri":"file:///b.bin","mimeType":"application/octet-stream","blob":"AAEC"}`)
}

func assertRawJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got: %v (%s)", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", got, want)
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
