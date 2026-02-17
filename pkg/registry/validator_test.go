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

// containsSubstring checks if any string in the slice contains the given substring.
func containsSubstring(strs []string, sub string) bool {
	for _, s := range strs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
