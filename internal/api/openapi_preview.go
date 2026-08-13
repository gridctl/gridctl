package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gridctl/gridctl/internal/openapipreview"
	"github.com/gridctl/gridctl/pkg/config"
)

// previewRequestMaxBytes caps the decoded body. The wire shape is a spec
// reference plus optional TLS paths — there is no reason to accept more.
const previewRequestMaxBytes = 64 * 1024

// SetOpenAPIPreviewer wires an externally-constructed previewer. The API server
// owns the limiter; the previewer's cache comes from the gateway builder,
// mirroring how SetProber works.
func (s *Server) SetOpenAPIPreviewer(p *openapipreview.Previewer) {
	s.openapiPreviewer = p
	if s.openapiPreviewLimiter == nil {
		s.openapiPreviewLimiter = newProbeLimiter()
	}
}

// handleOpenAPIPreview is the HTTP entry point for the wizard's "Load
// operations" button. Like handleProbe it is intentionally a thin shell — the
// parsing, caching, and error classification live in the openapipreview
// package where they are unit-tested without HTTP scaffolding.
func (s *Server) handleOpenAPIPreview(w http.ResponseWriter, r *http.Request) {
	if s.openapiPreviewer == nil {
		writeProbeError(w, http.StatusServiceUnavailable, openapipreview.CodeInternal,
			"OpenAPI preview is not configured on this daemon.",
			"Enter operation IDs manually, or upgrade to a build with preview support.")
		return
	}

	session := r.Header.Get(sessionHeader)
	if session == "" {
		session = r.RemoteAddr
	}
	release, sessionLimited, globalLimited := s.openapiPreviewLimiter.acquire(session)
	if sessionLimited || globalLimited {
		w.Header().Set("Retry-After", "3")
		status := http.StatusTooManyRequests
		msg := "Too many spec loads in progress for this session."
		if globalLimited {
			status = http.StatusServiceUnavailable
			msg = "Spec preview is at capacity. Try again in a few seconds."
		}
		writeProbeError(w, status, openapipreview.CodeRateLimited, msg, "")
		return
	}
	defer release()

	body, err := io.ReadAll(io.LimitReader(r.Body, previewRequestMaxBytes))
	if err != nil {
		writeProbeError(w, http.StatusBadRequest, openapipreview.CodeInvalidRequest,
			"Failed to read request body: "+err.Error(), "")
		return
	}
	var req openAPIPreviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeProbeError(w, http.StatusBadRequest, openapipreview.CodeInvalidRequest,
			"Invalid JSON: "+err.Error(), "")
		return
	}

	result, previewErr := s.openapiPreviewer.Preview(r.Context(), req.toPreviewRequest())
	if previewErr != nil {
		writeProbeError(w, previewFailureStatus(previewErr), previewErr.Code, previewErr.Message, previewErr.Hint)
		return
	}

	writeJSON(w, toPreviewResponse(result))
}

// previewFailureStatus maps a preview error code to an HTTP status. Unknown
// codes default to 422 — a semantically valid request whose operation failed.
func previewFailureStatus(e *openapipreview.Error) int {
	switch e.Code {
	case openapipreview.CodeInvalidRequest:
		return http.StatusBadRequest
	case openapipreview.CodeRateLimited:
		return http.StatusTooManyRequests
	case openapipreview.CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusUnprocessableEntity
	}
}

// openAPIPreviewRequest is the wire shape accepted by POST
// /api/openapi/operations. It mirrors the openapi block of the YAML schema with
// snake_case tags, keeping the wire contract local to the handler.
//
// There is deliberately no auth field: specs are fetched unauthenticated on the
// deployed path too, so accepting credentials here would imply a capability the
// gateway does not have.
type openAPIPreviewRequest struct {
	Spec string             `json:"spec"`
	TLS  *config.OpenAPITLS `json:"tls,omitempty"`
}

func (r openAPIPreviewRequest) toPreviewRequest() openapipreview.Request {
	out := openapipreview.Request{Spec: r.Spec}
	if r.TLS != nil {
		out.CertFile = r.TLS.CertFile
		out.KeyFile = r.TLS.KeyFile
		out.CAFile = r.TLS.CaFile
		out.InsecureSkipVerify = r.TLS.InsecureSkipVerify
	}
	return out
}

// openAPIOperationWire is one row of the picker.
//
// OperationID and ToolName are both present on purpose. The include/exclude
// filter matches OperationID (the raw spec value); ToolName is what the model
// actually sees. They differ whenever an operationId contains characters
// outside [a-zA-Z0-9_-], and a UI that showed only one of them would either
// write an ineffective filter or look like it was describing different tools.
type openAPIOperationWire struct {
	OperationID string   `json:"operation_id"`
	ToolName    string   `json:"tool_name"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Deprecated  bool     `json:"deprecated,omitempty"`
	Skipped     bool     `json:"skipped,omitempty"`
	SkipReason  string   `json:"skip_reason,omitempty"`
}

type openAPIPreviewResponse struct {
	Title        string                 `json:"title,omitempty"`
	Version      string                 `json:"version,omitempty"`
	Operations   []openAPIOperationWire `json:"operations"`
	SkippedCount int                    `json:"skipped_count"`
	LoadedAt     string                 `json:"loaded_at"`
	Cached       bool                   `json:"cached"`
}

func toPreviewResponse(result *openapipreview.Result) openAPIPreviewResponse {
	out := openAPIPreviewResponse{
		Title:      result.Title,
		Version:    result.Version,
		Operations: make([]openAPIOperationWire, 0, len(result.Operations)),
		LoadedAt:   result.LoadedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Cached:     result.Cached,
	}
	for _, op := range result.Operations {
		if op.Skipped {
			out.SkippedCount++
		}
		out.Operations = append(out.Operations, openAPIOperationWire{
			OperationID: op.OperationID,
			ToolName:    op.ToolName,
			Method:      op.Method,
			Path:        op.Path,
			Summary:     op.Summary,
			Description: op.Description,
			Tags:        op.Tags,
			Deprecated:  op.Deprecated,
			Skipped:     op.Skipped,
			SkipReason:  op.SkipReason,
		})
	}
	return out
}
