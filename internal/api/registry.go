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

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	switch {
	case path == "status":
		s.handleRegistryStatus(w, r)
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

	if action == "test" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRegistrySkillTest(w, r, name)
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
		var sk registry.AgentSkill
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

// handleRegistrySkillTest executes a skill and returns the result.
// POST /api/registry/skills/{name}/test
func (s *Server) handleRegistrySkillTest(w http.ResponseWriter, r *http.Request, name string) {
	var args map[string]any
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil && err.Error() != "EOF" {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if args == nil {
		args = make(map[string]any)
	}

	result, err := s.registryServer.CallTool(r.Context(), name, args)
	if err != nil {
		writeJSONError(w, "Execution error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, result)
}

// refreshRegistryRouter refreshes the registry server's tools and re-registers
// with the gateway router.
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
