package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Handler provides HTTP handlers for the MCP gateway.
type Handler struct {
	gateway *Gateway
}

// NewHandler creates a new MCP HTTP handler.
func NewHandler(gateway *Gateway) *Handler {
	return &Handler{gateway: gateway}
}

// ServeHTTP handles MCP requests at /mcp.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		// SSE endpoint for server-sent events (future enhancement)
		h.handleSSE(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost handles JSON-RPC requests.
func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Read request body with size limit
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, nil, ParseError, "Failed to read request body")
		return
	}

	// Parse JSON-RPC request
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeError(w, nil, ParseError, "Invalid JSON")
		return
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		h.writeError(w, req.ID, InvalidRequest, "Invalid JSON-RPC version")
		return
	}

	// Route to handler based on method
	resp := h.handleMethod(r, &req)
	h.writeResponse(w, resp)
}

// handleMethod routes the request to the appropriate handler.
func (h *Handler) handleMethod(r *http.Request, req *Request) Response {
	switch req.Method {
	case "initialize":
		return h.handleInitialize(req)
	case "notifications/initialized":
		// Client notification, just acknowledge
		return NewSuccessResponse(req.ID, nil)
	case "tools/list":
		return h.handleToolsList(r, req)
	case "tools/call":
		return h.handleToolsCall(r, req)
	case "ping":
		return NewSuccessResponse(req.ID, struct{}{})
	default:
		return NewErrorResponse(req.ID, MethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

// handleInitialize handles the initialize request.
func (h *Handler) handleInitialize(req *Request) Response {
	var params InitializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, InvalidParams, "Invalid initialize params")
		}
	}

	result, err := h.gateway.HandleInitialize(params)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError, err.Error())
	}

	return NewSuccessResponse(req.ID, result)
}

// handleToolsList handles the tools/list request.
func (h *Handler) handleToolsList(r *http.Request, req *Request) Response {
	// Check for agent identity header for access control
	agentName := r.Header.Get("X-Agent-Name")

	var result *ToolsListResult
	var err error
	if agentName != "" {
		// Filter tools based on agent's allowed MCP servers
		result, err = h.gateway.HandleToolsListForAgent(agentName)
	} else {
		// No agent header - return all tools
		result, err = h.gateway.HandleToolsList()
	}

	if err != nil {
		return NewErrorResponse(req.ID, InternalError, err.Error())
	}
	return NewSuccessResponse(req.ID, result)
}

// handleToolsCall handles the tools/call request.
func (h *Handler) handleToolsCall(r *http.Request, req *Request) Response {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return NewErrorResponse(req.ID, InvalidParams, "Invalid tools/call params")
	}

	// Check for agent identity header for access control
	agentName := r.Header.Get("X-Agent-Name")

	var result *ToolCallResult
	var err error
	if agentName != "" {
		// Validate agent has access to this tool's MCP server
		result, err = h.gateway.HandleToolsCallForAgent(r.Context(), agentName, params)
	} else {
		// No agent header - allow all tools
		result, err = h.gateway.HandleToolsCall(r.Context(), params)
	}

	if err != nil {
		return NewErrorResponse(req.ID, InternalError, err.Error())
	}

	return NewSuccessResponse(req.ID, result)
}

// handleSSE handles Server-Sent Events connections.
func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Keep connection open until client disconnects
	<-r.Context().Done()
}

// writeResponse writes a JSON-RPC response.
func (h *Handler) writeResponse(w http.ResponseWriter, resp Response) {
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// writeError writes a JSON-RPC error response.
func (h *Handler) writeError(w http.ResponseWriter, id *json.RawMessage, code int, message string) {
	resp := NewErrorResponse(id, code, message)
	h.writeResponse(w, resp)
}
