package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/vault"
)

// validKeyRegex matches valid variable key names (same pattern as variable names).
var validKeyRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validSetNameRegex matches valid variable set names.
var validSetNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// writeLocked writes the standard 423 vault-locked response.
func writeLocked(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusLocked)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "vault is locked",
		"hint":  "POST /api/var/unlock with passphrase",
	})
}

// deprecatedVaultHandler wraps a handler so every response carries the
// standard `Deprecation: true` / `Sunset` / `Link` triple advertising the
// canonical `/api/var/*` path. The handler itself is unchanged — both
// surfaces share their backing handler functions.
func deprecatedVaultHandler(canonical string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Sunset", "Wed, 31 Dec 2025 23:59:59 GMT")
		w.Header().Set("Link", `</api/var>; rel="successor-version"`)
		_ = canonical // reserved for future per-route Link rewriting
		next(w, r)
	}
}

// handleVaultStatus returns the lock state and counts.
// GET /api/var/status (canonical) and /api/vault/status (deprecated).
func (s *Server) handleVaultStatus(w http.ResponseWriter, _ *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}

	status := map[string]any{
		"locked":    s.vaultStore.IsLocked(),
		"encrypted": s.vaultStore.IsEncrypted(),
	}

	if !s.vaultStore.IsLocked() {
		// variables_count is the canonical field; secrets_count is retained
		// as an alias for older UI builds during the rollout window.
		count := len(s.vaultStore.List())
		status["variables_count"] = count
		status["secrets_count"] = count
		status["sets_count"] = len(s.vaultStore.ListSets())
	}

	writeJSON(w, status)
}

// handleVaultUnlock unlocks the variable store with a passphrase.
// POST /api/var/unlock
func (s *Server) handleVaultUnlock(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}

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

// handleVaultLock encrypts the variable store with a passphrase.
// POST /api/var/lock
func (s *Server) handleVaultLock(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}

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
		writeJSONError(w, "Failed to lock variable store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "locked"})
}

// variableEntry is the wire shape for /api/var list and detail responses.
type variableEntry struct {
	Key      string             `json:"key"`
	Type     vault.VariableType `json:"type"`
	IsSecret bool               `json:"is_secret"`
	Set      string             `json:"set,omitempty"`
}

// handleVaultList returns all variables with type and visibility (no values).
// GET /api/var
func (s *Server) handleVaultList(w http.ResponseWriter, _ *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	vars := s.vaultStore.List()
	entries := make([]variableEntry, len(vars))
	for i, v := range vars {
		entries[i] = variableEntry{
			Key:      v.Key,
			Type:     v.Type,
			IsSecret: v.IsSecret,
			Set:      v.Set,
		}
	}

	writeJSON(w, entries)
}

