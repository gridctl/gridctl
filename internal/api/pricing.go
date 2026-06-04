package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/pricing"
)

// pricingModelsResponse is the payload for GET /api/pricing/models.
type pricingModelsResponse struct {
	// Source is the active pricing source's short name (e.g. "litellm").
	Source string `json:"source"`
	// Models is the sorted list of canonical model IDs the source can
	// price. Used to populate the model combobox in the UI; free-text IDs
	// outside this list are still accepted everywhere (best-effort pricing).
	Models []string `json:"models"`
}

// handlePricingModels returns the canonical model IDs known to the active
// pricing source, for UI pickers. The list is computed once per source at
// construction, so this handler is a cheap snapshot read.
//
// GET /api/pricing/models
func (s *Server) handlePricingModels(w http.ResponseWriter, r *http.Request) {
	models := pricing.KnownModels()
	if models == nil {
		models = []string{}
	}
	writeJSON(w, pricingModelsResponse{
		Source: pricing.CurrentSource().Name(),
		Models: models,
	})
}
