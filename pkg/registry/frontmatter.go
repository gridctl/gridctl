package registry

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

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
		Name          string                `yaml:"name,omitempty"`
		Description   string                `yaml:"description,omitempty"`
		License       string                `yaml:"license,omitempty"`
		Compatibility string                `yaml:"compatibility,omitempty"`
		Metadata      map[string]string     `yaml:"metadata,omitempty"`
		AllowedTools  string                `yaml:"allowed-tools,omitempty"`
		State         ItemState             `yaml:"state,omitempty"`
		Inputs        map[string]SkillInput `yaml:"inputs,omitempty"`
		Workflow      []WorkflowStep        `yaml:"workflow,omitempty"`
		Output        *WorkflowOutput       `yaml:"output,omitempty"`
	}{
		Name:          skill.Name,
		Description:   skill.Description,
		License:       skill.License,
		Compatibility: skill.Compatibility,
		Metadata:      skill.Metadata,
		AllowedTools:  skill.AllowedTools,
		State:         skill.State,
		Inputs:        skill.Inputs,
		Workflow:      skill.Workflow,
		Output:        skill.Output,
	}

	yamlBytes, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("marshaling frontmatter: %w", err)
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
