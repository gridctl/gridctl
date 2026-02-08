package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"go.uber.org/mock/gomock"
)

// setupTestHandler creates a Handler with a gateway containing mock clients.
func setupTestHandler(t *testing.T) (*Handler, *Gateway) {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "echo", Description: "Echo tool"},
		{Name: "list-files", Description: "List files tool"},
	})
	// Override default CallTool with custom behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("result for " + name)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	h := NewHandler(g)
	return h, g
}

// makeJSONRPC builds a JSON-RPC 2.0 request body.
func makeJSONRPC(t *testing.T, id any, method string, params any) string {
	t.Helper()
	m := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	return string(b)
}

func postMCP(t *testing.T, handler *Handler, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestHandler_Initialize(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})
	w := postMCP(t, h, body)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.ProtocolVersion != MCPProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", MCPProtocolVersion, result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "gridctl-gateway" {
		t.Errorf("expected server name 'gridctl-gateway', got %s", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil || !result.Capabilities.Tools.ListChanged {
		t.Error("expected Tools.ListChanged capability")
	}
}

func TestHandler_Initialize_NoParams(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Initialize with no params should still succeed
	body := makeJSONRPC(t, 1, "initialize", nil)
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
}

func TestHandler_ToolsList(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "tools/list", nil)
	w := postMCP(t, h, body)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestHandler_ToolsCall(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "tools/call", map[string]any{
		"name":      "server1__echo",
		"arguments": map[string]any{"msg": "hello"},
	})
	w := postMCP(t, h, body)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("expected successful tool call")
	}
	if len(result.Content) == 0 || result.Content[0].Text != "result for echo" {
		t.Errorf("unexpected result content: %+v", result.Content)
	}
}

func TestHandler_ToolsCall_UnknownTool(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "tools/call", map[string]any{
		"name":      "unknown__tool",
		"arguments": map[string]any{},
	})
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for unknown tool")
	}
}

func TestHandler_ToolsCall_InvalidParams(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Send tools/call with invalid params (not a valid ToolCallParams structure)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not-an-object"}`
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for invalid tools/call params")
	}
	if resp.Error.Code != InvalidParams {
		t.Errorf("expected InvalidParams code %d, got %d", InvalidParams, resp.Error.Code)
	}
}

func TestHandler_Ping(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "ping", nil)
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
}

func TestHandler_NotificationsInitialized(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "notifications/initialized", nil)
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
}

func TestHandler_UnknownMethod(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "unknown/method", nil)
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != MethodNotFound {
		t.Errorf("expected MethodNotFound code %d, got %d", MethodNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown/method") {
		t.Errorf("expected error message to contain method name, got %s", resp.Error.Message)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	h, _ := setupTestHandler(t)

	w := postMCP(t, h, "{invalid json}")

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if resp.Error.Code != ParseError {
		t.Errorf("expected ParseError code %d, got %d", ParseError, resp.Error.Code)
	}
}

func TestHandler_EmptyBody(t *testing.T) {
	h, _ := setupTestHandler(t)

	w := postMCP(t, h, "")

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for empty body")
	}
	if resp.Error.Code != ParseError {
		t.Errorf("expected ParseError code %d, got %d", ParseError, resp.Error.Code)
	}
}

func TestHandler_InvalidJSONRPCVersion(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := `{"jsonrpc":"1.0","id":1,"method":"ping"}`
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON-RPC version")
	}
	if resp.Error.Code != InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", InvalidRequest, resp.Error.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h, _ := setupTestHandler(t)

	tests := []struct {
		method string
	}{
		{"PUT"},
		{"DELETE"},
		{"PATCH"},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/mcp", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", tc.method, w.Code)
			}
		})
	}
}

func TestHandler_GET_SSE(t *testing.T) {
	h, _ := setupTestHandler(t)

	// GET request to handler triggers the SSE placeholder
	req := httptest.NewRequest("GET", "/mcp", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, req)
	}()

	// Cancel immediately to unblock the handler
	cancel()
	<-done

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), `"type":"connected"`) {
		t.Errorf("expected connected event, got %s", w.Body.String())
	}
}

func TestHandler_AgentFiltering_ToolsList(t *testing.T) {
	h, g := setupTestHandler(t)

	// Register an agent with access only to echo tool
	g.RegisterAgent("my-agent", []config.ToolSelector{
		{Server: "server1", Tools: []string{"echo"}},
	})

	body := makeJSONRPC(t, 1, "tools/list", nil)
	w := postMCP(t, h, body, "X-Agent-Name", "my-agent")

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Errorf("expected 1 filtered tool, got %d", len(result.Tools))
		for _, tool := range result.Tools {
			t.Logf("  got tool: %s", tool.Name)
		}
	}
}

