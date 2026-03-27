package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

const maxEventHistory = 100

// streamableEvent is a single SSE event stored for Last-Event-ID replay.
type streamableEvent struct {
	ID   int64
	Type string
	Data []byte
}

// StreamableSession represents an active Streamable HTTP session.
type StreamableSession struct {
	ID string

	histMu sync.Mutex
	history []*streamableEvent
	nextID  atomic.Int64

	events    chan streamableEvent
	streamMu  sync.Mutex
	sseCancel context.CancelFunc // cancels the active GET SSE stream; nil if none
}

func newStreamableSession(id string) *StreamableSession {
	return &StreamableSession{
		ID:     id,
		events: make(chan streamableEvent, maxEventHistory),
	}
}

// pushEvent adds an event to the session history and enqueues it for the active SSE stream.
func (s *StreamableSession) pushEvent(eventType string, data []byte) int64 {
	id := s.nextID.Add(1)
	evt := &streamableEvent{ID: id, Type: eventType, Data: data}

	s.histMu.Lock()
	s.history = append(s.history, evt)
	if len(s.history) > maxEventHistory {
		s.history = s.history[len(s.history)-maxEventHistory:]
	}
	s.histMu.Unlock()

	select {
	case s.events <- *evt:
	default: // drop if buffer full or no active stream
	}
	return id
}

// eventsAfter returns all history events with ID > afterID.
func (s *StreamableSession) eventsAfter(afterID int64) []streamableEvent {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	var result []streamableEvent
	for _, e := range s.history {
		if e.ID > afterID {
			result = append(result, *e)
		}
	}
	return result
}

// setStream cancels any existing SSE stream and registers a new cancel function.
func (s *StreamableSession) setStream(cancel context.CancelFunc) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.sseCancel != nil {
		s.sseCancel()
	}
	s.sseCancel = cancel
}

// clearStream removes the active SSE stream cancel function.
func (s *StreamableSession) clearStream() {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	s.sseCancel = nil
}

// StreamableHTTPServer implements the MCP Streamable HTTP transport (spec 2025-06-18).
// It handles POST, GET, and DELETE requests at a single /mcp endpoint.
type StreamableHTTPServer struct {
	gateway        *Gateway
	allowedOrigins []string

	mu       sync.RWMutex
	sessions map[string]*StreamableSession
}

// NewStreamableHTTPServer creates a new Streamable HTTP server.
func NewStreamableHTTPServer(gateway *Gateway, allowedOrigins []string) *StreamableHTTPServer {
	return &StreamableHTTPServer{
		gateway:        gateway,
		allowedOrigins: allowedOrigins,
		sessions:       make(map[string]*StreamableSession),
	}
}

// SetAllowedOrigins updates the list of allowed origins for DNS rebinding protection.
func (s *StreamableHTTPServer) SetAllowedOrigins(origins []string) {
	s.allowedOrigins = origins
}

// ServeHTTP routes /mcp requests based on HTTP method.
func (s *StreamableHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.validateOrigin(r); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodGet:
		s.handleGet(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// validateOrigin rejects requests from disallowed origins to prevent DNS rebinding attacks.
// Requests without an Origin header (non-browser clients) are always allowed.
func (s *StreamableHTTPServer) validateOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid Origin header")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	for _, allowed := range s.allowedOrigins {
		if allowed == "*" || allowed == origin {
			return nil
		}
	}
	return fmt.Errorf("origin not allowed: %s", origin)
}

// handlePost handles POST /mcp — client→server messages.
// The first request must be initialize (no Mcp-Session-Id header).
// Subsequent requests must include a valid Mcp-Session-Id header.
func (s *StreamableHTTPServer) handlePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(nil, jsonrpc.ParseError, "Invalid JSON"))
		return
	}
	if req.JSONRPC != "2.0" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidRequest, "Invalid JSON-RPC version"))
		return
	}

	if req.Method == "initialize" {
		s.handleInitialize(w, r, &req)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header required", http.StatusNotFound)
		return
	}

	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	s.gateway.sessions.Touch(sessionID)
	resp := s.handleRequest(r.Context(), session, &req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleInitialize processes an initialize request and creates a new session.
// The assigned Mcp-Session-Id is returned in the response header.
func (s *StreamableHTTPServer) handleInitialize(w http.ResponseWriter, r *http.Request, req *jsonrpc.Request) {
	var params InitializeParams
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	result, gSession, err := s.gateway.HandleInitialize(params)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
		return
	}

	// Create transport-level session using the gateway-assigned session ID
	session := newStreamableSession(gSession.ID)
	s.mu.Lock()
	s.sessions[gSession.ID] = session
	s.mu.Unlock()

	w.Header().Set("Mcp-Session-Id", gSession.ID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, result))
}

