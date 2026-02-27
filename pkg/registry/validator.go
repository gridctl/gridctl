package registry

import (
	"fmt"
	"regexp"
	"sort"
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

	// Validate workflow (if present)
	if len(s.Workflow) > 0 {
		validateWorkflow(s, result)
	}

	// Validate inputs even without workflow (standalone validation)
	if len(s.Inputs) > 0 && len(s.Workflow) == 0 {
		validateInputTypes(s.Inputs, result)
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

var (
	validInputTypes   = map[string]bool{"string": true, "number": true, "boolean": true, "object": true, "array": true}
	validOnError      = map[string]bool{"fail": true, "skip": true, "continue": true, "": true}
	validOutputFormat = map[string]bool{"merged": true, "last": true, "custom": true, "": true}
)

// validateWorkflow validates the workflow definition within a skill.
func validateWorkflow(s *AgentSkill, result *ValidationResult) {
	// Validate input types
	validateInputTypes(s.Inputs, result)

	// Validate step IDs: uniqueness and format
	stepIDs := make(map[string]bool, len(s.Workflow))
	for _, step := range s.Workflow {
		if step.ID == "" {
			result.Errors = append(result.Errors, "workflow: step ID is required")
			continue
		}
		if !skillNamePattern.MatchString(step.ID) {
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: step ID '%s' must be lowercase alphanumeric with hyphens (matching %s)", step.ID, skillNamePattern.String()))
		}
		if stepIDs[step.ID] {
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: duplicate step ID '%s'", step.ID))
		}
		stepIDs[step.ID] = true
	}

	// Validate tool references
	for _, step := range s.Workflow {
		if step.Tool == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: step '%s' is missing a tool reference", step.ID))
			continue
		}
		if !strings.Contains(step.Tool, "__") {
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: step '%s' tool '%s' missing '__' separator (expected format: server__tool)", step.ID, step.Tool))
		}
	}

	// Validate depends_on references
	for _, step := range s.Workflow {
		for _, dep := range step.DependsOn {
			if !stepIDs[dep] {
				suggestion := suggestFromSet(dep, stepIDs)
				if suggestion != "" {
					result.Errors = append(result.Errors,
						fmt.Sprintf("workflow: step '%s' depends_on references unknown step '%s' (did you mean '%s'?)", step.ID, dep, suggestion))
				} else {
					result.Errors = append(result.Errors,
						fmt.Sprintf("workflow: step '%s' depends_on references unknown step '%s'", step.ID, dep))
				}
			}
		}
	}

	// Validate DAG (cycle detection)
	_, err := BuildWorkflowDAG(s.Workflow)
	if err != nil {
		result.Errors = append(result.Errors, "workflow: "+err.Error())
	}

	// Validate on_error values
	for _, step := range s.Workflow {
		if !validOnError[step.OnError] {
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: step '%s' on_error value '%s' is invalid (expected: fail, skip, or continue)", step.ID, step.OnError))
		}
	}

	// Validate allowed-tools consistency
	if s.AllowedTools != "" {
		allowedSet := parseAllowedTools(s.AllowedTools)
		for _, step := range s.Workflow {
			if step.Tool != "" && !matchesAllowedTools(step.Tool, allowedSet) {
				result.Errors = append(result.Errors,
					fmt.Sprintf("workflow: step '%s' tool '%s' not in allowed-tools", step.ID, step.Tool))
			}
		}
	}

	// Validate output
	if s.Output != nil {
		if !validOutputFormat[s.Output.Format] {
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: output format '%s' is invalid (expected: merged, last, or custom)", s.Output.Format))
		}
		if s.Output.Format == "custom" && s.Output.Template == "" {
			result.Errors = append(result.Errors,
				"workflow: output format 'custom' requires a non-empty template")
		}
		for _, ref := range s.Output.Include {
			if !stepIDs[ref] {
				result.Errors = append(result.Errors,
					fmt.Sprintf("workflow: output include references unknown step '%s'", ref))
			}
		}
	}

	// Warn on registry__ tool references (skill composition)
	for _, step := range s.Workflow {
		if strings.HasPrefix(step.Tool, "registry__") {
			skillName := strings.TrimPrefix(step.Tool, "registry__")
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("workflow: step '%s' references skill '%s' — this enables skill composition but may introduce cross-skill dependencies", step.ID, skillName))
		}
	}

	// Warn on template expressions referencing unknown inputs/steps (advisory only)
	validateTemplateRefs(s, stepIDs, result)
}

// validateInputTypes checks that all input types are valid.
func validateInputTypes(inputs map[string]SkillInput, result *ValidationResult) {
	for name, input := range inputs {
		if !validInputTypes[input.Type] {
			validTypes := sortedKeys(validInputTypes)
			result.Errors = append(result.Errors,
				fmt.Sprintf("workflow: input '%s' type '%s' is invalid (expected: %s)", name, input.Type, strings.Join(validTypes, ", ")))
		}
	}
}

// validateTemplateRefs checks template expressions in step args for references
// to declared inputs and step IDs. This is a warning, not an error.
func validateTemplateRefs(s *AgentSkill, stepIDs map[string]bool, result *ValidationResult) {
	templatePattern := regexp.MustCompile(`\{\{\s*(inputs|steps)\.([a-z0-9-]+)`)
	for _, step := range s.Workflow {
		for _, v := range step.Args {
			str, ok := v.(string)
			if !ok {
				continue
			}
			matches := templatePattern.FindAllStringSubmatch(str, -1)
			for _, match := range matches {
				kind := match[1]  // "inputs" or "steps"
				ref := match[2]   // input name or step ID
				switch kind {
				case "inputs":
					if _, exists := s.Inputs[ref]; !exists {
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("workflow: step '%s' references undeclared input '%s'", step.ID, ref))
					}
				case "steps":
					if !stepIDs[ref] {
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("workflow: step '%s' references unknown step '%s' in template expression", step.ID, ref))
					}
				}
			}
		}
	}
}

// parseAllowedTools extracts tool names from the allowed-tools string.
// The format is space-separated tool names, optionally with patterns like "Bash(git:*)".
func parseAllowedTools(allowedTools string) map[string]bool {
	tools := make(map[string]bool)
	for _, t := range strings.Fields(allowedTools) {
		// Strip any parenthetical patterns (e.g., "Bash(git:*)" -> "Bash")
		if idx := strings.Index(t, "("); idx != -1 {
			t = t[:idx]
		}
		tools[t] = true
	}
	return tools
}

// matchesAllowedTools checks if a workflow tool reference matches the allowed tools.
// Workflow tools use "server__tool" format; allowed-tools uses agent-level tool names.
// We check if the server part or the full tool reference appears in allowed tools.
func matchesAllowedTools(tool string, allowedSet map[string]bool) bool {
	if allowedSet[tool] {
		return true
	}
	// Check server name part
	if parts := strings.SplitN(tool, "__", 2); len(parts) == 2 {
		if allowedSet[parts[0]] {
			return true
		}
	}
	return false
}

// suggestFromSet finds a close match for a misspelled ID from a set of valid IDs.
func suggestFromSet(target string, validIDs map[string]bool) string {
	var best string
	bestDist := len(target)/2 + 1
	for id := range validIDs {
		d := levenshtein(target, id)
		if d < bestDist {
			bestDist = d
			best = id
		}
	}
	return best
}

// sortedKeys returns sorted keys from a map for deterministic output.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
