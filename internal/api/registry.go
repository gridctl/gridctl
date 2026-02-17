package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// handleRegistry routes all /api/registry/ requests.
func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/registry/")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	switch {
	case path == "status":
		s.handleRegistryStatus(w, r)
	case path == "skills":
		s.handleRegistrySkillsList(w, r)
	case path == "skills/validate":
		s.handleRegistryValidate(w, r)
	case strings.HasPrefix(path, "skills/"):
		s.handleRegistrySkillAction(w, r, strings.TrimPrefix(path, "skills/"))
	default:
		http.NotFound(w, r)
	}
}

// handleRegistryStatus returns registry summary counts.
// GET /api/registry/status
func (s *Server) handleRegistryStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.registryServer.Store().Status())
}

// handleRegistrySkillsList handles GET (list) and POST (create) for skills.
// GET  /api/registry/skills
// POST /api/registry/skills
func (s *Server) handleRegistrySkillsList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		skills := s.registryServer.Store().ListSkills()
		if skills == nil {
			skills = []*registry.AgentSkill{}
		}
		writeJSON(w, skills)

	case http.MethodPost:
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

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRegistrySkillAction handles individual skill operations.
// GET    /api/registry/skills/{name}
// PUT    /api/registry/skills/{name}
// DELETE /api/registry/skills/{name}
// POST   /api/registry/skills/{name}/activate
// POST   /api/registry/skills/{name}/disable
// GET    /api/registry/skills/{name}/files
// GET    /api/registry/skills/{name}/files/{path}
// PUT    /api/registry/skills/{name}/files/{path}
// DELETE /api/registry/skills/{name}/files/{path}
func (s *Server) handleRegistrySkillAction(w http.ResponseWriter, r *http.Request, subpath string) {
	parts := strings.SplitN(subpath, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	// State transitions
	if action == "activate" || action == "disable" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRegistrySkillStateChange(w, name, action)
		return
	}

	// File management
	if action == "files" || strings.HasPrefix(action, "files/") {
		filePath := ""
		if strings.HasPrefix(action, "files/") {
			filePath = strings.TrimPrefix(action, "files/")
		}
		s.handleRegistrySkillFiles(w, r, name, filePath)
		return
	}

	// CRUD on the skill itself
	switch r.Method {
	case http.MethodGet:
		sk, err := s.registryServer.Store().GetSkill(name)
		if err != nil {
			writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
			return
		}
		writeJSON(w, sk)

	case http.MethodPut:
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

	case http.MethodDelete:
		if err := s.registryServer.Store().DeleteSkill(name); err != nil {
			writeJSONError(w, "Failed to delete skill: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.refreshRegistryRouter()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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

// handleRegistrySkillFiles handles file management within a skill directory.
// GET    /api/registry/skills/{name}/files          — list files
// GET    /api/registry/skills/{name}/files/{path}   — read file
// PUT    /api/registry/skills/{name}/files/{path}   — write file
// DELETE /api/registry/skills/{name}/files/{path}   — delete file
func (s *Server) handleRegistrySkillFiles(w http.ResponseWriter, r *http.Request, skillName, filePath string) {
	// Verify skill exists
	if _, err := s.registryServer.Store().GetSkill(skillName); err != nil {
		writeJSONError(w, "Skill not found: "+skillName, http.StatusNotFound)
		return
	}

	if filePath == "" {
		// GET /api/registry/skills/{name}/files — list files
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		files, err := s.registryServer.Store().ListFiles(skillName)
		if err != nil {
			writeJSONError(w, "Failed to list files: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if files == nil {
			files = []registry.SkillFile{}
		}
		writeJSON(w, files)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// GET /api/registry/skills/{name}/files/{path} — read file
		data, err := s.registryServer.Store().ReadFile(skillName, filePath)
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

	case http.MethodPut:
		// PUT /api/registry/skills/{name}/files/{path} — write file
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
		if err != nil {
			writeJSONError(w, "Failed to read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.registryServer.Store().WriteFile(skillName, filePath, data); err != nil {
			writeJSONError(w, "Failed to write file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodDelete:
		// DELETE /api/registry/skills/{name}/files/{path} — delete file
		if err := s.registryServer.Store().DeleteFile(skillName, filePath); err != nil {
			writeJSONError(w, "Failed to delete file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