// handleGet handles GET /mcp — opens a server→client SSE stream.
// Clients can provide Last-Event-ID to resume after a disconnection.
func (s *StreamableHTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header required", http.StatusNotFound)
		return
	}

	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Register this SSE stream; cancel any previous stream for this session
	ctx, cancel := context.WithCancel(r.Context())
	session.setStream(cancel)
	defer session.clearStream()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Replay missed events if Last-Event-ID is provided; track lastSentID to
	// deduplicate events that are also queued in the channel buffer.
	var lastSentID int64
	if lastIDStr := r.Header.Get("Last-Event-ID"); lastIDStr != "" {
		if lastID, err := strconv.ParseInt(lastIDStr, 10, 64); err == nil {
			for _, evt := range session.eventsAfter(lastID) {
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, evt.Data)
				lastSentID = evt.ID
			}
			flusher.Flush()
		}
	}

	s.gateway.sessions.Touch(sessionID)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-session.events:
			if evt.ID <= lastSentID {
				continue // already sent via Last-Event-ID replay
			}
			lastSentID = evt.ID
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, evt.Data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleDelete handles DELETE /mcp — terminates a session.
func (s *StreamableHTTPServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header required", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	_, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	s.deleteSession(sessionID)
	w.WriteHeader(http.StatusOK)
}

// deleteSession tears down a session, cancels any active SSE stream,
// and removes it from both the transport and gateway session managers.
func (s *StreamableHTTPServer) deleteSession(sessionID string) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()

	if ok {
		session.streamMu.Lock()
		if session.sseCancel != nil {
			session.sseCancel()
			session.sseCancel = nil
		}
		session.streamMu.Unlock()
	}
	s.gateway.sessions.Delete(sessionID)
}

// handleRequest dispatches a JSON-RPC request to the appropriate gateway handler.
func (s *StreamableHTTPServer) handleRequest(ctx context.Context, session *StreamableSession, req *jsonrpc.Request) jsonrpc.Response {
	switch req.Method {
	case "notifications/initialized":
		return jsonrpc.NewSuccessResponse(req.ID, nil)
	case "tools/list":
		return s.handleToolsList(session, req)
	case "tools/call":
		return s.handleToolsCall(ctx, session, req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	case "ping":
		return jsonrpc.NewSuccessResponse(req.ID, struct{}{})
	default:
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

func (s *StreamableHTTPServer) handleToolsList(_ *StreamableSession, req *jsonrpc.Request) jsonrpc.Response {
	result, err := s.gateway.HandleToolsList()
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *StreamableHTTPServer) handleToolsCall(ctx context.Context, _ *StreamableSession, req *jsonrpc.Request) jsonrpc.Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid tools/call params")
	}
	result, err := s.gateway.HandleToolsCall(ctx, params)
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *StreamableHTTPServer) handlePromptsList(req *jsonrpc.Request) jsonrpc.Response {
	result, err := s.gateway.HandlePromptsList()
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *StreamableHTTPServer) handlePromptsGet(req *jsonrpc.Request) jsonrpc.Response {
	if req.Params == nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "params required for prompts/get")
	}
	var params PromptsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid prompts/get params")
	}
	result, err := s.gateway.HandlePromptsGet(params)
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *StreamableHTTPServer) handleResourcesList(req *jsonrpc.Request) jsonrpc.Response {
	result, err := s.gateway.HandleResourcesList()
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

func (s *StreamableHTTPServer) handleResourcesRead(req *jsonrpc.Request) jsonrpc.Response {
	if req.Params == nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "params required for resources/read")
	}
	var params ResourcesReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid resources/read params")
	}
	result, err := s.gateway.HandleResourcesRead(params)
	if err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error())
	}
	return jsonrpc.NewSuccessResponse(req.ID, result)
}

// SessionCount returns the number of active Streamable HTTP sessions.
func (s *StreamableHTTPServer) SessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// SessionIDs returns the IDs of all active sessions.
func (s *StreamableHTTPServer) SessionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Close tears down all active sessions and cancels their SSE streams.
func (s *StreamableHTTPServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		session.streamMu.Lock()
		if session.sseCancel != nil {
			session.sseCancel()
			session.sseCancel = nil
		}
		session.streamMu.Unlock()
		s.gateway.sessions.Delete(id)
	}
	s.sessions = make(map[string]*StreamableSession)
}
