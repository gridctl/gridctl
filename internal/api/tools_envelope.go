package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

const (
	toolsCallMaxBytes    = 2 << 20
	toolsClientMaxBytes  = 128
	toolsSchemaVersion   = 1
	toolsDefaultClient   = "cli"
	toolsDefaultLimit    = 20
	toolsMaxLimit        = 200
	toolsJSONContentType = "application/json"
)

const (
	errCodeInvalidRequest     = "invalid_request"
	errCodeInvalidJSON        = "invalid_json"
	errCodeInvalidContentType = "invalid_content_type"
	errCodePayloadTooLarge    = "payload_too_large"
	errCodeInvalidName        = "invalid_name"
	errCodeInvalidLimit       = "invalid_limit"
	errCodeInvalidCombination = "invalid_combination"
	errCodeNotFound           = "not_found"
	errCodeGatewayUnavailable = "gateway_unavailable"
	errCodeClientScope        = "client_scope"
	errCodeGateDenied         = "gate_denied"
	errCodeSchemaPin          = "schema_pin"
	errCodeToolError          = "tool_error"
	errCodeTransportError     = "transport_error"
	errCodeInputRequired      = "input_required"
	errCodeTimeout            = "timeout"
	errCodeCancelled          = "cancelled"
)

type toolsEndpointError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type toolsCallEnvelope struct {
	SchemaVersion int                 `json:"schema_version"`
	Client        string              `json:"client,omitempty"`
	Name          string              `json:"name,omitempty"`
	Outcome       mcp.CallOutcome     `json:"outcome"`
	Result        *mcp.ToolCallResult `json:"result"`
	Error         *toolsEndpointError `json:"error"`
}

type toolsDiscoverEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	Client        string     `json:"client"`
	Tools         []mcp.Tool `json:"tools"`
	TotalVisible  int        `json:"total_visible"`
	Matched       int        `json:"matched"`
	Returned      int        `json:"returned"`
	Truncated     bool       `json:"truncated"`
}

func writeToolsEndpointError(w http.ResponseWriter, status int, code, message, name, client string, outcome mcp.CallOutcome) {
	if outcome.Disposition == "" {
		outcome.Disposition = errCodeInvalidRequest
		if outcome.Stage == "" {
			outcome.Stage = "input"
		}
		if outcome.Reason == "" {
			outcome.Reason = code
		}
		if outcome.Completion == "" {
			outcome.Completion = mcp.CompletionNotStarted
		}
	}
	env := toolsCallEnvelope{
		SchemaVersion: toolsSchemaVersion,
		Client:        client,
		Name:          name,
		Outcome:       outcome,
		Result:        nil,
		Error:         &toolsEndpointError{Code: code, Message: message},
	}
	w.Header().Set("Content-Type", toolsJSONContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

func writeToolsEnvelope(w http.ResponseWriter, env toolsCallEnvelope) {
	w.Header().Set("Content-Type", toolsJSONContentType)
	_ = json.NewEncoder(w).Encode(env)
}

func normalizeDeclaredClient(raw string, present bool) (string, error) {
	if !present {
		return toolsDefaultClient, nil
	}
	if len(raw) > toolsClientMaxBytes || !utf8.ValidString(raw) {
		return "", errInvalidDeclaredClient
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errInvalidDeclaredClient
		}
	}
	if strings.TrimSpace(raw) == "" {
		return "", errInvalidDeclaredClient
	}
	normalized := mcp.NormalizeClientID(raw)
	if normalized == "" {
		return "", errInvalidDeclaredClient
	}
	return normalized, nil
}

var errInvalidDeclaredClient = errSentinel("invalid client")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func withDeclaredClient(ctx context.Context, client string) context.Context {
	ctx = mcp.WithClientID(ctx, client)
	return mcp.WithClientAccessID(ctx, client)
}

func outcomeErrorCode(out mcp.CallOutcome) string {
	switch out.Reason {
	case runs.ReasonClientScope:
		return errCodeClientScope
	case runs.ReasonGateDenied:
		return errCodeGateDenied
	case runs.ReasonSchemaPin:
		return errCodeSchemaPin
	case runs.ReasonUnknownTool:
		return errCodeUnknownTool
	case runs.ReasonToolError:
		return errCodeToolError
	case runs.ReasonTransportError:
		return errCodeTransportError
	case runs.ReasonInputRequired:
		return errCodeInputRequired
	case runs.ReasonDeadlineExceeded:
		return errCodeTimeout
	case runs.ReasonContextCanceled:
		return errCodeCancelled
	case runs.ReasonOK:
		return ""
	default:
		if out.Reason != "" {
			return out.Reason
		}
		return errCodeInvalidRequest
	}
}

func safeOutcomeMessage(out mcp.CallOutcome) string {
	switch out.Reason {
	case runs.ReasonOK:
		return ""
	case runs.ReasonClientScope:
		return "tool is not in this client's access scope"
	case runs.ReasonGateDenied:
		return "tool call denied by a call gate"
	case runs.ReasonSchemaPin:
		return "server is blocked pending schema approval"
	case runs.ReasonUnknownTool:
		return "unknown tool"
	case runs.ReasonNoReplica:
		return "no eligible replica"
	case runs.ReasonColdStart:
		return "cold start failed"
	case runs.ReasonToolError:
		return "tool returned an error"
	case runs.ReasonTransportError:
		return "downstream transport error"
	case runs.ReasonInputRequired:
		return "tool returned an input-required result"
	case runs.ReasonDeadlineExceeded:
		return "tool call timed out"
	case runs.ReasonContextCanceled:
		return "tool call was canceled"
	default:
		return "tool call failed"
	}
}

func includeDispatchResult(out mcp.CallOutcome) bool {
	switch out.Disposition {
	case runs.DispositionCompleted, runs.DispositionToolError, runs.DispositionInputRequired:
		return true
	default:
		return false
	}
}
