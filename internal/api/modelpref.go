package api

import (
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// modelPreferenceView is the wire shape for a skill or agent's model
// preference: the author's declaration (always present when declared),
// the policy-resolved value (present only when a loaded stack policy
// default or override decides the value; the UI never guesses), and
// the per-target honor matrix keyed by target slug. The whole object is
// omitted when there is neither a declaration nor a policy resolution,
// so older frontends see nothing new.
type modelPreferenceView struct {
	Declared *modelDeclarationView `json:"declared,omitempty"`
	Resolved *modelResolutionView  `json:"resolved,omitempty"`
	Honor    map[string]string     `json:"honor"`
}

// modelDeclarationView is the author-declared preference.
type modelDeclarationView struct {
	Value     string `json:"value"`
	SourceKey string `json:"sourceKey"`
}

// modelResolutionView is the policy-resolved preference with its
// provenance: "author", "default", or "override".
type modelResolutionView struct {
	Value      string `json:"value"`
	Resolution string `json:"resolution"`
}

// SetModelPolicyProvider wires the live model preference policies into
// the API (the controller points it at the gateway instance's compiled
// scopes, which hot reload keeps current). nil (the default) means no
// stack policy is available and responses carry declarations only.
func (s *Server) SetModelPolicyProvider(provider func() (skillPolicy, agentPolicy *registry.ModelPolicy)) {
	s.modelPolicies = provider
}

// currentModelPolicies resolves the live policies, nil-safe.
func (s *Server) currentModelPolicies() (skillPolicy, agentPolicy *registry.ModelPolicy) {
	if s.modelPolicies == nil {
		return nil, nil
	}
	return s.modelPolicies()
}

// honorWire converts an honor matrix to its wire form.
func honorWire(m map[string]registry.HonorStatus) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}

// modelPreferenceWire builds the wire view from a declaration and a
// policy, or nil when neither says anything. Resolved is attached only
// when the policy actually decided something (a default or an
// override): a loaded stack without a model_preferences block compiles
// to an empty known-absent policy, and echoing the author's own value
// back as "resolved" under it would make every declared skill read as
// policy-governed.
func modelPreferenceWire(name string, pref *registry.ModelPreference, pol *registry.ModelPolicy, honor map[string]registry.HonorStatus) *modelPreferenceView {
	declared := pref.Value()
	resolved, resolution := "", ""
	if pol != nil {
		resolved, resolution = pol.Resolve(name, declared)
	}
	if resolution == registry.ResolutionAuthor {
		resolved, resolution = "", ""
	}
	if declared == "" && resolved == "" {
		return nil
	}
	view := &modelPreferenceView{Honor: honorWire(honor)}
	if pref != nil {
		view.Declared = &modelDeclarationView{Value: declared, SourceKey: pref.SourceKey}
	}
	if resolved != "" {
		view.Resolved = &modelResolutionView{Value: resolved, Resolution: resolution}
	}
	return view
}

// skillModelPreference builds the view for one registry skill.
func (s *Server) skillModelPreference(sk *registry.AgentSkill) *modelPreferenceView {
	pol, _ := s.currentModelPolicies()
	return modelPreferenceWire(sk.Name, registry.ExtractModelPreference(sk), pol, registry.SkillHonorMatrix())
}

// agentModelPreference builds the view for one installed agent.
func (s *Server) agentModelPreference(a skills.InstalledAgent) *modelPreferenceView {
	var pref *registry.ModelPreference
	if v, ok := a.Definition.DeclaredModel(); ok && v != "" {
		pref = &registry.ModelPreference{Values: []string{v}, SourceKey: registry.ModelSourceTopLevel}
	}
	_, pol := s.currentModelPolicies()
	return modelPreferenceWire(a.Name, pref, pol, registry.AgentHonorMatrix())
}
