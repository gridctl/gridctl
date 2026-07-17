package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"

	"github.com/gridctl/gridctl/pkg/config"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/vault"
)

// lockFilePath returns the configured skill lock-file path, falling back to
// the global default. Tests inject a temp path via SetSkillSourcePaths.
func (s *Server) lockFilePath() string {
	if s.skillLockPath != "" {
		return s.skillLockPath
	}
	return skills.LockFilePath()
}

// configFilePath returns the configured skills.yaml path, falling back to the
// global default. Tests inject a temp path via SetSkillSourcePaths.
func (s *Server) configFilePath() string {
	if s.skillsConfigPath != "" {
		return s.skillsConfigPath
	}
	return skills.SkillsConfigPath()
}

// updateCachePath returns the configured skill-updates cache path, falling
// back to the global default. Tests inject a temp path via
// SetSkillUpdateCachePath.
func (s *Server) updateCachePath() string {
	if s.skillUpdateCachePath != "" {
		return s.skillUpdateCachePath
	}
	return skills.UpdateCachePath()
}

// SkillSourceStatus represents a skill source with its update status.
type SkillSourceStatus struct {
	Name           string             `json:"name"`
	Repo           string             `json:"repo"`
	Ref            string             `json:"ref,omitempty"`
	Path           string             `json:"path,omitempty"`
	AutoUpdate     bool               `json:"autoUpdate"`
	UpdateInterval string             `json:"updateInterval"`
	Skills         []SkillSourceEntry `json:"skills"`
	LastFetched    string             `json:"lastFetched,omitempty"`
	CommitSHA      string             `json:"commitSha,omitempty"`
	UpdateAvail    bool               `json:"updateAvailable"`
	// DriftedSkills lists the skills in this source whose on-disk SKILL.md has
	// local edits (drift) that a sync would otherwise overwrite.
	DriftedSkills []string `json:"driftedSkills,omitempty"`
}

// SkillSourceEntry represents a single skill within a source.
type SkillSourceEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	State       string `json:"state"`
	IsRemote    bool   `json:"isRemote"`
	ContentHash string `json:"contentHash,omitempty"`
	// HasLocalEdits is true when the on-disk SKILL.md diverges from the hash
	// snapshotted at the last import/sync (i.e. the user edited it locally).
	HasLocalEdits bool `json:"hasLocalEdits"`
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
	Available int                   `json:"available"`
	Sources   []SourceUpdateSummary `json:"sources"`
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

// AuthRequest is the optional auth payload accepted on /api/skills/sources/*
// endpoints. Raw Token values are transient; CredentialRef (e.g.
// "${vault:GIT_TOKEN}") is resolved against the live vault on every request.
type AuthRequest struct {
	Method        string `json:"method,omitempty"`        // "token" | "ssh-agent" | "ssh-key" | ""
	Token         string `json:"token,omitempty"`         // ephemeral plaintext
	CredentialRef string `json:"credentialRef,omitempty"` // e.g. "${vault:GIT_TOKEN}"
	SSHUser       string `json:"sshUser,omitempty"`
	SSHKeyPath    string `json:"sshKeyPath,omitempty"`
}

// toAuthConfig converts a request-body AuthRequest into a skills.AuthConfig,
// resolving any CredentialRef against the provided vault. An empty request
// (no method, no ref, no token) yields a zero-valued AuthConfig that
// signals ambient behavior.
func (r *AuthRequest) toAuthConfig(v *vault.Store) (skills.AuthConfig, error) {
	if r == nil || (r.Method == "" && r.Token == "" && r.CredentialRef == "" && r.SSHKeyPath == "") {
		return skills.AuthConfig{}, nil
	}

	method := r.Method
	if method == "" {
		// Infer from provided fields: token-ish wins, then ssh-key.
		switch {
		case r.Token != "" || r.CredentialRef != "":
			method = "token"
		case r.SSHKeyPath != "":
			method = "ssh-key"
		}
	}

	token := r.Token
	if r.CredentialRef != "" {
		resolved, err := resolveCredentialRef(r.CredentialRef, v)
		if err != nil {
			return skills.AuthConfig{}, err
		}
		token = resolved
	}

	return skills.AuthConfig{
		Method:        method,
		Token:         token,
		CredentialRef: r.CredentialRef,
		SSHUser:       r.SSHUser,
		SSHKeyPath:    r.SSHKeyPath,
	}, nil
}

