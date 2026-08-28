package api

import (
	"net/http"
	"sort"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/vault"
)

// buildVariableUsage normalizes a stack's reference index into the wire map for
// GET /api/var/usage. It is nil-safe and always returns a non-nil map so the
// JSON response is "{}" (not null) when nothing references any variable or no
// stack is loaded.
func buildVariableUsage(spec *config.Stack) map[string][]config.Consumer {
	usage, _ := config.BuildVariableUsage(spec, map[string][]string{})
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
	for _, ref := range spec.Secrets.Sets {
		// A scoped entry reaches only the workloads it names, so it yields one
		// consumer per receiving workload; an unscoped entry fans out and
		// yields the single untargeted consumer. This must track
		// injectSetSecrets exactly, or the index reports reach the stack does
		// not actually have.
		consumers := config.SetConsumersFor(ref, spec)
		if len(consumers) == 0 {
			continue
		}
		for _, v := range store.GetSetSecrets(ref.Name) {
			for _, c := range consumers {
				if hasConsumer(usage[v.Key], c) {
					continue
				}
				usage[v.Key] = append(usage[v.Key], c)
			}
		}
	}
}

// setConsumersFor builds the synthetic consumers a single secrets.sets entry
// contributes. Scoped entries are expanded against the stack's declared
// workloads so a scoped name that matches nothing contributes nothing rather
// than implying reach it does not have.
func setConsumersFor(ref config.SecretSetRef, spec *config.Stack) []config.Consumer {
	base := config.Consumer{
		Kind:  config.RefKindSecretsSet,
		Name:  ref.Name,
		Field: "secrets.sets",
	}
	if !ref.Scoped() {
		return []config.Consumer{base}
	}

	var out []config.Consumer
	for _, srv := range spec.MCPServers {
		if ref.InjectsIntoServer(srv.Name) {
			c := base
			c.Target = srv.Name
			c.TargetKind = config.RefKindMCPServer
			out = append(out, c)
		}
	}
	for _, res := range spec.Resources {
		if ref.InjectsIntoResource(res.Name) {
			c := base
			c.Target = res.Name
			c.TargetKind = config.RefKindResource
			out = append(out, c)
		}
	}
	return out
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

// driftEntry is one stack reference that no stored variable satisfies.
type driftEntry struct {
	Key         string                         `json:"key"`
	Consumers   []config.Consumer              `json:"consumers"`
	Declaration *config.VariableDeclaration    `json:"declaration,omitempty"`
	Diagnostics []config.DeclarationDiagnostic `json:"diagnostics,omitempty"`
	Unknown     bool                           `json:"unknown,omitempty"`
}

// buildVariableDrift lists the keys the stack references that the store cannot
// satisfy, newest definition of "missing" borrowed from the loader.
//
// spec.UnresolvedRefs comes from an environment-only expansion, so it already
// excludes references that carry a default operator (${var:KEY:-fallback} is
// valid config, not drift) but still includes keys the vault holds. Narrowing
// it by store membership leaves exactly the set that would fail a deploy,
// because the deploy-time resolver reads the store first and the environment
// second.
func buildVariableDrift(spec *config.Stack, store *vault.Store) []driftEntry {
	out := []driftEntry{}
	if spec == nil {
		return out
	}
	if store == nil || store.IsLocked() {
		keys := make([]string, 0, len(spec.Variables))
		for key := range spec.Variables {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			declaration := spec.Variables[key]
			consumers := spec.References[key]
			if consumers == nil {
				consumers = []config.Consumer{}
			}
			out = append(out, driftEntry{Key: key, Consumers: consumers, Declaration: &declaration, Unknown: true})
		}
		return out
	}
	// One snapshot of the key set rather than a lookup per reference: the
	// store reloads on read, so per-key checks could straddle an external
	// write and report a half-updated picture.
	stored := make(map[string]bool)
	metadata := make(map[string]config.VariableMetadata)
	for _, variable := range store.List() {
		stored[variable.Key] = true
		metadata[variable.Key] = config.VariableMetadata{Type: string(variable.Type), Secret: variable.IsSecret, Deprecated: variable.Deprecated}
	}
	diagnostics := config.DiagnoseDeclarations(spec, metadata, false)
	byKey := make(map[string][]config.DeclarationDiagnostic)
	for _, diagnostic := range diagnostics {
		byKey[diagnostic.Key] = append(byKey[diagnostic.Key], diagnostic)
	}
	seen := make(map[string]bool)
	for _, key := range spec.UnresolvedRefs {
		if stored[key] {
			continue
		}
		consumers := spec.References[key]
		if consumers == nil {
			consumers = []config.Consumer{}
		}
		declaration, declared := spec.Variables[key]
		entry := driftEntry{Key: key, Consumers: consumers, Diagnostics: byKey[key]}
		if declared {
			entry.Declaration = &declaration
		}
		out = append(out, entry)
		seen[key] = true
	}
	for key, keyDiagnostics := range byKey {
		if seen[key] || len(keyDiagnostics) == 0 {
			continue
		}
		declaration := spec.Variables[key]
		consumers := spec.References[key]
		if consumers == nil {
			consumers = []config.Consumer{}
		}
		out = append(out, driftEntry{Key: key, Consumers: consumers, Declaration: &declaration, Diagnostics: keyDiagnostics})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// handleVariableDrift returns the stack's references to variables that do not
// exist in the store, so the workspace can surface an authoring-time warning
// instead of leaving the operator to discover it as an apply failure. GET
// /api/var/drift.
//
// Keys and reference sites only, never values. A locked or absent store returns
// an empty list rather than reporting every reference as missing, since
// membership cannot be checked while locked.
func (s *Server) handleVariableDrift(w http.ResponseWriter, _ *http.Request) {
	spec := s.loadRunningSpec()
	writeJSON(w, buildVariableDrift(spec, s.vaultStore))
}
