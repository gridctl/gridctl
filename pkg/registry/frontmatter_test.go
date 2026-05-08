package registry

import (
	"strings"
	"testing"
)

func TestParseSkillMD_ValidFull(t *testing.T) {
	input := `---
name: code-review
description: Review code for quality issues and suggest improvements
license: Apache-2.0
compatibility: Requires git
metadata:
  author: example-org
  version: "1.0"
allowed-tools: Bash(git:*) Read
state: active
---

# Code Review Instructions

When reviewing code, follow these steps...
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "code-review" {
		t.Errorf("Name = %q, want %q", skill.Name, "code-review")
	}
	if skill.Description != "Review code for quality issues and suggest improvements" {
		t.Errorf("Description = %q", skill.Description)
	}
	if skill.License != "Apache-2.0" {
		t.Errorf("License = %q", skill.License)
	}
	if skill.Compatibility != "Requires git" {
		t.Errorf("Compatibility = %q", skill.Compatibility)
	}
	if skill.Metadata["author"] != "example-org" {
		t.Errorf("Metadata[author] = %q", skill.Metadata["author"])
	}
	if skill.Metadata["version"] != "1.0" {
		t.Errorf("Metadata[version] = %q", skill.Metadata["version"])
	}
	if skill.AllowedTools != "Bash(git:*) Read" {
		t.Errorf("AllowedTools = %q", skill.AllowedTools)
	}
	if skill.State != StateActive {
		t.Errorf("State = %q, want %q", skill.State, StateActive)
	}
	if !strings.Contains(skill.Body, "# Code Review Instructions") {
		t.Errorf("Body should contain header, got %q", skill.Body)
	}
}

func TestParseSkillMD_EmptyFrontmatter(t *testing.T) {
	input := "---\n---\nSome body content here."

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "" {
		t.Errorf("Name = %q, want empty", skill.Name)
	}
	if skill.Body != "Some body content here." {
		t.Errorf("Body = %q, want %q", skill.Body, "Some body content here.")
	}
	if skill.State != StateDraft {
		t.Errorf("State = %q, want %q", skill.State, StateDraft)
	}
}

func TestParseSkillMD_NoFrontmatter(t *testing.T) {
	input := "# Just Markdown\n\nNo frontmatter here."

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "" {
		t.Errorf("Name = %q, want empty", skill.Name)
	}
	if skill.Body != input {
		t.Errorf("Body = %q, want %q", skill.Body, input)
	}
	if skill.State != StateDraft {
		t.Errorf("State = %q, want %q", skill.State, StateDraft)
	}
}

func TestParseSkillMD_FrontmatterNoBody(t *testing.T) {
	input := "---\nname: minimal\ndescription: A minimal skill\n---\n"

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "minimal" {
		t.Errorf("Name = %q, want %q", skill.Name, "minimal")
	}
	if skill.Body != "" {
		t.Errorf("Body = %q, want empty", skill.Body)
	}
}

func TestParseSkillMD_BodyWithHorizontalRules(t *testing.T) {
	input := `---
name: test-skill
description: A test skill
---

# Section One

Some content.

---

# Section Two

More content after horizontal rule.

---

End of file.
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", skill.Name, "test-skill")
	}
	// Body should contain the horizontal rules
	if !strings.Contains(skill.Body, "# Section Two") {
		t.Errorf("Body should contain Section Two, got %q", skill.Body)
	}
	if strings.Count(skill.Body, "---") != 2 {
		t.Errorf("Body should contain 2 horizontal rules, got %d", strings.Count(skill.Body, "---"))
	}
}

func TestParseSkillMD_UnknownYAMLFields(t *testing.T) {
	input := `---
name: test-skill
description: A test
unknown_field: should be ignored
another_custom: also ignored
---

Body text.
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", skill.Name, "test-skill")
	}
	if !strings.Contains(skill.Body, "Body text.") {
		t.Errorf("Body = %q", skill.Body)
	}
}

func TestParseSkillMD_DefaultState(t *testing.T) {
	input := "---\nname: test\ndescription: A test\n---\n"

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.State != StateDraft {
		t.Errorf("State = %q, want %q", skill.State, StateDraft)
	}
}

func TestParseSkillMD_WindowsLineEndings(t *testing.T) {
	input := "---\r\nname: test\r\ndescription: A test\r\n---\r\n\r\n# Body\r\n"

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "test" {
		t.Errorf("Name = %q, want %q", skill.Name, "test")
	}
	if !strings.Contains(skill.Body, "# Body") {
		t.Errorf("Body = %q, should contain '# Body'", skill.Body)
	}
}

func TestRenderSkillMD_RoundTrip(t *testing.T) {
	original := &AgentSkill{
		Name:          "round-trip",
		Description:   "Test round-trip parsing",
		License:       "MIT",
		Compatibility: "Any",
		Metadata:      map[string]string{"version": "2.0"},
		AllowedTools:  "Read Write",
		State:         StateActive,
		Body:          "# Instructions\n\nDo the thing.\n",
	}

	rendered, err := RenderSkillMD(original)
	if err != nil {
		t.Fatalf("RenderSkillMD() error = %v", err)
	}

	parsed, err := ParseSkillMD(rendered)
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if parsed.Name != original.Name {
		t.Errorf("Name = %q, want %q", parsed.Name, original.Name)
	}
	if parsed.Description != original.Description {
		t.Errorf("Description = %q, want %q", parsed.Description, original.Description)
	}
	if parsed.License != original.License {
		t.Errorf("License = %q, want %q", parsed.License, original.License)
	}
	if parsed.Compatibility != original.Compatibility {
		t.Errorf("Compatibility = %q, want %q", parsed.Compatibility, original.Compatibility)
	}
	if parsed.AllowedTools != original.AllowedTools {
		t.Errorf("AllowedTools = %q, want %q", parsed.AllowedTools, original.AllowedTools)
	}
	if parsed.State != original.State {
		t.Errorf("State = %q, want %q", parsed.State, original.State)
	}
	if parsed.Body != original.Body {
		t.Errorf("Body = %q, want %q", parsed.Body, original.Body)
	}
}

func TestRenderSkillMD_Format(t *testing.T) {
	skill := &AgentSkill{
		Name:        "format-test",
		Description: "Check output format",
		State:       StateActive,
		Body:        "# Hello\n",
	}

	rendered, err := RenderSkillMD(skill)
	if err != nil {
		t.Fatalf("RenderSkillMD() error = %v", err)
	}

	output := string(rendered)

	if !strings.HasPrefix(output, "---\n") {
		t.Errorf("output should start with '---\\n', got prefix %q", output[:min(10, len(output))])
	}
	if !strings.Contains(output, "\n---\n") {
		t.Error("output should contain closing '---' delimiter")
	}
	if !strings.HasSuffix(output, "# Hello\n") {
		t.Errorf("output should end with body, got suffix %q", output[max(0, len(output)-20):])
	}
}

func TestRenderSkillMD_EmptyBody(t *testing.T) {
	skill := &AgentSkill{
		Name:        "no-body",
		Description: "Skill without body",
		State:       StateDraft,
	}

	rendered, err := RenderSkillMD(skill)
	if err != nil {
		t.Fatalf("RenderSkillMD() error = %v", err)
	}

	output := string(rendered)
	// Should end with the closing delimiter, no extra content
	if !strings.HasSuffix(output, "---\n") {
		t.Errorf("output should end with '---\\n', got %q", output[max(0, len(output)-10):])
	}
}
