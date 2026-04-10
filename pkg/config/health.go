package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// IssueSeverity represents the severity level of a validation issue.
type IssueSeverity string

const (
	SeverityError   IssueSeverity = "error"
	SeverityWarning IssueSeverity = "warning"
	SeverityInfo    IssueSeverity = "info"
)

// ValidationIssue is a validation finding with severity level.
type ValidationIssue struct {
	Field    string        `json:"field"`
	Message  string        `json:"message"`
	Severity IssueSeverity `json:"severity"`
}

// ValidationResult holds the complete output of spec validation.
type ValidationResult struct {
	Valid      bool              `json:"valid"`
	ErrorCount   int            `json:"errorCount"`
	WarningCount int            `json:"warningCount"`
	Issues     []ValidationIssue `json:"issues"`
}

// SpecHealth aggregates validation, drift, and dependency status.
type SpecHealth struct {
	Validation  ValidationStatus  `json:"validation"`
	Drift       DriftStatus       `json:"drift"`
	Dependencies DependencyStatus `json:"dependencies"`
}

// ValidationStatus summarizes the spec validation state.
type ValidationStatus struct {
	Status       string `json:"status"` // "valid", "warnings", "errors"
	ErrorCount   int    `json:"errorCount"`
	WarningCount int    `json:"warningCount"`
}

// DriftStatus summarizes drift between spec and running state.
type DriftStatus struct {
	Status  string   `json:"status"` // "in-sync", "drifted", "unknown"
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

// DependencyStatus summarizes skill dependency resolution.
type DependencyStatus struct {
	Status  string   `json:"status"` // "resolved", "missing"
	Missing []string `json:"missing,omitempty"`
}

// ValidateWithIssues runs full validation and returns structured issues with severity.
// This wraps the existing Validate() and adds warning-level checks.
func ValidateWithIssues(s *Stack) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Run existing validation to get errors
	if err := Validate(s); err != nil {
		result.Valid = false
		if ve, ok := err.(ValidationErrors); ok {
			for _, e := range ve {
				result.Issues = append(result.Issues, ValidationIssue{
					Field:    e.Field,
					Message:  e.Message,
					Severity: SeverityError,
				})
				result.ErrorCount++
			}
		} else {
			// Unexpected error type from validation
			result.Issues = append(result.Issues, ValidationIssue{
				Field:    "stack",
				Message:  err.Error(),
				Severity: SeverityError,
			})
			result.ErrorCount++
		}
	}

	// Add warning-level checks
	result.addWarnings(s)

	return result
}

// addWarnings appends warning-level issues for non-critical findings.
func (r *ValidationResult) addWarnings(s *Stack) {
	hasNetworks := len(s.Networks) > 0

	// Warn about network field set on non-container servers in simple mode
	if !hasNetworks {
		for i, srv := range s.MCPServers {
			if srv.Network != "" && !srv.IsContainerBased() {
				r.Issues = append(r.Issues, ValidationIssue{
					Field:    fmt.Sprintf("mcp-servers[%d].network", i),
					Message:  "network field ignored for non-container servers",
					Severity: SeverityWarning,
				})
				r.WarningCount++
			}
		}
	}

	// Warn about TLS cert/key/ca files that don't exist yet (may be created before apply)
	for i, srv := range s.MCPServers {
		if srv.OpenAPI == nil || srv.OpenAPI.TLS == nil {
			continue
		}
		tlsPrefix := fmt.Sprintf("mcp-servers[%d].openapi.tls", i)
		tls := srv.OpenAPI.TLS
		for _, f := range []struct{ field, path string }{
			{tlsPrefix + ".certFile", tls.CertFile},
			{tlsPrefix + ".keyFile", tls.KeyFile},
			{tlsPrefix + ".caFile", tls.CaFile},
		} {
			if f.path == "" {
				continue
			}
			if _, err := os.Stat(f.path); err != nil {
				r.Issues = append(r.Issues, ValidationIssue{
					Field:    f.field,
					Message:  fmt.Sprintf("file not found or not readable: %s", f.path),
					Severity: SeverityWarning,
				})
				r.WarningCount++
			}
		}
	}

	// Warn about gateway without auth
	if s.Gateway != nil && s.Gateway.Auth == nil {
		r.Issues = append(r.Issues, ValidationIssue{
			Field:    "gateway.auth",
			Message:  "no authentication configured — gateway is publicly accessible",
			Severity: SeverityWarning,
		})
		r.WarningCount++
	}
}

// ExpandStackVarsWithEnv expands environment variable references in stack fields.
func ExpandStackVarsWithEnv(s *Stack) {
	expandStackVars(s, EnvResolver())
}

// ValidateStackFile loads a stack file and validates it without deploying.
// Returns the parsed stack (for further use) and the validation result.
func ValidateStackFile(path string) (*Stack, *ValidationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading stack file: %w", err)
	}

	var stack Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return nil, nil, fmt.Errorf("parsing stack YAML: %w", err)
	}

	// Expand env vars (no vault — validate-only doesn't need secrets)
	expandStackVars(&stack, EnvResolver())

	// Apply defaults
	stack.SetDefaults()

	// Validate with severity
	result := ValidateWithIssues(&stack)

	return &stack, result, nil
}
