package registry

import (
	"regexp"
	"strings"
)

// topLevelModelKeyPattern matches the YAML spellings of a top-level
// `model` key at column zero: bare, double-quoted, or single-quoted,
// with optional space before the colon. An indented key belongs to a
// nested mapping and never matches.
var topLevelModelKeyPattern = regexp.MustCompile(`^(?:model|"model"|'model')\s*:`)

// RewriteTopLevelModelLine returns frontmatter-bearing bytes with the
// top-level `model:` key set to value (or removed when value is empty),
// preserving every other byte verbatim. It is deliberate line surgery,
// not a parse/re-render, for files whose bytes are the contract
// (identity-projected agents, and adopt's reversal of a projected
// rewrite). ok is false when the file has no recognizable frontmatter
// block or an existing model value is not a single-line scalar (never
// valid per the client docs, and not safely replaceable line-wise);
// callers fall back to pass-through. Callers that can parse the result
// should verify it, since text surgery cannot see every YAML form.
func RewriteTopLevelModelLine(raw []byte, value string) ([]byte, bool) {
	content := string(raw)
	lines := strings.SplitAfter(content, "\n")
	openIdx, closeIdx := -1, -1
	for i, line := range lines {
		if strings.TrimSpace(strings.TrimRight(line, "\n")) == "---" {
			if openIdx == -1 {
				openIdx = i
			} else {
				closeIdx = i
				break
			}
		}
	}
	if openIdx == -1 || closeIdx == -1 {
		return nil, false
	}

	modelIdx := -1
	for i := openIdx + 1; i < closeIdx; i++ {
		trimmed := strings.TrimRight(lines[i], "\n")
		if topLevelModelKeyPattern.MatchString(trimmed) || trimmed == "model" || trimmed == `"model"` || trimmed == `'model'` {
			modelIdx = i
			break
		}
	}
	if modelIdx >= 0 {
		trimmed := strings.TrimRight(lines[modelIdx], "\n")
		loc := topLevelModelKeyPattern.FindStringIndex(trimmed)
		if loc == nil {
			// A bare key with no colon means the value continues below.
			return nil, false
		}
		rest := strings.TrimSpace(trimmed[loc[1]:])
		// An empty remainder or a block indicator means a multi-line
		// value; refuse rather than corrupt.
		if rest == "" || strings.HasPrefix(rest, "|") || strings.HasPrefix(rest, ">") {
			return nil, false
		}
	}

	var b strings.Builder
	for i, line := range lines {
		if i == modelIdx {
			if value != "" {
				b.WriteString("model: " + value + "\n")
			}
			continue
		}
		if i == closeIdx && modelIdx == -1 && value != "" {
			b.WriteString("model: " + value + "\n")
		}
		b.WriteString(line)
	}
	return []byte(b.String()), true
}

// RenderWithModelPreference renders SKILL.md bytes with the resolved
// model preference applied to a copy of the skill. It backs the
// projection-time policy rewrite: the registry canonical is NEVER
// mutated; callers write the returned bytes into projected copies
// only.
//
// The honored key is always set: Claude Code reads top-level `model:`
// on projected skill files, so the rewrite guarantees it even for
// authors who declared only a metadata key. Any metadata declaration
// the author wrote is updated in place as well, so a projected file
// never carries two disagreeing declarations. All other frontmatter
// rides through the existing parse/render round trip untouched.
func RenderWithModelPreference(sk *AgentSkill, value string) ([]byte, error) {
	cp := *sk
	cp.Extra = make(map[string]any, len(sk.Extra)+1)
	for k, v := range sk.Extra {
		cp.Extra[k] = v
	}
	cp.Extra[ModelSourceTopLevel] = value
	if len(sk.Metadata) > 0 {
		cp.Metadata = make(SkillMetadata, len(sk.Metadata))
		for k, v := range sk.Metadata {
			cp.Metadata[k] = v
		}
		if _, ok := cp.Metadata["preferred-model"]; ok {
			cp.Metadata["preferred-model"] = value
		}
		if _, ok := cp.Metadata["model"]; ok {
			cp.Metadata["model"] = value
		}
	}
	return RenderSkillMD(&cp)
}
