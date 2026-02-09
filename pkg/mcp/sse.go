package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

// SSEServer handles Server-Sent Events connections for MCP.
type SSEServer struct {
	gateway *Gateway

	mu       sync.RWMutex
	sessions map[string]*SSESession
}

// SSESession represents a connected SSE client.
type SSESession struct {
	ID        string
	AgentName string // Agent identity for tool access control
	Writer    http.ResponseWriter
	Flusher   http.Flusher
	Done      chan struct{}
	MessageID atomic.Int64
}

// NewSSEServer creates a new SSE server.
func NewSSEServer(gateway *Gateway) *SSEServer {
	return &SSEServer{
		gateway:  gateway,
		sessions: make(map[string]*SSESession),
	}
}

// ServeHTTP handles SSE connections at /sse.
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Capture agent identity for access control
	agentName := r.URL.Query().Get("agent")
	if agentName == "" {
		agentName = r.Header.Get("X-Agent-Name")
	}

	// Create session
	session := &SSESession{
		ID:        generateSessionID(),
		AgentName: agentName,
		Writer:    w,
		Flusher:   flusher,
		Done:      make(chan struct{}),
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.sessions, session.ID)
		s.mu.Unlock()
		close(session.Done)
	}()

	// Build full URL for message endpoint
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Check for forwarded proto header
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	messageURL := fmt.Sprintf("%s://%s/message?sessionId=%s", scheme, host, session.ID)

	// Send endpoint information
	s.sendEvent(session, "endpoint", messageURL)

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Send keepalive using SSE comment (starts with :)
			// This doesn't trigger message parsing in MCP clients
			fmt.Fprint(session.Writer, ": keepalive\n\n")
			session.Flusher.Flush()
		}
	}
}

// HandleMessage handles POST requests to /message for a specific session.
func (s *SSEServer) HandleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Parse request with size limit
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Handle the request with session context for access control
	resp := s.handleRequest(r.Context(), session, &req)

	// Send response via SSE for SSE-only clients
	s.sendEvent(session, "message", resp)

	// Also return response directly for HTTP clients
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleRequest processes an MCP request.
func (s *SSEServer) handleRequest(ctx context.Context, session *SSESession, req *jsonrpc.Request) jsonrpc.Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return jsonrpc.NewSuccessResponse(req.ID, nil)
	case "tools/list":
		return s.handleToolsList(session, req)
	case "tools/call":
		return s.handleToolsCall(ctx, session, req)
	case "ping":
		return jsonrpc.NewSuccessResponse(req.ID, struct{}{})
	default:
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

func (s *SSEServer) handleInitialize(req *jsonrpc.Request) jsonrpc.Response {
	var params InitializeParams
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params) // params has defaults, unmarshal errors are non-fatal
	}

	result, err := s.gateway.HandleInitialize(params)
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *SSEServer) handleToolsList(session *SSESession, req *jsonrpc.Request) jsonrpc.Response {
	if session.AgentName != "" && !s.gateway.HasAgent(session.AgentName) {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidRequest, "unknown agent: "+session.AgentName)
	}

	var result *ToolsListResult
	var err error
	if session.AgentName != "" {
		result, err = s.gateway.HandleToolsListForAgent(session.AgentName)
	} else {
		result, err = s.gateway.HandleToolsList()
	}
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *SSEServer) handleToolsCall(ctx context.Context, session *SSESession, req *jsonrpc.Request) jsonrpc.Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid tools/call params")
	}

	if session.AgentName != "" && !s.gateway.HasAgent(session.AgentName) {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidRequest, "unknown agent: "+session.AgentName)
	}

	var result *ToolCallResult
	var err error
	if session.AgentName != "" {
		result, err = s.gateway.HandleToolsCallForAgent(ctx, session.AgentName, params)
	} else {
		result, err = s.gateway.HandleToolsCall(ctx, params)
	}
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

// sendEvent sends an SSE event to a session.
func (s *SSEServer) sendEvent(session *SSESession, event string, data any) {
	var dataStr string
	switch v := data.(type) {
	case string:
		dataStr = v
	default:
		b, _ := json.Marshal(v)
		dataStr = string(b)
	}

	// SSE format: id: <id>\nevent: <name>\ndata: <data>\n\n
	msgID := session.MessageID.Add(1)
	fmt.Fprintf(session.Writer, "id: %d\n", msgID)
	fmt.Fprintf(session.Writer, "event: %s\n", event)
	fmt.Fprintf(session.Writer, "data: %s\n", dataStr)
	fmt.Fprint(session.Writer, "\n")
	session.Flusher.Flush()
}

// Broadcast sends an event to all connected sessions.
func (s *SSEServer) Broadcast(event string, data any) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, session := range s.sessions {
		s.sendEvent(session, event, data)
	}
}

// Close terminates all active SSE connections.
func (s *SSEServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Clear session map; active SSE handlers will exit when
	// http.Server.Shutdown() cancels their request contexts.
	s.sessions = make(map[string]*SSESession)
}

// SessionCount returns the number of active sessions.
func (s *SSEServer) SessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}
