package registry

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Safety limits for template resolution.
const (
	maxExpressionLen = 500 // Max characters in a single {{ ... }} expression
	maxJSONPathDepth = 10  // Max dot-separated segments in JSON path
	maxResultSize    = 1 << 20 // 1MB cap for StepResult.Result
)

// templateRegex matches {{ ... }} expressions, compiled once at package init.
var templateRegex = regexp.MustCompile(`\{\{\s*(.+?)\s*\}\}`)

// allowedExprChars validates that an expression contains only safe characters.
// Permits alphanumeric, dots, underscores, hyphens, spaces, and comparison operators.
var allowedExprChars = regexp.MustCompile(`^[a-zA-Z0-9._\-\s!=<>]+$`)

// TemplateContext holds the data available to template expressions.
type TemplateContext struct {
	Inputs map[string]any           // User-provided arguments
	Steps  map[string]*StepResult   // Completed step results
}

// StepResult captures the output of a completed workflow step.
type StepResult struct {
	Result  string // Text content from ToolCallResult
	IsError bool   // Whether the step returned isError: true
	Raw     any    // Parsed JSON if result is valid JSON, otherwise nil
}

// NewStepResult creates a StepResult, truncating oversized results and parsing JSON.
func NewStepResult(result string, isError bool) *StepResult {
	if len(result) > maxResultSize {
		result = result[:maxResultSize]
	}
	sr := &StepResult{
		Result:  result,
		IsError: isError,
	}
	var parsed any
	if err := json.Unmarshal([]byte(result), &parsed); err == nil {
		sr.Raw = parsed
	}
	return sr
}

// ResolveArgs resolves template expressions in a step's args map.
// Returns a new map with all {{ ... }} expressions replaced with values.
func ResolveArgs(args map[string]any, ctx *TemplateContext) (map[string]any, error) {
	if args == nil {
		return nil, nil
	}
	result := make(map[string]any, len(args))
	for k, v := range args {
		resolved, err := resolveValue(v, ctx)
		if err != nil {
			return nil, fmt.Errorf("arg %q: %w", k, err)
		}
		result[k] = resolved
	}
	return result, nil
}

// ResolveString resolves template expressions in a single string value.
// If the entire string is a single expression, the typed value is returned.
// If mixed with literal text, expressions are resolved and concatenated as a string.
func ResolveString(s string, ctx *TemplateContext) (any, error) {
	matches := templateRegex.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}

	// Single expression covering the entire string: return typed value
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(s) {
		expr := strings.TrimSpace(s[matches[0][2]:matches[0][3]])
		return resolveExpression(expr, ctx)
	}

	// Mixed text: concatenate as string
	var b strings.Builder
	prev := 0
	for _, m := range matches {
		b.WriteString(s[prev:m[0]])
		expr := strings.TrimSpace(s[m[2]:m[3]])
		val, err := resolveExpression(expr, ctx)
		if err != nil {
			return nil, err
		}
		b.WriteString(fmt.Sprintf("%v", val))
		prev = m[1]
	}
	b.WriteString(s[prev:])
	return b.String(), nil
}

// ResolveTemplate resolves a full template string (used for output.template).
// Always returns a string, even for single expressions.
func ResolveTemplate(tmpl string, ctx *TemplateContext) (string, error) {
	var b strings.Builder
	prev := 0
	matches := templateRegex.FindAllStringSubmatchIndex(tmpl, -1)
	for _, m := range matches {
		b.WriteString(tmpl[prev:m[0]])
		expr := strings.TrimSpace(tmpl[m[2]:m[3]])
		val, err := resolveExpression(expr, ctx)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("%v", val))
		prev = m[1]
	}
	b.WriteString(tmpl[prev:])
	return b.String(), nil
}

// EvaluateCondition evaluates a condition expression against the template context.
// Supports: equality (==, !=), boolean checks, and existence checks.
func EvaluateCondition(expr string, ctx *TemplateContext) (bool, error) {
	// Strip {{ }} wrapper if present
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "{{") && strings.HasSuffix(expr, "}}") {
		expr = strings.TrimSpace(expr[2 : len(expr)-2])
	}

	if err := validateExpression(expr); err != nil {
		return false, err
	}

	// Check for comparison operators
	if idx := strings.Index(expr, "!="); idx > 0 {
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+2:])
		return evalComparison(left, right, ctx, false)
	}
	if idx := strings.Index(expr, "=="); idx > 0 {
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+2:])
		return evalComparison(left, right, ctx, true)
	}

	// Boolean/existence check
	val, err := resolveExpression(expr, ctx)
	if err != nil {
		return false, err
	}
	return isTruthy(val), nil
}

