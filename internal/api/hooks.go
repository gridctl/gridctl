package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/skills"
)

// hooksMgr returns the hook projection manager, lazily built against the
// real home directory and the live registry directory. Tests inject a
// temp manager via SetHooksManager before the first request.
func (s *Server) hooksMgr() (*hooksync.Manager, error) {
	s.hooksOnce.Do(func() {
		if s.hooksManager != nil {
			return
		}
		if s.registryServer == nil {
			s.hooksErr = errors.New("registry not available")
			return
		}
		mgr, err := hooksync.NewManager(s.registryServer.Store().Dir())
		if err != nil {
			s.hooksErr = err
			return
		}
		s.hooksManager = mgr
	})
	return s.hooksManager, s.hooksErr
}

// SetHooksManager injects the hook projection manager. Tests use it to
// keep projection handlers away from the real home directory. Pack
// handlers built by packsMgr pick up the same manager.
func (s *Server) SetHooksManager(m *hooksync.Manager) {
	s.hooksOnce.Do(func() {})
	s.hooksManager = m
}

func hookErrorStatus(err error) int {
	switch {
	case errors.Is(err, hooksync.ErrUnknownHook):
		return http.StatusNotFound
	case errors.Is(err, hooksync.ErrUnknownClient):
		return http.StatusBadRequest
	case errors.Is(err, hooksync.ErrNotProjected), errors.Is(err, hooksync.ErrNewerLockVersion):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

type hookUnsupportedClient struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Location  string `json:"location"`
	CoveredBy string `json:"covered_by,omitempty"`
	Detected  bool   `json:"detected"`
}

type hookFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// registryHookListItem is the catalog row: what the Library groups and
// badges. Document, files, and matcher groups stay on the single-hook GET.
type registryHookListItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Dialect     string   `json:"dialect,omitempty"`
	Events      []string `json:"events"`
	Scripts     int      `json:"scripts"`
	Source      string   `json:"source,omitempty"`
	PinStatus   string   `json:"pin_status"`
	Error       string   `json:"error,omitempty"`
}

// registryHook is the inspector payload: definition, files, pin status,
// and projection chips.
type registryHook struct {
	registryHookListItem
	Dir         string                      `json:"dir,omitempty"`
	Document    string                      `json:"document,omitempty"`
	Hooks       json.RawMessage             `json:"hooks,omitempty"`
	Files       []hookFile                  `json:"files"`
	Warnings    []string                    `json:"warnings,omitempty"`
	Projections []hooksync.ProjectionStatus `json:"projections"`
}

type registryHookList struct {
	Hooks              []registryHookListItem  `json:"hooks"`
	UnsupportedClients []hookUnsupportedClient `json:"unsupported_clients"`
}

func hookSource(dir string) string {
	origin, err := skills.ReadOrigin(dir)
	if err != nil {
		return ""
	}
	if origin.IsLocal() {
		return "local"
	}
	return origin.Repo
}

func (s *Server) hookPinStatus(name string) string {
	ps, err := s.loadHookPins()
	if err != nil {
		return "unpinned"
	}
	pin, ok := ps.Get(name)
	if !ok || pin == nil {
		return "unpinned"
	}
	if pin.Status == "" {
		return "unpinned"
	}
	return pin.Status
}

func (s *Server) newRegistryHookListItem(h skills.InstalledHook) registryHookListItem {
	item := registryHookListItem{
		Name:      h.Name,
		Events:    []string{},
		Source:    hookSource(h.Dir),
		PinStatus: s.hookPinStatus(h.Name),
		Error:     h.Err,
	}
	if h.Definition == nil {
		if item.Error == "" {
			item.Error = "hook.json is missing or does not validate"
		}
		item.Description = "(broken)"
		return item
	}
	item.Description = h.Definition.Description
	item.Dialect = h.Definition.Dialect
	item.Events = append([]string{}, h.Definition.Events...)
	if item.Events == nil {
		item.Events = []string{}
	}
	item.Scripts = len(h.Definition.Scripts)
	return item
}