// handleVaultCreate creates a new variable.
// POST /api/var
func (s *Server) handleVaultCreate(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	// Decode into a pointer-bool struct so omitted is_secret defaults to
	// true (Article XII secure default) rather than Go's zero-value false.
	var req struct {
		Key      string             `json:"key"`
		Value    string             `json:"value"`
		Type     vault.VariableType `json:"type"`
		IsSecret *bool              `json:"is_secret"`
		Set      string             `json:"set,omitempty"`
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

	if req.Type == "" {
		req.Type = vault.TypeString
	}
	if !vault.IsValidType(req.Type) {
		writeJSONError(w, "Invalid type: "+string(req.Type), http.StatusBadRequest)
		return
	}

	isSecret := true // Article XII: default secret.
	if req.IsSecret != nil {
		isSecret = *req.IsSecret
	}

	v := vault.Variable{
		Key:      req.Key,
		Value:    req.Value,
		Type:     req.Type,
		IsSecret: isSecret,
		Set:      req.Set,
	}
	if err := s.vaultStore.SetVariable(v); err != nil {
		writeJSONError(w, "Failed to save variable: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{
		"key":       req.Key,
		"type":      req.Type,
		"is_secret": isSecret,
		"status":    "created",
	})
}

// handleVaultKeyGet returns the value of a variable.
// GET /api/var/{key}
func (s *Server) handleVaultKeyGet(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	key := r.PathValue("key")
	v, ok := s.vaultStore.GetVariable(key)
	if !ok {
		writeJSONError(w, "Variable not found: "+key, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"key":       v.Key,
		"value":     v.Value,
		"type":      v.Type,
		"is_secret": v.IsSecret,
		"set":       v.Set,
	})
}

// handleVaultKeyPut updates a variable. Accepts partial updates: keys not
// present in the request body preserve their stored values. This is the
// upsert path the UI uses for inline edits.
//
// PUT /api/var/{key}
func (s *Server) handleVaultKeyPut(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	key := r.PathValue("key")
	var req struct {
		Value    *string             `json:"value"`
		Type     *vault.VariableType `json:"type"`
		IsSecret *bool               `json:"is_secret"`
		Set      *string             `json:"set"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	existing, exists := s.vaultStore.GetVariable(key)
	if !exists {
		// Create-on-PUT preserves the historic PUT semantics (the old API
		// upserted unconditionally).
		existing = vault.Variable{Key: key, Type: vault.TypeString, IsSecret: true}
	}

	if req.Value != nil {
		existing.Value = *req.Value
	}
	if req.Type != nil {
		if *req.Type == "" {
			existing.Type = vault.TypeString
		} else {
			if !vault.IsValidType(*req.Type) {
				writeJSONError(w, "Invalid type: "+string(*req.Type), http.StatusBadRequest)
				return
			}
			existing.Type = *req.Type
		}
	}
	if req.IsSecret != nil {
		existing.IsSecret = *req.IsSecret
	}
	if req.Set != nil {
		existing.Set = *req.Set
	}

	if err := s.vaultStore.SetVariable(existing); err != nil {
		writeJSONError(w, "Failed to update variable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"key":       key,
		"type":      existing.Type,
		"is_secret": existing.IsSecret,
		"status":    "updated",
	})
}

// handleVaultKeyDelete deletes a variable.
// DELETE /api/var/{key}
func (s *Server) handleVaultKeyDelete(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	key := r.PathValue("key")
	if err := s.vaultStore.Delete(key); err != nil {
		writeJSONError(w, "Failed to delete variable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleVaultSetsList returns all variable sets with member counts.
// GET /api/var/sets
func (s *Server) handleVaultSetsList(w http.ResponseWriter, _ *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	sets := s.vaultStore.ListSets()
	if sets == nil {
		sets = []vault.SetSummary{}
	}
	writeJSON(w, sets)
}

// handleVaultSetsCreate creates a new variable set.
// POST /api/var/sets
func (s *Server) handleVaultSetsCreate(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

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
// DELETE /api/var/sets/{name}
func (s *Server) handleVaultSetsDelete(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	name := r.PathValue("name")
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

// handleVaultAssignSet assigns or unassigns a variable to a set.
// PUT /api/var/{key}/set
func (s *Server) handleVaultAssignSet(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	key := r.PathValue("key")
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

// handleVaultImport bulk imports variables.
// POST /api/var/import
func (s *Server) handleVaultImport(w http.ResponseWriter, r *http.Request) {
	if s.vaultStore == nil {
		writeJSONError(w, "Variable store not available", http.StatusServiceUnavailable)
		return
	}
	if s.vaultStore.IsLocked() {
		writeLocked(w)
		return
	}

	// Accept either the legacy {"secrets": {KEY:VAL,...}} map (defaults
	// every entry to secret/string) or the new {"variables": [...]} shape
	// that preserves per-entry metadata.
	var req struct {
		Secrets   map[string]string `json:"secrets"`
		Variables []vault.Variable  `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	var count int
	switch {
	case len(req.Variables) > 0:
		c, err := s.vaultStore.ImportVariables(req.Variables)
		if err != nil {
			writeJSONError(w, "Failed to import variables: "+err.Error(), http.StatusInternalServerError)
			return
		}
		count = c
	case len(req.Secrets) > 0:
		c, err := s.vaultStore.Import(req.Secrets)
		if err != nil {
			writeJSONError(w, "Failed to import variables: "+err.Error(), http.StatusInternalServerError)
			return
		}
		count = c
	default:
		writeJSONError(w, "No variables provided", http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]any{"imported": count})
}
