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

	"github.com/gridctl/gridctl/pkg/mcp"
)

// setClientModelRequest is the wire shape for PUT /api/clients/{slug}/model.
// Model is the pricing model ID for this client (e.g. "claude-opus-4-7");
// an empty string removes the client's entry from client_models. Unknown
// model IDs are accepted — pricing is best-effort and validation surfaces
// them as load-time warnings, never hard errors.
type setClientModelRequest struct {
	Model string `json:"model"`
}

// setClientModelResponse is the success payload.
type setClientModelResponse struct {
	Client     string `json:"client"`
	ProfileKey string `json:"profileKey"`
	Model      string `json:"model"`
	Reloaded   bool   `json:"reloaded"`
	ReloadedAt string `json:"reloadedAt,omitempty"`
}

// handleSetClientModel writes a single client's pricing model into the live
// stack YAML's top-level `client_models:` map and triggers a hot reload.
// This is deliberately a separate path from the access-scope endpoint: a
// model declaration is pricing attribution only and must never create or
// touch a `clients:` block (whose presence flips the stack into default-deny
// access semantics). The YAML write is atomic and conflict-detected: an
// external edit between read and write surfaces as 409 so the UI can
// re-fetch.
//
// PUT /api/clients/{slug}/model
func (s *Server) handleSetClientModel(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		writeJSONError(w, "Client slug is required", http.StatusBadRequest)
		return
	}
	clientKey := mcp.NormalizeClientID(slug)
	if clientKey == "" {
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
	var req setClientModelRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	switch err := setClientModel(s.stackFile, clientKey, req.Model); {
	case err == nil:
		// proceed
	case errors.Is(err, errStackModified):
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	default:
		slog.Default().Warn("client model write failed", "client", clientKey, "error", err)
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Default().Info("client pricing model updated", "client", clientKey, "model", req.Model)

	resp := setClientModelResponse{
		Client:     slug,
		ProfileKey: clientKey,
		Model:      req.Model,
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

// setClientModel writes (or removes, when model is empty) the pricing model
// for clientKey in the stack YAML's top-level `client_models:` map at path.
// It mirrors setClientScope: it serializes concurrent callers on the same
// path, detects external edits via a pre-read hash vs. pre-write re-read,
// and writes atomically. Comments, ordering, and unrelated keys survive the
// yaml.Node round-trip.
func setClientModel(path, clientKey, model string) error {
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

	updated, err := patchClientModels(original, clientKey, model)
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

// patchClientModels rewrites the YAML source so the top-level
// `client_models:` map carries (or drops) the given client's model. An empty
// model deletes the client's key — never writing `model: ""` — and removes
// the whole `client_models:` mapping when it empties, so a fully cleared
// stack round-trips back to its pre-feature shape. The `clients:` access
// block is never touched.
func patchClientModels(source []byte, clientKey, model string) ([]byte, error) {
	if clientKey == "" {
		return nil, fmt.Errorf("client key must not be empty")
	}

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
		// Removal path: nothing to do when the map (or the key) is absent.
		modelsNode := findMappingValue(doc, "client_models")
		if modelsNode != nil && modelsNode.Kind == yaml.MappingNode {
			removeMappingKey(modelsNode, clientKey)
			if len(modelsNode.Content) == 0 {
				removeMappingKey(doc, "client_models")
			}
		}
	} else {
		modelsNode, err := ensureChildMapping(doc, "client_models")
		if err != nil {
			return nil, err
		}
		replaceOrInsertScalar(modelsNode, clientKey, model)
	}

	return encodeStackYAML(&root)
}

// replaceOrInsertScalar sets mapping[key] to a string scalar, replacing an
// existing value or appending the key.
func replaceOrInsertScalar(mapping *yaml.Node, key, value string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = scalarNode(value)
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), scalarNode(value))
}

// removeMappingKey deletes key (and its value) from a mapping node. No-op
// when the key is absent.
func removeMappingKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
