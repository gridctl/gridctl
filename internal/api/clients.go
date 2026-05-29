package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

const (
	// errCodeUnknownServer is returned on 422 when a client scope references a
	// server that the gateway does not know about.
	errCodeUnknownServer = "unknown_server"
	// errCodeInvalidClient is returned on 400 when the client identifier
	// normalizes to an empty profile key.
	errCodeInvalidClient = "invalid_client"
)

// setClientScopeRequest is the wire shape for PUT /api/clients/{slug}/scope.
// Both fields are allow-lists; nil or empty means "no restriction on this axis"
// (an empty profile sees every tool, per the clients: block semantics).
type setClientScopeRequest struct {
	Servers *[]string `json:"servers"`
	Tools   *[]string `json:"tools"`
}

// setClientScopeResponse is the success payload.
type setClientScopeResponse struct {
	Client     string   `json:"client"`
	ProfileKey string   `json:"profileKey"`
	Servers    []string `json:"servers"`
	Tools      []string `json:"tools"`
	Reloaded   bool     `json:"reloaded"`
	ReloadedAt string   `json:"reloadedAt,omitempty"`
}

// handleSetClientScope writes a single client's access profile (allowed servers
// and/or tools) into the live stack YAML's `clients:` block and triggers a hot
// reload. The profile key is the client's stable identifier, normalized the
// same way enforcement normalizes it so the written profile matches the key the
// gateway scopes on. The YAML write is atomic and conflict-detected: an external
// edit between read and write surfaces as 409 so the UI can re-fetch.
//
// PUT /api/clients/{slug}/scope
func (s *Server) handleSetClientScope(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		writeJSONError(w, "Client slug is required", http.StatusBadRequest)
		return
	}
	profileKey := mcp.NormalizeClientID(slug)
	if profileKey == "" {
		writeStructuredError(w, http.StatusBadRequest, errCodeInvalidClient,
			"Client identifier is empty after normalization.",
			"Provide a non-empty client slug.")
		return
	}

	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, setToolsRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req setClientScopeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Servers == nil && req.Tools == nil {
		writeJSONError(w, "Request must set servers and/or tools", http.StatusBadRequest)
		return
	}
	// Each axis is tri-state: a present array replaces it, an absent one leaves
	// it untouched (so a server-only edit preserves an operator's tool list).
	servers := normalizeScopeAxis(req.Servers)
	tools := normalizeScopeAxis(req.Tools)
	for _, v := range append(append([]string{}, derefStrings(servers)...), derefStrings(tools)...) {
		if v == "" {
			writeJSONError(w, "Server and tool names must be non-empty strings", http.StatusBadRequest)
			return
		}
	}

	// Validate the provided allow-lists against the live tool surface so a stale
	// UI cannot persist a reference to a server or tool that does not exist.
	if code, msg := s.validateClientScope(derefStrings(servers), derefStrings(tools)); code != "" {
		writeStructuredError(w, http.StatusUnprocessableEntity, code, msg,
			"Refresh the workspace to pick up the current servers and tools, then try again.")
		return
	}

	switch err := setClientScope(s.stackFile, profileKey, servers, tools); {
	case err == nil:
		// proceed
	case errors.Is(err, errStackModified):
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	default:
		slog.Default().Warn("client scope write failed", "client", profileKey, "error", err)
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Default().Info("client access scope updated", "client", profileKey,
		"servers", len(derefStrings(servers)), "tools", len(derefStrings(tools)))

	resp := setClientScopeResponse{
		Client:     slug,
		ProfileKey: profileKey,
		Servers:    derefStrings(servers),
		Tools:      derefStrings(tools),
	}

	if s.reloadHandler != nil {
		result, err := s.reloadHandler.Reload(r.Context())
		if err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed, err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		if !result.Success {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed, result.Message,
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		resp.Reloaded = true
		resp.ReloadedAt = time.Now().UTC().Format(time.RFC3339)
	}

	writeJSON(w, resp)
}

// validateClientScope checks the requested servers and tools against the live
// gateway surface. Returns an empty code when the scope is valid, otherwise a
// structured error code and message identifying the first offending reference.
func (s *Server) validateClientScope(servers, tools []string) (code, message string) {
	if s.gateway == nil {
		return "", ""
	}

	knownServers := make(map[string]bool)
	for _, st := range s.gateway.Status() {
		knownServers[st.Name] = true
	}
	for _, srv := range servers {
		if !knownServers[srv] {
			return errCodeUnknownServer, fmt.Sprintf("Unknown MCP server %q", srv)
		}
	}

	if len(tools) == 0 {
		return "", ""
	}
	knownTools := make(map[string]bool)
	if catalog, err := s.gateway.HandleToolsCatalog(); err == nil && catalog != nil {
		for _, t := range catalog.Tools {
			knownTools[t.Name] = true
		}
	}
	for _, t := range tools {
		if !knownTools[t] {
			return errCodeUnknownTool, fmt.Sprintf("Unknown tool %q", t)
		}
	}
	return "", ""
}

// normalizeScopeAxis canonicalizes a tri-state allow-list axis: nil stays nil
// (leave the axis untouched), while a present slice is copied and sorted for
// deterministic YAML output. A non-nil empty slice is preserved (it means
// "replace with no restriction").
func normalizeScopeAxis(p *[]string) *[]string {
	if p == nil {
		return nil
	}
	out := append([]string{}, *p...)
	sort.Strings(out)
	return &out
}

// derefStrings returns the slice value of a possibly-nil *[]string (nil -> []),
// for validation and response echoing.
func derefStrings(p *[]string) []string {
	if p == nil || *p == nil {
		return []string{}
	}
	return *p
}
