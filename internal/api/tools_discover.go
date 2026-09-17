package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func (s *Server) handleToolsDiscover(w http.ResponseWriter, r *http.Request) {
	if s.gateway == nil {
		writeToolsEndpointError(w, http.StatusServiceUnavailable, errCodeGatewayUnavailable, "gateway is unavailable", "", "", mcp.CallOutcome{
			Disposition: "routing_failed",
			Stage:       "daemon",
			Reason:      errCodeGatewayUnavailable,
			Completion:  mcp.CompletionNotStarted,
		})
		return
	}
	q := r.URL.Query()
	for _, key := range []string{"client", "server", "name", "query", "limit"} {
		if len(q[key]) > 1 {
			writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidRequest, "duplicate query parameter", "", "", mcp.CallOutcome{})
			return
		}
	}
	name := q.Get("name")
	server := q.Get("server")
	query := q.Get("query")
	if name != "" && (server != "" || query != "") {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidCombination, "name cannot be combined with server or query", name, "", mcp.CallOutcome{})
		return
	}
	if name != "" {
		if _, _, ok := splitCanonicalName(name); !ok {
			writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidName, "name must be server__tool with nonempty halves", name, "", mcp.CallOutcome{})
			return
		}
	}

	_, clientPresent := q["client"]
	normalized, err := normalizeDeclaredClient(q.Get("client"), clientPresent)
	if err != nil {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidClient, "client is invalid", name, "", mcp.CallOutcome{})
		return
	}

	limit := toolsDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 1 || n > toolsMaxLimit {
			writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidLimit, "limit must be between 1 and 200", name, normalized, mcp.CallOutcome{})
			return
		}
		limit = n
	}

	ctx := withDeclaredClient(r.Context(), normalized)
	result, err := s.gateway.DiscoverTools(ctx, mcp.DiscoverOptions{
		Server: server,
		Name:   name,
		Query:  query,
		Limit:  limit,
	})
	if err == mcp.ErrDiscoverNotFound {
		writeToolsEndpointError(w, http.StatusNotFound, errCodeNotFound, "tool or server not found", name, normalized, mcp.CallOutcome{
			Disposition: "routing_failed",
			Stage:       "routing",
			Reason:      errCodeNotFound,
			Completion:  mcp.CompletionNotStarted,
		})
		return
	}
	if err != nil {
		writeToolsEndpointError(w, http.StatusBadRequest, errCodeInvalidRequest, "discovery failed", name, normalized, mcp.CallOutcome{})
		return
	}
	if result.Tools == nil {
		result.Tools = []mcp.Tool{}
	}
	if result.Client == "" {
		result.Client = normalized
	}
	w.Header().Set("Content-Type", toolsJSONContentType)
	_ = json.NewEncoder(w).Encode(toolsDiscoverEnvelope{
		SchemaVersion: toolsSchemaVersion,
		Client:        result.Client,
		Tools:         result.Tools,
		TotalVisible:  result.TotalVisible,
		Matched:       result.Matched,
		Returned:      result.Returned,
		Truncated:     result.Truncated,
	})
}
