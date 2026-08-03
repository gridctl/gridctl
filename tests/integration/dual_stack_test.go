//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// The dual-stack matrix: the gateway must serve both MCP protocol
// generations concurrently, per peer, in both directions. These tests
// run the mock server in each generation and drive the gateway edge in
// each generation, covering all four combinations over real HTTP.

const statelessVersion = "2026-07-28"

// startModernMock starts the mock MCP server in stateless mode.
func startModernMock(t *testing.T, ctx context.Context) int {
	t.Helper()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port), "-protocol", statelessVersion)
	waitForPort(t, ctx, port)
	return port
}

// startLegacyMock starts the mock MCP server in handshake mode.
func startLegacyMock(t *testing.T, ctx context.Context) int {
	t.Helper()
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)
	return port
}

func registerServer(ctx context.Context, gw *mcp.Gateway, name string, port int) error {
	return gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:         name,
		Transport:    mcp.TransportHTTP,
		Endpoint:     fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:     true,
		ReadyTimeout: 10 * time.Second,
	})
}

// statelessPost sends a stateless-era request to the gateway transport
// with the required _meta and mirrored headers.
func statelessPost(t *testing.T, handler http.Handler, method string, params map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    statelessVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "conformance-driver", "version": "1.0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{"elicitation": map[string]any{}},
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	// httptest defaults Host to example.com, which the transport's DNS
	// rebinding protection rejects with 403 before any dispatch.
	req.Host = "localhost:8180"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("MCP-Protocol-Version", statelessVersion)
	req.Header.Set("Mcp-Method", method)
	if name, ok := params["name"].(string); ok && (method == "tools/call" || method == "prompts/get") {
		req.Header.Set("Mcp-Name", name)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding response (%d): %v: %s", w.Code, err, w.Body.String())
		}
	}
	return w, resp
}

