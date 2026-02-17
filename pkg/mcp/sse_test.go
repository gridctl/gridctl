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

func TestSSEServer_AgentIdentity_QueryParam(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add mock MCP servers
	client1 := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "read", Description: "Read tool"},
		{Name: "write", Description: "Write tool"},
	})
	client2 := setupMockAgentClient(ctrl, "server2", []Tool{
		{Name: "list", Description: "List tool"},
	})
	g.Router().AddClient(client1)
	g.Router().AddClient(client2)
	g.Router().RefreshTools()

	// Register agent with access to only server1
	g.RegisterAgent("my-agent", []config.ToolSelector{
		{Server: "server1"},
	})

	sse := NewSSEServer(g)

	// Connect via SSE with agent query param
	req := httptest.NewRequest("GET", "/sse?agent=my-agent", nil)
	w := httptest.NewRecorder()

	// Run SSE connection in background (it blocks)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	// Wait for session to be registered
	waitForSession(t, sse)

	// Get the session and verify agent name was captured
	sse.mu.RLock()
	var session *SSESession
	for _, s := range sse.sessions {
		session = s
		break
	}
	sse.mu.RUnlock()

	if session == nil {
		t.Fatal("expected session to be created")
	}
	if session.AgentName != "my-agent" {
		t.Errorf("expected agent name 'my-agent', got '%s'", session.AgentName)
	}

	cancel()
	<-done
}

func TestSSEServer_AgentIdentity_Header(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Connect via SSE with X-Agent-Name header (no query param)
	req := httptest.NewRequest("GET", "/sse", nil)
	req.Header.Set("X-Agent-Name", "header-agent")
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)

	sse.mu.RLock()
	var session *SSESession
	for _, s := range sse.sessions {
		session = s
		break
	}
	sse.mu.RUnlock()

	if session == nil {
		t.Fatal("expected session to be created")
	}
	if session.AgentName != "header-agent" {
		t.Errorf("expected agent name 'header-agent', got '%s'", session.AgentName)
	}

	cancel()
	<-done
}

func TestSSEServer_AgentIdentity_QueryParamPrecedence(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Both query param and header set - query param should win
	req := httptest.NewRequest("GET", "/sse?agent=query-agent", nil)
	req.Header.Set("X-Agent-Name", "header-agent")
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)

	sse.mu.RLock()
	var session *SSESession
	for _, s := range sse.sessions {
		session = s
		break
	}
	sse.mu.RUnlock()

	if session == nil {
		t.Fatal("expected session to be created")
	}
	if session.AgentName != "query-agent" {
		t.Errorf("expected query param to take precedence, got '%s'", session.AgentName)
	}

	cancel()
	<-done
}

func TestSSEServer_NoAgentIdentity(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Connect without any agent identity
	req := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)

	sse.mu.RLock()
	var session *SSESession
	for _, s := range sse.sessions {
		session = s
		break
	}
	sse.mu.RUnlock()

	if session == nil {
		t.Fatal("expected session to be created")
	}
	if session.AgentName != "" {
		t.Errorf("expected empty agent name, got '%s'", session.AgentName)
	}

	cancel()
	<-done
}

func TestSSEServer_ToolsListFiltering(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Set up two servers with different tools
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

	// Register agent with access only to server1
	g.RegisterAgent("restricted-agent", []config.ToolSelector{
		{Server: "server1"},
	})

	sse := NewSSEServer(g)

	tests := []struct {
		name          string
		agentName     string
		wantToolCount int
	}{
		{
			name:          "agent with restricted access sees filtered tools",
			agentName:     "restricted-agent",
			wantToolCount: 2, // only server1 tools
		},
		{
			name:          "no agent sees all tools",
			agentName:     "",
			wantToolCount: 3, // all tools
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := &SSESession{
				ID:        "test-session",
				AgentName: tc.agentName,
			}

			reqID := json.RawMessage(`1`)
			req := &jsonrpc.Request{
				ID:     &reqID,
				Method: "tools/list",
			}

			resp := sse.handleToolsList(session, req)
			if resp.Error != nil {
				t.Fatalf("unexpected error: %s", resp.Error.Message)
			}

			var result ToolsListResult
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			if len(result.Tools) != tc.wantToolCount {
				t.Errorf("expected %d tools, got %d", tc.wantToolCount, len(result.Tools))
				for _, tool := range result.Tools {
					t.Logf("  got tool: %s", tool.Name)
				}
			}
		})
	}
}

