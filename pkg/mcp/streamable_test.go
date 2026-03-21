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
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"go.uber.org/mock/gomock"
)

// initializeStreamable sends an initialize request and returns the session ID.
func initializeStreamable(t *testing.T, srv *StreamableHTTPServer, agentName string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	if agentName != "" {
		req.Header.Set("X-Agent-Name", agentName)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	sessionID := w.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize: expected Mcp-Session-Id in response header")
	}
	return sessionID
}

// streamablePost sends a JSON-RPC request with a session ID and returns the response.
func streamablePost(t *testing.T, srv *StreamableHTTPServer, sessionID string, method string, params any) jsonrpc.Response {
	t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		m["params"] = params
	}
	body, _ := json.Marshal(m)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("%s: expected 200, got %d: %s", method, w.Code, w.Body.String())
	}
	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestStreamableHTTPServer_Initialize(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	if len(sessionID) == 0 {
		t.Error("expected non-empty session ID")
	}
	if srv.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", srv.SessionCount())
	}
}

func TestStreamableHTTPServer_Initialize_SessionIDInHeader(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if w.Header().Get("Mcp-Session-Id") == "" {
		t.Error("expected Mcp-Session-Id response header")
	}
}

func TestStreamableHTTPServer_Initialize_ParsesProtocolVersion(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	// Verify the session is tracked
	ids := srv.SessionIDs()
	found := false
	for _, id := range ids {
		if id == sessionID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("session %s not in SessionIDs()", sessionID)
	}
}

func TestStreamableHTTPServer_Post_NoSessionID(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing session ID, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_Post_UnknownSessionID(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Mcp-Session-Id", "nonexistent-session")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown session, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_Post_InvalidJSON(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{invalid}"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with JSON-RPC error, got %d", w.Code)
	}
	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Error("expected JSON-RPC error for invalid JSON")
	}
	if resp.Error.Code != jsonrpc.ParseError {
		t.Errorf("expected ParseError code %d, got %d", jsonrpc.ParseError, resp.Error.Code)
	}
}

func TestStreamableHTTPServer_Ping(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	resp := streamablePost(t, srv, sessionID, "ping", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestStreamableHTTPServer_ToolsList(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "read", Description: "Read tool"},
		{Name: "write", Description: "Write tool"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	srv := NewStreamableHTTPServer(g, nil)
	sessionID := initializeStreamable(t, srv, "")

	resp := streamablePost(t, srv, sessionID, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestStreamableHTTPServer_ToolsCall(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("hello")}}, nil,
	)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	srv := NewStreamableHTTPServer(g, nil)
	sessionID := initializeStreamable(t, srv, "")

	params, _ := json.Marshal(ToolCallParams{Name: "server1__echo", Arguments: map[string]any{}})
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(params)}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(bodyBytes))
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Error("expected successful tool call")
	}
}

func TestStreamableHTTPServer_Delete(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 on DELETE, got %d", w.Code)
	}
	if srv.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after DELETE, got %d", srv.SessionCount())
	}
}

func TestStreamableHTTPServer_Delete_NoSessionID(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing session ID, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_Delete_UnknownSession(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "nonexistent")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown session, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_PostAfterDelete(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	// Delete the session
	del := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	del.Header.Set("Mcp-Session-Id", sessionID)
	delW := httptest.NewRecorder()
	srv.ServeHTTP(delW, del)

	// Subsequent POST should return 404
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after DELETE, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_Get_SSEHeaders(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()

	// Give the SSE stream a moment to start
	time.Sleep(5 * time.Millisecond)
	cancel()
	<-done

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %s", cc)
	}
}

func TestStreamableHTTPServer_Get_NoSessionID(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing session ID, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_Get_UnknownSession(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "nonexistent")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown session, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_Get_LastEventID_Replay(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	// Manually look up the session and push some events
	srv.mu.RLock()
	session := srv.sessions[sessionID]
	srv.mu.RUnlock()

	session.pushEvent("message", []byte(`{"test":1}`))
	session.pushEvent("message", []byte(`{"test":2}`))
	session.pushEvent("message", []byte(`{"test":3}`))

	// Connect with Last-Event-ID: 1 to replay events 2 and 3
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Last-Event-ID", "1")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(w, req)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	// Should contain events with ID 2 and 3 (replayed), but not ID 1
	if !strings.Contains(body, "id: 2") {
		t.Errorf("expected event id 2 in replay, got: %s", body)
	}
	if !strings.Contains(body, "id: 3") {
		t.Errorf("expected event id 3 in replay, got: %s", body)
	}
	if strings.Contains(body, "id: 1\n") {
		t.Errorf("event id 1 should not be replayed (afterID=1), got: %s", body)
	}
}

func TestStreamableHTTPServer_OriginValidation_Rejected(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), []string{"https://allowed.example.com"})

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for disallowed origin, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_OriginValidation_AllowedLocalhost(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})

	for _, origin := range []string{"http://localhost:8180", "http://127.0.0.1:8180"} {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected 200 for localhost origin %s, got %d", origin, w.Code)
			}
		})
	}
}

