package registry

import (
	"strings"
	"testing"
)

func TestValidateSkillFull_Valid(t *testing.T) {
	skill := &AgentSkill{
		Name:        "valid-skill",
		Description: "A perfectly valid skill",
		State:       StateActive,
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", result.Warnings)
	}
}

func TestValidateSkillFull_MissingName(t *testing.T) {
	skill := &AgentSkill{
		Description: "Missing name",
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "name is required") {
		t.Errorf("expected name error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_NameUppercase(t *testing.T) {
	skill := &AgentSkill{
		Name:        "MySkill",
		Description: "Has uppercase",
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "lowercase") {
		t.Errorf("expected lowercase error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_NameConsecutiveHyphens(t *testing.T) {
	skill := &AgentSkill{
		Name:        "my--skill",
		Description: "Consecutive hyphens",
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "consecutive hyphens") {
		t.Errorf("expected consecutive hyphens error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_NameLeadingTrailingHyphens(t *testing.T) {
	tests := []struct {
		name string
		val  string
	}{
		{name: "leading hyphen", val: "-skill"},
		{name: "trailing hyphen", val: "skill-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skill := &AgentSkill{
				Name:        tt.val,
				Description: "Test",
			}
			result := ValidateSkillFull(skill)
			if result.Valid() {
				t.Errorf("expected invalid for name %q", tt.val)
			}
		})
	}
}

func TestValidateSkillFull_NameTooLong(t *testing.T) {
	// 65 characters
	longName := strings.Repeat("a", 65)
	skill := &AgentSkill{
		Name:        longName,
		Description: "Too long name",
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "exceeds") {
		t.Errorf("expected length error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_EmptyDescription(t *testing.T) {
	skill := &AgentSkill{
		Name: "test",
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "description is required") {
		t.Errorf("expected description error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_DescriptionTooLong(t *testing.T) {
	skill := &AgentSkill{
		Name:        "test",
		Description: strings.Repeat("x", 1025),
	}

	result := ValidateSkillFull(skill)
	// Should be a warning, not an error
	if !result.Valid() {
		t.Errorf("expected valid (warning only), got errors: %v", result.Errors)
	}
	if !containsSubstring(result.Warnings, "description exceeds") {
		t.Errorf("expected description warning, got warnings: %v", result.Warnings)
	}
}

func TestValidateSkillFull_BodyTooManyLines(t *testing.T) {
	// Create body with 501 lines
	lines := make([]string, 501)
	for i := range lines {
		lines[i] = "line content"
	}
	skill := &AgentSkill{
		Name:        "test",
		Description: "Test",
		Body:        strings.Join(lines, "\n"),
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid (warning only), got errors: %v", result.Errors)
	}
	if !containsSubstring(result.Warnings, "body exceeds") && !containsSubstring(result.Warnings, "lines") {
		t.Errorf("expected body lines warning, got warnings: %v", result.Warnings)
	}
}

func TestValidateSkillFull_BodyTooManyTokens(t *testing.T) {
	// 5001 * 4 = 20004 bytes -> ~5001 tokens
	skill := &AgentSkill{
		Name:        "test",
		Description: "Test",
		Body:        strings.Repeat("word", 5001),
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid (warning only), got errors: %v", result.Errors)
	}
	if !containsSubstring(result.Warnings, "tokens") {
		t.Errorf("expected token warning, got warnings: %v", result.Warnings)
	}
}

func TestValidateSkillName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid simple", input: "test", wantErr: false},
		{name: "valid with hyphens", input: "my-cool-skill", wantErr: false},
		{name: "valid with numbers", input: "skill123", wantErr: false},
		{name: "valid single char", input: "a", wantErr: false},
		{name: "valid max length", input: strings.Repeat("a", 64), wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "uppercase", input: "MySkill", wantErr: true},
		{name: "spaces", input: "my skill", wantErr: true},
		{name: "underscores", input: "my_skill", wantErr: true},
		{name: "dots", input: "my.skill", wantErr: true},
		{name: "leading hyphen", input: "-skill", wantErr: true},
		{name: "trailing hyphen", input: "skill-", wantErr: true},
		{name: "consecutive hyphens", input: "my--skill", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 65), wantErr: true},
		{name: "special chars", input: "skill@1", wantErr: true},
		{name: "path traversal", input: "../etc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSkillName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSkillName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSkillFull_MultipleErrors(t *testing.T) {
	skill := &AgentSkill{} // Missing both name and description

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if len(result.Errors) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestValidationResult_Error(t *testing.T) {
	t.Run("no errors returns nil", func(t *testing.T) {
		result := &ValidationResult{Warnings: []string{"warning"}}
		if result.Error() != nil {
			t.Errorf("expected nil error, got %v", result.Error())
		}
	})

	t.Run("errors joined with semicolons", func(t *testing.T) {
		result := &ValidationResult{
			Errors: []string{"first error", "second error"},
		}
		err := result.Error()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "first error") || !strings.Contains(err.Error(), "second error") {
			t.Errorf("error should contain all errors, got: %v", err)
		}
	})
}

func TestValidateSkillFull_ValidWorkflow(t *testing.T) {
	skill := &AgentSkill{
		Name:        "valid-workflow",
		Description: "A skill with a valid workflow",
		Inputs: map[string]SkillInput{
			"target": {Type: "string", Required: true},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "network__ping", Args: map[string]any{"host": "{{ inputs.target }}"}},
			{ID: "step-b", Tool: "network__traceroute", DependsOn: StringOrSlice{"step-a"}},
		},
		Output: &WorkflowOutput{Format: "merged"},
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateSkillFull_MissingStepID(t *testing.T) {
	skill := &AgentSkill{
		Name:        "missing-id",
		Description: "Test",
		Workflow: []WorkflowStep{
			{Tool: "server__tool"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "step ID is required") {
		t.Errorf("expected step ID error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_DuplicateStepID(t *testing.T) {
	skill := &AgentSkill{
		Name:        "dup-id",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-1"},
			{ID: "step-a", Tool: "server__tool-2"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "duplicate step ID 'step-a'") {
		t.Errorf("expected duplicate ID error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_InvalidStepIDFormat(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-format",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "Step_A", Tool: "server__tool"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "step ID 'Step_A'") {
		t.Errorf("expected step ID format error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_InvalidToolReference(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-tool",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "network_ping"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "missing '__' separator") {
		t.Errorf("expected tool format error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_CycleDetection(t *testing.T) {
	skill := &AgentSkill{
		Name:        "cycle-test",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a", DependsOn: StringOrSlice{"step-b"}},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"step-a"}},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "cycle detected") {
		t.Errorf("expected cycle error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_InvalidDependsOnRef(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-dep",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool-a"},
			{ID: "step-b", Tool: "server__tool-b", DependsOn: StringOrSlice{"nonexistent"}},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "unknown step 'nonexistent'") {
		t.Errorf("expected unknown step error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_AllowedToolsMismatch(t *testing.T) {
	skill := &AgentSkill{
		Name:         "tools-mismatch",
		Description:  "Test",
		AllowedTools: "network Read",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "network__ping"},
			{ID: "step-b", Tool: "storage__upload"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "not in allowed-tools") {
		t.Errorf("expected allowed-tools error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_AllowedToolsMatch(t *testing.T) {
	skill := &AgentSkill{
		Name:         "tools-match",
		Description:  "Test",
		AllowedTools: "network storage",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "network__ping"},
			{ID: "step-b", Tool: "storage__upload"},
		},
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateSkillFull_InvalidOnError(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-onerror",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool", OnError: "retry"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "on_error value 'retry' is invalid") {
		t.Errorf("expected on_error error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_InvalidInputType(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-input",
		Description: "Test",
		Inputs: map[string]SkillInput{
			"param": {Type: "integer"},
		},
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "input 'param' type 'integer' is invalid") {
		t.Errorf("expected input type error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_InvalidOutputFormat(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-output",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool"},
		},
		Output: &WorkflowOutput{Format: "xml"},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "output format 'xml' is invalid") {
		t.Errorf("expected output format error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_CustomOutputRequiresTemplate(t *testing.T) {
	skill := &AgentSkill{
		Name:        "custom-no-template",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool"},
		},
		Output: &WorkflowOutput{Format: "custom"},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "requires a non-empty template") {
		t.Errorf("expected template error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_OutputIncludeUnknownStep(t *testing.T) {
	skill := &AgentSkill{
		Name:        "bad-include",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "server__tool"},
		},
		Output: &WorkflowOutput{
			Format:  "merged",
			Include: []string{"step-a", "step-z"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "output include references unknown step 'step-z'") {
		t.Errorf("expected include error, got: %v", result.Errors)
	}
}

func TestValidateSkillFull_RegistryToolWarning(t *testing.T) {
	skill := &AgentSkill{
		Name:        "composition-test",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a", Tool: "registry__other-skill"},
		},
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid, got errors: %v", result.Errors)
	}
	if !containsSubstring(result.Warnings, "references skill 'other-skill'") {
		t.Errorf("expected registry warning, got warnings: %v", result.Warnings)
	}
}

func TestValidateSkillFull_TemplateRefWarnings(t *testing.T) {
	skill := &AgentSkill{
		Name:        "template-ref",
		Description: "Test",
		Inputs: map[string]SkillInput{
			"host": {Type: "string"},
		},
		Workflow: []WorkflowStep{
			{
				ID:   "step-a",
				Tool: "server__tool",
				Args: map[string]any{
					"target":  "{{ inputs.host }}",
					"unknown": "{{ inputs.nonexistent }}",
					"ref":     "{{ steps.bad-step.result }}",
				},
			},
		},
	}

	result := ValidateSkillFull(skill)
	if !result.Valid() {
		t.Errorf("expected valid (warnings only), got errors: %v", result.Errors)
	}
	if !containsSubstring(result.Warnings, "undeclared input 'nonexistent'") {
		t.Errorf("expected input warning, got warnings: %v", result.Warnings)
	}
	if !containsSubstring(result.Warnings, "unknown step 'bad-step'") {
		t.Errorf("expected step warning, got warnings: %v", result.Warnings)
	}
}

func TestValidateSkillFull_ValidOnErrorValues(t *testing.T) {
	values := []string{"fail", "skip", "continue", ""}
	for _, v := range values {
		skill := &AgentSkill{
			Name:        "on-error-test",
			Description: "Test",
			Workflow: []WorkflowStep{
				{ID: "step-a", Tool: "server__tool", OnError: v},
			},
		}
		result := ValidateSkillFull(skill)
		if !result.Valid() {
			t.Errorf("on_error=%q should be valid, got errors: %v", v, result.Errors)
		}
	}
}

func TestValidateSkillFull_MissingToolReference(t *testing.T) {
	skill := &AgentSkill{
		Name:        "no-tool",
		Description: "Test",
		Workflow: []WorkflowStep{
			{ID: "step-a"},
		},
	}

	result := ValidateSkillFull(skill)
	if result.Valid() {
		t.Error("expected invalid, got valid")
	}
	if !containsSubstring(result.Errors, "missing a tool reference") {
		t.Errorf("expected missing tool error, got: %v", result.Errors)
	}
}

// containsSubstring checks if any string in the slice contains the given substring.
func containsSubstring(strs []string, sub string) bool {
	for _, s := range strs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