func TestSSEServer_ToolsCallFiltering(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "allowed", Description: "Allowed tool"},
		{Name: "denied", Description: "Denied tool"},
	})
	// Override default CallTool with custom behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("called " + name)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Register agent with access only to "allowed" tool
	g.RegisterAgent("filtered-agent", []config.ToolSelector{
		{Server: "server1", Tools: []string{"allowed"}},
	})

	sse := NewSSEServer(g)

	t.Run("allowed tool call succeeds", func(t *testing.T) {
		session := &SSESession{
			ID:        "test-session",
			AgentName: "filtered-agent",
		}

		params, _ := json.Marshal(ToolCallParams{
			Name:      "server1__allowed",
			Arguments: map[string]any{},
		})
		reqID := json.RawMessage(`1`)
		req := &jsonrpc.Request{
			ID:     &reqID,
			Method: "tools/call",
			Params: json.RawMessage(params),
		}

		resp := sse.handleToolsCall(context.Background(), session, req)
		if resp.Error != nil {
			t.Fatalf("unexpected error: %s", resp.Error.Message)
		}

		var result ToolCallResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}

		if result.IsError {
			t.Error("expected allowed tool call to succeed")
		}
	})

	t.Run("denied tool call returns access denied", func(t *testing.T) {
		session := &SSESession{
			ID:        "test-session",
			AgentName: "filtered-agent",
		}

		params, _ := json.Marshal(ToolCallParams{
			Name:      "server1__denied",
			Arguments: map[string]any{},
		})
		reqID := json.RawMessage(`1`)
		req := &jsonrpc.Request{
			ID:     &reqID,
			Method: "tools/call",
			Params: json.RawMessage(params),
		}

		resp := sse.handleToolsCall(context.Background(), session, req)
		if resp.Error != nil {
			t.Fatalf("unexpected error: %s", resp.Error.Message)
		}

		var result ToolCallResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}

		if !result.IsError {
			t.Error("expected denied tool call to fail")
		}
	})

	t.Run("no agent identity allows all tools", func(t *testing.T) {
		session := &SSESession{
			ID:        "test-session",
			AgentName: "", // no agent identity
		}

		params, _ := json.Marshal(ToolCallParams{
			Name:      "server1__denied",
			Arguments: map[string]any{},
		})
		reqID := json.RawMessage(`1`)
		req := &jsonrpc.Request{
			ID:     &reqID,
			Method: "tools/call",
			Params: json.RawMessage(params),
		}

		resp := sse.handleToolsCall(context.Background(), session, req)
		if resp.Error != nil {
			t.Fatalf("unexpected error: %s", resp.Error.Message)
		}

		var result ToolCallResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("failed to unmarshal result: %v", err)
		}

		if result.IsError {
			t.Error("expected unfiltered tool call to succeed")
		}
	})
}

func TestSSEServer_UnknownAgent_ToolsList(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Session with unknown agent name (not registered via RegisterAgent)
	session := &SSESession{
		ID:        "test-session",
		AgentName: "nonexistent-agent",
	}

	reqID := json.RawMessage(`1`)
	req := &jsonrpc.Request{
		ID:     &reqID,
		Method: "tools/list",
	}

	resp := sse.handleToolsList(session, req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent")
	}
	if resp.Error.Code != jsonrpc.InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", jsonrpc.InvalidRequest, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "nonexistent-agent") {
		t.Errorf("expected error to contain agent name, got %s", resp.Error.Message)
	}
}

func TestSSEServer_UnknownAgent_ToolsCall(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Session with unknown agent name
	session := &SSESession{
		ID:        "test-session",
		AgentName: "nonexistent-agent",
	}

	params, _ := json.Marshal(ToolCallParams{
		Name:      "server1__echo",
		Arguments: map[string]any{},
	})
	reqID := json.RawMessage(`1`)
	req := &jsonrpc.Request{
		ID:     &reqID,
		Method: "tools/call",
		Params: json.RawMessage(params),
	}

	resp := sse.handleToolsCall(context.Background(), session, req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent on tools/call")
	}
	if resp.Error.Code != jsonrpc.InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", jsonrpc.InvalidRequest, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "nonexistent-agent") {
		t.Errorf("expected error to contain agent name, got %s", resp.Error.Message)
	}
}

