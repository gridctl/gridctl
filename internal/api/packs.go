package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
)

// decodeJSONBody decodes a required JSON request body.
func decodeJSONBody(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

// skillsyncManager builds the skill projection manager against the live
// registry. Packs are its first REST consumer; there is deliberately no
// standalone /api/project/skills namespace yet.
func (s *Server) skillsyncManager() (*skillsync.Manager, error) {
	if s.registryServer == nil {
		return nil, errors.New("registry not available")
	}
	return skillsync.NewManager(s.registryServer.Store())
}

// packsMgr lazily builds the pack orchestration engine against the real
// home directory and the live registry. Tests inject a temp-home engine
// via SetPacksManagers before the first request.
func (s *Server) packsMgr() (*packops.Managers, error) {
	s.packsOnce.Do(func() {
		if s.packsManagers != nil {
			return
		}
		if s.registryServer == nil {
			s.packsErr = errors.New("registry not available")
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			s.packsErr = err
			return
		}
		sm, err := s.skillsyncManager()
		if err != nil {
			s.packsErr = err
			return
		}
		am, err := s.agentsMgr()
		if err != nil {
			s.packsErr = err
			return
		}
		wm, err := s.wiringMgr()
		if err != nil {
			s.packsErr = err
			return
		}
		cm, err := s.contextsMgr()
		if err != nil {
			s.packsErr = err
			return
		}
		s.packsManagers = &packops.Managers{Skills: sm, Agents: am, Wiring: wm, Contexts: cm, Home: home, LockPath: s.lockFilePath()}
	})
	return s.packsManagers, s.packsErr
}

// SetPacksManagers injects the pack engine. Tests use it to keep pack
// handlers away from the real home directory.
func (s *Server) SetPacksManagers(m *packops.Managers) {
	s.packsOnce.Do(func() {})
	s.packsManagers = m
}

// packsImporter builds the import engine the way the skill source
// handlers do (fresh per request; the import lockfile's cross-process
// lock serializes the writes).
func (s *Server) packsImporter() (*skills.Importer, error) {
	if s.registryServer == nil {
		return nil, errors.New("registry not available")
	}
	store := s.registryServer.Store()
	imp := skills.NewImporter(store, store.Dir(), s.lockFilePath(), slog.Default())
	imp.SetCredentialResolver(s.credentialResolver())
	return imp, nil
}

// packErrorStatus maps pkg/packops sentinel errors to HTTP statuses.
func packErrorStatus(err error) int {
	var fe *packops.FindingsError
	switch {
	case errors.Is(err, packops.ErrNotImported):
		return http.StatusNotFound
	case errors.Is(err, packops.ErrNameCollision):
		return http.StatusConflict
	case errors.As(err, &fe):
		return http.StatusConflict
	case errors.Is(err, packops.ErrNoManifest):
		return http.StatusUnprocessableEntity
	case errors.Is(err, skills.ErrImportLockBusy):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// packListItem is one installed pack in the list response: the identity
// fields without the per-resource rows.
type packListItem struct {
	packops.PackInfo
	NeedsAttention bool `json:"needs_attention"`
}

// handlePacksList lists installed packs with identity, origin, counts,
// and aggregate attention.
// GET /api/packs
func (s *Server) handlePacksList(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.packsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	statuses, err := mgr.Statuses(r.Context(), packops.StatusOptions{GatewayPort: s.gatewayPortOrDefault()})
	if err != nil {
		writeJSONError(w, err.Error(), packErrorStatus(err))
		return
	}
	items := make([]packListItem, 0, len(statuses))
	for _, st := range statuses {
		items = append(items, packListItem{PackInfo: st.Info, NeedsAttention: st.NeedsAttention})
	}
	writeJSON(w, map[string]any{"packs": items})
}

// handlePackGet returns one pack's identity plus its per-resource state
// rows. A name claimed by two sources is a 409 naming both repos.
// GET /api/packs/{name}
func (s *Server) handlePackGet(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.packsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	statuses, err := mgr.Statuses(r.Context(), packops.StatusOptions{Pack: name, GatewayPort: s.gatewayPortOrDefault()})
	if err != nil {
		writeJSONError(w, err.Error(), packErrorStatus(err))
		return
	}
	if len(statuses) != 1 {
		// findPack refuses collisions before this point; an unexpected
		// shape here is a server bug, not a client one.
		writeJSONError(w, "unexpected pack status shape", http.StatusInternalServerError)
		return
	}
	writeJSON(w, statuses[0])
}

// handlePackAdd imports a pack from git, mirroring `gridctl pack add`
// including the blocking security scan. Unlike the CLI (which partially
// imports and reports skips), findings without trust refuse before any
// write with a 409 carrying the flagged resources, so the trust decision
// always precedes the import. A POST against an already-imported origin
// is the documented update path: changed upstream rules refresh, locally
// edited rules are left alone, and the selection re-resolves.
// POST /api/packs
func (s *Server) handlePackAdd(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.packsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	imp, err := s.packsImporter()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Repo   string `json:"repo"`
		Ref    string `json:"ref,omitempty"`
		Path   string `json:"path,omitempty"`
		Trust  bool   `json:"trust,omitempty"`
		DryRun bool   `json:"dryRun,omitempty"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Repo == "" {
		writeJSONError(w, "repo is required", http.StatusBadRequest)
		return
	}

	res, err := mgr.Add(r.Context(), imp, packops.AddOptions{
		Repo:            req.Repo,
		Ref:             req.Ref,
		Path:            req.Path,
		Trust:           req.Trust,
		DryRun:          req.DryRun,
		BlockOnFindings: true,
	})
	if err != nil {
		var fe *packops.FindingsError
		if errors.As(err, &fe) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{
				"error":    err.Error(),
				"pack":     fe.Pack,
				"findings": fe.Resources,
			})
			return
		}
		if status := packErrorStatus(err); status != http.StatusInternalServerError {
			writeJSONError(w, err.Error(), status)
			return
		}
		writeGitError(w, "Pack import failed: ", err)
		return
	}

	s.refreshRegistryRouter()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"doc": res.Doc, "notes": res.Notes})
}

// handlePackPreview resolves a pack manifest against its repository
// without writing anything: the wizard's read-only review step.
// POST /api/packs/preview
func (s *Server) handlePackPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
		Ref  string `json:"ref,omitempty"`
		Path string `json:"path,omitempty"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Repo == "" {
		writeJSONError(w, "repo is required", http.StatusBadRequest)
		return
	}
	res, err := packops.Preview(r.Context(), packops.PreviewOptions{Repo: req.Repo, Ref: req.Ref, Path: req.Path})
	if err != nil {
		if status := packErrorStatus(err); status != http.StatusInternalServerError {
			writeJSONError(w, err.Error(), status)
			return
		}
		writeGitError(w, "Pack preview failed: ", err)
		return
	}
	writeJSON(w, res)
}

// handlePackApply projects one pack, with full CLI flag parity. The
// response is the honest non-transactional per-resource document.
// POST /api/packs/{name}/apply
func (s *Server) handlePackApply(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.packsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	var req struct {
		Clients []string `json:"clients,omitempty"`
		Force   bool     `json:"force,omitempty"`
		DryRun  bool     `json:"dry_run,omitempty"`
	}
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	doc, err := mgr.Apply(r.Context(), name, packops.ApplyOptions{Force: req.Force, DryRun: req.DryRun, Clients: req.Clients})
	if err != nil {
		writeJSONError(w, err.Error(), packErrorStatus(err))
		return
	}
	writeJSON(w, doc)
}

// handlePackRemove cascades one pack's removal. dry_run previews the
// cascade without executing; force removes drifted resources too.
// DELETE /api/packs/{name}?dry_run=1&force=1
func (s *Server) handlePackRemove(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.packsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	imp, err := s.packsImporter()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	opts := packops.RemoveOptions{
		Force:       queryFlag(r, "force"),
		DryRun:      queryFlag(r, "dry_run"),
		GatewayPort: s.gatewayPortOrDefault(),
	}
	doc, err := mgr.Remove(r.Context(), imp, name, opts)
	if err != nil {
		writeJSONError(w, err.Error(), packErrorStatus(err))
		return
	}
	if !opts.DryRun {
		s.refreshRegistryRouter()
	}
	writeJSON(w, doc)
}

// queryFlag reads a boolean query parameter ("1" or "true").
func queryFlag(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	return v == "1" || v == "true"
}