func hookProjections(mgr *hooksync.Manager, r *http.Request, name string) []hooksync.ProjectionStatus {
	if mgr == nil {
		return []hooksync.ProjectionStatus{}
	}
	statuses, err := mgr.Statuses(r.Context())
	if err != nil {
		slog.Warn("hook detail: projection status unavailable", "hook", name, "error", err) // #nosec G706 -- name passed ValidateHookName (lowercase, digits, hyphens) above
		return []hooksync.ProjectionStatus{}
	}
	out := make([]hooksync.ProjectionStatus, 0)
	for _, st := range statuses {
		if name == "" || st.Hook == name {
			out = append(out, st)
		}
	}
	return out
}

func unsupportedClients(mgr *hooksync.Manager) []hookUnsupportedClient {
	detected := map[string]bool{}
	if mgr != nil {
		for _, c := range mgr.DetectedUnsupported() {
			detected[c.Slug] = true
		}
	}
	out := make([]hookUnsupportedClient, 0, len(hooksync.UnsupportedClients()))
	for _, c := range hooksync.UnsupportedClients() {
		out = append(out, hookUnsupportedClient{
			Slug:      c.Slug,
			Name:      c.Name,
			Location:  c.Location,
			CoveredBy: c.CoveredBy,
			Detected:  detected[c.Slug],
		})
	}
	return out
}

// handleRegistryHooksList returns installed hooks and the unsupported-client table.
// GET /api/registry/hooks
func (s *Server) handleRegistryHooksList(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	listed, err := skills.ListHooks(s.registryServer.Store().Dir())
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]registryHookListItem, 0, len(listed))
	for _, h := range listed {
		items = append(items, s.newRegistryHookListItem(h))
	}
	var mgr *hooksync.Manager
	if m, merr := s.hooksMgr(); merr == nil {
		mgr = m
	}
	writeJSON(w, registryHookList{Hooks: items, UnsupportedClients: unsupportedClients(mgr)})
}

// handleRegistryHookGet returns one hook: definition, files, pin status, and projection chips.
// GET /api/registry/hooks/{name}
func (s *Server) handleRegistryHookGet(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	hook, err := loadRegistryHook(s.registryServer.Store().Dir(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errHookNotFound) || skills.ValidateHookName(name) != nil {
			status = http.StatusNotFound
		}
		writeJSONError(w, err.Error(), status)
		return
	}
	item := s.newRegistryHookListItem(*hook)
	detail := registryHook{
		registryHookListItem: item,
		Dir:                  hook.Dir,
		Files:                listHookFiles(hook.Dir),
		Projections:          []hooksync.ProjectionStatus{},
	}
	if data, rerr := os.ReadFile(filepath.Join(hook.Dir, "hook.json")); rerr == nil { // #nosec G304 -- name validated, fixed file name
		detail.Document = string(data)
		var raw map[string]json.RawMessage
		if json.Unmarshal(data, &raw) == nil {
			detail.Hooks = raw["hooks"]
		}
	}
	if hook.Definition != nil {
		detail.Warnings = append([]string{}, hook.Definition.Warnings...)
	}
	if mgr, merr := s.hooksMgr(); merr == nil {
		detail.Projections = hookProjections(mgr, r, name)
	}
	writeJSON(w, detail)
}

var errHookNotFound = errors.New("hook not found")

func loadRegistryHook(registryDir, name string) (*skills.InstalledHook, error) {
	if err := skills.ValidateHookName(name); err != nil {
		return nil, fmt.Errorf("%w: %s", errHookNotFound, name)
	}
	dir := skills.HookDir(registryDir, name)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", errHookNotFound, name)
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", errHookNotFound, name)
	}
	entry := &skills.InstalledHook{Name: name, Dir: dir}
	data, rerr := os.ReadFile(filepath.Join(dir, "hook.json")) // #nosec G304 -- name validated, fixed file name
	if rerr != nil {
		entry.Err = rerr.Error()
		return entry, nil
	}
	def, perr := skills.ParseHookJSON(data, dir)
	if perr != nil {
		entry.Err = perr.Error()
		return entry, nil
	}
	entry.Definition = def
	return entry, nil
}

func listHookFiles(dir string) []hookFile {
	files := []hookFile{}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || strings.Contains(name, ".pre-") {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".origin.json" {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		files = append(files, hookFile{Path: rel, Size: info.Size()})
		return nil
	})
	return files
}

