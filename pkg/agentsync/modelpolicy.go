package agentsync

import (
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// SetModelPolicy installs the compiled model preference policy for the
// agents scope (the stack.yaml `model_preferences.agents` block).
// Passing nil removes it: CLI call sites without stack context run
// pass-through for new work and preserve previously rewritten
// projections (see materialize).
func (m *Manager) SetModelPolicy(p *registry.ModelPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelPolicy = p
}

// currentModelPolicy reads the installed policy under the manager
// mutex, for lock-free read paths (Statuses) racing SetModelPolicy.
func (m *Manager) currentModelPolicy() *registry.ModelPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.modelPolicy
}

// declaredAgentModel reads the author-declared top-level `model:` value
// from canonical agent bytes. ok is false when the declaration exists
// but is not a scalar (rewriting around a structured value would risk
// corrupting the frontmatter, so policy skips such files). A file that
// fails to parse reports ("", true): nothing declared, nothing to
// preserve.
func declaredAgentModel(src []byte) (string, bool) {
	def, err := skills.ParseAgentMD(src)
	if err != nil {
		return "", true
	}
	return def.DeclaredModel()
}

// rewriteAgentModel returns agent bytes with the top-level `model:`
// frontmatter set to value (or removed when value is empty), preserving
// every other byte verbatim (agents are stored and projected verbatim,
// so this is deliberate line surgery, not a parse/re-render, which
// would normalize frontmatter the identity contract promises not to
// touch). ok is false when the surgery cannot be done safely, or when
// the result does not parse back to exactly the requested declaration:
// text surgery cannot see every YAML form, so the output is verified
// before it is trusted (a missed quoted-key spelling would otherwise
// emit a duplicate model key the client rejects).
func rewriteAgentModel(raw []byte, value string) ([]byte, bool) {
	out, ok := registry.RewriteTopLevelModelLine(raw, value)
	if !ok {
		return nil, false
	}
	def, err := skills.ParseAgentMD(out)
	if err != nil {
		return nil, false
	}
	got, scalarOK := def.DeclaredModel()
	if !scalarOK || got != value {
		return nil, false
	}
	return out, true
}
