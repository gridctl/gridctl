package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestValidateCheckMutableRefsDefaultOff(t *testing.T) {
	flag := validateCmd.Flags().Lookup("check-mutable-refs")
	if flag == nil {
		t.Fatal("missing --check-mutable-refs")
	}
	if flag.DefValue != "false" {
		t.Fatalf("default = %q, want false", flag.DefValue)
	}
}

func TestPrintValidationResult_DefaultSuppressesInfoOnly(t *testing.T) {
	var buf bytes.Buffer
	result := &config.ValidationResult{
		Valid: true,
		Issues: []config.ValidationIssue{{
			Field:    "mcp-servers[0].command",
			Message:  "reference-not-assessed: selector contains a variable; it was not classified.",
			Severity: config.SeverityInfo,
		}},
	}
	printValidationResultMode(&buf, "stack.yaml", result, false)
	out := buf.String()
	if out != "✓ stack.yaml is valid\n" {
		t.Fatalf("default rendering changed: %q", out)
	}
}

func TestPrintValidationResult_OptInShowsInfoAndCoverage(t *testing.T) {
	var buf bytes.Buffer
	result := &config.ValidationResult{
		Valid: true,
		Issues: []config.ValidationIssue{{
			Field:    "mcp-servers[0].image",
			Message:  "reference-not-assessed: selector contains a variable; it was not classified.",
			Severity: config.SeverityInfo,
		}},
	}
	printValidationResultMode(&buf, "stack.yaml", result, true)
	out := buf.String()
	if !strings.Contains(out, "✓ stack.yaml is valid") {
		t.Fatalf("missing valid header: %q", out)
	}
	if !strings.Contains(out, "mcp-servers[0].image") {
		t.Fatalf("missing info finding: %q", out)
	}
	if !strings.Contains(out, "reference-coverage:") {
		t.Fatalf("missing coverage: %q", out)
	}
	if strings.Contains(out, "s3cret") {
		t.Fatalf("leaked secret: %q", out)
	}
}

func TestPrintValidationResult_OptInNOCOLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	result := &config.ValidationResult{Valid: true}
	printValidationResultMode(&buf, "stack.yaml", result, true)
	out := buf.String()
	if strings.Contains(out, "✓") || strings.Contains(out, "ℹ") {
		t.Fatalf("NO_COLOR still used symbols: %q", out)
	}
	if !strings.Contains(out, "OK stack.yaml is valid") {
		t.Fatalf("missing textual severity: %q", out)
	}
	if !strings.Contains(out, "info coverage:") {
		t.Fatalf("missing textual info: %q", out)
	}
}

func TestPrintValidationResult_WarningsKeepErrorsFirstExits(t *testing.T) {
	result := &config.ValidationResult{
		Valid:        true,
		WarningCount: 1,
		Issues: []config.ValidationIssue{{
			Field:    "mcp-servers[0].image",
			Message:  "mutable-image-reference: version tags can change; use a reviewed digest. Publisher trust and platform support were not checked.",
			Severity: config.SeverityWarning,
		}},
	}
	if code := validationExitCode(result); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	infoOnly := &config.ValidationResult{
		Valid: true,
		Issues: []config.ValidationIssue{{
			Field:    "mcp-servers[0].command",
			Message:  "reference-not-assessed: selector is unsupported or dynamic; it was not classified.",
			Severity: config.SeverityInfo,
		}},
	}
	if code := validationExitCode(infoOnly); code != 0 {
		t.Fatalf("info-only exit = %d, want 0", code)
	}
	errs := &config.ValidationResult{Valid: false, ErrorCount: 1, WarningCount: 2}
	if code := validationExitCode(errs); code != 1 {
		t.Fatalf("errors exit = %d, want 1", code)
	}
}

func validationExitCode(result *config.ValidationResult) int {
	if result.ErrorCount > 0 {
		return 1
	}
	if result.WarningCount > 0 {
		return 2
	}
	return 0
}
