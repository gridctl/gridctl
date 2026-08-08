package config

import "github.com/gridctl/gridctl/pkg/registry"

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
	return &registry.ModelPolicy{
		Rewrite:   s.Rewrite,
		Default:   s.Default,
		Overrides: s.Overrides,
	}
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
