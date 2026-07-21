package api

import (
	"fmt"
	"net/http"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// handleGroupMCP serves a tool group's MCP endpoint through the shared
// streamable transport. Unknown groups 404 before any MCP handling — a
// mistyped link URL must never create a working full-surface session.
func (s *Server) handleGroupMCP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.gateway == nil || !s.gateway.HasGroup(name) {
		http.NotFound(w, r)
		return
	}
	s.streamableServer.ServeHTTP(w, r.WithContext(mcp.WithGroup(r.Context(), name)))
}

// handleGroupSSE mirrors the legacy /sse negotiation hint for group
// endpoints, pointing clients at the group's streamable path.
func (s *Server) handleGroupSSE(w http.ResponseWriter, r *http.Request) {
	// Echo only the configured group name, never the raw path input: the
	// exact-match lookup against the policy's name list both authorizes the
	// endpoint and yields a config-originated string for the response.
	name := s.configuredGroupName(r.PathValue("name"))
	if name == "" {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, "event: endpoint\n")
	fmt.Fprintf(w, "data: POST /groups/%s/mcp\n\n", name)
	flusher.Flush()
}

// configuredGroupName returns the configured group name equal to the raw
// path value, or "" when no such group exists. The returned string comes
// from the compiled policy (stack.yaml), not the request.
func (s *Server) configuredGroupName(raw string) string {
	if s.gateway == nil {
		return ""
	}
	for _, name := range s.gateway.CurrentGroupPolicy().Names() {
		if name == raw {
			return name
		}
	}
	return ""
}

// handleGroups handles GET /api/groups: every configured group resolved
// against the live tool surface. With no groups: block it returns
// configured: false and an empty array, never an error.
func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	resp := mcp.GroupsReport{Groups: []mcp.GroupStatus{}}
	if s.gateway != nil {
		if status := s.gateway.GroupsStatus(); len(status) > 0 {
			resp.Configured = true
			resp.Groups = status
		}
	}
	writeJSON(w, resp)
}
