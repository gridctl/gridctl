package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gridctl/gridctl/pkg/catalog"
)

// catalogRegistrySearch queries the MCP Registry; a seam so handler tests
// run without a network. Mirrors the CLI's searchRegistry seam.
var catalogRegistrySearch = func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
	return catalog.NewClient().Search(ctx, query)
}

// catalogResponse carries the merged catalog entries plus registry fetch
// state, so the UI can badge stale or degraded results the same way the
// CLI's search JSON document does.
type catalogResponse struct {
	Query         string          `json:"query"`
	Source        string          `json:"source"`
	Stale         bool            `json:"stale,omitempty"`
	RegistryError string          `json:"registry_error,omitempty"`
	Servers       []catalog.Entry `json:"servers"`
}

// handleCatalog handles GET /api/catalog?q=<query>&source=<tier>. It merges
// the embedded curated catalog with MCP Registry search results (curated
// first, deduped by registry namespace) and never fails because the
// registry is down: fetch errors degrade to curated or cached results with
// registry_error set. An empty query lists the curated catalog only,
// matching `gridctl search` semantics.
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "all"
	}
	if source != catalog.TierCurated && source != catalog.TierRegistry && source != "all" {
		writeJSONError(w, "invalid source '"+source+"' (allowed: curated, registry, all)", http.StatusBadRequest)
		return
	}

	var curated []catalog.Entry
	if source != catalog.TierRegistry {
		var err error
		curated, err = catalog.FilterCurated(query)
		if err != nil {
			writeJSONError(w, "loading curated catalog: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	resp := catalogResponse{Query: query, Source: source}
	var registryEntries []catalog.Entry
	// The registry is a substring search; an empty query would page through
	// the whole registry for no benefit, so it stays curated-only.
	if source != catalog.TierCurated && query != "" {
		entries, stale, err := catalogRegistrySearch(r.Context(), query)
		switch {
		case err != nil:
			resp.RegistryError = err.Error()
			slog.Warn("MCP Registry unavailable, serving curated results only", "error", err)
		case stale:
			resp.Stale = true
			registryEntries = entries
		default:
			registryEntries = entries
		}
	}

	resp.Servers = scrubSecretDefaults(catalog.Merge(curated, registryEntries))
	if resp.Servers == nil {
		resp.Servers = []catalog.Entry{}
	}
	writeJSON(w, resp)
}

// scrubSecretDefaults clears literal defaults on secret inputs before the
// payload leaves the API. Curated entries never carry them by construction
// (asserted in pkg/catalog tests); registry entries are community input,
// so the invariant is enforced here too. Inputs slices are cloned before
// mutation because curated entries share the package-level cached parse.
func scrubSecretDefaults(entries []catalog.Entry) []catalog.Entry {
	for i := range entries {
		e := &entries[i]
		dirty := false
		for _, in := range e.Inputs {
			if in.Secret && in.Default != "" {
				dirty = true
				break
			}
		}
		if !dirty {
			continue
		}
		inputs := append([]catalog.Input(nil), e.Inputs...)
		for j := range inputs {
			if inputs[j].Secret {
				inputs[j].Default = ""
			}
		}
		e.Inputs = inputs
	}
	return entries
}