// handleRegistryHookDelete retires projections first, then removes the
// registry directory, lock entry, and pin. A row that is not removed or
// already-gone refuses the delete with 409.
// DELETE /api/registry/hooks/{name}
func (s *Server) handleRegistryHookDelete(w http.ResponseWriter, r *http.Request) {
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	registryDir := s.registryServer.Store().Dir()
	if _, err := loadRegistryHook(registryDir, name); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errHookNotFound) || skills.ValidateHookName(name) != nil {
			status = http.StatusNotFound
		}
		writeJSONError(w, err.Error(), status)
		return
	}
	mgr, merr := s.hooksMgr()
	if merr != nil {
		writeJSONError(w, "Hook projection not available: "+merr.Error(), http.StatusServiceUnavailable)
		return
	}
	results, uerr := mgr.Unsync(r.Context(), []string{name}, hooksync.UnsyncOptions{})
	if uerr != nil && !errors.Is(uerr, hooksync.ErrNotProjected) {
		writeJSONError(w, uerr.Error(), hookErrorStatus(uerr))
		return
	}
	if row, blocked := hooksync.BlocksRemoval(results); blocked {
		writeJSONError(w, hooksync.RemovalRefusal(name, row), http.StatusConflict)
		return
	}
	if err := skills.DeleteHook(registryDir, name); err != nil {
		writeJSONError(w, "Failed to delete hook: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := skills.MutateLockFile(r.Context(), s.lockFilePath(), func(lf *skills.LockFile) (bool, error) {
		lf.RemoveHook(name)
		return true, nil
	}); err != nil && !os.IsNotExist(err) {
		slog.Warn("delete hook: lock update failed", "hook", name, "error", err) // #nosec G706 -- name passed ValidateHookName (lowercase, digits, hyphens) above
	}
	if ps, perr := s.loadHookPins(); perr == nil {
		if err := ps.Reset(r.Context(), name); err != nil && !errors.Is(err, hookpins.ErrNotPinned) {
			slog.Warn("delete hook: pin reset failed", "hook", name, "error", err) // #nosec G706 -- name passed ValidateHookName (lowercase, digits, hyphens) above
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProjectHooksStatus returns every recorded (hook, client) row.
// GET /api/project/hooks/status
func (s *Server) handleProjectHooksStatus(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.hooksMgr()
	if err != nil {
		writeJSONError(w, "Hook projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	statuses, err := mgr.Statuses(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), hookErrorStatus(err))
		return
	}
	if statuses == nil {
		statuses = []hooksync.ProjectionStatus{}
	}
	writeJSON(w, statuses)
}

type hookSyncRequest struct {
	Hooks   []string `json:"hooks,omitempty"`
	Clients []string `json:"clients,omitempty"`
	Force   bool     `json:"force,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

// handleProjectHooksSync projects hooks into Claude Code.
// POST /api/project/hooks/sync
func (s *Server) handleProjectHooksSync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.hooksMgr()
	if err != nil {
		writeJSONError(w, "Hook projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req hookSyncRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	results, err := mgr.Sync(r.Context(), req.Hooks, hooksync.SyncOptions{
		Clients: req.Clients,
		Force:   req.Force,
		DryRun:  req.DryRun,
	})
	if err != nil {
		writeJSONError(w, err.Error(), hookErrorStatus(err))
		return
	}
	if results == nil {
		results = []hooksync.SyncResult{}
	}
	writeJSON(w, results)
}

type hookUnsyncRequest struct {
	Hooks   []string `json:"hooks,omitempty"`
	Clients []string `json:"clients,omitempty"`
	All     bool     `json:"all,omitempty"`
	Force   bool     `json:"force,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

// handleProjectHooksUnsync removes recorded hook groups. An empty body is 400.
// POST /api/project/hooks/unsync
func (s *Server) handleProjectHooksUnsync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.hooksMgr()
	if err != nil {
		writeJSONError(w, "Hook projection not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req hookUnsyncRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !req.All && len(req.Hooks) == 0 {
		writeJSONError(w, "name hooks to unsync, or set all", http.StatusBadRequest)
		return
	}
	results, err := mgr.Unsync(r.Context(), req.Hooks, hooksync.UnsyncOptions{
		All:     req.All,
		Clients: req.Clients,
		Force:   req.Force,
		DryRun:  req.DryRun,
	})
	if err != nil {
		writeJSONError(w, err.Error(), hookErrorStatus(err))
		return
	}
	if results == nil {
		results = []hooksync.UnsyncResult{}
	}
	writeJSON(w, results)
}