// resolveValue recursively resolves template expressions in any value.
func resolveValue(v any, ctx *TemplateContext) (any, error) {
	switch val := v.(type) {
	case string:
		return ResolveString(val, ctx)
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, child := range val {
			resolved, err := resolveValue(child, ctx)
			if err != nil {
				return nil, err
			}
			result[k] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(val))
		for i, child := range val {
			resolved, err := resolveValue(child, ctx)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	default:
		return v, nil
	}
}

// resolveExpression resolves a single template expression (without {{ }} delimiters).
func resolveExpression(expr string, ctx *TemplateContext) (any, error) {
	if err := validateExpression(expr); err != nil {
		return nil, err
	}

	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("template: invalid expression %q", expr)
	}

	switch parts[0] {
	case "inputs":
		return resolveInput(parts[1], ctx)
	case "steps":
		return resolveStep(parts[1], ctx)
	default:
		return nil, fmt.Errorf("template: unknown namespace %q in expression %q", parts[0], expr)
	}
}

// resolveInput resolves an inputs.<name> expression.
func resolveInput(name string, ctx *TemplateContext) (any, error) {
	if ctx.Inputs == nil {
		return nil, fmt.Errorf("template: input %q not found in skill inputs", name)
	}
	val, ok := ctx.Inputs[name]
	if !ok {
		return nil, fmt.Errorf("template: input %q not found in skill inputs", name)
	}
	return val, nil
}

// resolveStep resolves a steps.<id>.<field>[.<path>] expression.
func resolveStep(path string, ctx *TemplateContext) (any, error) {
	parts := strings.SplitN(path, ".", 2)
	stepID := parts[0]

	if ctx.Steps == nil {
		return nil, fmt.Errorf("template: step %q not found in completed steps", stepID)
	}
	step, ok := ctx.Steps[stepID]
	if !ok {
		return nil, fmt.Errorf("template: step %q not found in completed steps", stepID)
	}

	if len(parts) < 2 {
		return nil, fmt.Errorf("template: incomplete step reference %q (need result, is_error, or json.<path>)", path)
	}

	field := parts[1]
	switch {
	case field == "result":
		return step.Result, nil
	case field == "is_error":
		return step.IsError, nil
	case strings.HasPrefix(field, "json."):
		jsonPath := field[5:] // strip "json."
		return resolveJSONPath(step, jsonPath, path)
	default:
		return nil, fmt.Errorf("template: unknown step field %q in expression %q (expected result, is_error, or json.<path>)", field, path)
	}
}

// resolveJSONPath extracts a value from a step's parsed JSON result using dot notation.
func resolveJSONPath(step *StepResult, path string, fullExpr string) (any, error) {
	if step.Raw == nil {
		return nil, fmt.Errorf("template: step result is not valid JSON in expression %q", fullExpr)
	}

	segments := strings.Split(path, ".")
	if len(segments) > maxJSONPathDepth {
		return nil, fmt.Errorf("template: JSON path exceeds maximum depth of %d in expression %q", maxJSONPathDepth, fullExpr)
	}

	var current any = step.Raw
	for _, seg := range segments {
		switch val := current.(type) {
		case map[string]any:
			next, ok := val[seg]
			if !ok {
				return nil, fmt.Errorf("template: JSON path %q not found in expression %q", seg, fullExpr)
			}
			current = next
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("template: expected array index but got %q in expression %q", seg, fullExpr)
			}
			if idx < 0 || idx >= len(val) {
				return nil, fmt.Errorf("template: array index %d out of bounds (length %d) in expression %q", idx, len(val), fullExpr)
			}
			current = val[idx]
		default:
			return nil, fmt.Errorf("template: cannot traverse into %T at %q in expression %q", val, seg, fullExpr)
		}
	}
	return current, nil
}

// validateExpression checks safety constraints on a template expression.
func validateExpression(expr string) error {
	if len(expr) > maxExpressionLen {
		return fmt.Errorf("template: expression exceeds maximum length of %d characters", maxExpressionLen)
	}
	if !allowedExprChars.MatchString(expr) {
		return fmt.Errorf("template: expression %q contains invalid characters", expr)
	}
	return nil
}

// evalComparison evaluates a comparison between a resolved left-hand expression
// and a literal right-hand value.
func evalComparison(left, right string, ctx *TemplateContext, equality bool) (bool, error) {
	val, err := resolveExpression(left, ctx)
	if err != nil {
		return false, err
	}

	match := valuesEqual(val, right)
	if equality {
		return match, nil
	}
	return !match, nil
}

// valuesEqual compares a resolved value against a string literal.
func valuesEqual(val any, literal string) bool {
	switch v := val.(type) {
	case bool:
		return fmt.Sprintf("%v", v) == literal
	case float64:
		return fmt.Sprintf("%v", v) == literal
	case string:
		return v == literal
	case json.Number:
		return string(v) == literal
	default:
		return fmt.Sprintf("%v", v) == literal
	}
}

// isTruthy determines whether a value is "truthy" for condition evaluation.
func isTruthy(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case nil:
		return false
	default:
		return true
	}
}
