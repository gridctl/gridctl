package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// UnmarshalYAML decodes metadata leniently. Values that are not strings
// (nested mappings, sequences, booleans, numbers) are coerced to their
// string form instead of failing the whole SKILL.md parse. Non-mapping
// metadata (e.g. a bare string) is ignored rather than treated as an error.
// The mapping node is walked by hand so one bad entry (or a duplicate key,
// which yaml.v3's map decoding rejects) cannot discard the valid keys.
func (m *SkillMetadata) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	out := make(SkillMetadata, len(value.Content)/2)
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode, valNode := value.Content[i], value.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		out[keyNode.Value] = metadataNodeString(valNode)
	}
	*m = out
	return nil
}

// metadataNodeString renders a YAML value node as a string. Scalars keep
// their literal form as written (so "1.20", dates, and hex stay verbatim);
// mappings and sequences become compact JSON.
func metadataNodeString(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return ""
		}
		return n.Value
	case yaml.AliasNode:
		if n.Alias != nil {
			return metadataNodeString(n.Alias)
		}
		return ""
	default:
		var v any
		if err := n.Decode(&v); err != nil {
			return ""
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

// knownFrontmatterKey reports whether k is a frontmatter key AgentSkill
// models with a typed field. Keep in sync with the switch in
// AgentSkill.UnmarshalYAML and the fm struct in RenderSkillMD.
func knownFrontmatterKey(k string) bool {
	switch k {
	case "name", "description", "license", "compatibility", "metadata",
		"allowed-tools", "acceptance_criteria", "state":
		return true
	}
	return false
}

// UnmarshalYAML decodes frontmatter into the typed fields and captures every
// unmodeled top-level key into Extra, so unknown keys survive the registry's
// parse/render round trip instead of being silently dropped. The mapping node
// is walked by hand for the same reason SkillMetadata's unmarshaler does it:
// yaml.v3's map decoding would reject a duplicate key outright, and a plain
// struct decode has no way to see unmatched keys at all. Known fields keep
// struct-decode strictness (a type mismatch fails the parse, as before);
// unknown values that fail to decode are skipped rather than fatal.
func (s *AgentSkill) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode, valNode := value.Content[i], value.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		var err error
		switch keyNode.Value {
		case "name":
			err = valNode.Decode(&s.Name)
		case "description":
			err = valNode.Decode(&s.Description)
		case "license":
			err = valNode.Decode(&s.License)
		case "compatibility":
			err = valNode.Decode(&s.Compatibility)
		case "metadata":
			err = valNode.Decode(&s.Metadata)
		case "allowed-tools":
			err = valNode.Decode(&s.AllowedTools)
		case "acceptance_criteria":
			err = valNode.Decode(&s.AcceptanceCriteria)
		case "state":
			err = valNode.Decode(&s.State)
		default:
			var v any
			if decErr := valNode.Decode(&v); decErr != nil {
				continue
			}
			if s.Extra == nil {
				s.Extra = make(map[string]any)
			}
			s.Extra[keyNode.Value] = v
		}
		if err != nil {
			return fmt.Errorf("parsing frontmatter key %q: %w", keyNode.Value, err)
		}
	}
	return nil
}

// ParseSkillMD parses a SKILL.md file into an AgentSkill.
// The file format is YAML frontmatter between --- delimiters followed by a markdown body.
func ParseSkillMD(data []byte) (*AgentSkill, error) {
	content := string(data)

	// Normalize Windows line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")

	// Check if the file starts with frontmatter delimiter
	trimmed := strings.TrimLeft(content, " \t")
	if !strings.HasPrefix(trimmed, "---") {
		// No frontmatter — entire content is body
		skill := &AgentSkill{Body: content}
		_ = validateState(&skill.State)
		return skill, nil
	}

	// Find frontmatter boundaries by scanning lines
	lines := strings.SplitAfter(content, "\n")
	openIdx := -1
	closeIdx := -1

	for i, line := range lines {
		stripped := strings.TrimRight(line, "\n")
		stripped = strings.TrimSpace(stripped)
		if stripped == "---" {
			if openIdx == -1 {
				openIdx = i
			} else {
				closeIdx = i
				break
			}
		}
	}

	// If we found the opening --- but no closing ---, treat as no frontmatter
	if closeIdx == -1 {
		skill := &AgentSkill{Body: content}
		_ = validateState(&skill.State)
		return skill, nil
	}

	// Extract frontmatter YAML (between the delimiters, exclusive)
	var fmBuilder strings.Builder
	for i := openIdx + 1; i < closeIdx; i++ {
		fmBuilder.WriteString(lines[i])
	}
	frontmatter := fmBuilder.String()

	// Extract body (everything after the closing ---)
	var bodyBuilder strings.Builder
	for i := closeIdx + 1; i < len(lines); i++ {
		bodyBuilder.WriteString(lines[i])
	}
	body := bodyBuilder.String()

	// Trim a single leading newline from the body
	body = strings.TrimPrefix(body, "\n")

	var skill AgentSkill
	if frontmatter != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &skill); err != nil {
			return nil, fmt.Errorf("parsing frontmatter: %w", err)
		}
	}

	skill.Body = body
	_ = validateState(&skill.State)

	return &skill, nil
}

// RenderSkillMD serializes an AgentSkill back to SKILL.md format.
func RenderSkillMD(skill *AgentSkill) ([]byte, error) {
	// Marshal frontmatter fields to YAML
	fm := struct {
		Name               string            `yaml:"name,omitempty"`
		Description        string            `yaml:"description,omitempty"`
		License            string            `yaml:"license,omitempty"`
		Compatibility      string            `yaml:"compatibility,omitempty"`
		Metadata           map[string]string `yaml:"metadata,omitempty"`
		AllowedTools       string            `yaml:"allowed-tools,omitempty"`
		AcceptanceCriteria []string          `yaml:"acceptance_criteria,omitempty"`
		State              ItemState         `yaml:"state,omitempty"`
	}{
		Name:               skill.Name,
		Description:        skill.Description,
		License:            skill.License,
		Compatibility:      skill.Compatibility,
		Metadata:           skill.Metadata,
		AllowedTools:       skill.AllowedTools,
		AcceptanceCriteria: skill.AcceptanceCriteria,
		State:              skill.State,
	}

	yamlBytes, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("marshaling frontmatter: %w", err)
	}

	// Emit unmodeled frontmatter keys after the known fields. Marshaling the
	// map separately keeps known-field output byte-identical for skills
	// without extras, and yaml.Marshal sorts map keys, so output stays
	// deterministic. Known keys are filtered so a hand-constructed Extra
	// (e.g. via the JSON API) can never emit a duplicate key.
	if len(skill.Extra) > 0 {
		extra := make(map[string]any, len(skill.Extra))
		for k, v := range skill.Extra {
			if !knownFrontmatterKey(k) {
				extra[k] = v
			}
		}
		if len(extra) > 0 {
			extraBytes, err := yaml.Marshal(extra)
			if err != nil {
				return nil, fmt.Errorf("marshaling extra frontmatter: %w", err)
			}
			yamlBytes = append(yamlBytes, extraBytes...)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(yamlBytes)
	buf.WriteString("---\n")
	if skill.Body != "" {
		buf.WriteString("\n")
		buf.WriteString(skill.Body)
	}

	return buf.Bytes(), nil
}
