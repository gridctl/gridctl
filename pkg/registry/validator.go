package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// skillNamePattern validates skill names per the agentskills.io spec:
// lowercase alphanumeric and hyphens, must start and end with alphanumeric.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

const (
	maxNameLength        = 64
	maxDescriptionLength = 1024
	maxBodyLines         = 500
	maxBodyTokens        = 5000
	bytesPerToken        = 4
)

// ValidationResult contains errors and warnings from skill validation.
type ValidationResult struct {
	Errors   []string // Fatal validation failures
	Warnings []string // Non-fatal advisories (e.g., body too long)
}

// Valid returns true if there are no errors (warnings are OK).
func (v *ValidationResult) Valid() bool {
	return len(v.Errors) == 0
}

// Error returns the first error as a Go error, or nil if valid.
func (v *ValidationResult) Error() error {
	if len(v.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("validation failed: %s", strings.Join(v.Errors, "; "))
}

// ValidateSkill validates an AgentSkill and returns just the error (convenience wrapper).
func ValidateSkill(s *AgentSkill) error {
	result := ValidateSkillFull(s)
	return result.Error()
}

// ValidateSkillFull validates an AgentSkill and returns both errors and warnings.
func ValidateSkillFull(s *AgentSkill) *ValidationResult {
	result := &ValidationResult{}

	// Validate name
	if err := ValidateSkillName(s.Name); err != nil {
		result.Errors = append(result.Errors, err.Error())
	}

	// Validate description (required)
	if s.Description == "" {
		result.Errors = append(result.Errors, "description is required")
	} else if len(s.Description) > maxDescriptionLength {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("description exceeds %d characters (%d)", maxDescriptionLength, len(s.Description)))
	}

	// Validate state
	state := s.State
	if err := validateState(&state); err != nil {
		result.Errors = append(result.Errors, err.Error())
	}

	// Validate body (warnings only)
	if s.Body != "" {
		lineCount := strings.Count(s.Body, "\n") + 1
		if lineCount > maxBodyLines {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("body exceeds %d lines (%d)", maxBodyLines, lineCount))
		}

		estimatedTokens := len(s.Body) / bytesPerToken
		if estimatedTokens > maxBodyTokens {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("body exceeds estimated %d tokens (~%d)", maxBodyTokens, estimatedTokens))
		}
	}

	return result
}

// ValidateSkillName validates a skill name against the agentskills.io spec.
func ValidateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("name exceeds %d characters (%d)", maxNameLength, len(name))
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("name %q must not contain consecutive hyphens", name)
	}
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("name %q must be lowercase alphanumeric with hyphens (matching %s)", name, skillNamePattern.String())
	}
	return nil
}
