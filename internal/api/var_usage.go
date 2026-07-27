package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/vault"
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

// appendSetConsumers adds a synthetic RefKindSecretsSet consumer for every
// variable that the stack's secrets.sets block injects into server/resource
// env (see injectSetSecrets). The reference index only records explicit
// ${var:KEY} sites, so without this an injected, load-bearing secret reports
// zero consumers and reads as safe to delete.
//
// Only keys and set names are used — never values. A locked or absent vault
// degrades to explicit references only, keeping the endpoint lock-safe.
func appendSetConsumers(usage map[string][]config.Consumer, spec *config.Stack, store *vault.Store) {
	if spec == nil || spec.Secrets == nil || len(spec.Secrets.Sets) == 0 {
		return
	}
	if store == nil || store.IsLocked() {
		return
	}
	for _, setName := range spec.Secrets.Sets {
		c := config.Consumer{
			Kind:  config.RefKindSecretsSet,
			Name:  setName,
			Field: "secrets.sets",
		}
		for _, v := range store.GetSetSecrets(setName) {
			if hasConsumer(usage[v.Key], c) {
				continue
			}
			usage[v.Key] = append(usage[v.Key], c)
		}
	}
}

func hasConsumer(consumers []config.Consumer, c config.Consumer) bool {
	for _, existing := range consumers {
		if existing == c {
			return true
		}
	}
	return false
}

// handleVariableUsage returns the variable-usage index for the active stack:
// which servers/resources reference each ${var:KEY}, plus synthetic
// secrets-set consumers for variables injected via secrets.sets. GET
// /api/var/usage.
//
// The explicit index is derived from the loaded stack file; set membership
// comes from the vault's metadata (keys and set names only), so no secret
// values are exposed and the endpoint stays safe to serve while the vault is
// locked (synthetic consumers are simply omitted). When no stack is deployed
// it returns an empty object.
func (s *Server) handleVariableUsage(w http.ResponseWriter, _ *http.Request) {
	spec := s.loadRunningSpec()
	usage := buildVariableUsage(spec)
	appendSetConsumers(usage, spec, s.vaultStore)
	writeJSON(w, usage)
}
