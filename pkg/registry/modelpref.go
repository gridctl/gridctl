package registry

import (
	"regexp"
	"strings"
)

// ModelPreference is the typed read-side view of a skill or agent
// author's declared model preference. It is a preference, never an
// enforcement point: clients resolve their own model (env vars and
// per-invocation parameters outrank projected frontmatter), and gridctl
// only surfaces, defaults, and overrides what the projected files
// declare.
//
// Values is a list so a future agentskills.io ordered-preference shape
// is a consumption change, not a migration; today it always holds one
// element. The author's raw key is untouched wherever it was written
// (top-level Extra or metadata); this view is derived, never stored.
type ModelPreference struct {
	// Values holds the declared preference(s), most preferred first.
	Values []string `json:"values"`
	// SourceKey names where the author declared it: "model",
	// "metadata.preferred-model", or "metadata.model".
	SourceKey string `json:"sourceKey"`
}

// Value returns the single effective preference (the first value), or
// "" for a nil preference.
func (p *ModelPreference) Value() string {
	if p == nil || len(p.Values) == 0 {
		return ""
	}
	return p.Values[0]
}

// Model preference source keys, in recognition precedence order.
const (
	ModelSourceTopLevel      = "model"
	ModelSourceMetaPreferred = "metadata.preferred-model"
	ModelSourceMetaModel     = "metadata.model"
)

// ExtractModelPreference derives the typed preference from a skill's
// frontmatter. Recognition precedence: top-level `model:` beats
// `metadata.preferred-model` beats `metadata.model`. Non-string or
// empty declarations yield nil. Extraction never mutates the skill.
func ExtractModelPreference(sk *AgentSkill) *ModelPreference {
	if sk == nil {
		return nil
	}
	if raw, ok := sk.Extra[ModelSourceTopLevel]; ok {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			return &ModelPreference{Values: []string{strings.TrimSpace(s)}, SourceKey: ModelSourceTopLevel}
		}
	}
	if s := strings.TrimSpace(sk.Metadata["preferred-model"]); s != "" {
		return &ModelPreference{Values: []string{s}, SourceKey: ModelSourceMetaPreferred}
	}
	if s := strings.TrimSpace(sk.Metadata["model"]); s != "" {
		return &ModelPreference{Values: []string{s}, SourceKey: ModelSourceMetaModel}
	}
	return nil
}

// ModelPreferenceFromKeys derives the typed preference from raw key
// values, for callers whose documents are not AgentSkill-shaped (agent
// definitions keep frontmatter as raw YAML nodes). topLevel is the
// top-level `model:` scalar; metadata the `metadata:` map (nil allowed).
func ModelPreferenceFromKeys(topLevel string, metadata map[string]string) *ModelPreference {
	if s := strings.TrimSpace(topLevel); s != "" {
		return &ModelPreference{Values: []string{s}, SourceKey: ModelSourceTopLevel}
	}
	if s := strings.TrimSpace(metadata["preferred-model"]); s != "" {
		return &ModelPreference{Values: []string{s}, SourceKey: ModelSourceMetaPreferred}
	}
	if s := strings.TrimSpace(metadata["model"]); s != "" {
		return &ModelPreference{Values: []string{s}, SourceKey: ModelSourceMetaModel}
	}
	return nil
}

// NormalizeModelValue canonicalizes a model value for comparison:
// trimmed and lowercased. Two declarations that normalize equal are the
// same preference; a rewrite is never forced over case or whitespace.
func NormalizeModelValue(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// knownModelAliases tracks the alias vocabulary Claude Code documents
// (https://code.claude.com/docs/en/model-config). Alias resolution is
// provider-conditional, so gridctl never claims which concrete version
// an alias resolves to; the list exists only so lint can warn on values
// that are neither an alias nor shaped like a model ID. Suffixed forms
// like "sonnet[1m]" are matched by pattern, not enumerated.
var knownModelAliases = map[string]bool{
	"default":  true,
	"best":     true,
	"fable":    true,
	"sonnet":   true,
	"opus":     true,
	"haiku":    true,
	"opusplan": true,
	"inherit":  true,
}

// aliasSuffixPattern matches alias forms carrying a bracketed variant
// suffix ("sonnet[1m]", "opus[1m]").
var aliasSuffixPattern = regexp.MustCompile(`^[a-z]+\[[a-z0-9]+\]$`)

// modelIDCharset constrains full model IDs to the characters provider
// IDs actually use; the "claude" substring requirement below is the
// deliberately generous shape test ("claude-opus-5",
// "anthropic.claude-sonnet-4-...", "us.anthropic...."). Lint warns on
// values matching neither an alias nor this shape; it never errors,
// because ID vocabularies churn faster than any release cadence.
var modelIDCharset = regexp.MustCompile(`^[a-z0-9][a-z0-9.:_-]*$`)

// KnownModelAliases returns the documented alias vocabulary, for lint
// messages and docs.
func KnownModelAliases() []string {
	return []string{"default", "best", "fable", "sonnet", "opus", "haiku", "sonnet[1m]", "opus[1m]", "opusplan", "inherit"}
}

// IsKnownModelValue reports whether a value is a documented alias, a
// suffixed alias form, or shaped like a full model ID. Callers treat a
// false as advisory-warn material, never an error.
func IsKnownModelValue(v string) bool {
	n := NormalizeModelValue(v)
	if n == "" {
		return false
	}
	if knownModelAliases[n] {
		return true
	}
	if aliasSuffixPattern.MatchString(n) {
		base := n[:strings.IndexByte(n, '[')]
		return knownModelAliases[base]
	}
	return modelIDCharset.MatchString(n) && strings.Contains(n, "claude")
}
