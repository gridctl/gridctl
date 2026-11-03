package skills

import (
	"encoding/json"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// RequiredHooksFromSkill reads metadata.requires.hooks from a skill's
// metadata map. Nested metadata values are stored as compact JSON strings.
// A missing key, a value that is not JSON, or a hooks value that is not an
// array of strings is ignored. Extra["hooks"] is the Claude Code frontmatter
// passthrough and is not read here.
func RequiredHooksFromSkill(meta registry.SkillMetadata) []string {
	if len(meta) == 0 {
		return nil
	}
	raw := strings.TrimSpace(meta["requires"])
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return nil
	}
	var doc struct {
		Hooks []json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil
	}
	return stringArray(doc.Hooks)
}

// RequiredHooksFromAgent reads metadata.requires.hooks from an agent
// definition's raw metadata node. A missing key or a non-string array is ignored.
func RequiredHooksFromAgent(def *AgentDefinition) []string {
	if def == nil {
		return nil
	}
	node, ok := def.ExtraByKey("metadata")
	if !ok || node == nil {
		return nil
	}
	var meta map[string]any
	if err := node.Decode(&meta); err != nil {
		return nil
	}
	requires, _ := meta["requires"].(map[string]any)
	hooks, ok := requires["hooks"].([]any)
	if !ok || len(hooks) == 0 {
		return nil
	}
	out := make([]string, 0, len(hooks))
	for _, item := range hooks {
		name, ok := item.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil
		}
		out = append(out, name)
	}
	return out
}

func stringArray(raw []json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var name string
		if err := json.Unmarshal(item, &name); err != nil || strings.TrimSpace(name) == "" {
			return nil
		}
		out = append(out, name)
	}
	return out
}
