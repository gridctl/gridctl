package registry

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const defaultCriterionTimeout = 30 * time.Second

// criterionPattern matches Given/When/Then acceptance criteria strings.
var criterionPattern = regexp.MustCompile(`(?i)GIVEN\s+(.+?)\s+WHEN\s+(.+?)\s+THEN\s+(.+)`)

// parsedCriterion holds the decomposed Given/When/Then components.
type parsedCriterion struct {
	Raw   string
	Given string
	When  string
	Then  string
}

// CriterionResult holds the outcome of a single acceptance criterion test.
type CriterionResult struct {
	Criterion  string `json:"criterion"`
	Passed     bool   `json:"passed"`
	Actual     string `json:"actual,omitempty"`     // Set when failed
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skipReason,omitempty"` // Set when skipped
}

// SkillTestResult holds the complete result of running a skill's acceptance criteria.
type SkillTestResult struct {
	Skill   string            `json:"skill"`
	Passed  int               `json:"passed"`
	Failed  int               `json:"failed"`
	Skipped int               `json:"skipped,omitempty"`
	Results []CriterionResult `json:"results"`
}

// ParseCriterion extracts Given/When/Then from a criterion string.
// Returns nil if the string does not match the expected pattern.
func ParseCriterion(s string) *parsedCriterion {
	m := criterionPattern.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	return &parsedCriterion{
		Raw:   s,
		Given: strings.TrimSpace(m[1]),
		When:  strings.TrimSpace(m[2]),
		Then:  strings.TrimSpace(m[3]),
	}
}

// resolveToolName extracts an MCP tool name from the WHEN clause.
// Handles "the skill is called", "<name> is called", and "server__tool" formats.
func resolveToolName(when, skillName string) string {
	lower := strings.ToLower(strings.TrimSpace(when))

	if strings.Contains(lower, "the skill is called") {
		return skillName
	}

	if idx := strings.LastIndex(lower, " is called"); idx != -1 {
		candidate := strings.TrimSpace(when[:idx])
		if candidate != "" {
			return candidate
		}
	}

	// Explicit server__tool reference anywhere in the clause
	for _, f := range strings.Fields(when) {
		clean := strings.Trim(f, ".,;:()")
		if strings.Contains(clean, "__") {
			return clean
		}
	}

	return skillName
}

// evaluateAssertion checks whether actual satisfies the THEN assertion.
//
// Supported assertion types:
//   - "is empty"        — result is blank
//   - "is not empty"    — result has content
//   - "matches <regex>" — result matches a regular expression
//   - "contains <text>" — result contains the substring (default)
//   - bare text         — treated as "contains <text>"
func evaluateAssertion(actual, then string) (bool, error) {
	then = strings.TrimSpace(then)
	lower := strings.ToLower(then)

	switch {
	case lower == "is empty":
		return strings.TrimSpace(actual) == "", nil
	case lower == "is not empty":
		return strings.TrimSpace(actual) != "", nil
	case strings.HasPrefix(lower, "matches "):
		pattern := strings.TrimSpace(then[len("matches "):])
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("invalid regex in THEN clause: %w", err)
		}
		return re.MatchString(actual), nil
	case strings.HasPrefix(lower, "contains "):
		substr := strings.TrimSpace(then[len("contains "):])
		return strings.Contains(actual, substr), nil
	default:
		return strings.Contains(actual, then), nil
	}
}

// RunAcceptanceCriteria runs a skill's acceptance criteria against live MCP tools.
// Criteria that don't match the Given/When/Then pattern are skipped with a warning.
// Each criterion is subject to criterionTimeout (0 uses the 30s default).
func (e *Executor) RunAcceptanceCriteria(ctx context.Context, skill *AgentSkill, criterionTimeout time.Duration) (*SkillTestResult, error) {
	if criterionTimeout <= 0 {
		criterionTimeout = defaultCriterionTimeout
	}

	result := &SkillTestResult{Skill: skill.Name}

	for _, criterion := range skill.AcceptanceCriteria {
		parsed := ParseCriterion(criterion)
		if parsed == nil {
			e.logger.Warn("skipping unparseable acceptance criterion",
				"skill", skill.Name,
				"criterion", criterion)
			result.Results = append(result.Results, CriterionResult{
				Criterion:  criterion,
				Skipped:    true,
				SkipReason: "does not match GIVEN ... WHEN ... THEN pattern",
			})
			result.Skipped++
			continue
		}

		toolName := resolveToolName(parsed.When, skill.Name)

		critCtx, cancel := context.WithTimeout(ctx, criterionTimeout)
		toolResult, err := e.caller.CallTool(critCtx, toolName, nil)
		cancel()

		if err != nil {
			result.Results = append(result.Results, CriterionResult{
				Criterion: criterion,
				Passed:    false,
				Actual:    fmt.Sprintf("tool call error: %v", err),
			})
			result.Failed++
			continue
		}

		actual := extractText(toolResult)
		if toolResult != nil && toolResult.IsError {
			result.Results = append(result.Results, CriterionResult{
				Criterion: criterion,
				Passed:    false,
				Actual:    actual,
			})
			result.Failed++
			continue
		}

		passed, assertErr := evaluateAssertion(actual, parsed.Then)
		if assertErr != nil {
			e.logger.Warn("skipping criterion with invalid assertion",
				"skill", skill.Name,
				"criterion", criterion,
				"error", assertErr)
			result.Results = append(result.Results, CriterionResult{
				Criterion:  criterion,
				Skipped:    true,
				SkipReason: fmt.Sprintf("assertion error: %v", assertErr),
			})
			result.Skipped++
			continue
		}

		cr := CriterionResult{
			Criterion: criterion,
			Passed:    passed,
		}
		if !passed {
			cr.Actual = actual
		}
		result.Results = append(result.Results, cr)
		if passed {
			result.Passed++
		} else {
			result.Failed++
		}
	}

	return result, nil
}
