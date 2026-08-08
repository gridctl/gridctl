package registry

// HonorStatus is what one projection target does with a declared model
// preference. The matrix is static, maintained truth about client
// behavior; it will go stale on client-doc timescales, so every entry
// cites its source in the tables below and the sync packages carry
// exhaustiveness tests that fail when a projection target gains no row.
type HonorStatus string

const (
	// HonorHonored: the client reads the key and uses it as a model
	// resolution input (below its own env var and per-invocation
	// overrides).
	HonorHonored HonorStatus = "honored"
	// HonorIgnored: the client documents no model key for this surface.
	HonorIgnored HonorStatus = "ignored"
	// HonorUnknown: consumer-dependent or unverified; surfaced as such
	// rather than guessed.
	HonorUnknown HonorStatus = "unknown"
	// HonorDropped: the projection render deliberately drops the key
	// (client model vocabularies are not Claude's); reported in
	// Rendered.Dropped at sync time too.
	HonorDropped HonorStatus = "dropped-on-render"
)

// skillHonor is the per-target truth for skill projections.
//   - claude-code: skills honor `model` inline as a turn-scoped session
//     override (code.claude.com/docs/en/skills).
//   - agents interop dir: multi-client by design; whether a consumer
//     honors the key is consumer-dependent.
//   - antigravity: no documented model key for skills.
var skillHonor = map[string]HonorStatus{
	"claude-code": HonorHonored,
	"agents":      HonorUnknown,
	"antigravity": HonorIgnored,
}

// agentHonor is the per-target truth for agent projections.
//   - claude-code: subagent `model` frontmatter is a documented
//     resolution input (code.claude.com/docs/en/sub-agents); Cursor
//     rides the same identity copy with unverified honor semantics, so
//     the identity target stays "honored" for its primary consumer.
//   - opencode, copilot, gemini: rendered dialects deliberately drop
//     `model` (pkg/agentsync/render.go).
var agentHonor = map[string]HonorStatus{
	"claude-code": HonorHonored,
	"opencode":    HonorDropped,
	"copilot":     HonorDropped,
	"gemini":      HonorDropped,
}

// SkillHonor returns the honor status for one skill projection target
// slug; unknown slugs report HonorUnknown rather than guessing.
func SkillHonor(slug string) HonorStatus {
	if s, ok := skillHonor[slug]; ok {
		return s
	}
	return HonorUnknown
}

// AgentHonor returns the honor status for one agent projection target
// slug; unknown slugs report HonorUnknown.
func AgentHonor(slug string) HonorStatus {
	if s, ok := agentHonor[slug]; ok {
		return s
	}
	return HonorUnknown
}

// SkillHonorMatrix returns a copy of the full skill-target matrix,
// keyed by target slug (REST and lint consumers).
func SkillHonorMatrix() map[string]HonorStatus {
	out := make(map[string]HonorStatus, len(skillHonor))
	for k, v := range skillHonor {
		out[k] = v
	}
	return out
}

// AgentHonorMatrix returns a copy of the full agent-target matrix,
// keyed by target slug.
func AgentHonorMatrix() map[string]HonorStatus {
	out := make(map[string]HonorStatus, len(agentHonor))
	for k, v := range agentHonor {
		out[k] = v
	}
	return out
}
