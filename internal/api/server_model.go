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

// setServerModelRequest is the wire shape for PUT /api/mcp-servers/{name}/model.
// Model is the pricing model ID for this server (e.g. "claude-opus-4-7"); an
// empty string removes the server's model: key so it falls back to
// gateway.default_model (or no attribution). Unknown model IDs are accepted —
// pricing is best-effort and validation surfaces them as load-time warnings,
// never hard errors.
type setServerModelRequest struct {
	Model string `json:"model"`
}

// setServerModelResponse is the success payload.
type setServerModelResponse struct {
	Server     string `json:"server"`
	Model      string `json:"model"`
	Reloaded   bool   `json:"reloaded"`
	ReloadedAt string `json:"reloadedAt,omitempty"`
}

// handleSetServerModel writes a single MCP server's pricing model into the
// live stack YAML and triggers a hot reload. Like the client-model endpoint
// this is pricing attribution only: it touches the server's model: scalar
// and nothing else. The YAML write is atomic and conflict-detected: an
// external edit between read and write surfaces as 409 so the UI can
// re-fetch.
//
// PUT /api/mcp-servers/{name}/model
func (s *Server) handleSetServerModel(w http.ResponseWriter, r *http.Request) {
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
	var req setServerModelRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	switch err := setServerModel(s.stackFile, serverName, req.Model); {
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
		slog.Default().Warn("server model write failed", "server", serverName, "error", err)
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Default().Info("server pricing model updated", "server", serverName, "model", req.Model)

	resp := setServerModelResponse{
		Server: serverName,
		Model:  req.Model,
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

// setServerModel writes (or removes, when model is empty) the pricing model
// for the named server in the stack YAML at path. It mirrors setClientModel:
// it serializes concurrent callers on the same path, detects external edits
// via a pre-read hash vs. pre-write re-read, and writes atomically. Comments,
// ordering, and unrelated keys survive the yaml.Node round-trip.
func setServerModel(path, serverName, model string) error {
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

	updated, err := patchServerModel(original, serverName, model)
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

// patchServerModel rewrites the YAML source so the named entry in the
// mcp-servers: sequence carries (or drops) the given model: scalar. An empty
// model deletes the key — never writing `model: ""` — so a cleared server
// round-trips back to its pre-attribution shape. Returns errServerNotFound
// when the server is absent from the stack.
func patchServerModel(source []byte, serverName, model string) ([]byte, error) {
	if serverName == "" {
		return nil, fmt.Errorf("server name must not be empty")
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

	serversSeq := findMappingValue(doc, "mcp-servers")
	if serversSeq == nil || serversSeq.Kind != yaml.SequenceNode {
		return nil, errServerNotFound
	}

	var target *yaml.Node
	for _, entry := range serversSeq.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if nameNode := findMappingValue(entry, "name"); nameNode != nil && nameNode.Value == serverName {
			target = entry
			break
		}
	}
	if target == nil {
		return nil, errServerNotFound
	}

	if model == "" {
		removeMappingKey(target, "model")
	} else {
		replaceOrInsertScalar(target, "model", model)
	}

	return encodeStackYAML(&root)
}
