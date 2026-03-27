package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/provisioner"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// newTestServer creates a Server with a fresh gateway and log buffer for testing.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	gateway := mcp.NewGateway()
	return NewServer(gateway, nil)
}

// newTestServerWithLogBuffer creates a Server with a log buffer configured.
func newTestServerWithLogBuffer(t *testing.T, bufferSize int) *Server {
	t.Helper()
	srv := newTestServer(t)
	srv.SetLogBuffer(logging.NewLogBuffer(bufferSize))
	return srv
}

// --- Health endpoint tests ---

func TestHandleHealth(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "OK" {
		t.Errorf("expected body %q, got %q", "OK", body)
	}
}

func TestHandleHealth_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/health", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

// --- Ready endpoint tests ---

func TestHandleReady_NoServers(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// No MCP servers registered -> all initialized (vacuously true) -> 200
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "OK" {
		t.Errorf("expected body %q, got %q", "OK", body)
	}
}

func TestHandleReady_AllInitialized(t *testing.T) {
	srv := newTestServer(t)

	// Add an initialized mock client
	mock := newMockAgentClient("test-server", []mcp.Tool{
		{Name: "test-tool", Description: "a test tool"},
	})
	srv.gateway.Router().AddClient(mock)
	// Register metadata so it appears in Status()
	registerMockServerMeta(srv.gateway, "test-server", mcp.TransportHTTP)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandleReady_NotInitialized(t *testing.T) {
	srv := newTestServer(t)

	// Create a mock client that is NOT initialized
	mock := &mockAgentClient{name: "unready-server", initialized: false}
	srv.gateway.Router().AddClient(mock)
	registerMockServerMeta(srv.gateway, "unready-server", mcp.TransportHTTP)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "unready-server") {
		t.Errorf("expected body to mention server name, got %q", body)
	}
}

func TestHandleReady_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Status endpoint tests ---

func TestHandleStatus(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	assertContentType(t, rec, "application/json")

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}

	// Verify expected top-level keys
	for _, key := range []string{"gateway", "mcp-servers", "resources", "sessions"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing key %q in status response", key)
		}
	}

	// Verify gateway info
	gateway, ok := result["gateway"].(map[string]any)
	if !ok {
		t.Fatal("gateway is not an object")
	}
	if name, ok := gateway["name"].(string); !ok || name != "gridctl-gateway" {
		t.Errorf("expected gateway name %q, got %v", "gridctl-gateway", gateway["name"])
	}
}

