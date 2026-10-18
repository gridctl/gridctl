package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/toolpolicy"
)

// SetPolicyStatusFunc installs the closure GET /api/policy reads. The
// builder wires a closure over the live policy and the pre-whitelist
// catalog so hot-reload swaps are reflected without re-wiring.
func (s *Server) SetPolicyStatusFunc(fn func() toolpolicy.StatusReport) {
	s.policyStatus = fn
}

// handlePolicy handles GET /api/policy: the authored block and the verdict
// each pre-whitelist catalog tool would receive. With no policy: block (or
// no wiring) it returns configured: false and empty collections, never an
// error. The CLI and UI render "not configured" from the payload.
func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.policyStatus == nil {
		writeJSON(w, toolpolicy.EmptyStatus())
		return
	}
	writeJSON(w, s.policyStatus())
}
