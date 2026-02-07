package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestSSEServer_AgentIdentity_QueryParam(t *testing.T) {
	g := NewGateway()

	// Add mock MCP servers
	client1 := NewMockAgentClient("server1", []Tool{
		{Name: "read", Description: "Read tool"},
		{Name: "write", Description: "Write tool"},
	})
	client2 := NewMockAgentClient("server2", []Tool{
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
	g := NewGateway()

	// Set up two servers with different tools
	client1 := NewMockAgentClient("server1", []Tool{
		{Name: "read", Description: "Read"},
		{Name: "write", Description: "Write"},
	})
	client2 := NewMockAgentClient("server2", []Tool{
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
			req := &Request{
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
	g := NewGateway()

	client := NewMockAgentClient("server1", []Tool{
		{Name: "allowed", Description: "Allowed tool"},
		{Name: "denied", Description: "Denied tool"},
	})
	client.SetCallToolFn(func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
		return &ToolCallResult{
			Content: []Content{NewTextContent("called " + name)},
		}, nil
	})
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
		req := &Request{
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
		req := &Request{
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
		req := &Request{
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

func TestSSEServer_HandleMessage_WithAgentFiltering(t *testing.T) {
	g := NewGateway()

	client := NewMockAgentClient("server1", []Tool{
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

	var resp Response
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
