package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// LinkEntry is one declared client connection in the optional top-level
// `link:` block. The block lists LLM clients that `gridctl apply` should
// link to this stack's gateway once it is healthy. Reconciliation is
// additive and idempotent: declared clients are linked if installed,
// already-linked clients are a no-op, and removing an entry never unlinks
// anything (removal stays explicit via `gridctl unlink` or
// `gridctl destroy --unlink`). Omitting the block preserves legacy
// behavior: nothing is auto-linked.
//
// An entry is either a bare client slug ("- claude-code") or a mapping:
//
//	link:
//	  - claude
//	  - client: cursor
//	    group: dev        # link the group endpoint; entry name defaults to gridctl-dev
//	    client_id: cursor # stable identifier for per-client access scoping
//	    name: gridctl     # server entry name override in the client config
type LinkEntry struct {
	Client   string `yaml:"client" json:"client"`
	Group    string `yaml:"group,omitempty" json:"group,omitempty"`
	ClientID string `yaml:"client_id,omitempty" json:"client_id,omitempty"`
	Name     string `yaml:"name,omitempty" json:"name,omitempty"`
}

// UnmarshalYAML accepts the scalar shorthand ("- cursor") or the mapping
// form. Any other node kind is rejected so a stray nested sequence fails
// loudly instead of decoding to an empty entry.
func (e *LinkEntry) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		return value.Decode(&e.Client)
	case yaml.MappingNode:
		type rawLinkEntry LinkEntry // shed the method to avoid recursion
		return value.Decode((*rawLinkEntry)(e))
	default:
		return fmt.Errorf("link entry must be a client slug or a mapping (line %d)", value.Line)
	}
}

// IsShorthand reports whether the entry carries nothing beyond the client
// slug, so YAML emitters can round-trip it back to the scalar form.
func (e LinkEntry) IsShorthand() bool {
	return e.Group == "" && e.ClientID == "" && e.Name == ""
}

// EffectiveName resolves the server entry name this link writes into the
// client config: an explicit name wins, a group link defaults to
// "gridctl-<group>" (matching `gridctl link --group`), everything else uses
// "gridctl".
func (e LinkEntry) EffectiveName() string {
	if e.Name != "" {
		return e.Name
	}
	if e.Group != "" {
		return "gridctl-" + e.Group
	}
	return "gridctl"
}

// linkClientSlugs mirrors provisioner.Registry.AllSlugs(). The config
// package deliberately avoids importing pkg/provisioner (config is
// foundational; see the clientmodels.go precedent for pkg/mcp), so the
// linkable set is copied here and guarded against drift by
// links_parity_test.go.
var linkClientSlugs = map[string]bool{
	"claude":      true,
	"claude-code": true,
	"cursor":      true,
	"windsurf":    true,
	"vscode":      true,
	"gemini":      true,
	"antigravity": true,
	"opencode":    true,
	"grok":        true,
	"continue":    true,
	"cline":       true,
	"anythingllm": true,
	"roo":         true,
	"zed":         true,
	"goose":       true,
}

// linkClientSlugList returns the supported slugs in a stable order for
// error messages.
func linkClientSlugList() []string {
	// Keep the registry's display order rather than sorting alphabetically,
	// matching the CLI's "Supported clients:" output.
	return []string{
		"claude", "claude-code", "cursor", "windsurf", "vscode", "gemini",
		"antigravity", "opencode", "grok", "continue", "cline",
		"anythingllm", "roo", "zed", "goose",
	}
}

// validateLinks checks the optional `link:` block: every entry must name a
// known client slug, a client may be declared at most once (the UI models
// one connection per client; multi-group links into one client are
// deferred), and a group reference must exist in the stack's `groups:`
// block since the stack file declares both sides.
func validateLinks(s *Stack) ValidationErrors {
	var errs ValidationErrors
	seen := make(map[string]int, len(s.Link))
	for i, entry := range s.Link {
		prefix := fmt.Sprintf("link[%d]", i)
		if entry.Client == "" {
			errs = append(errs, ValidationError{prefix, "client is required"})
			continue
		}
		if !linkClientSlugs[entry.Client] {
			errs = append(errs, ValidationError{
				prefix,
				fmt.Sprintf("unknown client %q (supported: %s)", entry.Client, joinSlugs(linkClientSlugList())),
			})
			continue
		}
		if first, dup := seen[entry.Client]; dup {
			errs = append(errs, ValidationError{
				prefix,
				fmt.Sprintf("client %q already declared at link[%d]; a client may be linked once per stack", entry.Client, first),
			})
			continue
		}
		seen[entry.Client] = i
		if entry.Group != "" {
			if _, ok := s.Groups[entry.Group]; !ok {
				errs = append(errs, ValidationError{
					prefix + ".group",
					fmt.Sprintf("references unknown group '%s'", entry.Group),
				})
			}
		}
	}
	return errs
}

// SupportedLinkClientsForTest exposes the copied slug list to the external
// parity test (links_parity_test.go), mirroring
// NormalizedClientModelKeyForTest. Not for production use.
func SupportedLinkClientsForTest() []string {
	return linkClientSlugList()
}

func joinSlugs(slugs []string) string {
	out := ""
	for i, s := range slugs {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