func TestSSEServer_UnknownAgent_ViaHandleMessage(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Manually register a session with an unknown agent name
	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:        "test-session-id",
		AgentName: "nonexistent-agent",
		Writer:    sseW,
		Flusher:   sseW,
		Done:      make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	// Send tools/list via HandleMessage
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	msgReq := httptest.NewRequest("POST", "/message?sessionId=test-session-id", bytes.NewReader(body))
	msgW := httptest.NewRecorder()
	sse.HandleMessage(msgW, msgReq)

	if msgW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", msgW.Code)
	}

	var resp jsonrpc.Response
	if err := json.NewDecoder(msgW.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown agent via HandleMessage")
	}
	if resp.Error.Code != jsonrpc.InvalidRequest {
		t.Errorf("expected InvalidRequest code %d, got %d", jsonrpc.InvalidRequest, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "nonexistent-agent") {
		t.Errorf("expected error to contain agent name, got %s", resp.Error.Message)
	}
}

func TestSSEServer_HandleMessage_WithAgentFiltering(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "read", Description: "Read tool"},
		{Name: "write", Description: "Write tool"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Register agent with access only to "read"
	g.RegisterAgent("read-only-agent", []config.ToolSelector{
		{Server: "server1", Tools: []string{"read"}},
	})

	sse := NewSSEServer(g)

	// Manually register a session to avoid concurrent writes from ServeHTTP goroutine
	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:        "test-session-id",
		AgentName: "read-only-agent",
		Writer:    sseW,
		Flusher:   sseW,
		Done:      make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()

	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	// Send tools/list via HandleMessage
	listReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}
	body, _ := json.Marshal(listReq)

	msgReq := httptest.NewRequest("POST", "/message?sessionId=test-session-id", bytes.NewReader(body))
	msgW := httptest.NewRecorder()
	sse.HandleMessage(msgW, msgReq)

	if msgW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", msgW.Code)
	}

	var resp jsonrpc.Response
	if err := json.NewDecoder(msgW.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Should only see "read" tool from server1
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(result.Tools))
		for _, tool := range result.Tools {
			t.Logf("  got tool: %s", tool.Name)
		}
	}
}

func TestSSEServer_Connection_Headers(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	req := httptest.NewRequest("GET", "/sse", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)
	cancel()
	<-done

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %s", cc)
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %s", conn)
	}
}

func TestSSEServer_Connection_EndpointEvent(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	req := httptest.NewRequest("GET", "/sse", nil)
	req.Host = "localhost:8180"
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: endpoint") {
		t.Error("expected endpoint event in SSE stream")
	}
	if !strings.Contains(body, "/message?sessionId=") {
		t.Error("expected message URL with sessionId in endpoint event")
	}
	if !strings.Contains(body, "http://localhost:8180/message") {
		t.Errorf("expected full message URL, got body: %s", body)
	}
}

func TestSSEServer_Connection_ForwardedProto(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	req := httptest.NewRequest("GET", "/sse", nil)
	req.Host = "example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "https://example.com/message") {
		t.Errorf("expected https scheme from X-Forwarded-Proto, got body: %s", body)
	}
}

func TestSSEServer_SessionCleanupOnDisconnect(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	req := httptest.NewRequest("GET", "/sse", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sse.ServeHTTP(w, req)
	}()

	waitForSession(t, sse)
	if sse.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", sse.SessionCount())
	}

	// Disconnect
	cancel()
	<-done

	// Session should be cleaned up
	if sse.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after disconnect, got %d", sse.SessionCount())
	}
}

func TestSSEServer_HandleMessage_MissingSessionId(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	req := httptest.NewRequest("POST", "/message", strings.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing sessionId, got %d", w.Code)
	}
}

func TestSSEServer_HandleMessage_InvalidSession(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	req := httptest.NewRequest("POST", "/message?sessionId=nonexistent", strings.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for invalid session, got %d", w.Code)
	}
}

func TestSSEServer_HandleMessage_MethodNotAllowed(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	req := httptest.NewRequest("GET", "/message?sessionId=test", nil)
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on /message, got %d", w.Code)
	}
}

