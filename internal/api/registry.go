package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// handleRegistry routes all /api/registry/ requests.
func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/registry/")

	// Check if registry server is available.
	// The gateway builder always creates a registry server instance (even when
	// ~/.gridctl/registry/ is empty), so this nil check is a defensive guard
	// for edge cases where the API server is used without a gateway (e.g., tests).
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	switch {
	case path == "status":
		s.handleRegistryStatus(w, r)
	case path == "prompts":
		s.handleRegistryPromptsList(w, r)
	case strings.HasPrefix(path, "prompts/"):
		s.handleRegistryPromptAction(w, r, strings.TrimPrefix(path, "prompts/"))
	case path == "skills":
		s.handleRegistrySkillsList(w, r)
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

// handleRegistryPromptsList handles GET (list) and POST (create) for prompts.
// GET  /api/registry/prompts
// POST /api/registry/prompts
func (s *Server) handleRegistryPromptsList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prompts := s.registryServer.Store().ListPrompts()
		if prompts == nil {
			prompts = []*registry.Prompt{}
		}
		writeJSON(w, prompts)
	case http.MethodPost:
		var p registry.Prompt
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := p.Validate(); err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := s.registryServer.Store().GetPrompt(p.Name); err == nil {
			writeJSONError(w, "Prompt already exists: "+p.Name, http.StatusConflict)
			return
		}
		if err := s.registryServer.Store().SavePrompt(&p); err != nil {
			writeJSONError(w, "Failed to save prompt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.refreshRegistryRouter()
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, p)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRegistryPromptAction handles individual prompt operations.
// GET    /api/registry/prompts/{name}
// PUT    /api/registry/prompts/{name}
// DELETE /api/registry/prompts/{name}
// POST   /api/registry/prompts/{name}/activate
// POST   /api/registry/prompts/{name}/disable
func (s *Server) handleRegistryPromptAction(w http.ResponseWriter, r *http.Request, subpath string) {
	parts := strings.SplitN(subpath, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "activate" || action == "disable" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRegistryPromptStateChange(w, name, action)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, err := s.registryServer.Store().GetPrompt(name)
		if err != nil {
			writeJSONError(w, "Prompt not found: "+name, http.StatusNotFound)
			return
		}
		writeJSON(w, p)
	case http.MethodPut:
		var p registry.Prompt
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		p.Name = name
		if err := p.Validate(); err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := s.registryServer.Store().GetPrompt(name); err != nil {
			writeJSONError(w, "Prompt not found: "+name, http.StatusNotFound)
			return
		}
		if err := s.registryServer.Store().SavePrompt(&p); err != nil {
			writeJSONError(w, "Failed to save prompt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.refreshRegistryRouter()
		writeJSON(w, p)
	case http.MethodDelete:
		if err := s.registryServer.Store().DeletePrompt(name); err != nil {
			writeJSONError(w, "Failed to delete prompt: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.refreshRegistryRouter()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleRegistryPromptStateChange updates a prompt's state to active or disabled.
func (s *Server) handleRegistryPromptStateChange(w http.ResponseWriter, name, action string) {
	p, err := s.registryServer.Store().GetPrompt(name)
	if err != nil {
		writeJSONError(w, "Prompt not found: "+name, http.StatusNotFound)
		return
	}
	switch action {
	case "activate":
		p.State = registry.StateActive
	case "disable":
		p.State = registry.StateDisabled
	}
	if err := s.registryServer.Store().SavePrompt(p); err != nil {
		writeJSONError(w, "Failed to update state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.refreshRegistryRouter()
	writeJSON(w, p)
}

// handleRegistrySkillsList handles GET (list) and POST (create) for skills.
// GET  /api/registry/skills
// POST /api/registry/skills
func (s *Server) handleRegistrySkillsList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		skills := s.registryServer.Store().ListSkills()
		if skills == nil {
			skills = []*registry.Skill{}
		}
		writeJSON(w, skills)
	case http.MethodPost:
		var sk registry.Skill
		if err := json.NewDecoder(r.Body).Decode(&sk); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := sk.Validate(); err != nil {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
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
func (s *Server) handleRegistrySkillAction(w http.ResponseWriter, r *http.Request, subpath string) {
	parts := strings.SplitN(subpath, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "activate" || action == "disable" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRegistrySkillStateChange(w, name, action)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sk, err := s.registryServer.Store().GetSkill(name)
		if err != nil {
			writeJSONError(w, "Skill not found: "+name, http.StatusNotFound)
			return
		}
		writeJSON(w, sk)
	case http.MethodPut:
		var sk registry.Skill
		if err := json.NewDecoder(r.Body).Decode(&sk); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		sk.Name = name
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

// refreshRegistryRouter refreshes the registry server's tools and re-registers
// with the gateway router. This handles progressive disclosure: if the registry
// gains its first content, it gets registered; if tools change, the router updates.
func (s *Server) refreshRegistryRouter() {
	if s.registryServer == nil {
		return
	}
	_ = s.registryServer.RefreshTools(context.Background())
	if s.registryServer.HasContent() {
		s.gateway.Router().AddClient(s.registryServer)
	}
	s.gateway.Router().RefreshTools()
}
