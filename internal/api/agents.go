package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/skills"
)

// agentsMgr returns the agent projection manager, lazily built against
// the real home directory and the live registry directory. Tests inject
// a temp manager via SetAgentsManager before the first request; the lazy
// path is only reached in production, where the real home is correct.
func (s *Server) agentsMgr() (*agentsync.Manager, error) {
	s.agentsOnce.Do(func() {
		if s.agentsManager != nil {
			return
		}
		if s.registryServer == nil {
			s.agentsErr = errors.New("registry not available")
			return
		}
		mgr, err := agentsync.NewManager(s.registryServer.Store().Dir())
		if err != nil {
			s.agentsErr = err
			return
		}
		s.agentsManager = mgr
	})
	return s.agentsManager, s.agentsErr
}

// SetAgentsManager injects the agent projection manager. Tests use it to
// keep projection handlers away from the real home directory.
func (s *Server) SetAgentsManager(m *agentsync.Manager) {
	s.agentsOnce.Do(func() {})
	s.agentsManager = m
}

// agentErrorStatus maps pkg/agentsync sentinel errors to HTTP statuses.
// Agents are resources (404 when unknown); clients are enum values in
// request bodies (400 when unknown).
func agentErrorStatus(err error) int {
	switch {
	case errors.Is(err, agentsync.ErrUnknownAgent):
		return http.StatusNotFound
	case errors.Is(err, agentsync.ErrUnknownClient):
		return http.StatusBadRequest
	case errors.Is(err, agentsync.ErrNotAvailable), errors.Is(err, agentsync.ErrNotProjected),
		errors.Is(err, agentsync.ErrNewerLockVersion):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// agentExists reports whether the canonical AGENT.md exists, without
// parsing it. PUT and DELETE gate existence on this rather than GetAgent
// so a hand-corrupted store file stays repairable and deletable over
// REST (GetAgent fails on parse errors, which would lock the file out of
// exactly the endpoints that could fix it).
func agentExists(registryDir, name string) bool {
	if skills.ValidateAgentName(name) != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(skills.AgentDir(registryDir, name), "AGENT.md"))
	return err == nil
}

// decodeOptionalBody decodes an optional JSON request body into dst. An
// absent or empty body (including chunked requests with no bytes, where
// ContentLength is -1) leaves dst zero-valued: several projection
// endpoints document the empty request as their sync-everything default.
func decodeOptionalBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// agentExtraField is one passthrough frontmatter key in wire form. The
// wire shape is an ordered array, never a map: AgentDefinition.Extra is
// ordered, JSON objects are not, and the canonical file is projected
// verbatim, so key order is part of the contract, not a cosmetic detail.
type agentExtraField struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// agentExtraWire projects a definition's passthrough frontmatter into
// wire form. Values that fail to decode are skipped (the raw file still
// carries them; extra is read-only display data, and edits go through raw).
func agentExtraWire(def *skills.AgentDefinition) []agentExtraField {
	fields := make([]agentExtraField, 0, len(def.Extra))
	for _, f := range def.Extra {
		var v any
		if err := f.Value.Decode(&v); err != nil {
			slog.Debug("agent extra key did not decode for the wire", "key", f.Key, "error", err)
			continue
		}
		fields = append(fields, agentExtraField{Key: f.Key, Value: v})
	}
	return fields
}

// registryAgentListItem is the list projection of an agent: what the
// catalog filters, sorts, groups, and badges on. Body and Raw stay out
// of the list (the UI polls it) and come from the single-agent GET.
type registryAgentListItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Source is the imported source's name from the lock file; empty for
	// an agent with no recorded source.
	Source string            `json:"source,omitempty"`
	Extra  []agentExtraField `json:"extra,omitempty"`
	Dir    string            `json:"dir,omitempty"`
}

// registryAgent is the full wire shape: the list projection plus the
// markdown body and the verbatim file. Raw is the editing surface; PUT
// accepts it back and writes it byte-for-byte.
type registryAgent struct {
	registryAgentListItem
	Body string `json:"body"`
	Raw  string `json:"raw"`
}

// agentSources maps agent names to their lock-file source names. Best
// effort: a missing or unreadable lock file yields no sources, not an
// error, matching how the skills list treats provenance.
func (s *Server) agentSources() map[string]string {
	lf, err := skills.ReadLockFile(s.lockFilePath())
	if err != nil {
		return nil
	}
	sources := make(map[string]string)
	for srcName, src := range lf.Sources {
		for agentName := range src.Agents {
			sources[agentName] = srcName
		}
	}
	return sources
}

