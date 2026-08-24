package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gridctl/gridctl/pkg/modelsync"
)

// modelsMgr returns the model routing manager, lazily built against the
// real home directory. Tests inject a temp-home manager via
// SetModelsManager before the first request; the lazy path is only
// reached in production, where the real home is correct. Reset shares
// this instance so fragment writes serialize on one in-process mutex.
func (s *Server) modelsMgr() (*modelsync.Manager, error) {
	s.modelsOnce.Do(func() {
		if s.modelsManager != nil {
			return
		}
		s.modelsManager, s.modelsErr = modelsync.NewManager()
	})
	return s.modelsManager, s.modelsErr
}

// SetModelsManager injects the model routing manager. Tests use it to
// keep the models handlers away from the real home directory.
func (s *Server) SetModelsManager(m *modelsync.Manager) {
	s.modelsOnce.Do(func() {})
	s.modelsManager = m
}

// modelsErrorStatus maps pkg/modelsync sentinel errors to HTTP statuses.
func modelsErrorStatus(err error) int {
	switch {
	case errors.Is(err, modelsync.ErrNoPolicy):
		return http.StatusNotFound
	case errors.Is(err, modelsync.ErrNotSynced), errors.Is(err, modelsync.ErrNewerLockVersion):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// modelsRoutingTiers is the tier-to-backend map in LiteLLM's fixed
// vocabulary. A struct, not a map: the wire order is the render order.
type modelsRoutingTiers struct {
	Simple    string `json:"SIMPLE"`
	Medium    string `json:"MEDIUM"`
	Complex   string `json:"COMPLEX"`
	Reasoning string `json:"REASONING"`
}

// modelsRouting is the read-only routing summary the dialog renders: a
// small DTO projected from the parsed policy, never the policy document
// itself (Policy has YAML tags only, and dumping passthrough or Extra
// would look like an editor payload).
type modelsRouting struct {
	EntryModel  string             `json:"entry_model"`
	DefaultTier string             `json:"default_tier"`
	Backends    []string           `json:"backends"`
	Tiers       modelsRoutingTiers `json:"tiers"`
}

// modelsStatusResponse is the status document: the CLI's modelsStatusDoc
// shape plus the routing summary. Targets is variable-length: one
// never-synced fragment row with no policy; include and OpenCode rows
// only when declared or recorded.
type modelsStatusResponse struct {
	PolicyPath     string             `json:"policy_path"`
	PolicyExists   bool               `json:"policy_exists"`
	NeedsAttention bool               `json:"needs_attention"`
	// PolicyError carries a parse failure so status stays a 200 with an
	// honest document instead of a 500 nothing can render.
	PolicyError string             `json:"policy_error,omitempty"`
	Routing     *modelsRouting     `json:"routing,omitempty"`
	Targets     []modelsync.Status `json:"targets"`
}

func newModelsRouting(p *modelsync.Policy) *modelsRouting {
	backends := p.Backends
	if backends == nil {
		backends = []string{}
	}
	return &modelsRouting{
		EntryModel:  p.Router.EntryModel,
		DefaultTier: p.Router.DefaultTier,
		Backends:    backends,
		Tiers: modelsRoutingTiers{
			Simple:    p.Tiers.Simple,
			Medium:    p.Tiers.Medium,
			Complex:   p.Tiers.Complex,
			Reasoning: p.Tiers.Reasoning,
		},
	}
}

// handleProjectModelsStatus returns the model routing status document.
// GET /api/project/models/status
func (s *Server) handleProjectModelsStatus(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.modelsMgr()
	if err != nil {
		writeJSONError(w, "Model routing not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	doc := modelsStatusResponse{
		PolicyPath:   mgr.PolicyPath(),
		PolicyExists: mgr.HasPolicy(),
		Targets:      []modelsync.Status{},
	}
	p, perr := mgr.LoadPolicy()
	switch {
	case perr == nil:
		doc.Routing = newModelsRouting(p)
	case errors.Is(perr, modelsync.ErrNoPolicy):
		// Fresh install: Statuses below reports the never-synced row.
	default:
		// An unparseable policy would also fail Statuses; report it in
		// the document rather than turning status into a 500.
		doc.PolicyError = perr.Error()
		writeJSON(w, doc)
		return
	}
	statuses, err := mgr.Statuses(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), modelsErrorStatus(err))
		return
	}
	doc.NeedsAttention = modelsync.NeedsAttention(statuses)
	if statuses != nil {
		doc.Targets = statuses
	}
	writeJSON(w, doc)
}

// modelsSyncRequest is the body for POST /api/project/models/sync.
// SyncOptions carries no JSON tags, so the wire shape is defined here.
// All fields optional; the empty body is a plain write-everything sync.
type modelsSyncRequest struct {
	DryRun bool `json:"dry_run,omitempty"`
	Diff   bool `json:"diff,omitempty"`
	Force  bool `json:"force,omitempty"`
}

// handleProjectModelsSync projects the policy into every declared
// target. The engine refuses an invalid policy with an unmatchable
// composite error, so the handler validates first and returns the
// findings as a 409 the UI can render row by row.
// POST /api/project/models/sync
func (s *Server) handleProjectModelsSync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.modelsMgr()
	if err != nil {
		writeJSONError(w, "Model routing not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req modelsSyncRequest
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	p, err := mgr.LoadPolicy()
	if err != nil {
		if errors.Is(err, modelsync.ErrNoPolicy) {
			writeJSONError(w, err.Error(), http.StatusNotFound)
		} else {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	if issues := mgr.Validate(p); modelsync.HasErrors(issues) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "policy is invalid; fix the validation errors before syncing",
			"issues": issues,
		})
		return
	}
	results, err := mgr.Sync(r.Context(), modelsync.SyncOptions{
		DryRun: req.DryRun,
		Diff:   req.Diff,
		Force:  req.Force,
	})
	if err != nil {
		writeJSONError(w, err.Error(), modelsErrorStatus(err))
		return
	}
	if results == nil {
		results = []modelsync.SyncResult{}
	}
	writeJSON(w, results)
}

// handleProjectModelsAdopt records every recorded target's on-disk
// state as gridctl-owned. No file is modified, and the include line is
// not adoptable; ErrNotSynced (nothing recorded) is a 409 whose message
// the UI renders verbatim.
// POST /api/project/models/adopt
func (s *Server) handleProjectModelsAdopt(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.modelsMgr()
	if err != nil {
		writeJSONError(w, "Model routing not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	results, err := mgr.Adopt(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), modelsErrorStatus(err))
		return
	}
	if results == nil {
		results = []modelsync.AdoptResult{}
	}
	writeJSON(w, results)
}

// handleProjectModelsAckRestart clears the restart-pending latch. The
// engine has no result struct for this; the UI refetches status.
// POST /api/project/models/ack-restart
func (s *Server) handleProjectModelsAckRestart(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.modelsMgr()
	if err != nil {
		writeJSONError(w, "Model routing not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := mgr.AckRestart(r.Context()); err != nil {
		writeJSONError(w, err.Error(), modelsErrorStatus(err))
		return
	}
	writeJSON(w, map[string]bool{"acknowledged": true})
}

// modelsValidateResponse mirrors the CLI's validate document.
type modelsValidateResponse struct {
	PolicyPath string            `json:"policy_path"`
	Valid      bool              `json:"valid"`
	Issues     []modelsync.Issue `json:"issues"`
}

// handleProjectModelsValidate validates the policy document.
// GET /api/project/models/validate
func (s *Server) handleProjectModelsValidate(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.modelsMgr()
	if err != nil {
		writeJSONError(w, "Model routing not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	p, err := mgr.LoadPolicy()
	if err != nil {
		if errors.Is(err, modelsync.ErrNoPolicy) {
			writeJSONError(w, err.Error(), http.StatusNotFound)
		} else {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	issues := mgr.Validate(p)
	if issues == nil {
		issues = []modelsync.Issue{}
	}
	writeJSON(w, modelsValidateResponse{
		PolicyPath: mgr.PolicyPath(),
		Valid:      !modelsync.HasErrors(issues),
		Issues:     issues,
	})
}
