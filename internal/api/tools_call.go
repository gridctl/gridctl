package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

type toolsCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Client    *string         `json:"client"`
}

func (s *Server) handleToolsCall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.gateway == nil {
		writeToolsEndpointError(w, http.StatusServiceUnavailable, errCodeGatewayUnavailable, "gateway is unavailable", "", "", mcp.CallOutcome{
			Disposition: "routing_failed",
			Stage:       "daemon",
			Reason:      errCodeGatewayUnavailable,
			Completion:  mcp.CompletionNotStarted,
		})
		return
	}
	ct := r.Header.Get("Content-Type")
	if mediaType := strings.TrimSpace(strings.Split(ct, ";")[0]); !strings.EqualFold(mediaType, toolsJSONContentType) {
		writeToolsEndpointError(w, http.StatusUnsupportedMediaType, errCodeInvalidContentType, "Content-Type must be application/json", "", "", mcp.CallOutcome{})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, toolsCallMaxBytes+1))
	if err != nil {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidRequest, "failed to read request body", "", "", mcp.CallOutcome{})
		return
	}
	if len(body) > toolsCallMaxBytes {
		writeToolsEndpointError(w, http.StatusRequestEntityTooLarge, errCodePayloadTooLarge, "request body exceeds 2 MiB", "", "", mcp.CallOutcome{})
		return
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var req toolsCallRequest
	if err := dec.Decode(&req); err != nil {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidJSON, "invalid JSON object", "", "", mcp.CallOutcome{})
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidJSON, "request must contain exactly one JSON object", "", "", mcp.CallOutcome{})
		return
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidJSON, "request must be a JSON object", "", "", mcp.CallOutcome{})
		return
	}

	name := strings.TrimSpace(req.Name)
	if _, tool, ok := splitCanonicalName(name); !ok || tool == "" {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidName, "name must be server__tool with nonempty halves", "", "", mcp.CallOutcome{})
		return
	}

	client, clientOK := "", false
	if req.Client != nil {
		clientOK = true
		client = *req.Client
	}
	normalized, err := normalizeDeclaredClient(client, clientOK)
	if err != nil {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidClient, "client is invalid", name, "", mcp.CallOutcome{})
		return
	}

	args, err := decodeToolArguments(req.Arguments)
	if err != nil {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidJSON, "arguments must be a JSON object", name, normalized, mcp.CallOutcome{})
		return
	}

	ctx := withDeclaredClient(r.Context(), normalized)
	result, outcome, callErr := s.gateway.CallCanonicalTool(ctx, mcp.ToolCallParams{
		Name:      name,
		Arguments: args,
	})
	if callErr != nil && outcome.Disposition == "" {
		outcome = mcp.CallOutcome{
			Disposition: runs.DispositionTransportError,
			Stage:       runs.StageDownstream,
			Reason:      runs.ReasonTransportError,
			Completion:  mcp.CompletionUnknown,
		}
	}

	env := toolsCallEnvelope{
		SchemaVersion: toolsSchemaVersion,
		Client:        normalized,
		Name:          name,
		Outcome:       outcome,
	}
	if includeDispatchResult(outcome) && result != nil {
		env.Result = result
	}
	if code := outcomeErrorCode(outcome); code != "" {
		env.Error = &toolsEndpointError{Code: code, Message: safeOutcomeMessage(outcome)}
	}
	writeToolsEnvelope(w, env)
}

func splitCanonicalName(name string) (server, tool string, ok bool) {
	server, tool, err := mcp.ParsePrefixedTool(name)
	if err != nil || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

func decodeToolArguments(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errSentinel("arguments null")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errSentinel("arguments not object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, errSentinel("trailing")
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}
