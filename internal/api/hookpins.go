package api

import (
	"errors"
	"net/http"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skills"
)

// hookPinStore returns the stackless hook pin store. Tests inject one via
// SetHookPinStore. Production builds hookpins.New, which resolves
// ~/.gridctl/pins/hooks/global.json on first use and honors GRIDCTL_HOME.
func (s *Server) hookPinStore() *hookpins.Store {
	s.hookPinsOnce.Do(func() {
		if s.hookPins != nil {
			return
		}
		s.hookPins = hookpins.New()
	})
	return s.hookPins
}

// SetHookPinStore injects the hook pin store. Tests use it so pin handlers
// never touch the real home directory.
func (s *Server) SetHookPinStore(ps *hookpins.Store) {
	s.hookPinsOnce.Do(func() {})
	s.hookPins = ps
}

// loadHookPins reloads the pin file. The store is stateless between calls.
func (s *Server) loadHookPins() (*hookpins.Store, error) {
	ps := s.hookPinStore()
	if ps == nil {
		return nil, errors.New("hook pinning not available")
	}
	if err := ps.Load(); err != nil {
		return nil, err
	}
	return ps, nil
}

type hookPinDiffResponse struct {
	Hook          string         `json:"hook"`
	Status        string         `json:"status"`
	CompositeHash string         `json:"composite_hash"`
	OldDocument   string         `json:"old_document,omitempty"`
	NewDocument   string         `json:"new_document,omitempty"`
	AddedFiles    []string       `json:"added_files"`
	RemovedFiles  []string       `json:"removed_files"`
	ModifiedFiles []string       `json:"modified_files"`
	Findings      []pins.Finding `json:"findings"`
}

// handleListHookPins returns every hook pin, keyed by name.
// GET /api/hook-pins
func (s *Server) handleListHookPins(w http.ResponseWriter, _ *http.Request) {
	ps, err := s.loadHookPins()
	if err != nil {
		writeJSON(w, map[string]*hookpins.HookPin{})
		return
	}
	pinsMap := ps.Pins()
	if pinsMap == nil {
		pinsMap = map[string]*hookpins.HookPin{}
	}
	writeJSON(w, pinsMap)
}

