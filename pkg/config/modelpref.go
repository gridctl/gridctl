package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// Compile converts one model_preferences scope into the policy form the
// projection engines consume. A nil scope compiles to a non-nil empty
// policy: on a loaded stack, an absent scope is a KNOWN absence (the
// documented off switch, reconciling rewritten projections back to
// pass-through), never the unknown-policy preserve state, which is
// reserved for call sites with no stack at all.
func (s *ModelPreferenceScope) Compile() *registry.ModelPolicy {
	if s == nil {
		return &registry.ModelPolicy{}
	}
	p := &registry.ModelPolicy{
		Rewrite:   s.Rewrite,
		Default:   s.Default,
		Overrides: s.Overrides,
	}
	for slug, c := range s.Clients {
		if c == nil {
			continue
		}
		if p.Clients == nil {
			p.Clients = make(map[string]registry.ClientModelPolicy, len(s.Clients))
		}
		p.Clients[slug] = registry.ClientModelPolicy{Default: c.Default, Overrides: c.Overrides}
	}
	return p
}

// ModelPolicies compiles the stack's model_preferences block into the
// per-scope policies for the skill and agent projection engines. A
// loaded stack always yields non-nil policies, block or no block:
// deleting the block is the same known-absent off switch as
// `rewrite: false`. Only a nil stack (no stack context at all) yields
// nil policies, which the engines treat as preserve-not-revert.
func (s *Stack) ModelPolicies() (skills, agents *registry.ModelPolicy) {
	if s == nil {
		return nil, nil
	}
	if s.ModelPreferences == nil {
		return &registry.ModelPolicy{}, &registry.ModelPolicy{}
	}
	return s.ModelPreferences.Skills.Compile(), s.ModelPreferences.Agents.Compile()
}

// validateModelPreferenceClients reports the structural errors of the
// `clients` sub-blocks: the only non-advisory model preference findings.
// A clients block under skills, a key that is not a rendered agent
// target (claude-code included: the identity target takes the top-level
// default and overrides), and an empty value can never take effect, so
// they fail validation rather than silently projecting nothing. Value
// shape is advisory and lives in addModelPreferencesIssues.
func validateModelPreferenceClients(mp *ModelPreferencesConfig) ValidationErrors {
	if mp == nil {
		return nil
	}
	var errs ValidationErrors
	if mp.Skills != nil && mp.Skills.Clients != nil {
		errs = append(errs, ValidationError{"model_preferences.skills.clients", "client model preferences apply to rendered agent projections only; move the block under model_preferences.agents"})
	}
	if mp.Agents == nil {
		return errs
	}
	targets := strings.Join(registry.RenderedAgentTargets(), ", ")
	for _, slug := range slices.Sorted(maps.Keys(mp.Agents.Clients)) {
		field := "model_preferences.agents.clients." + slug
		switch {
		case slug == "claude-code":
			errs = append(errs, ValidationError{field, fmt.Sprintf("claude-code is the identity target and takes the top-level default and overrides; clients keys are rendered agent targets (%s)", targets)})
			continue
		case !registry.IsRenderedAgentTarget(slug):
			errs = append(errs, ValidationError{field, fmt.Sprintf("%q is not a rendered agent target (%s)", slug, targets)})
			continue
		}
		c := mp.Agents.Clients[slug]
		if c == nil || (c.Default == "" && len(c.Overrides) == 0) {
			errs = append(errs, ValidationError{field, "declares no value; set default or overrides, or remove the entry"})
			continue
		}
		if c.Default != "" && strings.TrimSpace(c.Default) == "" {
			errs = append(errs, ValidationError{field + ".default", "must not be empty"})
		}
		for _, name := range slices.Sorted(maps.Keys(c.Overrides)) {
			if strings.TrimSpace(c.Overrides[name]) == "" {
				errs = append(errs, ValidationError{field + ".overrides." + name, "must not be empty"})
			}
		}
	}
	return errs
}