func TestSSEServer_HandleMessage_InvalidJSON(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Register a session
	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:      "test-session",
		Writer:  sseW,
		Flusher: sseW,
		Done:    make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	req := httptest.NewRequest("POST", "/message?sessionId=test-session", strings.NewReader("{invalid}"))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestSSEServer_HandleMessage_Initialize(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:      "test-session",
		Writer:  sseW,
		Flusher: sseW,
		Done:    make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
		},
	})

	req := httptest.NewRequest("POST", "/message?sessionId=test-session", bytes.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.ProtocolVersion != MCPProtocolVersion {
		t.Errorf("expected protocol version %s, got %s", MCPProtocolVersion, result.ProtocolVersion)
	}
}

func TestSSEServer_HandleMessage_Ping(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:      "test-session",
		Writer:  sseW,
		Flusher: sseW,
		Done:    make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})

	req := httptest.NewRequest("POST", "/message?sessionId=test-session", bytes.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestSSEServer_HandleMessage_UnknownMethod(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:      "test-session",
		Writer:  sseW,
		Flusher: sseW,
		Done:    make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "nonexistent/method",
	})

	req := httptest.NewRequest("POST", "/message?sessionId=test-session", bytes.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != jsonrpc.MethodNotFound {
		t.Errorf("expected MethodNotFound code %d, got %d", jsonrpc.MethodNotFound, resp.Error.Code)
	}
}

