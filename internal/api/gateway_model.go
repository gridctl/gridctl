package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// setDefaultModelRequest is the wire shape for PUT /api/gateway/default-model.
// Model is the stack-wide fallback pricing model ID; an empty string removes
// gateway.default_model. Unknown model IDs are accepted — pricing is
// best-effort and validation surfaces them as load-time warnings, never hard
// errors.
type setDefaultModelRequest struct {
	Model string `json:"model"`
}

// setDefaultModelResponse is the success payload.
type setDefaultModelResponse struct {
	Model      string `json:"model"`
	Reloaded   bool   `json:"reloaded"`
	ReloadedAt string `json:"reloadedAt,omitempty"`
}

// handleSetDefaultModel writes gateway.default_model into the live stack YAML
// and triggers a hot reload. Pricing attribution only: the gateway: block is
// created when absent and removed again when clearing default_model empties
// a block this endpoint created, so a fully cleared stack round-trips back to
// its pre-feature shape. The YAML write is atomic and conflict-detected: an
// external edit between read and write surfaces as 409 so the UI can
// re-fetch.
//
// PUT /api/gateway/default-model
func (s *Server) handleSetDefaultModel(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, setToolsRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req setDefaultModelRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	switch err := setGatewayDefaultModel(s.stackFile, req.Model); {
	case err == nil:
		// proceed
	case errors.Is(err, errStackModified):
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	default:
		slog.Default().Warn("gateway default model write failed", "error", err)
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Default().Info("gateway default pricing model updated", "model", req.Model)

	resp := setDefaultModelResponse{Model: req.Model}

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

// setGatewayDefaultModel writes (or removes, when model is empty) the
// gateway.default_model scalar in the stack YAML at path. Same atomic
// read-verify-write discipline as setClientModel and setServerModel.
func setGatewayDefaultModel(path, model string) error {
	if path == "" {
		return errStackFileEmpty
	}

	mu := stackFileLock(path)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read stack file: %w", err)
	}
	originalHash := sha256.Sum256(original)

	updated, err := patchGatewayDefaultModel(original, model)
	if err != nil {
		return err
	}

	fireBetweenReadsHook()

	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read stack file: %w", err)
	}
	if sha256.Sum256(current) != originalHash {
		return errStackModified
	}

	return atomicWrite(path, updated)
}

// patchGatewayDefaultModel rewrites the YAML source so the gateway: block
// carries (or drops) default_model. An empty model deletes the key — never
// writing `default_model: ""` — and removes the gateway: block only when the
// key removal emptied it, so a gateway block that carried only default_model
// round-trips away cleanly while a block with other keys (auth, code_mode,
// ...) is left intact. Clearing when nothing is set is a no-op.
func patchGatewayDefaultModel(source []byte, model string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return nil, fmt.Errorf("parse stack yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("parse stack yaml: not a document")
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse stack yaml: top-level not a mapping")
	}

	if model == "" {
		gw := findMappingValue(doc, "gateway")
		if gw == nil || gw.Kind != yaml.MappingNode {
			// Absent (or a bare `gateway:` null) — nothing to clear. A bare
			// block was not created by this endpoint, so it is left alone.
			return encodeStackYAML(&root)
		}
		hadKey := findMappingValue(gw, "default_model") != nil
		removeMappingKey(gw, "default_model")
		// Drop the block only when removing OUR key emptied it: that shape
		// is exactly what the set path creates from scratch. A pre-existing
		// empty mapping (no key removed) stays untouched.
		if hadKey && len(gw.Content) == 0 {
			removeMappingKey(doc, "gateway")
		}
	} else {
		gw, err := ensureChildMapping(doc, "gateway")
		if err != nil {
			return nil, err
		}
		replaceOrInsertScalar(gw, "default_model", model)
	}

	return encodeStackYAML(&root)
}