func TestHandler_AgentFiltering_ToolsCall(t *testing.T) {
	h, g := setupTestHandler(t)

	g.RegisterAgent("my-agent", []config.ToolSelector{
		{Server: "server1", Tools: []string{"echo"}},
	})

	// Allowed tool
	body := makeJSONRPC(t, 1, "tools/call", map[string]any{
		"name":      "server1__echo",
		"arguments": map[string]any{},
	})
	w := postMCP(t, h, body, "X-Agent-Name", "my-agent")

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.IsError {
		t.Error("expected allowed tool call to succeed")
	}

	// Denied tool
	body = makeJSONRPC(t, 2, "tools/call", map[string]any{
		"name":      "server1__list-files",
		"arguments": map[string]any{},
	})
	w = postMCP(t, h, body, "X-Agent-Name", "my-agent")

	resp = decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if !result.IsError {
		t.Error("expected denied tool call to return IsError")
	}
}

func TestHandler_AgentFiltering_UnknownAgent(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Unknown agent should get an error
	body := makeJSONRPC(t, 1, "tools/list", nil)
	w := postMCP(t, h, body, "X-Agent-Name", "nonexistent-agent")

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent")
	}
	if resp.Error.Code != InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", InvalidRequest, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "nonexistent-agent") {
		t.Errorf("expected error to contain agent name, got %s", resp.Error.Message)
	}
}

func TestHandler_AgentFiltering_UnknownAgent_ToolsCall(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "tools/call", map[string]any{
		"name":      "server1__echo",
		"arguments": map[string]any{},
	})
	w := postMCP(t, h, body, "X-Agent-Name", "nonexistent-agent")

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent on tools/call")
	}
	if resp.Error.Code != InvalidRequest {
		t.Errorf("expected InvalidRequest code, got %d", resp.Error.Code)
	}
}

func TestHandler_NoAgentHeader_ReturnsAllTools(t *testing.T) {
	h, g := setupTestHandler(t)

	// Register an agent but don't send X-Agent-Name header
	g.RegisterAgent("my-agent", []config.ToolSelector{
		{Server: "server1", Tools: []string{"echo"}},
	})

	body := makeJSONRPC(t, 1, "tools/list", nil)
	w := postMCP(t, h, body) // no X-Agent-Name header

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Without agent header, should return all tools
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools without agent filter, got %d", len(result.Tools))
	}
}

func TestHandler_ContentType(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 1, "ping", nil)
	w := postMCP(t, h, body)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestHandler_RequestSizeLimit(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Create a body slightly over MaxRequestBodySize (1MB)
	oversizedBody := `{"jsonrpc":"2.0","id":1,"method":"ping","params":"` + strings.Repeat("x", MaxRequestBodySize) + `"}`
	w := postMCP(t, h, oversizedBody)

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for oversized request body")
	}
	if resp.Error.Code != ParseError {
		t.Errorf("expected ParseError code %d, got %d", ParseError, resp.Error.Code)
	}
}

func TestHandler_ToolsCall_AgentError(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fail", Description: "Failing tool"},
	})
	// Override default CallTool to return error
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("internal failure")).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	h := NewHandler(g)

	body := makeJSONRPC(t, 1, "tools/call", map[string]any{
		"name":      "server1__fail",
		"arguments": map[string]any{},
	})
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for agent failure")
	}
}

func TestHandler_Initialize_InvalidParams(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Initialize with invalid params structure
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":"not-an-object"}`
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.Error == nil {
		t.Fatal("expected error for invalid initialize params")
	}
	if resp.Error.Code != InvalidParams {
		t.Errorf("expected InvalidParams code %d, got %d", InvalidParams, resp.Error.Code)
	}
}

func TestHandler_ResponseFormat(t *testing.T) {
	h, _ := setupTestHandler(t)

	body := makeJSONRPC(t, 42, "ping", nil)
	w := postMCP(t, h, body)

	resp := decodeResponse(t, w)
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %s", resp.JSONRPC)
	}
	if resp.ID == nil {
		t.Fatal("expected response ID to be set")
	}
	var id int
	if err := json.Unmarshal(*resp.ID, &id); err != nil {
		t.Fatalf("failed to parse response ID: %v", err)
	}
	if id != 42 {
		t.Errorf("expected ID 42, got %d", id)
	}
}

func TestHandler_LargeValidRequest(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Large but valid request (under 1MB)
	largeArgs := make(map[string]any)
	largeArgs["data"] = strings.Repeat("x", 500*1024) // 500KB
	body := makeJSONRPC(t, 1, "tools/call", map[string]any{
		"name":      "server1__echo",
		"arguments": largeArgs,
	})
	w := postMCP(t, h, body)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for large valid request, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error for large valid request: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
}

func TestHandler_Notification_NoID(t *testing.T) {
	h, _ := setupTestHandler(t)

	// Notifications have no ID field
	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for notification, got %d", w.Code)
	}
}
