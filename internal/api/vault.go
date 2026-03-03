package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/vault"
)

// validKeyRegex matches valid vault key names (same pattern as variable names).
var validKeyRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validSetNameRegex matches valid variable set names.
var validSetNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// handleVault routes all /api/vault requests.
func (s *Server) handleVault(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Vault not available", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/vault")
	path = strings.TrimPrefix(path, "/")

	// Split into segments for clean routing
	segments := strings.SplitN(path, "/", 3)
	first := segments[0]
	second := ""
	if len(segments) > 1 {
		second = segments[1]
	}

	// Routes that work regardless of lock state
	switch {
	case first == "status" && r.Method == http.MethodGet:
		s.handleVaultStatus(w, r)
		return
	case first == "unlock" && r.Method == http.MethodPost:
		s.handleVaultUnlock(w, r)
		return
	case first == "lock" && r.Method == http.MethodPost:
		s.handleVaultLock(w, r)
		return
	}

	// All other routes require the vault to be unlocked
	if s.vaultStore.IsLocked() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusLocked)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "vault is locked",
			"hint":  "POST /api/vault/unlock with passphrase",
		})
		return
	}

	switch {
	case first == "" && r.Method == http.MethodGet:
		s.handleVaultList(w, r)
	case first == "" && r.Method == http.MethodPost:
		s.handleVaultCreate(w, r)
	case first == "import" && r.Method == http.MethodPost:
		s.handleVaultImport(w, r)
	case first == "sets" && second == "" && r.Method == http.MethodGet:
		s.handleVaultSetsList(w, r)
	case first == "sets" && second == "" && r.Method == http.MethodPost:
		s.handleVaultSetsCreate(w, r)
	case first == "sets" && second != "" && r.Method == http.MethodDelete:
		s.handleVaultSetsDelete(w, r, second)
	case first != "" && second == "set" && r.Method == http.MethodPut:
		s.handleVaultAssignSet(w, r, first)
	case first != "" && first != "import" && first != "sets" && first != "status" && first != "unlock" && first != "lock":
		s.handleVaultKey(w, r, first)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVaultStatus returns the lock state and counts.
// GET /api/vault/status
func (s *Server) handleVaultStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"locked":    s.vaultStore.IsLocked(),
		"encrypted": s.vaultStore.IsEncrypted(),
	}

	if !s.vaultStore.IsLocked() {
		status["secrets_count"] = len(s.vaultStore.List())
		status["sets_count"] = len(s.vaultStore.ListSets())
	}

	writeJSON(w, status)
}

// handleVaultUnlock unlocks the vault with a passphrase.
// POST /api/vault/unlock
func (s *Server) handleVaultUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Passphrase == "" {
		writeJSONError(w, "Passphrase is required", http.StatusBadRequest)
		return
	}

	if !s.vaultStore.IsLocked() {
		writeJSON(w, map[string]string{"status": "already_unlocked"})
		return
	}

	if err := s.vaultStore.Unlock(req.Passphrase); err != nil {
		writeJSONError(w, "wrong passphrase or corrupted vault", http.StatusUnauthorized)
		return
	}

	writeJSON(w, map[string]string{"status": "unlocked"})
}

// handleVaultLock encrypts the vault with a passphrase.
// POST /api/vault/lock
func (s *Server) handleVaultLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Passphrase == "" {
		writeJSONError(w, "Passphrase is required", http.StatusBadRequest)
		return
	}

	if err := s.vaultStore.Lock(req.Passphrase); err != nil {
		writeJSONError(w, "Failed to lock vault: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "locked"})
}

// handleVaultList returns all vault keys with set assignments (no values).
// GET /api/vault
func (s *Server) handleVaultList(w http.ResponseWriter, r *http.Request) {
	secrets := s.vaultStore.List()

	type keyEntry struct {
		Key string `json:"key"`
		Set string `json:"set,omitempty"`
	}

	entries := make([]keyEntry, len(secrets))
	for i, sec := range secrets {
		entries[i] = keyEntry{Key: sec.Key, Set: sec.Set}
	}

	writeJSON(w, entries)
}

// handleVaultCreate creates a new secret.
// POST /api/vault
func (s *Server) handleVaultCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Set   string `json:"set,omitempty"`
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

	if req.Set != "" {
		if err := s.vaultStore.SetWithSet(req.Key, req.Value, req.Set); err != nil {
			writeJSONError(w, "Failed to save secret: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := s.vaultStore.Set(req.Key, req.Value); err != nil {
			writeJSONError(w, "Failed to save secret: "+err.Error(), http.StatusInternalServerError)
			return
		}
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

// handleVaultSetsList returns all variable sets with member counts.
// GET /api/vault/sets
func (s *Server) handleVaultSetsList(w http.ResponseWriter, r *http.Request) {
	sets := s.vaultStore.ListSets()
	if sets == nil {
		sets = []vault.SetSummary{}
	}
	writeJSON(w, sets)
}

// handleVaultSetsCreate creates a new variable set.
// POST /api/vault/sets
func (s *Server) handleVaultSetsCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		writeJSONError(w, "Name is required", http.StatusBadRequest)
		return
	}
	if !validSetNameRegex.MatchString(req.Name) {
		writeJSONError(w, "Invalid set name: must match [a-z0-9][a-z0-9-]*", http.StatusBadRequest)
		return
	}

	if err := s.vaultStore.CreateSet(req.Name); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSONError(w, err.Error(), http.StatusConflict)
		} else {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"name": req.Name, "status": "created"})
}

// handleVaultSetsDelete deletes a variable set.
// DELETE /api/vault/sets/{name}
func (s *Server) handleVaultSetsDelete(w http.ResponseWriter, r *http.Request, name string) {
	if err := s.vaultStore.DeleteSet(name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSONError(w, err.Error(), http.StatusNotFound)
		} else {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVaultAssignSet assigns or unassigns a secret to a set.
// PUT /api/vault/{key}/set
func (s *Server) handleVaultAssignSet(w http.ResponseWriter, r *http.Request, key string) {
	var req struct {
		Set string `json:"set"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.vaultStore.SetSecretSet(key, req.Set); err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]string{"key": key, "set": req.Set, "status": "updated"})
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