func TestStreamableHTTPServer_OriginValidation_NoOriginAllowed(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	// No Origin header — should always be allowed (non-browser client)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for request without Origin, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_OriginValidation_WildcardAllowsAll(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), []string{"*"})

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Origin", "https://any.example.com")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for wildcard allowed origins, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_MethodNotAllowed(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	req := httptest.NewRequest(http.MethodPut, "/mcp", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for PUT, got %d", w.Code)
	}
}

func TestStreamableHTTPServer_AgentIdentity_QueryParam(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "read", Description: "Read"}})
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.RegisterAgent("my-agent", []config.ToolSelector{{Server: "server1"}})

	srv := NewStreamableHTTPServer(g, nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp?agent=my-agent", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	sessionID := w.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("expected session ID")
	}

	srv.mu.RLock()
	session := srv.sessions[sessionID]
	srv.mu.RUnlock()

	if session.AgentName != "my-agent" {
		t.Errorf("expected agent name 'my-agent', got %q", session.AgentName)
	}
}

func TestStreamableHTTPServer_AgentIdentity_Header(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "read", Description: "Read"}})
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.RegisterAgent("header-agent", []config.ToolSelector{{Server: "server1"}})

	srv := NewStreamableHTTPServer(g, nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("X-Agent-Name", "header-agent")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	sessionID := w.Header().Get("Mcp-Session-Id")
	srv.mu.RLock()
	session := srv.sessions[sessionID]
	srv.mu.RUnlock()

	if session.AgentName != "header-agent" {
		t.Errorf("expected agent name 'header-agent', got %q", session.AgentName)
	}
}

func TestStreamableHTTPServer_AgentIdentity_QueryParamPrecedence(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05", "clientInfo": map[string]any{"name": "c", "version": "1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp?agent=query-agent", bytes.NewReader(body))
	req.Header.Set("X-Agent-Name", "header-agent")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	sessionID := w.Header().Get("Mcp-Session-Id")
	srv.mu.RLock()
	session := srv.sessions[sessionID]
	srv.mu.RUnlock()

	if session.AgentName != "query-agent" {
		t.Errorf("expected query param to take precedence, got %q", session.AgentName)
	}
}

func TestStreamableHTTPServer_ToolsListFiltering(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client1 := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "read", Description: "Read"},
		{Name: "write", Description: "Write"},
	})
	client2 := setupMockAgentClient(ctrl, "server2", []Tool{
		{Name: "list", Description: "List"},
	})
	g.Router().AddClient(client1)
	g.Router().AddClient(client2)
	g.Router().RefreshTools()
	g.RegisterAgent("restricted-agent", []config.ToolSelector{{Server: "server1"}})

	srv := NewStreamableHTTPServer(g, nil)

	tests := []struct {
		name          string
		agentName     string
		wantToolCount int
	}{
		{"agent with restricted access", "restricted-agent", 2},
		{"no agent sees all tools", "", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessionID := initializeStreamable(t, srv, tc.agentName)
			resp := streamablePost(t, srv, sessionID, "tools/list", nil)
			if resp.Error != nil {
				t.Fatalf("unexpected error: %s", resp.Error.Message)
			}
			var result ToolsListResult
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Tools) != tc.wantToolCount {
				t.Errorf("expected %d tools, got %d", tc.wantToolCount, len(result.Tools))
			}
		})
	}
}