func TestHandleStatus_WithMCPServer(t *testing.T) {
	srv := newTestServer(t)

	mock := newMockAgentClient("my-server", []mcp.Tool{
		{Name: "tool-a", Description: "Tool A"},
		{Name: "tool-b", Description: "Tool B"},
	})
	srv.gateway.Router().AddClient(mock)
	srv.gateway.Router().RefreshTools()
	registerMockServerMeta(srv.gateway, "my-server", mcp.TransportHTTP)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result struct {
		MCPServers []MCPServerStatus `json:"mcp-servers"`
		Sessions   int               `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(result.MCPServers))
	}
	if result.MCPServers[0].Name != "my-server" {
		t.Errorf("expected server name %q, got %q", "my-server", result.MCPServers[0].Name)
	}
	if result.MCPServers[0].ToolCount != 2 {
		t.Errorf("expected 2 tools, got %d", result.MCPServers[0].ToolCount)
	}
}

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- MCP Servers endpoint tests ---

func TestHandleMCPServers(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	assertContentType(t, rec, "application/json")

	// Should return an empty array, not null
	var result []any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestHandleMCPServers_WithServer(t *testing.T) {
	srv := newTestServer(t)

	mock := newMockAgentClient("test-mcp", []mcp.Tool{
		{Name: "do-thing", Description: "does a thing"},
	})
	srv.gateway.Router().AddClient(mock)
	registerMockServerMeta(srv.gateway, "test-mcp", mcp.TransportStdio)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []mcp.MCPServerStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 server, got %d", len(result))
	}
	if result[0].Name != "test-mcp" {
		t.Errorf("expected %q, got %q", "test-mcp", result[0].Name)
	}
	if result[0].Transport != mcp.TransportStdio {
		t.Errorf("expected transport %q, got %q", mcp.TransportStdio, result[0].Transport)
	}
}

func TestHandleMCPServers_IncludesOutputFormat(t *testing.T) {
	srv := newTestServer(t)

	mock := newMockAgentClient("format-server", []mcp.Tool{
		{Name: "do-thing", Description: "does a thing"},
	})
	srv.gateway.Router().AddClient(mock)
	srv.gateway.SetServerMeta(mcp.MCPServerConfig{
		Name:         "format-server",
		Transport:    mcp.TransportHTTP,
		OutputFormat: "toon",
	})

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/mcp-servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []MCPServerStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 server, got %d", len(result))
	}
	if result[0].OutputFormat != "toon" {
		t.Errorf("output format = %q, want %q", result[0].OutputFormat, "toon")
	}
}

func TestHandleMCPServers_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/mcp-servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Tools endpoint tests ---

func TestHandleTools_Empty(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	assertContentType(t, rec, "application/json")

	var result mcp.ToolsListResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(result.Tools))
	}
}

func TestHandleTools_WithTools(t *testing.T) {
	srv := newTestServer(t)

	mock := newMockAgentClient("toolbox", []mcp.Tool{
		{Name: "read-file", Description: "Read a file"},
		{Name: "write-file", Description: "Write a file"},
	})
	srv.gateway.Router().AddClient(mock)
	srv.gateway.Router().RefreshTools()

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result mcp.ToolsListResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}

	// Tools should be prefixed with server name
	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["toolbox__read-file"] {
		t.Error("expected tool 'toolbox__read-file' in response")
	}
	if !toolNames["toolbox__write-file"] {
		t.Error("expected tool 'toolbox__write-file' in response")
	}
}

func TestHandleTools_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/tools", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Gateway logs endpoint tests ---

func TestHandleGatewayLogs_NoBuffer(t *testing.T) {
	srv := newTestServer(t)
	// logBuffer is nil by default
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d entries", len(result))
	}
}

func TestHandleGatewayLogs_WithEntries(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)

	srv.logBuffer.Add(logging.BufferedEntry{
		Level:   "INFO",
		Message: "gateway started",
	})
	srv.logBuffer.Add(logging.BufferedEntry{
		Level:   "WARN",
		Message: "connection slow",
	})

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []logging.BufferedEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result[0].Message != "gateway started" {
		t.Errorf("expected first message %q, got %q", "gateway started", result[0].Message)
	}
	if result[1].Message != "connection slow" {
		t.Errorf("expected second message %q, got %q", "connection slow", result[1].Message)
	}
}

func TestHandleGatewayLogs_LevelFilter(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)

	srv.logBuffer.Add(logging.BufferedEntry{Level: "INFO", Message: "info msg"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "WARN", Message: "warn msg"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "ERROR", Message: "error msg"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "INFO", Message: "another info"})

	handler := srv.Handler()

	// Filter for ERROR only
	req := httptest.NewRequest(http.MethodGet, "/api/logs?level=ERROR", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []logging.BufferedEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Level != "ERROR" {
		t.Errorf("expected level ERROR, got %q", result[0].Level)
	}
}

func TestHandleGatewayLogs_MultiLevelFilter(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)

	srv.logBuffer.Add(logging.BufferedEntry{Level: "INFO", Message: "info"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "WARN", Message: "warn"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "ERROR", Message: "error"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "DEBUG", Message: "debug"})

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/logs?level=WARN,ERROR", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var result []logging.BufferedEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	for _, entry := range result {
		if entry.Level != "WARN" && entry.Level != "ERROR" {
			t.Errorf("unexpected level %q in filtered results", entry.Level)
		}
	}
}

func TestHandleGatewayLogs_LinesParam(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)

	for i := 0; i < 20; i++ {
		srv.logBuffer.Add(logging.BufferedEntry{
			Level:   "INFO",
			Message: "log entry",
		})
	}

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/logs?lines=5", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var result []logging.BufferedEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 5 {
		t.Errorf("expected 5 entries, got %d", len(result))
	}
}

func TestHandleGatewayLogs_InvalidLinesParam(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)
	srv.logBuffer.Add(logging.BufferedEntry{Level: "INFO", Message: "test"})

	handler := srv.Handler()

	// Invalid lines param should default to 100
	req := httptest.NewRequest(http.MethodGet, "/api/logs?lines=abc", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []logging.BufferedEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 entry (default 100, but only 1 in buffer), got %d", len(result))
	}
}

func TestHandleGatewayLogs_CaseInsensitiveLevelFilter(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)
	srv.logBuffer.Add(logging.BufferedEntry{Level: "INFO", Message: "info"})
	srv.logBuffer.Add(logging.BufferedEntry{Level: "ERROR", Message: "error"})

	handler := srv.Handler()

	// Filter with lowercase should match uppercase stored levels
	req := httptest.NewRequest(http.MethodGet, "/api/logs?level=error", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var result []logging.BufferedEntry
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry for case-insensitive filter, got %d", len(result))
	}
	if result[0].Level != "ERROR" {
		t.Errorf("expected level ERROR, got %q", result[0].Level)
	}
}

func TestHandleGatewayLogs_EmptyBuffer(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 100)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var result []any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %d entries", len(result))
	}
}

func TestHandleGatewayLogs_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Reload endpoint tests ---

func TestHandleReload_NotEnabled(t *testing.T) {
	srv := newTestServer(t)
	// reloadHandler is nil by default
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/reload", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if errMsg, ok := result["error"]; !ok || !strings.Contains(errMsg, "--watch") {
		t.Errorf("expected error mentioning --watch flag, got %v", result)
	}
}

func TestHandleReload_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}


// --- CORS middleware tests ---

func TestCORSMiddleware_PreflightRequest(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAllowedOrigins([]string{"http://localhost:3000"})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodOptions, "/api/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if val := rec.Header().Get("Access-Control-Allow-Origin"); val != "http://localhost:3000" {
		t.Errorf("expected CORS origin %q, got %q", "http://localhost:3000", val)
	}
	if val := rec.Header().Get("Access-Control-Allow-Methods"); val == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
	if val := rec.Header().Get("Access-Control-Allow-Headers"); val == "" {
		t.Error("expected Access-Control-Allow-Headers header")
	}
}

func TestCORSMiddleware_WildcardOrigin(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAllowedOrigins([]string{"*"})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://any-origin.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if val := rec.Header().Get("Access-Control-Allow-Origin"); val != "http://any-origin.example.com" {
		t.Errorf("expected CORS origin to echo request origin, got %q", val)
	}
}

func TestCORSMiddleware_DisallowedOrigin(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAllowedOrigins([]string{"http://allowed.example.com"})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://disallowed.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if val := rec.Header().Get("Access-Control-Allow-Origin"); val != "" {
		t.Errorf("expected no CORS header for disallowed origin, got %q", val)
	}
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAllowedOrigins([]string{"*"})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// No Origin header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if val := rec.Header().Get("Access-Control-Allow-Origin"); val != "" {
		t.Errorf("expected no CORS header without Origin, got %q", val)
	}
}

func TestCORSMiddleware_RegularRequestIncludesCORSHeaders(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAllowedOrigins([]string{"http://localhost:5173"})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if val := rec.Header().Get("Access-Control-Allow-Origin"); val != "http://localhost:5173" {
		t.Errorf("expected CORS origin on regular request, got %q", val)
	}
	if val := rec.Header().Get("Vary"); val != "Origin" {
		t.Errorf("expected Vary: Origin, got %q", val)
	}
}

func TestCORSMiddleware_ExtraHeaders(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAllowedOrigins([]string{"*"})
	srv.SetAuth("api_key", "test", "X-Custom-Key")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodOptions, "/api/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("X-Custom-Key", "test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	allowHeaders := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "X-Custom-Key") {
		t.Errorf("expected X-Custom-Key in allowed headers, got %q", allowHeaders)
	}
}

// --- Method not allowed table-driven test ---

func TestMethodNotAllowed_AllEndpoints(t *testing.T) {
	srv := newTestServerWithLogBuffer(t, 10)
	srv.SetDockerClient(&mockDockerClient{})
	srv.SetStackName("test-stack")
	handler := srv.Handler()

	tests := []struct {
		path   string
		method string
	}{
		{"/health", http.MethodPost},
		{"/health", http.MethodPut},
		{"/ready", http.MethodPost},
		{"/ready", http.MethodDelete},
		{"/api/status", http.MethodPost},
		{"/api/status", http.MethodPut},
		{"/api/mcp-servers", http.MethodPost},
		{"/api/mcp-servers", http.MethodDelete},
		{"/api/tools", http.MethodPost},
		{"/api/tools", http.MethodPut},
		{"/api/logs", http.MethodPost},
		{"/api/logs", http.MethodDelete},
		{"/api/reload", http.MethodGet},
		{"/api/reload", http.MethodPut},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s %s, got %d", tt.method, tt.path, rec.Code)
			}
		})
	}
}

// --- writeJSON / writeJSONError tests ---

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, map[string]string{"hello": "world"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	assertContentType(t, rec, "application/json")

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["hello"] != "world" {
		t.Errorf("expected %q, got %q", "world", result["hello"])
	}
}

func TestWriteJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONError(rec, "something went wrong", http.StatusBadRequest)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	assertContentType(t, rec, "application/json")

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["error"] != "something went wrong" {
		t.Errorf("expected error %q, got %q", "something went wrong", result["error"])
	}
}

// --- Helper types and functions ---

// assertContentType checks that the response Content-Type header matches.
func assertContentType(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, expected) {
		t.Errorf("expected Content-Type %q, got %q", expected, ct)
	}
}

// registerMockServerMeta registers metadata for a mock server so it appears in Gateway.Status().
func registerMockServerMeta(g *mcp.Gateway, name string, transport mcp.Transport) {
	g.SetServerMeta(mcp.MCPServerConfig{
		Name:      name,
		Transport: transport,
	})
}

// mockAgentClient is a test double for the mcp.AgentClient interface.
type mockAgentClient struct {
	name        string
	tools       []mcp.Tool
	initialized bool
}

func newMockAgentClient(name string, tools []mcp.Tool) *mockAgentClient {
	return &mockAgentClient{name: name, tools: tools, initialized: true}
}

func (m *mockAgentClient) Name() string                       { return m.name }
func (m *mockAgentClient) Initialize(_ context.Context) error { return nil }
func (m *mockAgentClient) RefreshTools(_ context.Context) error { return nil }
func (m *mockAgentClient) Tools() []mcp.Tool                  { return m.tools }
func (m *mockAgentClient) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
	return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("mock")}}, nil
}
func (m *mockAgentClient) IsInitialized() bool        { return m.initialized }
func (m *mockAgentClient) ServerInfo() mcp.ServerInfo  { return mcp.ServerInfo{Name: m.name} }

// mockDockerClient is a minimal Docker client mock for API tests.
// It only implements the methods used by the API handlers.
type mockDockerClient struct {
	containers     []types.Container
	logOutput      string
	lastLogOptions container.LogsOptions
	restartCalled  bool
	stopCalled     bool
	listError      error
	logsError      error
	restartError   error
	stopError      error
}

func (m *mockDockerClient) ContainerList(_ context.Context, opts container.ListOptions) ([]types.Container, error) {
	if m.listError != nil {
		return nil, m.listError
	}
	return m.containers, nil
}

func (m *mockDockerClient) ContainerLogs(_ context.Context, _ string, opts container.LogsOptions) (io.ReadCloser, error) {
	m.lastLogOptions = opts
	if m.logsError != nil {
		return nil, m.logsError
	}
	return io.NopCloser(strings.NewReader(m.logOutput)), nil
}

func (m *mockDockerClient) ContainerRestart(_ context.Context, _ string, _ container.StopOptions) error {
	m.restartCalled = true
	return m.restartError
}

func (m *mockDockerClient) ContainerStop(_ context.Context, _ string, _ container.StopOptions) error {
	m.stopCalled = true
	return m.stopError
}

// Unused interface methods - required to satisfy dockerclient.DockerClient
func (m *mockDockerClient) ContainerCreate(_ context.Context, _ *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *v1.Platform, _ string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}
func (m *mockDockerClient) ContainerStart(_ context.Context, _ string, _ container.StartOptions) error {
	return nil
}
func (m *mockDockerClient) ContainerRemove(_ context.Context, _ string, _ container.RemoveOptions) error {
	return nil
}
func (m *mockDockerClient) ContainerInspect(_ context.Context, _ string) (types.ContainerJSON, error) {
	return types.ContainerJSON{}, nil
}
func (m *mockDockerClient) ContainerAttach(_ context.Context, _ string, _ container.AttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}
func (m *mockDockerClient) NetworkList(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
	return nil, nil
}
func (m *mockDockerClient) NetworkCreate(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}
func (m *mockDockerClient) NetworkRemove(_ context.Context, _ string) error { return nil }
func (m *mockDockerClient) ImageList(_ context.Context, _ image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}
func (m *mockDockerClient) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockDockerClient) ImageBuild(_ context.Context, _ io.Reader, _ types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	return types.ImageBuildResponse{}, nil
}
func (m *mockDockerClient) Ping(_ context.Context) (types.Ping, error) { return types.Ping{}, nil }
func (m *mockDockerClient) Close() error                               { return nil }

var _ dockerclient.DockerClient = &mockDockerClient{}

// --- Clients endpoint tests ---

func TestHandleClients_NoProvisioners(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	assertContentType(t, rec, "application/json")

	var clients []ClientStatus
	if err := json.NewDecoder(rec.Body).Decode(&clients); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(clients) != 0 {
		t.Errorf("expected empty clients list, got %d", len(clients))
	}
}

func TestHandleClients_WithProvisioners(t *testing.T) {
	srv := newTestServer(t)
	reg := provisioner.NewRegistry()
	srv.SetProvisionerRegistry(reg, "test-gw")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var clients []ClientStatus
	if err := json.NewDecoder(rec.Body).Decode(&clients); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return all registered clients (12 in current registry)
	if len(clients) == 0 {
		t.Fatal("expected non-empty clients list")
	}

	// Verify structure of first client
	first := clients[0]
	if first.Name == "" {
		t.Error("expected non-empty client name")
	}
	if first.Slug == "" {
		t.Error("expected non-empty client slug")
	}
	if first.Transport == "" {
		t.Error("expected non-empty transport")
	}
}

func TestHandleClients_MethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/clients", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

// --- Token Metrics endpoint tests ---

func newTestServerWithMetrics(t *testing.T) *Server {
	t.Helper()
	srv := newTestServer(t)
	acc := metrics.NewAccumulator(100)
	srv.SetMetricsAccumulator(acc)
	return srv
}

func TestHandleStatus_IncludesTokenUsage(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	// Record some metrics
	srv.metricsAccumulator.Record("test-server", 100, 50)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	tokenUsageRaw, ok := result["token_usage"]
	if !ok {
		t.Fatal("expected token_usage in status response")
	}

	var tokenUsage metrics.TokenUsage
	if err := json.Unmarshal(tokenUsageRaw, &tokenUsage); err != nil {
		t.Fatalf("failed to unmarshal token_usage: %v", err)
	}

	if tokenUsage.Session.InputTokens != 100 {
		t.Errorf("expected input_tokens=100, got %d", tokenUsage.Session.InputTokens)
	}
	if tokenUsage.Session.OutputTokens != 50 {
		t.Errorf("expected output_tokens=50, got %d", tokenUsage.Session.OutputTokens)
	}
	if tokenUsage.Session.TotalTokens != 150 {
		t.Errorf("expected total_tokens=150, got %d", tokenUsage.Session.TotalTokens)
	}
}

func TestHandleStatus_NoTokenUsageWithoutAccumulator(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if _, ok := result["token_usage"]; ok {
		t.Error("expected no token_usage when accumulator is nil")
	}
}

func TestHandleGetMetricsTokens(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.Record("server-a", 200, 100)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/tokens?range=1h", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result metrics.TimeSeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result.Range != "1h" {
		t.Errorf("range = %q, want %q", result.Range, "1h")
	}
	if len(result.Points) == 0 {
		t.Error("expected at least 1 data point")
	}
}

func TestHandleGetMetricsTokens_DefaultRange(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/tokens", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result metrics.TimeSeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result.Range != "1h" {
		t.Errorf("default range = %q, want %q", result.Range, "1h")
	}
}

func TestHandleDeleteMetricsTokens(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.metricsAccumulator.Record("server-a", 100, 50)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodDelete, "/api/metrics/tokens", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Verify metrics were cleared
	snap := srv.metricsAccumulator.Snapshot()
	if snap.Session.TotalTokens != 0 {
		t.Errorf("expected 0 tokens after clear, got %d", snap.Session.TotalTokens)
	}
}

func TestHandleMetricsTokens_MethodNotAllowed(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	handler := srv.Handler()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/metrics/tokens", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

func TestHandleGetMetricsTokens_NoAccumulator(t *testing.T) {
	srv := newTestServer(t) // no accumulator
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/tokens?range=1h", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result metrics.TimeSeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(result.Points) != 0 {
		t.Errorf("expected 0 data points, got %d", len(result.Points))
	}
}
