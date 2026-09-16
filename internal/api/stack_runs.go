package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"
)

type stackRunsRequest struct {
	Enabled    *bool `json:"enabled,omitempty"`
	OmitLabels *bool `json:"omit_labels,omitempty"`
	Retention  *struct {
		MaxSizeMB  *int `json:"max_size_mb,omitempty"`
		MaxAgeDays *int `json:"max_age_days,omitempty"`
	} `json:"retention,omitempty"`
}

func (s *Server) handlePatchStackRuns(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, telemetryRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req stackRunsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Enabled == nil && req.OmitLabels == nil && req.Retention == nil {
		writeJSONError(w, "Request must include enabled, omit_labels, or retention", http.StatusBadRequest)
		return
	}

	mu := stackFileLock(s.stackFile)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	originalHash := sha256.Sum256(original)
	updated, err := patchStackRuns(original, req)
	if err != nil {
		writeJSONError(w, "Failed to patch stack: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.validatePatchedStack(w, updated, "runs patch"); err != nil {
		return
	}
	current, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to re-read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if sha256.Sum256(current) != originalHash {
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	}
	if err := atomicWrite(s.stackFile, updated); err != nil {
		writeJSONError(w, "Failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if handler := s.ReloadHandler(); handler != nil {
		if result, err := handler.Reload(r.Context()); err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		} else if !result.Success {
			if writePreflightFailure(w, result) {
				return
			}
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				result.Message,
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
	}
	writeJSON(w, map[string]any{"success": true})
}

func patchStackRuns(source []byte, req stackRunsRequest) ([]byte, error) {
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
	runsNode := findOrCreateMapping(doc, "runs")
	if runsNode == nil {
		return nil, fmt.Errorf("locate runs mapping")
	}
	if req.Enabled != nil {
		setMappingBool(runsNode, "enabled", *req.Enabled)
	}
	if req.OmitLabels != nil {
		setMappingBool(runsNode, "omit_labels", *req.OmitLabels)
	}
	if req.Retention != nil {
		ret := findOrCreateMapping(runsNode, "retention")
		if ret != nil {
			if req.Retention.MaxSizeMB != nil {
				setMappingInt(ret, "max_size_mb", *req.Retention.MaxSizeMB)
			}
			if req.Retention.MaxAgeDays != nil {
				setMappingInt(ret, "max_age_days", *req.Retention.MaxAgeDays)
			}
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