func TestStreamableHTTPServer_UnknownAgent_ToolsList(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "nonexistent-agent")

	resp := streamablePost(t, srv, sessionID, "tools/list", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent")
	}
	if resp.Error.Code != jsonrpc.InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", jsonrpc.InvalidRequest, resp.Error.Code)
	}
}

func TestStreamableHTTPServer_UnknownAgent_ToolsCall(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "nonexistent-agent")

	params, _ := json.Marshal(ToolCallParams{Name: "server1__echo", Arguments: map[string]any{}})
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(params)}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(bodyBytes))
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent on tools/call")
	}
	if resp.Error.Code != jsonrpc.InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", jsonrpc.InvalidRequest, resp.Error.Code)
	}
}

func TestStreamableHTTPServer_SessionCount(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	if srv.SessionCount() != 0 {
		t.Errorf("expected 0 sessions initially, got %d", srv.SessionCount())
	}

	id1 := initializeStreamable(t, srv, "")
	if srv.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", srv.SessionCount())
	}

	_ = initializeStreamable(t, srv, "")
	if srv.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", srv.SessionCount())
	}

	// Delete first session
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", id1)
	srv.ServeHTTP(httptest.NewRecorder(), req)

	if srv.SessionCount() != 1 {
		t.Errorf("expected 1 session after delete, got %d", srv.SessionCount())
	}
}

func TestStreamableHTTPServer_Close(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)

	initializeStreamable(t, srv, "")
	initializeStreamable(t, srv, "")

	if srv.SessionCount() != 2 {
		t.Fatalf("expected 2 sessions before close, got %d", srv.SessionCount())
	}

	srv.Close()

	if srv.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after Close, got %d", srv.SessionCount())
	}
}

func TestStreamableHTTPServer_Close_CancelsSSEStream(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		srv.ServeHTTP(w, req)
	}()

	// Give the SSE stream time to start
	time.Sleep(10 * time.Millisecond)

	// Close the server — should cancel the stream
	srv.Close()

	select {
	case <-streamDone:
		// Stream was cancelled — correct
	case <-time.After(200 * time.Millisecond):
		t.Error("expected SSE stream to be cancelled after Close()")
		cancel()
	}
}

func TestStreamableHTTPServer_NotificationsInitialized(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	resp := streamablePost(t, srv, sessionID, "notifications/initialized", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestStreamableHTTPServer_UnknownMethod(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	resp := streamablePost(t, srv, sessionID, "nonexistent/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != jsonrpc.MethodNotFound {
		t.Errorf("expected MethodNotFound code %d, got %d", jsonrpc.MethodNotFound, resp.Error.Code)
	}
}

func TestStreamableHTTPServer_Get_CancelCancelsOldStream(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	ctx1, cancel1 := context.WithCancel(context.Background())
	req1 := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx1)
	req1.Header.Set("Mcp-Session-Id", sessionID)
	w1 := httptest.NewRecorder()

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		srv.ServeHTTP(w1, req1)
	}()

	// Wait for first stream to register
	time.Sleep(10 * time.Millisecond)

	// Open a second GET stream for the same session — should cancel the first
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2 := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx2)
	req2.Header.Set("Mcp-Session-Id", sessionID)
	w2 := httptest.NewRecorder()

	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		srv.ServeHTTP(w2, req2)
	}()

	// First stream should have been cancelled
	select {
	case <-done1:
		// First stream was cancelled — correct
	case <-time.After(200 * time.Millisecond):
		t.Error("expected first SSE stream to be cancelled when second opens")
		cancel1()
	}

	cancel2()
	<-done2
}