// handleGetHookPin returns one hook pin.
// GET /api/hook-pins/{name}
func (s *Server) handleGetHookPin(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadHookPins()
	if err != nil {
		writeJSONError(w, "Hook pinning not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if rejectHookPinName(w, name) {
		return
	}
	pin, ok := ps.Get(name)
	if !ok {
		writeJSONError(w, "No pin found for hook: "+name, http.StatusNotFound)
		return
	}
	writeJSON(w, pin)
}

// handleHookPinDiff returns what changed since a hook was pinned.
// GET /api/hook-pins/{name}/diff
func (s *Server) handleHookPinDiff(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadHookPins()
	if err != nil {
		writeJSONError(w, "Hook pinning not available", http.StatusServiceUnavailable)
		return
	}
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if rejectHookPinName(w, name) {
		return
	}
	vr, err := ps.Verify(name, s.registryServer.Store().Dir())
	switch {
	case errors.Is(err, hookpins.ErrDigestUnavailable):
		writeJSONError(w, "Hook content could not be hashed: "+err.Error(), http.StatusInternalServerError)
		return
	case errors.Is(err, hookpins.ErrNotFound):
		writeJSONError(w, "Hook not found in registry: "+name, http.StatusNotFound)
		return
	case errors.Is(err, hookpins.ErrNotPinned):
		writeJSONError(w, "No pin found for hook: "+name, http.StatusNotFound)
		return
	case err != nil:
		writeJSONError(w, "Failed to verify hook: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp := hookPinDiffResponse{
		Hook:          name,
		Status:        vr.Status,
		CompositeHash: vr.CompositeHash,
		AddedFiles:    []string{},
		RemovedFiles:  []string{},
		ModifiedFiles: []string{},
		Findings:      []pins.Finding{},
	}
	if d := vr.Diff; d != nil {
		resp.OldDocument = d.OldDocument
		resp.NewDocument = d.NewDocument
		if d.AddedFiles != nil {
			resp.AddedFiles = d.AddedFiles
		}
		if d.RemovedFiles != nil {
			resp.RemovedFiles = d.RemovedFiles
		}
		if d.ModifiedFiles != nil {
			resp.ModifiedFiles = d.ModifiedFiles
		}
		if d.Findings != nil {
			resp.Findings = d.Findings
		}
	}
	writeJSON(w, resp)
}

// approveHookPinRequest is the body for POST /api/hook-pins/{name}/approve.
// expect is the reviewed composite hash. expected_hash is accepted as the
// skill-pin alias.
type approveHookPinRequest struct {
	Expect       string   `json:"expect"`
	ExpectedHash string   `json:"expected_hash"`
	Reason       string   `json:"reason"`
	Evidence     []string `json:"evidence"`
	ApprovedBy   string   `json:"approved_by"`
}

func (r approveHookPinRequest) expect() string {
	if r.Expect != "" {
		return r.Expect
	}
	return r.ExpectedHash
}

// handleApproveHookPin re-pins a hook's current bytes.
// POST /api/hook-pins/{name}/approve
func (s *Server) handleApproveHookPin(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadHookPins()
	if err != nil {
		writeJSONError(w, "Hook pinning not available", http.StatusServiceUnavailable)
		return
	}
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if rejectHookPinName(w, name) {
		return
	}
	var body approveHookPinRequest
	if err := decodeApproveBody(r, &body); err != nil {
		if errors.Is(err, errApproveBodyTooLarge) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONError(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := pinapprovals.ValidateReason(body.Reason); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	evidence, err := pinapprovals.ValidateReferences(body.Evidence)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	approvedBy, approvedBySource, err := resolveApprovalAttribution(r, body.ApprovedBy)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	registryDir := s.registryServer.Store().Dir()
	findings, scanned := scanHookForApproval(name, registryDir)
	err = ps.Approve(r.Context(), name, registryDir, hookpins.ApproveRequest{
		ExpectedHash:     body.expect(),
		Reason:           body.Reason,
		Evidence:         evidence,
		ApprovedBy:       approvedBy,
		ApprovedBySource: approvedBySource,
		Findings:         findings,
		Scanned:          scanned,
	})
	switch {
	case approvalInputStatus(err):
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, hookpins.ErrDigestUnavailable):
		writeJSONError(w, "Hook content could not be hashed: "+err.Error(), http.StatusInternalServerError)
		return
	case errors.Is(err, hookpins.ErrNotFound):
		writeJSONError(w, "Hook not found in registry: "+name, http.StatusNotFound)
		return
	case errors.Is(err, hookpins.ErrNotPinned):
		writeJSONError(w, "No pin found for hook: "+name, http.StatusNotFound)
		return
	case errors.Is(err, hookpins.ErrHashMismatch):
		writeJSONError(w, "Hook content changed since the reviewed diff; fetch the diff again and re-review", http.StatusConflict)
		return
	case errors.Is(err, hookpins.ErrReasonRequired):
		writeJSONError(w, "This hook carries unresolved findings; approving it requires a reason", http.StatusBadRequest)
		return
	case err != nil:
		writeJSONError(w, "Failed to approve hook pin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeApproveJSON(w, map[string]any{"hook": name, "status": "approved"}, approvedBy, approvedBySource, len(evidence))
}

// rejectHookPinName refuses a name that is not a hook name before any
// registry path is built. The 404 matches a missing pin so the route does
// not say whether a directory exists.
func rejectHookPinName(w http.ResponseWriter, name string) bool {
	if skills.ValidateHookName(name) == nil {
		return false
	}
	writeJSONError(w, "No pin found for hook: "+name, http.StatusNotFound)
	return true
}

func scanHookForApproval(name, registryDir string) ([]pins.Finding, bool) {
	hook, err := skills.GetHook(registryDir, name)
	if err != nil || hook.Definition == nil {
		return nil, false
	}
	scan := skills.ScanHook(name, hook.Definition.Raw, hook.Dir)
	if scan == nil || len(scan.Findings) == 0 {
		return nil, true
	}
	out := make([]pins.Finding, 0, len(scan.Findings))
	for _, f := range scan.Findings {
		sev := pins.SeverityWarn
		if f.Severity == "danger" {
			sev = pins.SeverityCritical
		}
		out = append(out, pins.Finding{
			Code:     "hook-scan",
			Severity: sev,
			Field:    f.StepID,
			Message:  f.Description,
			Snippet:  f.Pattern,
		})
	}
	return out, true
}

// handleResetHookPin deletes a hook's pin record.
// DELETE /api/hook-pins/{name}
func (s *Server) handleResetHookPin(w http.ResponseWriter, r *http.Request) {
	ps, err := s.loadHookPins()
	if err != nil {
		writeJSONError(w, "Hook pinning not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if rejectHookPinName(w, name) {
		return
	}
	if _, ok := ps.Get(name); !ok {
		writeJSONError(w, "No pin found for hook: "+name, http.StatusNotFound)
		return
	}
	if err := ps.Reset(r.Context(), name); err != nil {
		writeJSONError(w, "Failed to reset hook pin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
