package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	setToolsRequestMaxBytes = 64 * 1024

	// errCodeStackModified is the structured code returned with 409 responses
	// so the UI can distinguish it from other failures and offer a
	// Reload-file affordance.
	errCodeStackModified = "stack_modified"
	// errCodeReloadFailed is returned on 502 when the YAML write succeeded
	// but the gateway reload reported an error.
	errCodeReloadFailed = "reload_failed"
	// errCodeUnknownTool is returned on 400 when the request includes a tool
	// name that the server has not advertised.
	errCodeUnknownTool = "unknown_tool"
)

// setServerToolsRequest is the wire shape for PUT /api/mcp-servers/{name}/tools.
type setServerToolsRequest struct {
	Tools *[]string `json:"tools"`
}

// setServerToolsResponse is the success payload.
type setServerToolsResponse struct {
	Server     string   `json:"server"`
	Tools      []string `json:"tools"`
	Reloaded   bool     `json:"reloaded"`
	ReloadedAt string   `json:"reloadedAt,omitempty"`
}

// structuredError carries a code + hint alongside the error message so the
// UI can render stable copy and conditional affordances per code. Mirrors the
// probe endpoint's envelope.
type structuredError struct {
	Error structuredErrorPayload `json:"error"`
}

type structuredErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func writeStructuredError(w http.ResponseWriter, status int, code, message, hint string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(structuredError{
		Error: structuredErrorPayload{Code: code, Message: message, Hint: hint},
	})
}

// handleSetServerTools updates a single MCP server's tool whitelist in the
// live stack YAML and triggers a hot reload. The YAML write is atomic; a
// concurrent external edit detected between our initial read and the write
// surfaces as 409 so the UI can re-fetch without clobbering the user's work.
//
// PUT /api/mcp-servers/{name}/tools
func (s *Server) handleSetServerTools(w http.ResponseWriter, r *http.Request) {
	serverName := r.PathValue("name")
	if serverName == "" {
		writeJSONError(w, "Server name is required", http.StatusBadRequest)
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

	var req setServerToolsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Tools == nil {
		writeJSONError(w, "Request body must include a tools array", http.StatusBadRequest)
		return
	}
	// Normalize the slice early so downstream code never has to distinguish
	// nil from empty. An empty array is a valid "expose all tools" directive.
	tools := *req.Tools
	if tools == nil {
		tools = []string{}
	}
	for _, t := range tools {
		if t == "" {
			writeJSONError(w, "Tool names must be non-empty strings", http.StatusBadRequest)
			return
		}
	}

	// Validate against the server's currently-discovered tools. An outdated
	// client could send a name that no longer exists; we reject those with a
	// specific 400 code so the UI can show a targeted message.
	if err := s.validateToolsAgainstServer(serverName, tools); err != nil {
		writeStructuredError(w, http.StatusBadRequest, errCodeUnknownTool, err.Error(),
			"The tool list is out of date. Refresh the sidebar and try again.")
		return
	}

	switch err := setServerTools(s.stackFile, serverName, tools); {
	case err == nil:
		// proceed
	case errors.Is(err, errServerNotFound):
		writeJSONError(w, "MCP server not found: "+serverName, http.StatusNotFound)
		return
	case errors.Is(err, errStackModified):
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	default:
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := setServerToolsResponse{
		Server: serverName,
		Tools:  tools,
	}

	// Trigger reload when live-reload is enabled. In no-watch mode we
	// respond with reloaded: false so the UI can show a hint to run
	// `gridctl reload` manually.
	if s.reloadHandler != nil {
		result, err := s.reloadHandler.Reload(r.Context())
		if err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		if !result.Success {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				result.Message,
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		resp.Reloaded = true
		resp.ReloadedAt = time.Now().UTC().Format(time.RFC3339)
	}

	writeJSON(w, resp)
}

// validateToolsAgainstServer rejects any tool name not in the server's
// currently-reported tool list. An empty whitelist is always allowed (it
// disables filtering). If the server is unknown to the gateway we skip the
// check — setServerTools will return errServerNotFound anyway, which maps
// to a clearer 404.
func (s *Server) validateToolsAgainstServer(serverName string, tools []string) error {
	if len(tools) == 0 || s.gateway == nil {
		return nil
	}
	known := make(map[string]struct{})
	for _, st := range s.gateway.Status() {
		if st.Name != serverName {
			continue
		}
		for _, t := range st.Tools {
			known[t] = struct{}{}
		}
		// Found the server; validate.
		for _, t := range tools {
			if _, ok := known[t]; !ok {
				return errors.New("unknown tool: " + t)
			}
		}
		return nil
	}
	// Server not in gateway status — let setServerTools surface the 404.
	return nil
}
