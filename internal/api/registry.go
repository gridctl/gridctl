package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"

	"github.com/gridctl/gridctl/pkg/registry"
)

// handleRegistryStatus returns registry summary counts.
// GET /api/registry/status
func (s *Server) handleRegistryStatus(w http.ResponseWriter, _ *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, s.registryServer.Store().Status())
}

// handleRegistrySkillsList returns all skills.
// GET /api/registry/skills
func (s *Server) handleRegistrySkillsList(w http.ResponseWriter, _ *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	skills := s.registryServer.Store().ListSkills()
	if skills == nil {
		skills = []*registry.AgentSkill{}
	}
	writeJSON(w, skills)
}

// handleRegistrySkillCreate creates a new skill.
// POST /api/registry/skills
func (s *Server) handleRegistrySkillCreate(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	var sk registry.AgentSkill
	if err := json.NewDecoder(r.Body).Decode(&sk); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := sk.Validate(); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Check name uniqueness
	if _, err := s.registryServer.Store().GetSkill(sk.Name); err == nil {
		writeJSONError(w, "Skill already exists: "+sk.Name, http.StatusConflict)
		return
	}
	if err := s.registryServer.Store().SaveSkill(&sk); err != nil {
		writeJSONError(w, "Failed to save skill: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshRegistryRouter()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, sk)
}

// handleRegistrySkillGet returns a single skill.
// GET /api/registry/skills/{name}
func (s *Server) handleRegistrySkillGet(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	sk, err := s.registryServer.Store().GetSkill(name)
	if err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	writeJSON(w, sk)
}

// handleRegistrySkillPut updates a skill.
// PUT /api/registry/skills/{name}
func (s *Server) handleRegistrySkillPut(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	var sk registry.AgentSkill
	if err := json.NewDecoder(r.Body).Decode(&sk); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	sk.Name = name // URL path takes precedence
	if err := sk.Validate(); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.registryServer.Store().GetSkill(name); err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	if err := s.registryServer.Store().SaveSkill(&sk); err != nil {
		writeJSONError(w, "Failed to save skill: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshRegistryRouter()
	writeJSON(w, sk)
}

// handleRegistrySkillDelete deletes a skill.
// DELETE /api/registry/skills/{name}
func (s *Server) handleRegistrySkillDelete(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if err := s.registryServer.Store().DeleteSkill(name); err != nil {
		writeJSONError(w, "Failed to delete skill: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshRegistryRouter()
	w.WriteHeader(http.StatusNoContent)
}

// handleRegistrySkillActivate activates a skill.
// POST /api/registry/skills/{name}/activate
func (s *Server) handleRegistrySkillActivate(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	s.handleRegistrySkillStateChange(w, r.PathValue("name"), "activate")
}

// handleRegistrySkillDisable disables a skill.
// POST /api/registry/skills/{name}/disable
func (s *Server) handleRegistrySkillDisable(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	s.handleRegistrySkillStateChange(w, r.PathValue("name"), "disable")
}

// handleRegistrySkillStateChange updates a skill's state to active or disabled.
func (s *Server) handleRegistrySkillStateChange(w http.ResponseWriter, name, action string) {
	sk, err := s.registryServer.Store().GetSkill(name)
	if err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	switch action {
	case "activate":
		sk.State = registry.StateActive
	case "disable":
		sk.State = registry.StateDisabled
	}
	if err := s.registryServer.Store().SaveSkill(sk); err != nil {
		writeJSONError(w, "Failed to update state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshRegistryRouter()
	writeJSON(w, sk)
}

// setRegistrySkillsBatchRequest is the wire shape for PUT /api/registry/skills/batch.
type setRegistrySkillsBatchRequest struct {
	Skills []batchSkillStateEntry `json:"skills"`
}

type batchSkillStateEntry struct {
	Name  string             `json:"name"`
	State registry.ItemState `json:"state"`
}

// batchSkillResult reports one skill's applied state in a batch success.
type batchSkillResult struct {
	Name  string             `json:"name"`
	State registry.ItemState `json:"state"`
}

type setRegistrySkillsBatchResponse struct {
	Skills []batchSkillResult `json:"skills"`
}

// handleRegistrySkillsBatch sets the state of MULTIPLE skills in one request,
// then refreshes the registry router once instead of once per skill.
//
// Transaction semantics are all-or-nothing on validation: every entry is
// checked up front (known skill, and a target state of active or disabled)
// before any write, so an unknown skill (404) or an invalid state (400) rejects
// the whole batch with nothing changed. Only active and disabled are accepted
// (bulk actions enable or disable; they never set draft). The write phase
// itself is best-effort rather than transactional: a mid-batch SaveSkill
// failure (500) can leave earlier entries persisted, matching the per-skill
// endpoint's non-atomic behavior across calls. Unlike the MCP tools batch, the
// registry store has no external-edit/reload model, so there is no 409 path.
//
// PUT /api/registry/skills/batch
func (s *Server) handleRegistrySkillsBatch(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	var req setRegistrySkillsBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Skills) == 0 {
		writeJSONError(w, "Request body must include a non-empty skills array", http.StatusBadRequest)
		return
	}

	// Validate and resolve every entry before any write touches disk.
	store := s.registryServer.Store()
	updates := make([]*registry.AgentSkill, 0, len(req.Skills))
	seen := make(map[string]struct{}, len(req.Skills))
	for _, entry := range req.Skills {
		if entry.Name == "" {
			writeJSONError(w, "Each skill entry must include a name", http.StatusBadRequest)
			return
		}
		if _, dup := seen[entry.Name]; dup {
			writeJSONError(w, "Duplicate skill in batch: "+entry.Name, http.StatusBadRequest)
			return
		}
		seen[entry.Name] = struct{}{}

		if entry.State != registry.StateActive && entry.State != registry.StateDisabled {
			writeJSONError(w, "Skill "+entry.Name+" has invalid state; must be active or disabled", http.StatusBadRequest)
			return
		}
		sk, err := store.GetSkill(entry.Name)
		if err != nil {
			writeJSONError(w, "Skill not found: "+entry.Name, http.StatusNotFound)
			return
		}
		sk.State = entry.State
		updates = append(updates, sk)
	}

	// All entries validated; apply the writes, then refresh once.
	for _, sk := range updates {
		if err := store.SaveSkill(sk); err != nil {
			writeJSONError(w, "Failed to update skill "+sk.Name+": "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.refreshRegistryRouter()

	resp := setRegistrySkillsBatchResponse{Skills: make([]batchSkillResult, 0, len(updates))}
	for _, sk := range updates {
		resp.Skills = append(resp.Skills, batchSkillResult{Name: sk.Name, State: sk.State})
	}
	writeJSON(w, resp)
}

// handleRegistrySkillFileList lists files in a skill directory.
// GET /api/registry/skills/{name}/files
func (s *Server) handleRegistrySkillFileList(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if _, err := s.registryServer.Store().GetSkill(name); err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	files, err := s.registryServer.Store().ListFiles(name)
	if err != nil {
		writeJSONError(w, "Failed to list files: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if files == nil {
		files = []registry.SkillFile{}
	}
	writeJSON(w, files)
}

// handleRegistrySkillFileGet reads a file from a skill directory.
// GET /api/registry/skills/{name}/files/{path...}
func (s *Server) handleRegistrySkillFileGet(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	filePath := r.PathValue("path")
	if filePath == "" {
		http.NotFound(w, r)
		return
	}
	if _, err := s.registryServer.Store().GetSkill(name); err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	data, err := s.registryServer.Store().ReadFile(name, filePath)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			writeJSONError(w, "File not found: "+filePath, http.StatusNotFound)
		} else {
			writeJSONError(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", detectContentType(filePath))
	_, _ = w.Write(data)
}

// handleRegistrySkillFilePut writes a file in a skill directory.
// PUT /api/registry/skills/{name}/files/{path...}
func (s *Server) handleRegistrySkillFilePut(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	filePath := r.PathValue("path")
	if filePath == "" {
		http.NotFound(w, r)
		return
	}
	if _, err := s.registryServer.Store().GetSkill(name); err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSONError(w, "Failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.registryServer.Store().WriteFile(name, filePath, data); err != nil {
		writeJSONError(w, "Failed to write file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRegistrySkillFileDelete deletes a file from a skill directory.
// DELETE /api/registry/skills/{name}/files/{path...}
func (s *Server) handleRegistrySkillFileDelete(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	filePath := r.PathValue("path")
	if filePath == "" {
		http.NotFound(w, r)
		return
	}
	if _, err := s.registryServer.Store().GetSkill(name); err != nil {
		writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
		return
	}
	if err := s.registryServer.Store().DeleteFile(name, filePath); err != nil {
		writeJSONError(w, "Failed to delete file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// detectContentType returns a MIME type based on file extension.
func detectContentType(path string) string {
	switch filepath.Ext(path) {
	case ".md":
		return "text/markdown"
	case ".sh":
		return "text/x-shellscript"
	case ".py":
		return "text/x-python"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".csv":
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

// handleRegistryValidate validates SKILL.md content without saving.
// POST /api/registry/skills/validate
func (s *Server) handleRegistryValidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"` // Raw SKILL.md content
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	skill, err := registry.ParseSkillMD([]byte(req.Content))
	if err != nil {
		writeJSON(w, map[string]any{
			"valid":    false,
			"errors":   []string{"Failed to parse SKILL.md: " + err.Error()},
			"warnings": []string{},
		})
		return
	}

	result := registry.ValidateSkillFull(skill)
	writeJSON(w, map[string]any{
		"valid":    result.Valid(),
		"errors":   result.Errors,
		"warnings": result.Warnings,
		"parsed":   skill,
	})
}

// refreshRegistryRouter refreshes the registry and re-registers with the gateway router.
// This handles progressive disclosure: if the registry gains content, it registers;
// if all content is removed, the registry is deregistered.
func (s *Server) refreshRegistryRouter() {
	if s.registryServer == nil {
		return
	}
	_ = s.registryServer.RefreshTools(context.Background())
	if s.registryServer.HasContent() {
		s.gateway.Router().AddClient(s.registryServer)
	} else {
		s.gateway.Router().RemoveClient("registry")
	}
	s.gateway.Router().RefreshTools()
}
