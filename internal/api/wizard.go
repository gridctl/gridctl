package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WizardDraft represents a saved wizard draft.
type WizardDraft struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	ResourceType string                 `json:"resourceType"`
	FormData     map[string]interface{} `json:"formData"`
	CreatedAt    string                 `json:"createdAt"`
	UpdatedAt    string                 `json:"updatedAt"`
}

// wizardDraftsDir returns the path to the wizard drafts directory.
func wizardDraftsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gridctl", "cache", "wizard-drafts")
}

// handleWizardDraftsList returns all saved wizard drafts.
// GET /api/wizard/drafts
func (s *Server) handleWizardDraftsList(w http.ResponseWriter, _ *http.Request) {
	dir := wizardDraftsDir()
	if dir == "" {
		writeJSON(w, []WizardDraft{})
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist yet — return empty list
		writeJSON(w, []WizardDraft{})
		return
	}

	var drafts []WizardDraft
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var draft WizardDraft
		if err := json.Unmarshal(data, &draft); err != nil {
			continue
		}
		drafts = append(drafts, draft)
	}

	// Sort by updated time, newest first
	sort.Slice(drafts, func(i, j int) bool {
		return drafts[i].UpdatedAt > drafts[j].UpdatedAt
	})

	if drafts == nil {
		drafts = []WizardDraft{}
	}
	writeJSON(w, drafts)
}

// handleWizardDraftCreate saves a new wizard draft.
// POST /api/wizard/drafts
func (s *Server) handleWizardDraftCreate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		Name         string                 `json:"name"`
		ResourceType string                 `json:"resourceType"`
		FormData     map[string]interface{} `json:"formData"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeJSONError(w, "Draft name is required", http.StatusBadRequest)
		return
	}

	dir := wizardDraftsDir()
	if dir == "" {
		writeJSONError(w, "Failed to determine drafts directory", http.StatusInternalServerError)
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSONError(w, "Failed to create drafts directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate unique ID
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		writeJSONError(w, "Failed to generate ID", http.StatusInternalServerError)
		return
	}
	id := hex.EncodeToString(idBytes)

	now := time.Now().UTC().Format(time.RFC3339)
	draft := WizardDraft{
		ID:           id,
		Name:         req.Name,
		ResourceType: req.ResourceType,
		FormData:     req.FormData,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		writeJSONError(w, "Failed to serialize draft", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(dir, id+".json")
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		writeJSONError(w, "Failed to save draft: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, draft)
}

// handleWizardDraftDelete removes a wizard draft.
// DELETE /api/wizard/drafts/{id}
func (s *Server) handleWizardDraftDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, "Draft ID is required", http.StatusBadRequest)
		return
	}

	// Sanitize ID to prevent path traversal
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		writeJSONError(w, "Invalid draft ID", http.StatusBadRequest)
		return
	}

	dir := wizardDraftsDir()
	if dir == "" {
		writeJSONError(w, "Failed to determine drafts directory", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(dir, id+".json")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, "Draft not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to delete draft: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
