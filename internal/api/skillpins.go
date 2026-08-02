package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

// skillPinDiffResponse is the wire shape for GET /api/skill-pins/{name}/diff.
// CompositeHash is the approval-binding fingerprint of the CURRENT content;
// the approve endpoint compares it against expected_hash so content cannot
// change between review and approval (the tool-pins expected_server_hash
// precedent). Slices are always non-nil so consumers can iterate without
// null checks.
type skillPinDiffResponse struct {
	Skill         string         `json:"skill"`
	Status        string         `json:"status"`
	CompositeHash string         `json:"composite_hash"`
	OldDocument   string         `json:"old_document,omitempty"`
	NewDocument   string         `json:"new_document,omitempty"`
	AddedFiles    []string       `json:"added_files"`
	RemovedFiles  []string       `json:"removed_files"`
	ModifiedFiles []string       `json:"modified_files"`
	Findings      []pins.Finding `json:"findings"`
}

// handleListSkillPins returns every skill pin, keyed by skill name.
// GET /api/skill-pins
//
// A nil store returns an empty object with 200 rather than an error status:
// the UI polls this endpoint, and a stack without skill pinning should
// render an empty view, not console spam (the /api/pins precedent).
func (s *Server) handleListSkillPins(w http.ResponseWriter, _ *http.Request) {
	if s.skillPinStore == nil {
		writeJSON(w, map[string]*skillpins.SkillPin{})
		return
	}
	writeJSON(w, s.skillPinStore.GetAll())
}

// handleGetSkillPin returns the pin record for a single skill.
// GET /api/skill-pins/{name}
func (s *Server) handleGetSkillPin(w http.ResponseWriter, r *http.Request) {
	if s.skillPinStore == nil {
		writeJSONError(w, "Skill pinning not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	pin, ok := s.skillPinStore.Get(name)
	if !ok {
		writeJSONError(w, "No pin found for skill: "+name, http.StatusNotFound)
		return
	}
	writeJSON(w, pin)
}

// handleSkillPinDiff returns what changed since a skill was pinned, plus the
// composite hash an approval must echo. Viewing a diff never persists.
// GET /api/skill-pins/{name}/diff
func (s *Server) handleSkillPinDiff(w http.ResponseWriter, r *http.Request) {
	if s.skillPinStore == nil {
		writeJSONError(w, "Skill pinning not available", http.StatusServiceUnavailable)
		return
	}
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	store := s.registryServer.Store()

	vr, err := s.skillPinStore.Verify(name, store)
	switch {
	case errors.Is(err, skillpins.ErrDigestUnavailable):
		// Checked before ErrNotFound: unreadable content is a fail-closed
		// condition, never "this skill does not exist".
		writeJSONError(w, "Skill content could not be hashed: "+err.Error(), http.StatusInternalServerError)
		return
	case errors.Is(err, registry.ErrNotFound):
		writeJSONError(w, "Skill not found in registry: "+name, http.StatusNotFound)
		return
	case errors.Is(err, skillpins.ErrNotPinned):
		writeJSONError(w, "No pin found for skill: "+name, http.StatusNotFound)
		return
	case err != nil:
		writeJSONError(w, "Failed to verify skill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := skillPinDiffResponse{
		Skill:         name,
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
		resp.AddedFiles = d.AddedFiles
		resp.RemovedFiles = d.RemovedFiles
		resp.ModifiedFiles = d.ModifiedFiles
		resp.Findings = d.Findings
	}
	writeJSON(w, resp)
}

// approveSkillPinRequest is the optional body for the approve endpoint.
type approveSkillPinRequest struct {
	// ExpectedHash binds the approval to the reviewed diff's composite hash.
	// Empty approves unconditionally.
	ExpectedHash string `json:"expected_hash"`
	// Reason is the human justification, required when the current content
	// carries unresolved advisory findings.
	Reason string `json:"reason"`
}

// handleApproveSkillPin re-pins a skill's current content, clearing drift.
// POST /api/skill-pins/{name}/approve
func (s *Server) handleApproveSkillPin(w http.ResponseWriter, r *http.Request) {
	if s.skillPinStore == nil {
		writeJSONError(w, "Skill pinning not available", http.StatusServiceUnavailable)
		return
	}
	if s.registryServer == nil {
		writeJSONError(w, "Registry not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")

	var body approveSkillPinRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	err := s.skillPinStore.Approve(name, s.registryServer.Store(), body.ExpectedHash, body.Reason)
	switch {
	case errors.Is(err, skillpins.ErrDigestUnavailable):
		writeJSONError(w, "Skill content could not be hashed: "+err.Error(), http.StatusInternalServerError)
		return
	case errors.Is(err, registry.ErrNotFound):
		writeJSONError(w, "Skill not found in registry: "+name, http.StatusNotFound)
		return
	case errors.Is(err, skillpins.ErrHashMismatch):
		writeJSONError(w, "Skill content changed since the reviewed diff; fetch the diff again and re-review", http.StatusConflict)
		return
	case errors.Is(err, skillpins.ErrReasonRequired):
		writeJSONError(w, "This skill carries unresolved advisory findings; approving it requires a reason", http.StatusBadRequest)
		return
	case err != nil:
		writeJSONError(w, "Failed to approve skill pin: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"skill": name, "status": "approved"})
}

// handleResetSkillPin deletes a skill's pin record; the next registry
// refresh re-pins fresh.
// DELETE /api/skill-pins/{name}
func (s *Server) handleResetSkillPin(w http.ResponseWriter, r *http.Request) {
	if s.skillPinStore == nil {
		writeJSONError(w, "Skill pinning not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if _, ok := s.skillPinStore.Get(name); !ok {
		writeJSONError(w, "No pin found for skill: "+name, http.StatusNotFound)
		return
	}
	if err := s.skillPinStore.Reset(name); err != nil {
		writeJSONError(w, "Failed to reset skill pin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// skillGovernance is the per-skill governance summary attached to registry
// API responses: factual provenance, pin state, advisory finding counts, and
// the exposure-policy verdict. Provenance and pin state come from the skill
// pin store; the policy verdict from the gateway's live policy, so the API
// always reports what is actually enforced.
type skillGovernance struct {
	Source             string               `json:"source,omitempty"`
	Origin             *skillpins.OriginRef `json:"origin,omitempty"`
	PinStatus          string               `json:"pinStatus,omitempty"`
	FindingsCount      int                  `json:"findingsCount,omitempty"`
	MaxFindingSeverity string               `json:"maxFindingSeverity,omitempty"`
	PolicyDenied       bool                 `json:"policyDenied,omitempty"`
	PolicyRule         string               `json:"policyRule,omitempty"`
}

// skillGovernanceFor assembles the governance summary for one skill, or nil
// when nothing is known (no pin store and no policy).
func (s *Server) skillGovernanceFor(name string) *skillGovernance {
	var g skillGovernance
	populated := false

	if s.skillPinStore != nil {
		if pin, ok := s.skillPinStore.Get(name); ok {
			g.Source = pin.Source
			g.Origin = pin.Origin
			g.PinStatus = pin.Status
			g.FindingsCount = len(pin.Findings)
			g.MaxFindingSeverity = pins.MaxSeverity(pin.Findings)
			populated = true
		}
	}
	if s.gateway != nil {
		if allowed, rule := s.gateway.CurrentSkillPolicy().Evaluate(name); !allowed {
			g.PolicyDenied = true
			g.PolicyRule = rule
			populated = true
		}
	}
	if !populated {
		return nil
	}
	return &g
}