// TestDualStack_ModernServerLegacyClient covers: a 2026-07-28-only
// downstream registers through the probe and serves tools to a
// handshake-era client (acceptance criterion 1).
func TestDualStack_ModernServerLegacyClient(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := startModernMock(t, ctx)
	gw := mcp.NewGateway()
	if err := registerServer(ctx, gw, "modern", port); err != nil {
		t.Fatalf("registering modern server: %v", err)
	}

	statuses := gw.Status()
	if len(statuses) != 1 || statuses[0].ProtocolGeneration != "stateless" {
		t.Fatalf("expected stateless generation, got %+v", statuses)
	}
	if statuses[0].ProtocolVersion != statelessVersion {
		t.Errorf("protocol version = %q", statuses[0].ProtocolVersion)
	}

	// Legacy client flow against the gateway: initialize, list, call.
	handler := mcp.NewStreamableHTTPServer(gw, nil)
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"legacy","version":"1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initBody))
	req.Host = "localhost:8180"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initialize: %d %s", w.Code, w.Body.String())
	}
	sessionID := w.Header().Get("Mcp-Session-Id")

	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"modern__echo","arguments":{"message":"hi"}}}`
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(callBody))
	req.Host = "localhost:8180"
	req.Header.Set("Mcp-Session-Id", sessionID)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Echo: hi") {
		t.Fatalf("legacy call through modern server failed: %d %s", w.Code, w.Body.String())
	}
	// The legacy wire shape must not leak stateless-only interim fields.
	if strings.Contains(w.Body.String(), "input_required") {
		t.Errorf("unexpected MRTR fields on legacy path: %s", w.Body.String())
	}
}

// TestDualStack_ModernClientLegacyServer covers: a stateless-era client
// completes discover, list, and call against the gateway fronting a
// handshake-era server, with correct resultType, cache metadata, and
// error codes (acceptance criterion 2).
func TestDualStack_ModernClientLegacyServer(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := startLegacyMock(t, ctx)
	gw := mcp.NewGateway()
	if err := registerServer(ctx, gw, "legacy", port); err != nil {
		t.Fatalf("registering legacy server: %v", err)
	}
	if gen := gw.Status()[0].ProtocolGeneration; gen != "handshake" {
		t.Fatalf("expected handshake generation, got %q", gen)
	}

	handler := mcp.NewStreamableHTTPServer(gw, nil)

	// server/discover with no initialize (criterion 3: a dual-era
	// prober must classify the gateway as modern).
	w, resp := statelessPost(t, handler, "server/discover", nil)
	if w.Code != http.StatusOK || resp["error"] != nil {
		t.Fatalf("discover: %d %v", w.Code, resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["resultType"] != "complete" {
		t.Errorf("discover resultType = %v", result["resultType"])
	}

	// tools/list: resultType complete, ttlMs pinned to 0 by the legacy
	// downstream, cacheScope private.
	w, resp = statelessPost(t, handler, "tools/list", nil)
	if w.Code != http.StatusOK || resp["error"] != nil {
		t.Fatalf("tools/list: %d %v", w.Code, resp["error"])
	}
	result = resp["result"].(map[string]any)
	if result["resultType"] != "complete" || result["ttlMs"] != float64(0) || result["cacheScope"] != "private" {
		t.Errorf("list bridging fields wrong: resultType=%v ttlMs=%v cacheScope=%v", result["resultType"], result["ttlMs"], result["cacheScope"])
	}

	// tools/call: legacy server's result gets resultType synthesized.
	w, resp = statelessPost(t, handler, "tools/call", map[string]any{"name": "legacy__echo", "arguments": map[string]any{"message": "yo"}})
	if w.Code != http.StatusOK || resp["error"] != nil {
		t.Fatalf("tools/call: %d %v", w.Code, resp["error"])
	}
	result = resp["result"].(map[string]any)
	if result["resultType"] != "complete" {
		t.Errorf("call resultType = %v", result["resultType"])
	}

	// Era-mismatched version is rejected with -32022 and the supported list.
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2099-01-01","io.modelcontextprotocol/clientCapabilities":{}}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Host = "localhost:8180"
	req.Header.Set("MCP-Protocol-Version", "2099-01-01")
	req.Header.Set("Mcp-Method", "tools/list")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	if w2.Code != http.StatusBadRequest || !strings.Contains(w2.Body.String(), "-32022") {
		t.Fatalf("expected 400 -32022, got %d %s", w2.Code, w2.Body.String())
	}
}

// TestDualStack_MRTRRoundTripByteExact covers: a modern client's MRTR
// retry round-trips requestState byte-exact through the gateway's
// rename envelope to a digest-checking modern server (criterion 8).
func TestDualStack_MRTRRoundTripByteExact(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := startModernMock(t, ctx)
	gw := mcp.NewGateway()
	if err := registerServer(ctx, gw, "modern", port); err != nil {
		t.Fatalf("registering modern server: %v", err)
	}
	handler := mcp.NewStreamableHTTPServer(gw, nil)

	// First call: interim result with enveloped requestState.
	w, resp := statelessPost(t, handler, "tools/call", map[string]any{"name": "modern__ask_secret", "arguments": map[string]any{}})
	if w.Code != http.StatusOK || resp["error"] != nil {
		t.Fatalf("initial call: %d %v", w.Code, resp["error"])
	}
	result := resp["result"].(map[string]any)
	if result["resultType"] != "input_required" {
		t.Fatalf("expected input_required, got %v (%v)", result["resultType"], result)
	}
	state, _ := result["requestState"].(string)
	if state == "" {
		t.Fatal("interim result carries no requestState")
	}
	if strings.Contains(state, "mock-ask-secret-state") {
		t.Fatal("requestState relayed bare; expected the gridctl envelope")
	}

	// Retry with the echoed state and gathered input.
	w, resp = statelessPost(t, handler, "tools/call", map[string]any{
		"name":           "modern__ask_secret",
		"arguments":      map[string]any{},
		"requestState":   state,
		"inputResponses": map[string]any{"secret_word": map[string]any{"action": "accept", "content": map[string]any{"word": "swordfish"}}},
	})
	if w.Code != http.StatusOK || resp["error"] != nil {
		t.Fatalf("retry: %d %v", w.Code, resp["error"])
	}
	result = resp["result"].(map[string]any)
	content, _ := json.Marshal(result)
	if result["resultType"] != "complete" || !strings.Contains(string(content), "secret accepted") {
		t.Fatalf("retry did not complete: %s", content)
	}
}

// TestDualStack_LegacyClientMRTRToolGetsClearError covers the
// deliberate cross-era MRTR gap: a handshake-era client calling a
// modern tool that needs input gets a clear error, not an interim
// result it cannot act on.
func TestDualStack_LegacyClientMRTRToolGetsClearError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := startModernMock(t, ctx)
	gw := mcp.NewGateway()
	if err := registerServer(ctx, gw, "modern", port); err != nil {
		t.Fatalf("registering modern server: %v", err)
	}
	handler := mcp.NewStreamableHTTPServer(gw, nil)

	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"legacy","version":"1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initBody))
	req.Host = "localhost:8180"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	sessionID := w.Header().Get("Mcp-Session-Id")

	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"modern__ask_secret","arguments":{}}}`
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(callBody))
	req.Host = "localhost:8180"
	req.Header.Set("Mcp-Session-Id", sessionID)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("call: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"isError":true`) || !strings.Contains(body, "MRTR") {
		t.Fatalf("expected clear cross-era MRTR error, got %s", body)
	}
}

// TestDualStack_GenerationPinSkipsProbe covers criterion 6: pinning
// protocol_generation forces an era without probing.
func TestDualStack_GenerationPinSkipsProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port := startLegacyMock(t, ctx)
	gw := mcp.NewGateway()
	err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "pinned",
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:           true,
		ReadyTimeout:       10 * time.Second,
		ProtocolGeneration: "handshake",
	})
	if err != nil {
		t.Fatalf("registering pinned server: %v", err)
	}
	if gen := gw.Status()[0].ProtocolGeneration; gen != "handshake" {
		t.Fatalf("expected handshake generation, got %q", gen)
	}

	// Pinning stateless against a legacy server must fail loudly.
	err = gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:               "mispinned",
		Transport:          mcp.TransportHTTP,
		Endpoint:           fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		External:           true,
		ReadyTimeout:       10 * time.Second,
		ProtocolGeneration: "stateless",
	})
	if err == nil || !strings.Contains(err.Error(), "stateless") {
		t.Fatalf("expected stateless-pin failure, got %v", err)
	}
}