// resolveCredentialRef expands a "${vault:KEY}" reference against the live
// vault. An unresolved reference is a hard error — we never fall through
// to an unauthenticated clone.
func resolveCredentialRef(ref string, v *vault.Store) (string, error) {
	if v == nil {
		return "", fmt.Errorf("vault not configured; cannot resolve %s", ref)
	}
	resolver := config.VaultResolver(v)
	expanded, unresolved, _ := config.ExpandString(ref, resolver)
	if len(unresolved) > 0 {
		return "", fmt.Errorf("vault key %q not found", unresolved[0])
	}
	return expanded, nil
}

// credentialResolver returns a skills.CredentialResolver that expands
// references against the server's live vault.
func (s *Server) credentialResolver() skills.CredentialResolver {
	return func(ref string) (string, error) {
		return resolveCredentialRef(ref, s.vaultStore)
	}
}

// gitErrorStatus maps a classified git error to an HTTP status code.
func gitErrorStatus(err error) int {
	switch {
	case errors.Is(err, gitpkg.ErrAuthRequired), errors.Is(err, gitpkg.ErrAuthFailed):
		return http.StatusUnauthorized
	case errors.Is(err, gitpkg.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, gitpkg.ErrProtocolMismatch),
		errors.Is(err, gitpkg.ErrEmptyToken),
		errors.Is(err, gitpkg.ErrHostKeyMismatch):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// writeGitError classifies and redacts an error from a git operation before
// writing it to the response. Callers should use this rather than passing
// raw go-git errors straight through writeJSONError.
func writeGitError(w http.ResponseWriter, prefix string, err error) {
	classified := gitpkg.ClassifyError(err)
	redacted := gitpkg.RedactError(classified)
	writeJSONError(w, prefix+redacted.Error(), gitErrorStatus(classified))
}

// handleSkillSourcesList returns all configured skill sources with update status.
// GET /api/skills/sources
func (s *Server) handleSkillSourcesList(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	store := s.registryServer.Store()
	lockPath := s.lockFilePath()
	lf, _ := skills.ReadLockFile(lockPath)

	// Per-skill drift (local edits). Local hashing only — no git fetch — so it
	// is cheap enough to compute on every list. Fails open to "no drift" on
	// error so a transient read problem never hides sources.
	driftedSet := make(map[string]bool)
	if drifted, err := skills.DetectDrift(r.Context(), store, lockPath, ""); err == nil {
		for _, name := range drifted {
			driftedSet[name] = true
		}
	}

	// Load skills.yaml config
	cfg, err := skills.LoadSkillsConfig(s.configFilePath())
	if err != nil {
		// No config = no sources; return lock file sources
		cfg = skills.DefaultSkillsConfig()
	}

	// Read the background-checker cache so we can mark sources with pending
	// updates without forcing a live git fetch on every page load. Fail
	// open: a missing/unreadable cache leaves UpdateAvail=false.
	updateStatus, _ := skills.ReadUpdateCacheAt(s.updateCachePath())

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
			if driftedSet[skillName] {
				entry.HasLocalEdits = true
				src.DriftedSkills = append(src.DriftedSkills, skillName)
			}
			src.Skills = append(src.Skills, entry)

			// The cache is keyed by skill name; if any skill in this source
			// has a pending update, surface it at the source level.
			if updateStatus != nil {
				if _, ok := updateStatus.Updates[skillName]; ok {
					src.UpdateAvail = true
				}
			}
		}

		sort.Strings(src.DriftedSkills)
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
		Repo       string       `json:"repo"`
		Ref        string       `json:"ref,omitempty"`
		Path       string       `json:"path,omitempty"`
		Trust      bool         `json:"trust,omitempty"`
		NoActivate bool         `json:"noActivate,omitempty"`
		Selected   []string     `json:"selected,omitempty"`
		Auth       *AuthRequest `json:"auth,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Repo == "" {
		writeJSONError(w, "repo is required", http.StatusBadRequest)
		return
	}

	authCfg, err := req.Auth.toAuthConfig(s.vaultStore)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	store := s.registryServer.Store()
	registryDir := store.Dir()
	lockPath := s.lockFilePath()
	logger := slog.Default()

	imp := skills.NewImporter(store, registryDir, lockPath, logger)
	imp.SetCredentialResolver(s.credentialResolver())
	result, err := imp.Import(skills.ImportOptions{
		Repo:       req.Repo,
		Ref:        req.Ref,
		Path:       req.Path,
		Trust:      req.Trust,
		NoActivate: req.NoActivate,
		Selected:   req.Selected,
		Auth:       authCfg,
	})
	if err != nil {
		writeGitError(w, "Import failed: ", err)
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

	lockPath := s.lockFilePath()
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

	lockPath := s.lockFilePath()
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

	var auth *AuthRequest
	if r.Body != nil && r.ContentLength > 0 {
		var req struct {
			Auth *AuthRequest `json:"auth,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		auth = req.Auth
	}

	authCfg, err := s.resolveCheckAuth(auth, src.CredentialRef)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger := slog.Default()
	newSHA, changed, err := skills.FetchAndCompare(src.Repo, src.Ref, src.CommitSHA, authCfg, logger)
	if err != nil {
		writeGitError(w, "Check failed: ", err)
		return
	}

	writeJSON(w, map[string]any{
		"source":     sourceName,
		"currentSha": src.CommitSHA,
		"latestSha":  newSHA,
		"hasUpdate":  changed,
	})
}

