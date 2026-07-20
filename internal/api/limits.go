package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/limits"
)

// SetLimitsStatusFunc installs the closure GET /api/limits reads. The
// builder wires a closure over the live limits policy so hot-reload swaps
// are reflected without re-wiring.
func (s *Server) SetLimitsStatusFunc(fn func() limits.StatusReport) {
	s.limitsStatus = fn
}

// handleLimits handles GET /api/limits: the consumption snapshot for every
// configured budget and rate limit. With no limits: block (or no wiring) it
// returns configured: false and an empty entries array, never an error —
// the CLI and UI render "not configured" from the payload.
func (s *Server) handleLimits(w http.ResponseWriter, r *http.Request) {
	if s.limitsStatus == nil {
		writeJSON(w, limits.StatusReport{Entries: []limits.EntryStatus{}})
		return
	}
	writeJSON(w, s.limitsStatus())
}