func TestSSEServer_HandleMessage_ToolsCall_InvalidParams(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:      "test-session",
		Writer:  sseW,
		Flusher: sseW,
		Done:    make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"invalid"}`
	req := httptest.NewRequest("POST", "/message?sessionId=test-session", strings.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)

	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid tools/call params")
	}
	if resp.Error.Code != jsonrpc.InvalidParams {
		t.Errorf("expected InvalidParams code %d, got %d", jsonrpc.InvalidParams, resp.Error.Code)
	}
}

func TestSSEServer_HandleMessage_SSEEventSent(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	sseW := httptest.NewRecorder()
	session := &SSESession{
		ID:      "test-session",
		Writer:  sseW,
		Flusher: sseW,
		Done:    make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, session.ID)
		sse.mu.Unlock()
	}()

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})

	req := httptest.NewRequest("POST", "/message?sessionId=test-session", bytes.NewReader(body))
	msgW := httptest.NewRecorder()
	sse.HandleMessage(msgW, req)

	// Verify SSE event was written to the session writer (sseW)
	sseBody := sseW.Body.String()
	if !strings.Contains(sseBody, "event: message") {
		t.Error("expected SSE message event to be written to session writer")
	}
	if !strings.Contains(sseBody, "id: 1") {
		t.Error("expected SSE event ID in session writer output")
	}
}

func TestSSEServer_Broadcast(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Create two sessions
	w1 := httptest.NewRecorder()
	w2 := httptest.NewRecorder()
	s1 := &SSESession{
		ID: "session-1", Writer: w1, Flusher: w1, Done: make(chan struct{}),
	}
	s2 := &SSESession{
		ID: "session-2", Writer: w2, Flusher: w2, Done: make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[s1.ID] = s1
	sse.sessions[s2.ID] = s2
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, s1.ID)
		delete(sse.sessions, s2.ID)
		sse.mu.Unlock()
	}()

	sse.Broadcast("update", map[string]string{"status": "ok"})

	for i, w := range []*httptest.ResponseRecorder{w1, w2} {
		body := w.Body.String()
		if !strings.Contains(body, "event: update") {
			t.Errorf("session %d: expected broadcast event 'update'", i+1)
		}
		if !strings.Contains(body, `"status":"ok"`) {
			t.Errorf("session %d: expected broadcast data", i+1)
		}
	}
}

func TestSSEServer_Broadcast_StringData(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	w1 := httptest.NewRecorder()
	s1 := &SSESession{
		ID: "session-1", Writer: w1, Flusher: w1, Done: make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[s1.ID] = s1
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, s1.ID)
		sse.mu.Unlock()
	}()

	sse.Broadcast("info", "plain text message")

	body := w1.Body.String()
	if !strings.Contains(body, "data: plain text message") {
		t.Errorf("expected string data in SSE event, got: %s", body)
	}
}

func TestSSEServer_Close(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Add sessions
	w1 := httptest.NewRecorder()
	s1 := &SSESession{
		ID: "session-1", Writer: w1, Flusher: w1, Done: make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[s1.ID] = s1
	sse.mu.Unlock()

	if sse.SessionCount() != 1 {
		t.Fatalf("expected 1 session, got %d", sse.SessionCount())
	}

	sse.Close()

	if sse.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after Close, got %d", sse.SessionCount())
	}
}

func TestSSEServer_Close_BroadcastsCloseEvent(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Create two sessions
	w1 := httptest.NewRecorder()
	w2 := httptest.NewRecorder()
	s1 := &SSESession{
		ID: "session-1", Writer: w1, Flusher: w1, Done: make(chan struct{}),
	}
	s2 := &SSESession{
		ID: "session-2", Writer: w2, Flusher: w2, Done: make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[s1.ID] = s1
	sse.sessions[s2.ID] = s2
	sse.mu.Unlock()

	sse.Close()

	// Both sessions should have received the close event before being cleared
	for i, w := range []*httptest.ResponseRecorder{w1, w2} {
		body := w.Body.String()
		if !strings.Contains(body, "event: close") {
			t.Errorf("session %d: expected close event, got: %s", i+1, body)
		}
		if !strings.Contains(body, "data: server shutting down") {
			t.Errorf("session %d: expected shutdown message, got: %s", i+1, body)
		}
	}

	if sse.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after Close, got %d", sse.SessionCount())
	}
}

func TestSSEServer_Close_NoSessions(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Close with no sessions should not panic
	sse.Close()

	if sse.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", sse.SessionCount())
	}
}

func TestSSEServer_Broadcast_DisconnectedClient(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Create one healthy session and one with a panicking writer
	healthy := httptest.NewRecorder()
	s1 := &SSESession{
		ID: "healthy", Writer: healthy, Flusher: healthy, Done: make(chan struct{}),
	}
	s2 := &SSESession{
		ID: "broken", Writer: &panicWriter{}, Flusher: &panicWriter{}, Done: make(chan struct{}),
	}
	sse.mu.Lock()
	sse.sessions[s1.ID] = s1
	sse.sessions[s2.ID] = s2
	sse.mu.Unlock()
	defer func() {
		sse.mu.Lock()
		delete(sse.sessions, s1.ID)
		delete(sse.sessions, s2.ID)
		sse.mu.Unlock()
	}()

	// Broadcast should not panic even with a broken writer
	sse.Broadcast("test", "hello")

	body := healthy.Body.String()
	if !strings.Contains(body, "event: test") {
		t.Errorf("healthy session should receive broadcast, got: %s", body)
	}
	if !strings.Contains(body, "data: hello") {
		t.Errorf("healthy session should receive data, got: %s", body)
	}
}

func TestSSEServer_SessionCount(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	if sse.SessionCount() != 0 {
		t.Errorf("expected 0 sessions initially, got %d", sse.SessionCount())
	}

	// Add sessions manually
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		s := &SSESession{
			ID: "session-" + strings.Repeat("x", i+1), Writer: w, Flusher: w, Done: make(chan struct{}),
		}
		sse.mu.Lock()
		sse.sessions[s.ID] = s
		sse.mu.Unlock()
	}

	if sse.SessionCount() != 3 {
		t.Errorf("expected 3 sessions, got %d", sse.SessionCount())
	}
}

func TestSSEServer_MultipleSessions(t *testing.T) {
	g := NewGateway()
	sse := NewSSEServer(g)

	// Connect two SSE clients
	var cancels []context.CancelFunc
	var dones []chan struct{}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/sse", nil)
		ctx, cancel := context.WithCancel(req.Context())
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			defer close(done)
			sse.ServeHTTP(w, req)
		}()

		cancels = append(cancels, cancel)
		dones = append(dones, done)
	}

	// Wait for all sessions
	for i := 0; i < 50; i++ {
		if sse.SessionCount() == 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if sse.SessionCount() != 3 {
		t.Errorf("expected 3 sessions, got %d", sse.SessionCount())
	}

	// Disconnect all
	for _, cancel := range cancels {
		cancel()
	}
	for _, done := range dones {
		<-done
	}

	if sse.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after all disconnects, got %d", sse.SessionCount())
	}
}

// panicWriter simulates a disconnected client that panics on write.
type panicWriter struct{}

func (pw *panicWriter) Header() http.Header        { return http.Header{} }
func (pw *panicWriter) Write([]byte) (int, error)   { panic("write to closed connection") }
func (pw *panicWriter) WriteHeader(int)             {}
func (pw *panicWriter) Flush()                      { panic("flush on closed connection") }

// waitForSession polls until at least one SSE session is registered.
func waitForSession(t *testing.T, sse *SSEServer) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if sse.SessionCount() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for SSE session")
}

// sseTestPromptProvider wraps a MockAgentClient to implement PromptProvider for SSE tests.
type sseTestPromptProvider struct {
	AgentClient
	prompts []PromptData
}

func (p *sseTestPromptProvider) ListPromptData() []PromptData {
	return p.prompts
}

func (p *sseTestPromptProvider) GetPromptData(name string) (*PromptData, error) {
	for _, pd := range p.prompts {
		if pd.Name == name {
			return &pd, nil
		}
	}
	return nil, fmt.Errorf("prompt %q: not found", name)
}

// setupSSEWithRegistry creates an SSE server with a registry PromptProvider.
func setupSSEWithRegistry(t *testing.T) *SSEServer {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	pp := &sseTestPromptProvider{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:        "code-review",
				Description: "Review code for issues",
				Content:     "Review this {{language}} code: {{code}}",
				Arguments: []PromptArgumentData{
					{Name: "language", Description: "Language", Required: true},
					{Name: "code", Description: "Code", Required: true},
				},
			},
		},
	}
	g.Router().AddClient(pp)

	return NewSSEServer(g)
}

// ssePost sends a JSON-RPC request via HandleMessage to the given SSE server/session.
func ssePost(t *testing.T, sse *SSEServer, sessionID string, method string, params any) jsonrpc.Response {
	t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		m["params"] = params
	}
	body, _ := json.Marshal(m)
	req := httptest.NewRequest("POST", "/message?sessionId="+sessionID, bytes.NewReader(body))
	w := httptest.NewRecorder()
	sse.HandleMessage(w, req)
	var resp jsonrpc.Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestSSEServer_HandleMessage_PromptsList(t *testing.T) {
	sse := setupSSEWithRegistry(t)

	sseW := httptest.NewRecorder()
	session := &SSESession{ID: "test-session", Writer: sseW, Flusher: sseW, Done: make(chan struct{})}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()

	resp := ssePost(t, sse, "test-session", "prompts/list", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result PromptsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(result.Prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(result.Prompts))
	}
}

func TestSSEServer_HandleMessage_PromptsGet(t *testing.T) {
	sse := setupSSEWithRegistry(t)

	sseW := httptest.NewRecorder()
	session := &SSESession{ID: "test-session", Writer: sseW, Flusher: sseW, Done: make(chan struct{})}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()

	resp := ssePost(t, sse, "test-session", "prompts/get", map[string]any{
		"name":      "code-review",
		"arguments": map[string]any{"language": "Go", "code": "func main() {}"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result PromptsGetResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	expected := "Review this Go code: func main() {}"
	if result.Messages[0].Content.Text != expected {
		t.Errorf("expected %q, got %q", expected, result.Messages[0].Content.Text)
	}
}

func TestSSEServer_HandleMessage_ResourcesList(t *testing.T) {
	sse := setupSSEWithRegistry(t)

	sseW := httptest.NewRecorder()
	session := &SSESession{ID: "test-session", Writer: sseW, Flusher: sseW, Done: make(chan struct{})}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()

	resp := ssePost(t, sse, "test-session", "resources/list", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result ResourcesListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(result.Resources))
	}
	if result.Resources[0].URI != "skills://registry/code-review" {
		t.Errorf("expected URI 'skills://registry/code-review', got %q", result.Resources[0].URI)
	}
}

func TestSSEServer_HandleMessage_ResourcesRead(t *testing.T) {
	sse := setupSSEWithRegistry(t)

	sseW := httptest.NewRecorder()
	session := &SSESession{ID: "test-session", Writer: sseW, Flusher: sseW, Done: make(chan struct{})}
	sse.mu.Lock()
	sse.sessions[session.ID] = session
	sse.mu.Unlock()

	resp := ssePost(t, sse, "test-session", "resources/read", map[string]any{
		"uri": "skills://registry/code-review",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result ResourcesReadResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Contents))
	}
	if result.Contents[0].Text == "" {
		t.Error("expected non-empty content text")
	}
}
