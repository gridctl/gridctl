package registry

// ModelPolicy is one scope's compiled model preference policy (the
// stack.yaml `model_preferences:` block for either the skills or the
// agents scope), consumed by the projection engines. The controller
// compiles it from the loaded stack; CLI call sites without stack
// context run with a nil policy, which is pure pass-through and, for
// projections a policy previously rewrote, preserve-not-revert; the
// sync packages own that rule.
type ModelPolicy struct {
	// Rewrite opts the scope into projection rewrite. False means
	// surfacing-only: Resolve still answers (for status and REST), but
	// nothing on disk changes.
	Rewrite bool
	// Default applies where the author declared nothing.
	Default string
	// Overrides maps exact registry names to the preference applied
	// regardless of the author's declaration, in either direction.
	Overrides map[string]string
	// Clients maps rendered agent target slugs (RenderedAgentTargets) to
	// explicit values in that client's own model vocabulary. Agents
	// scope only; never translated from Default or Overrides.
	Clients map[string]ClientModelPolicy
}

// ClientModelPolicy is one rendered client's model preference: values
// are written verbatim into that client's rendered frontmatter.
type ClientModelPolicy struct {
	// Default applies to every agent without an override.
	Default string
	// Overrides maps exact agent names to the value for that agent.
	Overrides map[string]string
}

// Resolution provenance values, shared with the REST surface.
const (
	ResolutionAuthor   = "author"
	ResolutionDefault  = "default"
	ResolutionOverride = "override"
)

// Resolve answers the effective preference for one name: override
// beats the author's declaration beats the default. The returned
// resolution names which source won ("override", "author", "default"),
// or "" when nothing applies.
func (p *ModelPolicy) Resolve(name, declared string) (value, resolution string) {
	if p != nil {
		if v, ok := p.Overrides[name]; ok && v != "" {
			return v, ResolutionOverride
		}
	}
	if declared != "" {
		return declared, ResolutionAuthor
	}
	if p != nil && p.Default != "" {
		return p.Default, ResolutionDefault
	}
	return "", ""
}

// ResolveClient answers the client-specific value for one agent on one
// rendered target: a non-empty override beats the client default. The
// author's declaration never contributes (its vocabulary belongs to
// the identity target). Resolution is "override", "default", or "" when
// no value applies. Rewrite is not consulted; callers gate on it.
func (p *ModelPolicy) ResolveClient(slug, name string) (value, resolution string) {
	if p == nil {
		return "", ""
	}
	c, ok := p.Clients[slug]
	if !ok {
		return "", ""
	}
	if v := c.Overrides[name]; v != "" {
		return v, ResolutionOverride
	}
	if c.Default != "" {
		return c.Default, ResolutionDefault
	}
	return "", ""
}

// NeedsRewrite reports whether projection must write a resolved value
// that differs from the author's declaration. Equality is judged after
// normalization, so case or whitespace never forces a rewrite (and
// never forces a skill off its symlink channel).
func (p *ModelPolicy) NeedsRewrite(name, declared string) (string, bool) {
	if p == nil || !p.Rewrite {
		return "", false
	}
	resolved, _ := p.Resolve(name, declared)
	if resolved == "" || NormalizeModelValue(resolved) == NormalizeModelValue(declared) {
		return "", false
	}
	return resolved, true
}
