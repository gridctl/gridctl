package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/config"
)

// buildVariableUsage normalizes a stack's reference index into the wire map for
// GET /api/var/usage. It is nil-safe and always returns a non-nil map so the
// JSON response is "{}" (not null) when nothing references any variable or no
// stack is loaded.
func buildVariableUsage(spec *config.Stack) map[string][]config.Consumer {
	usage := map[string][]config.Consumer{}
	if spec == nil {
		return usage
	}
	for key, consumers := range spec.References {
		usage[key] = consumers
	}
	return usage
}

// handleVariableUsage returns the variable-usage index for the active stack:
// which servers/resources reference each ${var:KEY}. GET /api/var/usage.
//
// The index is derived from the loaded stack file, never from the vault, so it
// exposes no secret values and is safe to serve while the vault is locked. When
// no stack is deployed it returns an empty object.
func (s *Server) handleVariableUsage(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, buildVariableUsage(s.loadRunningSpec()))
}
