package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gridctl/gridctl/pkg/wiring"
)

// wiringErrorStatus maps pkg/wiring sentinel errors to HTTP statuses.
// Clients are enum values in request bodies (400 when unknown); the
// ownership refusals are conflicts whose engine message the UI renders
// verbatim.
func wiringErrorStatus(err error) int {
	switch {
	case errors.Is(err, wiring.ErrUnknownClient):
		return http.StatusBadRequest
	case errors.Is(err, wiring.ErrNotDetected), errors.Is(err, wiring.ErrNothingToAdopt),
		errors.Is(err, wiring.ErrForeign), errors.Is(err, wiring.ErrDrifted),
		errors.Is(err, wiring.ErrNotRecorded), errors.Is(err, wiring.ErrCannotPlan):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// handleProjectWiringStatus returns every (client, entry) wiring
// ownership row in the engine's six-state vocabulary (in-sync, stale,
// drifted, target-missing, foreign, missing) with detail, remediation,
// and pack tag — the full form of the fact /api/clients collapses into
// its single drifted boolean.
// GET /api/project/wiring/status
func (s *Server) handleProjectWiringStatus(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.wiringMgr()
	if err != nil {
		writeJSONError(w, "Wiring ownership not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	rows, err := mgr.Statuses(r.Context(), wiring.StatusOptions{Port: s.gatewayPortOrDefault()})
	if err != nil {
		writeJSONError(w, err.Error(), wiringErrorStatus(err))
		return
	}
	if rows == nil {
		rows = []wiring.Row{}
	}
	writeJSON(w, rows)
}

// handleProjectWiringAdopt records ownership of an entry's current value
// without rewriting it (the take-ownership verb for foreign and drifted
// entries). Refusals are 409 with the engine's message verbatim: the UI
// renders that text, so it must never collapse into a generic error.
// POST /api/project/wiring/adopt
func (s *Server) handleProjectWiringAdopt(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.wiringMgr()
	if err != nil {
		writeJSONError(w, "Wiring ownership not available: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Client string `json:"client"`
		// Name is the config entry key; empty means the default gateway
		// entry name.
		Name string `json:"name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Client == "" {
		writeJSONError(w, "client is required", http.StatusBadRequest)
		return
	}
	name := req.Name
	if name == "" {
		name = wiring.DefaultServerName
	}

	// Adopt mutates only the project lockfile (under pkg/project's own
	// cross-process flock), never stack.yaml, so the stack-file lock the
	// link handlers hold does not apply here.
	result, err := mgr.Adopt(r.Context(), req.Client, name)
	if err != nil {
		writeJSONError(w, err.Error(), wiringErrorStatus(err))
		return
	}
	writeJSON(w, result)
}
