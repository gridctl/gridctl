package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
)

const (
	setToolsRequestMaxBytes = 64 * 1024

	// setToolsBatchRequestMaxBytes caps the batch body. It is larger than the
	// single-server cap because the payload carries one tools list per server;
	// 512KiB comfortably holds a fleet of servers with long whitelists.
	setToolsBatchRequestMaxBytes = 512 * 1024

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

type structuredIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type structuredErrorPayload struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Hint    string            `json:"hint,omitempty"`
	Issues  []structuredIssue `json:"issues,omitempty"`
}

func writeStructuredError(w http.ResponseWriter, status int, code, message, hint string, issues ...structuredIssue) {
	payload := structuredErrorPayload{Code: code, Message: message, Hint: hint}
	if len(issues) > 0 {
		payload.Issues = issues
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(structuredError{Error: payload})
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

	switch err := setServerTools(s.stackFile, serverName, tools, s.candidateValidator(r.Context())); {
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
		if writeCandidateValidation(w, err) {
			return
		}
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
	if handler := s.ReloadHandler(); handler != nil {
		result, err := handler.Reload(r.Context())
		if err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		if !result.Success {
			if writePreflightFailure(w, result) {
				return
			}
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

// setServerToolsBatchRequest is the wire shape for PUT /api/mcp-servers/tools.
type setServerToolsBatchRequest struct {
	Servers []batchServerToolsEntry `json:"servers"`
}

type batchServerToolsEntry struct {
	Name  string    `json:"name"`
	Tools *[]string `json:"tools"`
}

// batchServerResult reports one server's applied whitelist in a batch success.
type batchServerResult struct {
	Server string   `json:"server"`
	Tools  []string `json:"tools"`
}

// setServerToolsBatchResponse is the success payload. The batch is atomic, so
// every listed server was applied and (when live-reload is enabled) a single
// reload ran once for the whole batch.
type setServerToolsBatchResponse struct {
	Servers    []batchServerResult `json:"servers"`
	Reloaded   bool                `json:"reloaded"`
	ReloadedAt string              `json:"reloadedAt,omitempty"`
}

// handleSetServerToolsBatch applies tool-whitelist changes to MULTIPLE servers
// in one atomic YAML write, then triggers a SINGLE hot reload — avoiding the
// N-reloads cost of calling the per-server endpoint in a loop.
//
// Transaction semantics are all-or-nothing: every server's tools are validated
// up front, and if any tool is unknown the whole batch is rejected (400
// unknown_tool) with nothing written. This keeps the on-disk stack from ever
// landing in a half-applied state. A concurrent external edit surfaces as 409
// (stack_modified, nothing written); a write that lands but fails to reload
// surfaces as 502 (reload_failed) with the YAML changes persisted — identical
// to the single-server endpoint's contract.
//
// PUT /api/mcp-servers/tools
func (s *Server) handleSetServerToolsBatch(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, setToolsBatchRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req setServerToolsBatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Servers) == 0 {
		writeJSONError(w, "Request body must include a non-empty servers array", http.StatusBadRequest)
		return
	}

	// Build the update set, validating each entry up front so the whole batch
	// is rejected before any write touches disk.
	updates := make([]serverToolsUpdate, 0, len(req.Servers))
	seen := make(map[string]struct{}, len(req.Servers))
	for _, entry := range req.Servers {
		if entry.Name == "" {
			writeJSONError(w, "Each server entry must include a name", http.StatusBadRequest)
			return
		}
		if _, dup := seen[entry.Name]; dup {
			writeJSONError(w, "Duplicate server in batch: "+entry.Name, http.StatusBadRequest)
			return
		}
		seen[entry.Name] = struct{}{}

		if entry.Tools == nil {
			writeJSONError(w, "Server "+entry.Name+" must include a tools array", http.StatusBadRequest)
			return
		}
		tools := *entry.Tools
		if tools == nil {
			tools = []string{}
		}
		for _, t := range tools {
			if t == "" {
				writeJSONError(w, "Tool names must be non-empty strings", http.StatusBadRequest)
				return
			}
		}
		if err := s.validateToolsAgainstServer(entry.Name, tools); err != nil {
			writeStructuredError(w, http.StatusBadRequest, errCodeUnknownTool,
				err.Error()+" (server "+entry.Name+")",
				"The tool list is out of date. Refresh and try again.")
			return
		}
		updates = append(updates, serverToolsUpdate{Server: entry.Name, Tools: tools})
	}

	switch err := setServerToolsBatch(s.stackFile, updates, s.candidateValidator(r.Context())); {
	case err == nil:
		// proceed
	case errors.Is(err, errServerNotFound):
		// err already reads "mcp server not found in stack: <name>".
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, errStackModified):
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	default:
		if writeCandidateValidation(w, err) {
			return
		}
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := setServerToolsBatchResponse{Servers: make([]batchServerResult, 0, len(updates))}
	for _, u := range updates {
		// serverToolsUpdate and batchServerResult share the same fields; the
		// conversion echoes each applied server's name + persisted whitelist.
		resp.Servers = append(resp.Servers, batchServerResult(u))
	}

	// Exactly one reload for the whole batch.
	if handler := s.ReloadHandler(); handler != nil {
		result, err := handler.Reload(r.Context())
		if err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		if !result.Success {
			if writePreflightFailure(w, result) {
				return
			}
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

// candidateValidator checks patched stack bytes through the daemon's load
// pipeline when a reload handler is present. Stackless servers skip the
// check; the write path already fails without a stack file.
func (s *Server) candidateValidator(ctx context.Context) func([]byte) error {
	handler := s.ReloadHandler()
	if handler == nil {
		return nil
	}
	return func(data []byte) error {
		return handler.ValidateCandidate(ctx, data)
	}
}

// writeCandidateValidation classifies a candidate-validation failure.
// Hidden-tool references get a dedicated code and issues array. Any other
// validation failure is a generic 400 and does not write. Non-validation
// errors return false so the caller can use its existing status mapping.
func writeCandidateValidation(w http.ResponseWriter, err error) bool {
	var ve config.ValidationErrors
	if !errors.As(err, &ve) {
		return false
	}
	var issues []structuredIssue
	var hidden []string
	var all []string
	for _, item := range ve {
		all = append(all, item.Error())
		if config.IsHiddenToolReference(item) {
			issues = append(issues, structuredIssue{Field: item.Field, Message: item.Message})
			hidden = append(hidden, item.Error())
		}
	}
	if len(issues) > 0 {
		writeStructuredError(w, http.StatusBadRequest, "hidden_tool_reference", strings.Join(hidden, "; "),
			"Remove the tool from the named group or keep it exposed on this server.", issues...)
		return true
	}
	writeJSONError(w, strings.Join(all, "; "), http.StatusBadRequest)
	return true
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