// resolveCheckAuth prefers an explicit request-body auth; falls back to the
// stored CredentialRef on the lock file entry when the request omits auth.
func (s *Server) resolveCheckAuth(req *AuthRequest, storedRef string) (skills.AuthConfig, error) {
	if req != nil {
		return req.toAuthConfig(s.vaultStore)
	}
	if storedRef == "" {
		return skills.AuthConfig{}, nil
	}
	token, err := resolveCredentialRef(storedRef, s.vaultStore)
	if err != nil {
		return skills.AuthConfig{}, err
	}
	return skills.AuthConfig{
		Method:        "token",
		Token:         token,
		CredentialRef: storedRef,
	}, nil
}

// syncSkill applies the drift-safe update policy to a single skill within a
// source and returns its per-skill outcome:
//   - not drifted: updated normally.
//   - drifted, force=false: skipped ("local edits"). Its tracking metadata is
//     advanced to the latest upstream commit so it stops showing as an
//     available update, but the on-disk SKILL.md and InstalledHash are left
//     untouched (drift remains visible).
//   - drifted, force=true: the current SKILL.md is backed up, then overwritten.
//
// authCfg is used only for the on-demand FetchAndCompare (skip-advance and
// backup naming); Importer.Update independently re-resolves credentials from
// the stored origin.
func (s *Server) syncSkill(ctx context.Context, imp *skills.Importer, authCfg skills.AuthConfig, src skills.LockedSource, skillName string, drifted, force bool) SkillSyncResult {
	entry := SkillSyncResult{Skill: skillName}

	if !drifted {
		result, err := imp.Update(skillName, false, false)
		if err != nil {
			entry.Error = gitpkg.RedactError(err).Error()
			return entry
		}
		entry.Imported = len(result.Imported)
		entry.Warnings = result.Warnings
		return entry
	}

	if !force {
		// Skip the overwrite but advance tracking to the latest upstream commit
		// so the reviewed version no longer reads as "update available".
		if newSHA, changed, ferr := skills.FetchAndCompare(src.Repo, src.Ref, src.CommitSHA, authCfg, slog.Default()); ferr == nil && changed && newSHA != "" {
			if aerr := imp.AdvanceTracking(ctx, skillName, newSHA); aerr != nil {
				entry.Warnings = append(entry.Warnings, "failed to advance tracking: "+aerr.Error())
			}
		}
		entry.Skipped = "local edits"
		return entry
	}

	// Forced overwrite of a drifted skill: back up the current SKILL.md first.
	newSHA, _, _ := skills.FetchAndCompare(src.Repo, src.Ref, src.CommitSHA, authCfg, slog.Default())
	if backup, berr := imp.BackupSkillFile(ctx, skillName, skills.ShortSHA(newSHA)); berr != nil {
		entry.Warnings = append(entry.Warnings, "backup failed: "+berr.Error())
	} else {
		entry.Backup = backup
	}

	result, err := imp.Update(skillName, false, true)
	if err != nil {
		entry.Error = gitpkg.RedactError(err).Error()
		return entry
	}
	entry.Imported = len(result.Imported)
	entry.Warnings = append(entry.Warnings, result.Warnings...)
	return entry
}

