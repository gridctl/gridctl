package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// SkillSourceStatus represents a skill source with its update status.
type SkillSourceStatus struct {
	Name           string              `json:"name"`
	Repo           string              `json:"repo"`
	Ref            string              `json:"ref,omitempty"`
	Path           string              `json:"path,omitempty"`
	AutoUpdate     bool                `json:"autoUpdate"`
	UpdateInterval string              `json:"updateInterval"`
	Skills         []SkillSourceEntry  `json:"skills"`
	LastFetched    string              `json:"lastFetched,omitempty"`
	CommitSHA      string              `json:"commitSha,omitempty"`
	UpdateAvail    bool                `json:"updateAvailable"`
}

// SkillSourceEntry represents a single skill within a source.
type SkillSourceEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	State       string `json:"state"`
	IsRemote    bool   `json:"isRemote"`
	ContentHash string `json:"contentHash,omitempty"`
}

// SkillPreview represents a previewed skill from a repo (not yet imported).
type SkillPreview struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Body        string                   `json:"body"`
	Valid       bool                     `json:"valid"`
	Errors      []string                 `json:"errors,omitempty"`
	Warnings    []string                 `json:"warnings,omitempty"`
	Findings    []skills.SecurityFinding `json:"findings,omitempty"`
	Exists      bool                     `json:"exists"`
}

// UpdateSummary represents pending updates across all sources.
type UpdateSummary struct {
	Available int                    `json:"available"`
	Sources   []SourceUpdateSummary  `json:"sources"`
}

// SourceUpdateSummary represents update status for a single source.
type SourceUpdateSummary struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Current   string `json:"currentSha"`
	Latest    string `json:"latestSha,omitempty"`
	HasUpdate bool   `json:"hasUpdate"`
	Error     string `json:"error,omitempty"`
}

// handleSkillSourcesList returns all configured skill sources with update status.
// GET /api/skills/sources
func (s *Server) handleSkillSourcesList(w http.ResponseWriter, _ *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	store := s.registryServer.Store()
	lockPath := skills.LockFilePath()
	lf, _ := skills.ReadLockFile(lockPath)

	// Load skills.yaml config
	cfg, err := skills.LoadSkillsConfig(skills.SkillsConfigPath())
	if err != nil {
		// No config = no sources; return lock file sources
		cfg = skills.DefaultSkillsConfig()
	}

	var sources []SkillSourceStatus

	// Build sources from lock file (which records what's actually imported)
	for srcName, locked := range lf.Sources {
		src := SkillSourceStatus{
			Name:      srcName,
			Repo:      locked.Repo,
			Ref:       locked.Ref,
			CommitSHA: locked.CommitSHA,
		}

		if !locked.FetchedAt.IsZero() {
			src.LastFetched = locked.FetchedAt.Format("2006-01-02T15:04:05Z")
		}

		// Match with config for auto-update settings
		for _, cfgSrc := range cfg.Sources {
			if cfgSrc.Repo == locked.Repo || cfgSrc.Name == srcName {
				src.AutoUpdate = cfg.EffectiveAutoUpdate(&cfgSrc)
				src.UpdateInterval = cfgSrc.UpdateInterval
				src.Path = cfgSrc.Path
				break
			}
		}

		// Build skill entries
		for skillName := range locked.Skills {
			entry := SkillSourceEntry{
				Name:     skillName,
				IsRemote: true,
			}
			if sk, err := store.GetSkill(skillName); err == nil {
				entry.Description = sk.Description
				entry.State = string(sk.State)
			}
			src.Skills = append(src.Skills, entry)
		}

		sources = append(sources, src)
	}

	if sources == nil {
		sources = []SkillSourceStatus{}
	}
	writeJSON(w, sources)
}

