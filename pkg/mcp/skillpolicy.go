package mcp

import "path"

// SkillPolicySpec is the config-agnostic description of the stack.yaml
// `skills:` policy block (the ClientAccessSpec pattern: the controller builds
// it without pkg/mcp importing pkg/config). Allow and Deny are skill-name
// globs in path.Match syntax; Default is the fate of a skill matching
// neither list ("deny" denies, anything else — including empty — allows).
// A nil *SkillPolicySpec means no block was configured at all.
type SkillPolicySpec struct {
	Default string
	Allow   []string
	Deny    []string
}

// SkillPolicy is the compiled global skill exposure filter applied at the
// gateway's prompt/resource boundary and in projection sync. It is global by
// design: per-client skill scoping remains an explicit v1 deferral (see the
// ClientsConfig doc in pkg/config).
//
// A nil *SkillPolicy means no `skills:` block was configured: every active
// skill is exposed (legacy behavior, Article IX). Evaluation order: a Deny
// match always wins, then an Allow match admits, then Default decides.
// Denial is a visibility filter, never a state change — denied skills stay
// in the registry and its API, flagged with the matching rule.
type SkillPolicy struct {
	defaultDeny bool
	allow       []string
	deny        []string
}

// NewSkillPolicy compiles a spec. A nil spec returns a nil policy.
func NewSkillPolicy(spec *SkillPolicySpec) *SkillPolicy {
	if spec == nil {
		return nil
	}
	return &SkillPolicy{
		defaultDeny: spec.Default == "deny",
		allow:       append([]string(nil), spec.Allow...),
		deny:        append([]string(nil), spec.Deny...),
	}
}

// DefaultDenyRule is the rule string reported when a skill is denied by the
// block's default rather than a listed glob.
const DefaultDenyRule = "default: deny"

// Evaluate reports whether a skill name is exposed, and on denial, the rule
// responsible (the matching deny glob, or DefaultDenyRule). A nil policy
// allows everything. Unparseable patterns never match (they are rejected at
// config validation; a stray one failing open here would be silent policy).
func (p *SkillPolicy) Evaluate(name string) (allowed bool, rule string) {
	if p == nil {
		return true, ""
	}
	for _, g := range p.deny {
		if ok, err := path.Match(g, name); err == nil && ok {
			return false, g
		}
	}
	for _, g := range p.allow {
		if ok, err := path.Match(g, name); err == nil && ok {
			return true, ""
		}
	}
	if p.defaultDeny {
		return false, DefaultDenyRule
	}
	return true, ""
}

// Allows reports whether a skill name is exposed.
func (p *SkillPolicy) Allows(name string) bool {
	allowed, _ := p.Evaluate(name)
	return allowed
}