// skillUpdateRequest is the optional body accepted by the per-source and
// sync-all update endpoints. All fields are optional; an empty body preserves
// the historical "update everything, skip drifted" behavior with force=false.
type skillUpdateRequest struct {
	Force  bool         `json:"force,omitempty"`
	Skills []string     `json:"skills,omitempty"` // restrict to these skills (empty = all)
	Auth   *AuthRequest `json:"auth,omitempty"`
}

// handleSkillSourceUpdate applies available updates for a source. Locally-edited
// (drifted) skills are skipped unless force is set; an optional skills filter
// restricts the operation to named skills.
// POST /api/skills/sources/{name}/update
func (s *Server) handleSkillSourceUpdate(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	var req skillUpdateRequest
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	lockPath := s.lockFilePath()
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
	ctx := r.Context()

	authCfg, err := s.resolveCheckAuth(req.Auth, src.CredentialRef)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	imp := skills.NewImporter(store, registryDir, lockPath, slog.Default())
	imp.SetCredentialResolver(s.credentialResolver())

	// Drifted skills in this source (local edits). Computed once up front.
	driftedSet := make(map[string]bool)
	if drifted, derr := skills.DetectDrift(ctx, store, lockPath, sourceName); derr == nil {
		for _, name := range drifted {
			driftedSet[name] = true
		}
	}

	// Optional filter: restrict to named skills that actually belong to this
	// source. An empty filter means all skills in the source.
	filter := make(map[string]bool, len(req.Skills))
	for _, name := range req.Skills {
		filter[name] = true
	}

	skillNames := make([]string, 0, len(src.Skills))
	for skillName := range src.Skills {
		if len(filter) > 0 && !filter[skillName] {
			continue
		}
		skillNames = append(skillNames, skillName)
	}
	sort.Strings(skillNames)

	results := make([]SkillSyncResult, 0, len(skillNames))
	for _, skillName := range skillNames {
		results = append(results, s.syncSkill(ctx, imp, authCfg, src, skillName, driftedSet[skillName], req.Force))
	}

	s.refreshRegistryRouter()

	writeJSON(w, map[string]any{
		"source":  sourceName,
		"results": results,
	})
}

// handleSkillSourcePreview previews skills in a source without importing.
// GET  /api/skills/sources/{name}/preview  — query-param driven, no auth
// POST /api/skills/sources/{name}/preview  — JSON body with optional auth
func (s *Server) handleSkillSourcePreview(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	// Query-param values are the GET path; POST bodies override/augment.
	repo := r.URL.Query().Get("repo")
	ref := r.URL.Query().Get("ref")
	path := r.URL.Query().Get("path")

	var reqBody struct {
		Repo string       `json:"repo,omitempty"`
		Ref  string       `json:"ref,omitempty"`
		Path string       `json:"path,omitempty"`
		Auth *AuthRequest `json:"auth,omitempty"`
	}
	if r.Method == http.MethodPost && r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.Repo != "" {
			repo = reqBody.Repo
		}
		if reqBody.Ref != "" {
			ref = reqBody.Ref
		}
		if reqBody.Path != "" {
			path = reqBody.Path
		}
	}

	var storedRef string
	if repo == "" {
		lockPath := s.lockFilePath()
		lf, _ := skills.ReadLockFile(lockPath)
		if src, ok := lf.Sources[sourceName]; ok {
			repo = src.Repo
			if ref == "" {
				ref = src.Ref
			}
			storedRef = src.CredentialRef
		}
	}

	if repo == "" {
		writeJSONError(w, "repo URL required (query param, body, or existing source)", http.StatusBadRequest)
		return
	}

	authCfg, err := s.resolveCheckAuth(reqBody.Auth, storedRef)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger := slog.Default()
	result, err := skills.CloneAndDiscover(repo, ref, path, authCfg, logger)
	if err != nil {
		writeGitError(w, "Clone failed: ", err)
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

	malformed := result.Malformed
	if malformed == nil {
		malformed = []skills.MalformedSkill{}
	}

	writeJSON(w, map[string]any{
		"repo":      repo,
		"ref":       ref,
		"commitSha": result.CommitSHA,
		"skills":    previews,
		"malformed": malformed,
	})
}