// handleSkillSourceAdd adds a new skill source (triggers clone + import).
// POST /api/skills/sources
func (s *Server) handleSkillSourceAdd(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Repo       string   `json:"repo"`
		Ref        string   `json:"ref,omitempty"`
		Path       string   `json:"path,omitempty"`
		Trust      bool     `json:"trust,omitempty"`
		NoActivate bool     `json:"noActivate,omitempty"`
		Selected   []string `json:"selected,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Repo == "" {
		writeJSONError(w, "repo is required", http.StatusBadRequest)
		return
	}

	store := s.registryServer.Store()
	registryDir := store.Dir()
	lockPath := skills.LockFilePath()
	logger := slog.Default()

	imp := skills.NewImporter(store, registryDir, lockPath, logger)
	result, err := imp.Import(skills.ImportOptions{
		Repo:       req.Repo,
		Ref:        req.Ref,
		Path:       req.Path,
		Trust:      req.Trust,
		NoActivate: req.NoActivate,
		Selected:   req.Selected,
	})
	if err != nil {
		writeJSONError(w, "Import failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.refreshRegistryRouter()

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, result)
}

// handleSkillSourceRemove removes a skill source and its imported skills.
// DELETE /api/skills/sources/{name}
func (s *Server) handleSkillSourceRemove(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	lockPath := skills.LockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		writeJSONError(w, "Failed to read lock file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	src, ok := lf.Sources[sourceName]
	if !ok {
		writeJSONError(w, "Source not found: "+sourceName, http.StatusNotFound)
		return
	}

	store := s.registryServer.Store()
	registryDir := store.Dir()
	imp := skills.NewImporter(store, registryDir, lockPath, slog.Default())

	// Remove each skill from this source
	var removed []string
	for skillName := range src.Skills {
		if err := imp.Remove(skillName); err != nil {
			slog.Warn("failed to remove skill", "skill", skillName, "error", err)
			continue
		}
		removed = append(removed, skillName)
	}

	s.refreshRegistryRouter()

	writeJSON(w, map[string]any{
		"removed": removed,
		"source":  sourceName,
	})
}

// handleSkillSourceCheck triggers an update check for a source.
// POST /api/skills/sources/{name}/check
func (s *Server) handleSkillSourceCheck(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")

	lockPath := skills.LockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		writeJSONError(w, "Failed to read lock file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	src, ok := lf.Sources[sourceName]
	if !ok {
		writeJSONError(w, "Source not found: "+sourceName, http.StatusNotFound)
		return
	}

	logger := slog.Default()
	newSHA, changed, err := skills.FetchAndCompare(src.Repo, src.Ref, src.CommitSHA, logger)
	if err != nil {
		writeJSONError(w, "Check failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"source":     sourceName,
		"currentSha": src.CommitSHA,
		"latestSha":  newSHA,
		"hasUpdate":  changed,
	})
}

// handleSkillSourceUpdate applies available updates for a source.
// POST /api/skills/sources/{name}/update
func (s *Server) handleSkillSourceUpdate(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	lockPath := skills.LockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		writeJSONError(w, "Failed to read lock file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	src, ok := lf.Sources[sourceName]
	if !ok {
		writeJSONError(w, "Source not found: "+sourceName, http.StatusNotFound)
		return
	}

	store := s.registryServer.Store()
	registryDir := store.Dir()
	imp := skills.NewImporter(store, registryDir, lockPath, slog.Default())

	// Update each skill from this source
	var results []map[string]any
	for skillName := range src.Skills {
		result, err := imp.Update(skillName, false, false)
		entry := map[string]any{"skill": skillName}
		if err != nil {
			entry["error"] = err.Error()
		} else {
			entry["imported"] = len(result.Imported)
			entry["warnings"] = result.Warnings
		}
		results = append(results, entry)
	}

	s.refreshRegistryRouter()

	writeJSON(w, map[string]any{
		"source":  sourceName,
		"results": results,
	})
}

// handleSkillSourcePreview previews skills in a source without importing.
// GET /api/skills/sources/{name}/preview
func (s *Server) handleSkillSourcePreview(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	// Get repo URL from query params or lock file
	repo := r.URL.Query().Get("repo")
	ref := r.URL.Query().Get("ref")
	path := r.URL.Query().Get("path")

	if repo == "" {
		lockPath := skills.LockFilePath()
		lf, _ := skills.ReadLockFile(lockPath)
		if src, ok := lf.Sources[sourceName]; ok {
			repo = src.Repo
			if ref == "" {
				ref = src.Ref
			}
		}
	}

	if repo == "" {
		writeJSONError(w, "repo URL required (query param or existing source)", http.StatusBadRequest)
		return
	}

	logger := slog.Default()
	result, err := skills.CloneAndDiscover(repo, ref, path, logger)
	if err != nil {
		writeJSONError(w, "Clone failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	store := s.registryServer.Store()
	var previews []SkillPreview

	for _, discovered := range result.Skills {
		preview := SkillPreview{
			Name:        discovered.Name,
			Description: discovered.Skill.Description,
			Body:        discovered.Skill.Body,
		}

		// Validate
		vr := registry.ValidateSkillFull(discovered.Skill)
		preview.Valid = vr.Valid()
		preview.Errors = vr.Errors
		preview.Warnings = vr.Warnings

		// Security scan
		scanResult := skills.ScanSkill(discovered.Skill)
		if !scanResult.Safe {
			preview.Findings = scanResult.Findings
		}

		// Check if already exists
		if _, err := store.GetSkill(discovered.Name); err == nil {
			preview.Exists = true
		}

		previews = append(previews, preview)
	}

	if previews == nil {
		previews = []SkillPreview{}
	}

	writeJSON(w, map[string]any{
		"repo":      repo,
		"ref":       ref,
		"commitSha": result.CommitSHA,
		"skills":    previews,
	})
}

// handleSkillUpdates returns pending update summary across all sources.
// GET /api/skills/updates
func (s *Server) handleSkillUpdates(w http.ResponseWriter, _ *http.Request) {
	lockPath := skills.LockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		writeJSONError(w, "Failed to read lock file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger := slog.Default()
	summary := UpdateSummary{}

	for srcName, src := range lf.Sources {
		entry := SourceUpdateSummary{
			Name:    srcName,
			Repo:    src.Repo,
			Current: src.CommitSHA,
		}

		newSHA, changed, err := skills.FetchAndCompare(src.Repo, src.Ref, src.CommitSHA, logger)
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.Latest = newSHA
			entry.HasUpdate = changed
			if changed {
				summary.Available++
			}
		}

		summary.Sources = append(summary.Sources, entry)
	}

	if summary.Sources == nil {
		summary.Sources = []SourceUpdateSummary{}
	}

	writeJSON(w, summary)
}