func TestStreamableHTTPServer_GatewaySessionCount(t *testing.T) {
	g := NewGateway()
	srv := NewStreamableHTTPServer(g, nil)

	if g.SessionCount() != 0 {
		t.Errorf("expected 0 gateway sessions initially, got %d", g.SessionCount())
	}

	id1 := initializeStreamable(t, srv, "")
	if g.SessionCount() != 1 {
		t.Errorf("expected 1 gateway session after initialize, got %d", g.SessionCount())
	}

	// Delete via DELETE /mcp
	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", id1)
	srv.ServeHTTP(httptest.NewRecorder(), req)

	if g.SessionCount() != 0 {
		t.Errorf("expected 0 gateway sessions after DELETE, got %d", g.SessionCount())
	}
}

func TestStreamableHTTPServer_ToolsCall_InvalidParams(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv, "")

	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"invalid"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid tools/call params")
	}
	if resp.Error.Code != jsonrpc.InvalidParams {
		t.Errorf("expected InvalidParams code %d, got %d", jsonrpc.InvalidParams, resp.Error.Code)
	}
}

// streamableTestPromptProvider wraps a MockAgentClient for prompt tests.
type streamableTestPromptProvider struct {
	AgentClient
	prompts []PromptData
}

func (p *streamableTestPromptProvider) ListPromptData() []PromptData { return p.prompts }
func (p *streamableTestPromptProvider) GetPromptData(name string) (*PromptData, error) {
	for _, pd := range p.prompts {
		if pd.Name == name {
			return &pd, nil
		}
	}
	return nil, fmt.Errorf("prompt %q: not found", name)
}

func setupStreamableWithRegistry(t *testing.T) (*StreamableHTTPServer, string) {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()
	mock := setupMockAgentClient(ctrl, "registry", nil)
	pp := &streamableTestPromptProvider{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:        "code-review",
				Description: "Review code",
				Content:     "Review this {{language}} code: {{code}}",
				Arguments: []PromptArgumentData{
					{Name: "language", Required: true},
					{Name: "code", Required: true},
				},
			},
		},
	}
	g.Router().AddClient(pp)

	srv := NewStreamableHTTPServer(g, nil)
	sessionID := initializeStreamable(t, srv, "")
	return srv, sessionID
}

func TestStreamableHTTPServer_PromptsList(t *testing.T) {
	srv, sessionID := setupStreamableWithRegistry(t)

	resp := streamablePost(t, srv, sessionID, "prompts/list", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var result PromptsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(result.Prompts))
	}
}

func TestStreamableHTTPServer_PromptsGet(t *testing.T) {
	srv, sessionID := setupStreamableWithRegistry(t)

	resp := streamablePost(t, srv, sessionID, "prompts/get", map[string]any{
		"name":      "code-review",
		"arguments": map[string]any{"language": "Go", "code": "func main() {}"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var result PromptsGetResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	expected := "Review this Go code: func main() {}"
	if result.Messages[0].Content.Text != expected {
		t.Errorf("expected %q, got %q", expected, result.Messages[0].Content.Text)
	}
}

func TestStreamableHTTPServer_PromptsGet_NilParams(t *testing.T) {
	srv, sessionID := setupStreamableWithRegistry(t)

	resp := streamablePost(t, srv, sessionID, "prompts/get", nil)
	if resp.Error == nil {
		t.Fatal("expected error for nil params on prompts/get")
	}
}

func TestStreamableHTTPServer_ResourcesList(t *testing.T) {
	srv, sessionID := setupStreamableWithRegistry(t)

	resp := streamablePost(t, srv, sessionID, "resources/list", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var result ResourcesListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(result.Resources))
	}
}

func TestStreamableHTTPServer_ResourcesRead(t *testing.T) {
	srv, sessionID := setupStreamableWithRegistry(t)

	resp := streamablePost(t, srv, sessionID, "resources/read", map[string]any{
		"uri": "skills://registry/code-review",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var result ResourcesReadResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
}

func TestStreamableHTTPServer_ResourcesRead_NilParams(t *testing.T) {
	srv, sessionID := setupStreamableWithRegistry(t)

	resp := streamablePost(t, srv, sessionID, "resources/read", nil)
	if resp.Error == nil {
		t.Fatal("expected error for nil params on resources/read")
	}
}
