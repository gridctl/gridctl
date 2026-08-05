package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

	histMu  sync.Mutex
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

// StreamableHTTPServer implements the MCP Streamable HTTP transport for
// both protocol generations: the handshake era (2025-11-25 and earlier;
// sessions, GET SSE streams, DELETE teardown) and the stateless era
// (2026-07-28; per-request _meta, no sessions, POST only). It serves a
// single /mcp endpoint and classifies each request's generation at the
// edge (see handlePost).
type StreamableHTTPServer struct {
	gateway        *Gateway
	allowedOrigins []string
	allowedHosts   []string

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

// SetAllowedHosts updates the Host allowlist consulted by validateHost.
// Empty means loopback-only, the secure default.
func (s *StreamableHTTPServer) SetAllowedHosts(hosts []string) {
	s.allowedHosts = hosts
}

// ServeHTTP routes /mcp requests based on HTTP method.
func (s *StreamableHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Host first: it is the primary DNS rebinding control, because a browser
	// must send the attacker's domain in Host and page scripts cannot override
	// it (Host is a forbidden header name). Origin below is defense in depth.
	if err := s.validateHost(r); err != nil {
		http.Error(w, "Forbidden: "+err.Error(), http.StatusForbidden)
		return
	}
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

// validateHost rejects requests carrying an attacker-controlled Host header,
// which is what actually stops DNS rebinding: after a rebind the browser
// believes it is same-origin with the attacker's domain, so it omits Origin on
// GET and HEAD entirely, leaving Host as the only value that still names the
// attacker.
//
// The check applies only when the request arrived on a loopback address, read
// from the listener via http.LocalAddrContextKey rather than from the
// configured bind address. That distinction matters: it keeps loopback clients
// guarded even when the daemon listens on 0.0.0.0, without breaking
// deployments that are deliberately reachable over the network, where remote
// clients legitimately send their own Host.
//
// Scope, stated plainly: this guards the MCP endpoint only, and only for
// loopback-arriving connections. A browser rebound onto the machine's LAN
// address is not covered, because a Host value cannot be judged for a listener
// that is meant to be reachable by name. Closing that requires binding
// loopback by default, which is tracked separately.
//
// A request with no recorded local address is treated as loopback, so the
// protection fails closed.
func (s *StreamableHTTPServer) validateHost(r *http.Request) error {
	return ValidateRequestHost(r, s.allowedHosts)
}

// ValidateRequestHost is the shared DNS rebinding check. The MCP transport and
// the REST API both route through it so the two surfaces cannot drift: a Host
// value accepted on one must be accepted on the other. See validateHost above
// for the reasoning.
func ValidateRequestHost(r *http.Request, allowedHosts []string) error {
	local := RequestLocalAddr(r)
	if local != nil && !AddrIsLoopback(local) {
		return nil
	}
	if r.Host == "" {
		return fmt.Errorf("missing Host header")
	}
	hostname, port, ok := splitHostPortLenient(r.Host)
	if !ok {
		return fmt.Errorf("invalid Host header %q", r.Host)
	}
	for _, allowed := range allowedHosts {
		// Empty entries would match an empty authority; wildcards are not
		// supported here, since an allow-all Host list defeats the control.
		if allowed == "" {
			continue
		}
		allowedHost, _, allowedOK := splitHostPortLenient(allowed)
		if !allowedOK {
			continue
		}
		if strings.EqualFold(allowed, r.Host) || strings.EqualFold(allowedHost, hostname) {
			return nil
		}
	}
	if !HostIsLoopback(hostname) {
		return fmt.Errorf("invalid Host header %q", r.Host)
	}
	// A loopback name pointed at the wrong port is still someone else's
	// service; only enforce when the real listener port is known.
	if port != "" && local != nil {
		if _, localPort, ok := splitHostPortLenient(local.String()); ok && localPort != "" && port != localPort {
			return fmt.Errorf("invalid Host header %q", r.Host)
		}
	}
	return nil
}

// RequestLocalAddr returns the address the request arrived on, or nil when the
// transport recorded none (httptest requests, custom servers).
func RequestLocalAddr(r *http.Request) net.Addr {
	addr, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	return addr
}

// AddrIsLoopback reports whether a listener address is on the loopback
// interface. Anything that does not parse as host:port is treated as loopback
// so the caller fails closed: a non-TCP listener (Unix socket, net.Pipe) must
// not silently disable Host validation.
func AddrIsLoopback(addr net.Addr) bool {
	// Strict host:port here, unlike a Host header, where a bare name is
	// legitimate: a TCP listener address always carries a port, so anything
	// else is a non-TCP listener and must fail closed.
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil || host == "" {
		return true
	}
	return HostIsLoopback(host)
}

// HostIsLoopback reports whether hostname names the loopback interface.
// ParseIP plus IsLoopback covers all of 127.0.0.0/8 and ::1 (including
// v4-mapped v6), which a literal comparison against "127.0.0.1" would miss.
// Note 0.0.0.0 is deliberately NOT loopback: DNS can answer with it, so it is
// a known rebinding vector despite routing locally on some platforms.
func HostIsLoopback(hostname string) bool {
	// A trailing dot is the fully-qualified form of the same name.
	hostname = strings.TrimSuffix(hostname, ".")
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

// splitHostPortLenient splits an authority into host and port, tolerating
// values that carry no port at all (Host may be a bare name) and unbracketed
// IPv6 literals. ok is false for authorities that are syntactically invalid,
// which callers must treat as a rejection rather than as a bare hostname.
func splitHostPortLenient(hostport string) (host, port string, ok bool) {
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		// SplitHostPort accepts a bare trailing colon; that is malformed, not
		// an omitted port, and must not skip the port comparison.
		if p == "" && strings.HasSuffix(hostport, ":") {
			return "", "", false
		}
		return h, p, true
	}
	// No port. Accept a bracketed IPv6 literal, reject other stray brackets
	// rather than normalizing malformed authorities into valid-looking names.
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		inner := hostport[1 : len(hostport)-1]
		if net.ParseIP(inner) == nil {
			return "", "", false
		}
		return inner, "", true
	}
	if strings.ContainsAny(hostport, "[]") {
		return "", "", false
	}
	return hostport, "", true
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

	// Era classification: modern per-request _meta selects the stateless
	// path, as does server/discover in any shape (it doubles as the
	// backward-compatibility probe and must never 404 into a legacy
	// verdict) and a MCP-Protocol-Version header declaring a
	// stateless-era version (so a modern request with missing or
	// incomplete _meta gets the stateless edge's -32602 rejection
	// instead of falling through to session lookup). initialize selects
	// the handshake path. Everything else is session traffic.
	meta, hasMeta := parseRequestMeta(req.Params)
	if hasMeta || req.Method == "server/discover" ||
		EraOfVersion(r.Header.Get("MCP-Protocol-Version")) == EraStateless {
		s.handleStateless(w, r, &req, meta)
		return
	}

	if req.Method == "initialize" {
		s.handleInitialize(w, r, &req)
		return
	}

	if !s.checkProtocolVersionHeader(w, r) {
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
	// Expired (manager-reaped) sessions 404 immediately so the client
	// re-initializes cleanly, instead of continuing on a session no
	// status surface counts and no observer can attribute.
	if s.gateway.sessions.Get(sessionID) == nil {
		s.pruneExpired(sessionID)
		http.Error(w, "session expired", http.StatusNotFound)
		return
	}

	s.gateway.sessions.Touch(sessionID)

	// Thread the originating client ID into the request context so tool-call
	// observers can attribute calls per client. Sessions created before
	// PR 2 may have an empty ClientID; WithClientID is a no-op in that case.
	ctx := r.Context()
	if gSession := s.gateway.sessions.Get(sessionID); gSession != nil {
		ctx = WithClientID(ctx, gSession.ClientID)
		ctx = WithClientAccessID(ctx, gSession.AccessID)
		// The session's group is authoritative over the request path, in
		// BOTH directions: a session created on /groups/x/mcp stays bound
		// to x wherever it posts, and a default /mcp session posting to a
		// group path stays full-surface. The value is stored even when
		// empty so it shadows anything a route wrapper injected.
		ctx = context.WithValue(ctx, groupKey{}, gSession.Group)
	}

	resp := s.handleRequest(ctx, session, &req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// checkProtocolVersionHeader enforces the MCP-Protocol-Version header on
// post-initialize requests. Per the transport spec, an absent header is
// tolerated (the session-negotiated version applies), while a present but
// unsupported value gets a 400 naming the supported set. The spec leaves
// header-vs-body precedence undefined (modelcontextprotocol#2721); gridctl's
// stance is that the session-negotiated version is authoritative and the
// header is validated for supported-set membership only.
func (s *StreamableHTTPServer) checkProtocolVersionHeader(w http.ResponseWriter, r *http.Request) bool {
	v := r.Header.Get("MCP-Protocol-Version")
	if v == "" || IsSupportedProtocolVersion(v) {
		return true
	}
	http.Error(w, fmt.Sprintf("unsupported protocol version %q (supported: %s)",
		v, supportedProtocolVersionList()), http.StatusBadRequest)
	return false
}

// handleInitialize processes an initialize request and creates a new session.
// The assigned Mcp-Session-Id is returned in the response header.
func (s *StreamableHTTPServer) handleInitialize(w http.ResponseWriter, r *http.Request, req *jsonrpc.Request) {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "invalid initialize params: "+err.Error()))
			return
		}
	}

	// The group (if any) was injected into the request context by the
	// /groups/{name}/mcp route wrapper, which already validated it exists.
	result, gSession, err := s.gateway.HandleInitialize(params, clientAccessIDFromRequest(r), GroupFromContext(r.Context()))
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
// The stateless era removed the GET stream; a request declaring a
// stateless version gets the spec's 405.
func (s *StreamableHTTPServer) handleGet(w http.ResponseWriter, r *http.Request) {
	if EraOfVersion(r.Header.Get("MCP-Protocol-Version")) == EraStateless {
		http.Error(w, "Method not allowed for this protocol version", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkProtocolVersionHeader(w, r) {
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
	// A transport record whose gateway session has been reaped is an
	// expired session: tear it down and 404 so the client re-initializes,
	// rather than serving an unattributed stream that no status surface
	// counts.
	if s.gateway.sessions.Get(sessionID) == nil {
		s.pruneExpired(sessionID)
		http.Error(w, "session expired", http.StatusNotFound)
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
			// The keepalive also marks the session live: a connected but
			// quiet SSE client must never age past the idle cutoff and get
			// reaped (then pruned) out from under its open stream.
			s.gateway.sessions.Touch(sessionID)
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleDelete handles DELETE /mcp — terminates a session. The
// stateless era has no sessions; a request declaring a stateless
// version gets the spec's 405.
func (s *StreamableHTTPServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	if EraOfVersion(r.Header.Get("MCP-Protocol-Version")) == EraStateless {
		http.Error(w, "Method not allowed for this protocol version", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkProtocolVersionHeader(w, r) {
		return
	}

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
		return s.handleToolsList(ctx, session, req)
	case "tools/call":
		return s.handleToolsCall(ctx, session, req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	case "resources/templates/list":
		return jsonrpc.NewSuccessResponse(req.ID, s.gateway.HandleResourceTemplatesList())
	case "ping":
		return jsonrpc.NewSuccessResponse(req.ID, struct{}{})
	default:
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

func (s *StreamableHTTPServer) handleToolsList(ctx context.Context, _ *StreamableSession, req *jsonrpc.Request) jsonrpc.Response {
	result, err := s.gateway.HandleToolsList(ctx)
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
	// Cross-era MRTR is out of scope by design: a handshake-era client
	// cannot relay a stateless server's input_required round trip, so
	// it gets a clear error instead of an interim result it cannot act
	// on.
	if result.ResultType == ResultTypeInputRequired {
		result = &ToolCallResult{
			Content: []Content{NewTextContent(
				"tool requires additional input via MRTR (2026-07-28), which this session's protocol generation cannot relay; use a client that speaks the stateless generation",
			)},
			IsError: true,
		}
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

func (s *StreamableHTTPServer) handlePromptsGet(ctx context.Context, req *jsonrpc.Request) jsonrpc.Response {
	if req.Params == nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "params required for prompts/get")
	}
	var params PromptsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, "Invalid prompts/get params")
	}
	result, err := s.gateway.HandlePromptsGet(ctx, params)
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

// SessionCount returns the number of active Streamable HTTP sessions,
// counting only sessions the gateway's SessionManager still recognizes,
// so this figure always agrees with Gateway.SessionCount.
func (s *StreamableHTTPServer) SessionCount() int {
	return len(s.SessionEntries())
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

// SessionEntry describes one active session for status surfaces.
// Sessions exist only on the handshake era (the stateless era has
// none), so Generation is currently always "handshake"; it is derived
// from the negotiated version rather than hardcoded so the field stays
// honest if a future revision reintroduces session semantics.
//
// ClientName and ClientVersion are the client-supplied clientInfo from
// initialize; AccessID is the normalized identifier that string-matches
// provisioner client slugs, so status surfaces can attribute a session
// to a linked client without new normalization logic.
type SessionEntry struct {
	ID              string `json:"id"`
	Generation      string `json:"generation"`
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ClientName      string `json:"clientName,omitempty"`
	ClientVersion   string `json:"clientVersion,omitempty"`
	AccessID        string `json:"accessId,omitempty"`
}

// SessionEntries returns the active sessions with their negotiated
// protocol version, generation, and client identity.
//
// The gateway's SessionManager is authoritative: it is the store the
// periodic cleanup sweeps, and every transport session is created
// alongside a manager entry. A transport record whose manager entry is
// gone (idle past the cleanup cutoff; clients that crash never send the
// graceful DELETE) is an expired session — it is excluded from the
// listing and lazily torn down here, so /api/status's count and this
// listing agree by construction instead of drifting apart as orphans
// accumulate. A client that later posts on a pruned ID receives the
// spec's 404 and re-initializes cleanly, rather than continuing on a
// session the gateway no longer attributes.
func (s *StreamableHTTPServer) SessionEntries() []SessionEntry {
	ids := s.SessionIDs()
	entries := make([]SessionEntry, 0, len(ids))
	for _, id := range ids {
		gs := s.gateway.sessions.Get(id)
		if gs == nil {
			s.pruneExpired(id)
			continue
		}
		entries = append(entries, SessionEntry{
			ID:              id,
			Generation:      string(EraOfVersion(gs.ProtocolVersion)),
			ProtocolVersion: gs.ProtocolVersion,
			ClientName:      gs.ClientInfo.Name,
			ClientVersion:   gs.ClientInfo.Version,
			AccessID:        gs.AccessID,
		})
	}
	return entries
}

// pruneExpired tears down a transport session whose gateway session has
// been reaped: the transport-level counterpart of the manager's cleanup,
// mirroring deleteSession minus the (already absent) manager entry.
func (s *StreamableHTTPServer) pruneExpired(id string) {
	s.mu.Lock()
	session, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
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
