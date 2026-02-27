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

func TestParseSkillMD_WithWorkflow(t *testing.T) {
	input := `---
name: network-check
description: Check network connectivity
inputs:
  device-ip:
    type: string
    description: Target device IP
    required: true
  protocol:
    type: string
    default: icmp
    enum: [icmp, tcp]
workflow:
  - id: check-reachable
    tool: network__ping
    args:
      target: "{{ inputs.device-ip }}"
      protocol: "{{ inputs.protocol }}"
  - id: get-interfaces
    tool: network__get-interfaces
    args:
      device: "{{ inputs.device-ip }}"
    depends_on: check-reachable
    on_error: skip
output:
  format: merged
---

# Network Check

Validates connectivity to a network device.
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "network-check" {
		t.Errorf("Name = %q, want %q", skill.Name, "network-check")
	}

	// Verify inputs
	if len(skill.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(skill.Inputs))
	}
	ip := skill.Inputs["device-ip"]
	if ip.Type != "string" || !ip.Required {
		t.Errorf("device-ip input = %+v", ip)
	}
	proto := skill.Inputs["protocol"]
	if proto.Default != "icmp" || len(proto.Enum) != 2 {
		t.Errorf("protocol input = %+v", proto)
	}

	// Verify workflow
	if len(skill.Workflow) != 2 {
		t.Fatalf("expected 2 workflow steps, got %d", len(skill.Workflow))
	}
	if skill.Workflow[0].ID != "check-reachable" {
		t.Errorf("step 0 ID = %q", skill.Workflow[0].ID)
	}
	if skill.Workflow[1].OnError != "skip" {
		t.Errorf("step 1 on_error = %q, want 'skip'", skill.Workflow[1].OnError)
	}
	if len(skill.Workflow[1].DependsOn) != 1 || skill.Workflow[1].DependsOn[0] != "check-reachable" {
		t.Errorf("step 1 depends_on = %v", skill.Workflow[1].DependsOn)
	}

	// Verify output
	if skill.Output == nil || skill.Output.Format != "merged" {
		t.Errorf("output = %+v", skill.Output)
	}

	// Verify body preserved
	if !strings.Contains(skill.Body, "# Network Check") {
		t.Errorf("Body should contain header, got %q", skill.Body)
	}
}

func TestParseSkillMD_WithoutWorkflow_BackwardCompat(t *testing.T) {
	input := `---
name: simple-skill
description: A simple knowledge skill
---

# Instructions

Just a regular skill without workflow.
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if skill.Name != "simple-skill" {
		t.Errorf("Name = %q", skill.Name)
	}
	if skill.IsExecutable() {
		t.Error("skill without workflow should not be executable")
	}
	if skill.Inputs != nil {
		t.Errorf("Inputs should be nil, got %v", skill.Inputs)
	}
	if skill.Workflow != nil {
		t.Errorf("Workflow should be nil, got %v", skill.Workflow)
	}
	if skill.Output != nil {
		t.Errorf("Output should be nil, got %v", skill.Output)
	}
}

func TestParseSkillMD_DependsOnSingleString(t *testing.T) {
	input := `---
name: dep-test
description: Test depends_on as single string
workflow:
  - id: step-a
    tool: server__tool-a
  - id: step-b
    tool: server__tool-b
    depends_on: step-a
---
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if len(skill.Workflow[1].DependsOn) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(skill.Workflow[1].DependsOn))
	}
	if skill.Workflow[1].DependsOn[0] != "step-a" {
		t.Errorf("depends_on[0] = %q, want 'step-a'", skill.Workflow[1].DependsOn[0])
	}
}

func TestParseSkillMD_DependsOnArray(t *testing.T) {
	input := `---
name: dep-test
description: Test depends_on as array
workflow:
  - id: step-a
    tool: server__tool-a
  - id: step-b
    tool: server__tool-b
  - id: step-c
    tool: server__tool-c
    depends_on: [step-a, step-b]
---
`

	skill, err := ParseSkillMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseSkillMD() error = %v", err)
	}

	if len(skill.Workflow[2].DependsOn) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(skill.Workflow[2].DependsOn))
	}
	if skill.Workflow[2].DependsOn[0] != "step-a" || skill.Workflow[2].DependsOn[1] != "step-b" {
		t.Errorf("depends_on = %v", skill.Workflow[2].DependsOn)
	}
}

func TestRenderSkillMD_RoundTrip_WithWorkflow(t *testing.T) {
	original := &AgentSkill{
		Name:        "workflow-roundtrip",
		Description: "Test round-trip with workflow",
		State:       StateActive,
		Inputs: map[string]SkillInput{
			"target": {Type: "string", Required: true, Description: "Target host"},
		},
		Workflow: []WorkflowStep{
			{
				ID:   "step-1",
				Tool: "network__ping",
				Args: map[string]any{"host": "{{ inputs.target }}"},
			},
			{
				ID:        "step-2",
				Tool:      "network__traceroute",
				DependsOn: StringOrSlice{"step-1"},
				OnError:   "skip",
			},
		},
		Output: &WorkflowOutput{Format: "merged"},
		Body:   "# Workflow Skill\n",
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
	if !parsed.IsExecutable() {
		t.Error("parsed skill should be executable")
	}
	if len(parsed.Workflow) != 2 {
		t.Fatalf("expected 2 workflow steps, got %d", len(parsed.Workflow))
	}
	if parsed.Workflow[0].ID != "step-1" {
		t.Errorf("step 0 ID = %q", parsed.Workflow[0].ID)
	}
	if parsed.Workflow[1].OnError != "skip" {
		t.Errorf("step 1 on_error = %q", parsed.Workflow[1].OnError)
	}
	if len(parsed.Inputs) != 1 {
		t.Errorf("expected 1 input, got %d", len(parsed.Inputs))
	}
	if parsed.Output == nil || parsed.Output.Format != "merged" {
		t.Errorf("output = %+v", parsed.Output)
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