// SourceSyncSummary is the aggregate response from a bulk sync.
type SourceSyncSummary struct {
	Sources       []SourceSyncResult `json:"sources"`
	SyncedSources int                `json:"syncedSources"`
	UpdatedSkills int                `json:"updatedSkills"`
	SkippedSkills int                `json:"skippedSkills"`
	FailedSources int                `json:"failedSources"`
	PinnedSources int                `json:"pinnedSources"`
}

// SourceSyncResult is the per-source outcome of a bulk sync.
type SourceSyncResult struct {
	Name   string            `json:"name"`
	Repo   string            `json:"repo"`
	Pinned bool              `json:"pinned,omitempty"`
	Skills []SkillSyncResult `json:"skills,omitempty"`
	Error  string            `json:"error,omitempty"`
}

// SkillSyncResult is the per-skill outcome within a sync.
type SkillSyncResult struct {
	Skill    string   `json:"skill"`
	Imported int      `json:"imported,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
	// Skipped, when set, is the reason a drifted skill was left untouched
	// (e.g. "local edits"). Its tracking metadata is still advanced.
	Skipped string `json:"skipped,omitempty"`
	// Backup is the file name of the pre-overwrite SKILL.md backup written
	// when a drifted skill was force-overwritten.
	Backup string `json:"backup,omitempty"`
}

// syncAllConcurrency caps the number of sources synced in parallel. Matches
// the background-checker's bound to keep behavior consistent and to avoid
// saturating shared git hosts under bulk operations.
const syncAllConcurrency = 3

// handleSkillSourcesSyncAll syncs every imported source in parallel, honoring
// pins. Mirrors the per-source semantics of handleSkillSourceUpdate.
// POST /api/skills/sources/update
func (s *Server) handleSkillSourcesSyncAll(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	var req skillUpdateRequest
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	lockPath := s.lockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		writeJSONError(w, "Failed to read lock file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	store := s.registryServer.Store()
	registryDir := store.Dir()
	logger := slog.Default()
	imp := skills.NewImporter(store, registryDir, lockPath, logger)
	imp.SetCredentialResolver(s.credentialResolver())

	// Drift (local edits) across every imported skill, computed once before the
	// fan-out so the goroutines never read the lock file while it is being
	// rewritten. Local hashing only — no git fetch.
	driftedSet := make(map[string]bool)
	if drifted, derr := skills.DetectDrift(ctx, store, lockPath, ""); derr == nil {
		for _, name := range drifted {
			driftedSet[name] = true
		}
	}

	// Pre-sort source names for deterministic output.
	names := make([]string, 0, len(lf.Sources))
	for name := range lf.Sources {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]SourceSyncResult, len(names))
	// ghostsBySource collects, per source index, the names of skills that are
	// recorded in the lock file but no longer present in the registry (e.g.
	// deleted via the UI). Each goroutine writes only its own index, so the
	// slice is race-free without a mutex. Pruned once after the fan-out.
	ghostsBySource := make([][]string, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, syncAllConcurrency)

	for i, name := range names {
		src := lf.Sources[name]
		results[i] = SourceSyncResult{Name: name, Repo: src.Repo}

		if skills.IsPinnedRef(src.Ref) {
			results[i].Pinned = true
			continue
		}

		wg.Add(1)
		go func(idx int, sourceName string, source skills.LockedSource) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Best-effort auth for the on-demand fetches in syncSkill (skip
			// tracking-advance, backup naming). Errors are non-fatal: Update
			// independently resolves credentials from the stored origin.
			authCfg, _ := s.resolveCheckAuth(req.Auth, source.CredentialRef)

			// Skill names sorted so per-skill output order is stable too.
			skillNames := make([]string, 0, len(source.Skills))
			for sn := range source.Skills {
				skillNames = append(skillNames, sn)
			}
			sort.Strings(skillNames)

			for _, skillName := range skillNames {
				// Stop processing further skills in this source if the
				// client disconnected. In-flight Update calls still complete
				// (Importer.Update doesn't accept ctx today) but no new
				// fetches are kicked off.
				if ctx.Err() != nil {
					results[idx].Skills = append(results[idx].Skills, SkillSyncResult{
						Skill: skillName,
						Error: "canceled",
					})
					continue
				}

				// The skill is in the lock file but no longer in the registry
				// (deleted out from under the lock file, e.g. via the UI).
				// Updating it would fail reading the missing origin sidecar and
				// surface as a spurious failure. Record it for pruning and skip.
				// We gate on registry presence rather than the Update error so a
				// present skill whose update merely failed (transient/auth) is
				// still reported and retained.
				if _, err := store.GetSkill(skillName); err != nil {
					ghostsBySource[idx] = append(ghostsBySource[idx], skillName)
					results[idx].Skills = append(results[idx].Skills, SkillSyncResult{
						Skill:    skillName,
						Warnings: []string{"skill no longer in registry; removed stale lock entry"},
					})
					continue
				}

				entry := s.syncSkill(ctx, imp, authCfg, source, skillName, driftedSet[skillName], req.Force)
				results[idx].Skills = append(results[idx].Skills, entry)
			}
		}(i, name, src)
	}

	wg.Wait()

	// Prune stale lock entries for skills that were deleted from the registry.
	// Done on the main goroutine after the fan-out so the lock-file write can't
	// race the in-flight Update calls (which may write the lock file via the
	// importer). Re-read first to pick up any writes those updates made.
	var ghosts []string
	for _, g := range ghostsBySource {
		ghosts = append(ghosts, g...)
	}
	if len(ghosts) > 0 {
		if lf2, err := skills.ReadLockFile(lockPath); err != nil {
			logger.Warn("sync: failed to read lock file to prune stale skills", "error", err)
		} else {
			for _, skillName := range ghosts {
				lf2.RemoveSkill(skillName)
			}
			if err := skills.WriteLockFile(lockPath, lf2); err != nil {
				logger.Warn("sync: failed to write lock file after pruning stale skills", "error", err)
			}
		}
	}

	summary := SourceSyncSummary{Sources: results}
	for _, r := range results {
		switch {
		case r.Pinned:
			summary.PinnedSources++
		case r.Error != "":
			summary.FailedSources++
		default:
			// Source had at least one skill error → counts as failed.
			sourceFailed := false
			for _, sk := range r.Skills {
				if sk.Error != "" {
					sourceFailed = true
				}
				summary.UpdatedSkills += sk.Imported
				if sk.Skipped != "" {
					summary.SkippedSkills++
				}
			}
			if sourceFailed {
				summary.FailedSources++
			} else {
				summary.SyncedSources++
			}
		}
	}

	s.refreshRegistryRouter()

	writeJSON(w, summary)
}

// handleSkillUpdates returns pending update summary across all sources.
// GET /api/skills/updates
func (s *Server) handleSkillUpdates(w http.ResponseWriter, _ *http.Request) {
	lockPath := s.lockFilePath()
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

		authCfg, authErr := s.resolveCheckAuth(nil, src.CredentialRef)
		if authErr != nil {
			entry.Error = authErr.Error()
			summary.Sources = append(summary.Sources, entry)
			continue
		}

		newSHA, changed, err := skills.FetchAndCompare(src.Repo, src.Ref, src.CommitSHA, authCfg, logger)
		if err != nil {
			entry.Error = gitpkg.RedactError(err).Error()
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

// SkillDiffResponse is the body of the per-skill compare-with-upstream endpoint.
type SkillDiffResponse struct {
	Skill       string `json:"skill"`
	Local       string `json:"local"`
	Upstream    string `json:"upstream"`
	UnifiedDiff string `json:"unifiedDiff,omitempty"`
	Drifted     bool   `json:"drifted"`
}

// handleSkillDiff returns the local vs upstream SKILL.md for an imported skill
// without writing anything to the registry. The upstream side is the content
// an update would install; auth is taken from the skill's stored credentialRef.
// GET /api/skills/sources/{name}/skills/{skill}/diff
func (s *Server) handleSkillDiff(w http.ResponseWriter, r *http.Request) {
	skillName := r.PathValue("skill")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	store := s.registryServer.Store()
	imp := skills.NewImporter(store, store.Dir(), s.lockFilePath(), slog.Default())
	imp.SetCredentialResolver(s.credentialResolver())

	diff, err := imp.Diff(r.Context(), skillName)
	if err != nil {
		writeGitError(w, "Diff failed: ", err)
		return
	}

	writeJSON(w, SkillDiffResponse{
		Skill:       diff.Skill,
		Local:       diff.Local,
		Upstream:    diff.Upstream,
		UnifiedDiff: unifiedDiff(diff.Local, diff.Upstream, 3),
		Drifted:     diff.Drifted,
	})
}

// handleSkillDetach removes a skill's origin sidecar and lock-file entry so it
// becomes local-only and is no longer touched by sync.
// POST /api/skills/sources/{name}/skills/{skill}/detach
func (s *Server) handleSkillDetach(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")
	skillName := r.PathValue("skill")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	lockPath := s.lockFilePath()
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
	if _, ok := src.Skills[skillName]; !ok {
		writeJSONError(w, "Skill not found in source: "+skillName, http.StatusNotFound)
		return
	}

	store := s.registryServer.Store()
	imp := skills.NewImporter(store, store.Dir(), lockPath, slog.Default())

	if err := imp.Detach(r.Context(), skillName); err != nil {
		writeJSONError(w, "Detach failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.refreshRegistryRouter()

	writeJSON(w, map[string]any{"detached": skillName})
}

// handleSkillReset force-updates a single skill to its upstream content,
// backing up the current (possibly edited) SKILL.md first.
// POST /api/skills/sources/{name}/skills/{skill}/reset
func (s *Server) handleSkillReset(w http.ResponseWriter, r *http.Request) {
	sourceName := r.PathValue("name")
	skillName := r.PathValue("skill")

	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}

	var req skillUpdateRequest
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	lockPath := s.lockFilePath()
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
	if _, ok := src.Skills[skillName]; !ok {
		writeJSONError(w, "Skill not found in source: "+skillName, http.StatusNotFound)
		return
	}

	store := s.registryServer.Store()
	ctx := r.Context()

	authCfg, err := s.resolveCheckAuth(req.Auth, src.CredentialRef)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	imp := skills.NewImporter(store, store.Dir(), lockPath, slog.Default())
	imp.SetCredentialResolver(s.credentialResolver())

	// Reset always overwrites to upstream. Pass drifted=true so a locally-edited
	// skill is backed up before the overwrite; a clean skill simply re-pulls.
	drifted := false
	if d, derr := skills.DetectDrift(ctx, store, lockPath, sourceName); derr == nil {
		for _, name := range d {
			if name == skillName {
				drifted = true
				break
			}
		}
	}

	entry := s.syncSkill(ctx, imp, authCfg, src, skillName, drifted, true)

	s.refreshRegistryRouter()

	if entry.Error != "" {
		writeJSONError(w, "Reset failed: "+entry.Error, http.StatusInternalServerError)
		return
	}
	writeJSON(w, entry)
}