// newRegistryAgentListItem projects one installed agent into list shape.
func newRegistryAgentListItem(a skills.InstalledAgent, sources map[string]string) registryAgentListItem {
	return registryAgentListItem{
		Name:        a.Name,
		Description: a.Definition.Description,
		Source:      sources[a.Name],
		Extra:       agentExtraWire(a.Definition),
		Dir:         a.Dir,
	}
}

// newRegistryAgent projects one installed agent into full shape.
func newRegistryAgent(a skills.InstalledAgent, sources map[string]string) registryAgent {
	return registryAgent{
		registryAgentListItem: newRegistryAgentListItem(a, sources),
		Body:                  a.Definition.Body,
		Raw:                   string(a.Definition.Raw),
	}
}

// handleRegistryAgentsList returns all installed agents.
//
// The default response omits each agent's body and raw file; pass
// ?full=1 for the complete shapes.
//
// GET /api/registry/agents
func (s *Server) handleRegistryAgentsList(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	agents, err := skills.ListAgents(s.registryServer.Store().Dir())
	if err != nil {
		writeJSONError(w, "Failed to list agents: "+err.Error(), http.StatusInternalServerError)
		return
	}
	sources := s.agentSources()

	if r.URL.Query().Get("full") == "1" {
		items := make([]registryAgent, 0, len(agents))
		for _, a := range agents {
			items = append(items, newRegistryAgent(a, sources))
		}
		writeJSON(w, items)
		return
	}

	items := make([]registryAgentListItem, 0, len(agents))
	for _, a := range agents {
		items = append(items, newRegistryAgentListItem(a, sources))
	}
	writeJSON(w, items)
}

// handleRegistryAgentGet returns a single agent, body and raw included.
// GET /api/registry/agents/{name}
func (s *Server) handleRegistryAgentGet(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	a, err := skills.GetAgent(s.registryServer.Store().Dir(), name)
	if err != nil {
		writeJSONError(w, "Agent not found: "+name, http.StatusNotFound)
		return
	}
	writeJSON(w, newRegistryAgent(*a, s.agentSources()))
}

