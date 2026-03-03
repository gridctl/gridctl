package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// validKeyRegex matches valid vault key names (same pattern as variable names).
var validKeyRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// handleVault routes all /api/vault requests.
func (s *Server) handleVault(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Vault not available", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/vault")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.handleVaultList(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.handleVaultCreate(w, r)
	case path == "import" && r.Method == http.MethodPost:
		s.handleVaultImport(w, r)
	case path != "" && path != "import":
		s.handleVaultKey(w, r, path)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVaultList returns all vault keys (no values).
// GET /api/vault
func (s *Server) handleVaultList(w http.ResponseWriter, r *http.Request) {
	keys := s.vaultStore.Keys()
	if keys == nil {
		keys = []string{}
	}

	type keyEntry struct {
		Key string `json:"key"`
	}

	entries := make([]keyEntry, len(keys))
	for i, k := range keys {
		entries[i] = keyEntry{Key: k}
	}

	writeJSON(w, entries)
}

// handleVaultCreate creates a new secret.
// POST /api/vault
func (s *Server) handleVaultCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Key == "" {
		writeJSONError(w, "Key is required", http.StatusBadRequest)
		return
	}
	if !validKeyRegex.MatchString(req.Key) {
		writeJSONError(w, "Invalid key name: must match [a-zA-Z_][a-zA-Z0-9_]*", http.StatusBadRequest)
		return
	}

	if err := s.vaultStore.Set(req.Key, req.Value); err != nil {
		writeJSONError(w, "Failed to save secret: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"key": req.Key, "status": "created"})
}

// handleVaultKey handles individual key operations.
// GET    /api/vault/{key}
// PUT    /api/vault/{key}
// DELETE /api/vault/{key}
func (s *Server) handleVaultKey(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodGet:
		value, ok := s.vaultStore.Get(key)
		if !ok {
			writeJSONError(w, "Secret not found: "+key, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"key": key, "value": value})

	case http.MethodPut:
		var req struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.vaultStore.Set(key, req.Value); err != nil {
			writeJSONError(w, "Failed to update secret: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"key": key, "status": "updated"})

	case http.MethodDelete:
		if err := s.vaultStore.Delete(key); err != nil {
			writeJSONError(w, "Failed to delete secret: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVaultImport bulk imports secrets.
// POST /api/vault/import
func (s *Server) handleVaultImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.Secrets) == 0 {
		writeJSONError(w, "No secrets provided", http.StatusBadRequest)
		return
	}

	count, err := s.vaultStore.Import(req.Secrets)
	if err != nil {
		writeJSONError(w, "Failed to import secrets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"imported": count})
}