// handleRegistryAgentPut updates an agent's file. The body carries the
// whole file as raw text; the server re-parses, gates on the security
// scan, and writes the bytes verbatim (identity projections copy
// canonical bytes, so any normalization here would read as drift on
// every synced client). Editing only: agents enter the store through
// import, so an unknown name is 404, not an upsert.
// PUT /api/registry/agents/{name}
func (s *Server) handleRegistryAgentPut(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	registryDir := s.registryServer.Store().Dir()

	var req struct {
		Raw string `json:"raw"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Raw == "" {
		writeJSONError(w, "raw is required (the full AGENT.md content)", http.StatusBadRequest)
		return
	}
	if !agentExists(registryDir, name) {
		writeJSONError(w, "Agent not found: "+name, http.StatusNotFound)
		return
	}

	def, err := skills.ParseAgentMD([]byte(req.Raw))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if def.Name != "" && def.Name != name {
		writeJSONError(w, fmt.Sprintf("frontmatter names the agent %q, not %q; renames are not supported", def.Name, name), http.StatusBadRequest)
		return
	}
	// The scan gate skills get on import applies to edits too: an agent
	// definition is instructions a client executes with tool access. No
	// trust override exists over REST.
	if scan := skills.ScanAgent(def); !scan.Safe {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":    "security scan blocked the save",
			"findings": scan.Findings,
		})
		return
	}

	saved, err := skills.SaveAgent(registryDir, name, []byte(req.Raw))
	if err != nil {
		writeJSONError(w, "Failed to save agent: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a := skills.InstalledAgent{Name: name, Definition: saved, Dir: skills.AgentDir(registryDir, name)}
	writeJSON(w, newRegistryAgent(a, s.agentSources()))
}

// handleRegistryAgentDelete removes an agent from the canonical store,
// via the importer so origin and lock-file entries leave with it.
// Agents are not gateway-routed content, so no registry-router refresh.
// DELETE /api/registry/agents/{name}
func (s *Server) handleRegistryAgentDelete(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	store := s.registryServer.Store()
	if !agentExists(store.Dir(), name) {
		writeJSONError(w, "Agent not found: "+name, http.StatusNotFound)
		return
	}

	// Retire the agent's projections before the store entry goes: an
	// orphaned project-lock row would keep reporting status for an agent
	// the catalog no longer serves, and a later named sync of that name
	// would fail the whole batch. Best-effort like the skill-delete lock
	// scrub: the store deletion below is authoritative, and a projection
	// cleanup failure is logged, not surfaced.
	if mgr, merr := s.agentsMgr(); merr == nil {
		if _, uerr := mgr.Unsync(r.Context(), []string{name}, agentsync.UnsyncOptions{}); uerr != nil && !errors.Is(uerr, agentsync.ErrNotProjected) {
			slog.Warn("delete agent: failed to remove projections", "agent", name, "error", uerr) // #nosec G706 -- name passed agentExists (ValidateAgentName: lowercase, digits, hyphens) above
		}
	}

	imp := skills.NewImporter(store, store.Dir(), s.lockFilePath(), slog.Default())
	if err := imp.RemoveAgent(name); err != nil {
		writeJSONError(w, "Failed to delete agent: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProjectAgentsStatus returns every (agent, client) projection row.
// GET /api/project/agents/status
func (s *Server) handleProjectAgentsStatus(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.agentsMgr()
	if err != nil {
		writeJSONError(w, "Agent projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	statuses, err := mgr.Statuses(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), agentErrorStatus(err))
		return
	}
	if statuses == nil {
		statuses = []agentsync.ProjectionStatus{}
	}
	writeJSON(w, statuses)
}

// agentSyncRequest is the body for POST /api/project/agents/sync. All
// fields optional; an empty body syncs every agent to every available
// client, matching the CLI default.
type agentSyncRequest struct {
	Agents  []string `json:"agents,omitempty"`
	Clients []string `json:"clients,omitempty"`
	Force   bool     `json:"force,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

// handleProjectAgentsSync projects agents into client directories.
// POST /api/project/agents/sync
func (s *Server) handleProjectAgentsSync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.agentsMgr()
	if err != nil {
		writeJSONError(w, "Agent projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req agentSyncRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	results, err := mgr.Sync(r.Context(), req.Agents, agentsync.SyncOptions{
		Clients: req.Clients,
		Force:   req.Force,
		DryRun:  req.DryRun,
	})
	if err != nil {
		writeJSONError(w, err.Error(), agentErrorStatus(err))
		return
	}
	if results == nil {
		results = []agentsync.SyncResult{}
	}
	writeJSON(w, results)
}

// agentUnsyncRequest is the body for POST /api/project/agents/unsync.
// Either named agents or all is required; an empty request is refused so
// a stray POST cannot silently strip every projection.
type agentUnsyncRequest struct {
	Agents  []string `json:"agents,omitempty"`
	Clients []string `json:"clients,omitempty"`
	All     bool     `json:"all,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

// handleProjectAgentsUnsync removes projected agent files.
// POST /api/project/agents/unsync
func (s *Server) handleProjectAgentsUnsync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.agentsMgr()
	if err != nil {
		writeJSONError(w, "Agent projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req agentUnsyncRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !req.All && len(req.Agents) == 0 {
		writeJSONError(w, "name agents to unsync, or set all", http.StatusBadRequest)
		return
	}
	results, err := mgr.Unsync(r.Context(), req.Agents, agentsync.UnsyncOptions{
		All:     req.All,
		Clients: req.Clients,
		DryRun:  req.DryRun,
	})
	if err != nil {
		writeJSONError(w, err.Error(), agentErrorStatus(err))
		return
	}
	if results == nil {
		results = []agentsync.UnsyncResult{}
	}
	writeJSON(w, results)
}

// handleProjectAgentsAdopt pulls a hand-edited projected file back into
// the canonical store. Refusals (lossy render targets, invalid projected
// content) are 409 with the refusal's full message: the UI renders that
// text verbatim, so it must never collapse into a generic error.
// POST /api/project/agents/adopt
func (s *Server) handleProjectAgentsAdopt(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.agentsMgr()
	if err != nil {
		writeJSONError(w, "Agent projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Agent  string `json:"agent"`
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Agent == "" || req.Client == "" {
		writeJSONError(w, "agent and client are required", http.StatusBadRequest)
		return
	}

	result, err := mgr.Adopt(r.Context(), req.Agent, req.Client)
	if err != nil {
		var refusal *agentsync.AdoptRefusal
		switch {
		case errors.As(err, &refusal):
			writeJSONError(w, refusal.Error(), http.StatusConflict)
		case errors.Is(err, agentsync.ErrUnknownAgent):
			writeJSONError(w, "Agent not found: "+req.Agent, http.StatusNotFound)
		default:
			writeJSONError(w, err.Error(), agentErrorStatus(err))
		}
		return
	}
	writeJSON(w, result)
}
