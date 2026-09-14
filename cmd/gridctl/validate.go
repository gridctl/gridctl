package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/stackpolicy"

	"github.com/spf13/cobra"
)

var validateFormat string
var validateCheckMutableRefs bool
var validatePolicy string

var validateCmd = &cobra.Command{
	Use:   "validate [stack.yaml]",
	Short: "Validate a stack specification without deploying",
	Long: `Validates the full Stack Spec including config schema, transport rules,
and field-level constraints without deploying any containers.

--check-mutable-refs is an opt-in, local, deterministic diagnostic of
literal image and package selectors. It is off by default and does not
change REST validation, health counts, apply, or stack schema semantics.

--policy evaluates captured declarations against a versioned offline
policy file without environment, secret, registry, or runtime resolution.
Flag absence preserves ordinary validation. An explicit empty, missing,
or invalid policy fails and does not fall back to ordinary validation.

Exit codes:
  0  Valid (no errors or warnings), or declared-stack policy accepted
  1  Validation errors found, or policy input/evaluation not accepted
  2  Warnings only (no errors)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if validateFormat, err = resolveFormat(validateFormat, cmd.Flags().Changed("format"), *validateJSON); err != nil {
			return err
		}
		checkMutable, err := cmd.Flags().GetBool("check-mutable-refs")
		if err != nil {
			return err
		}
		policySet := cmd.Flags().Changed("policy")
		return runValidate(cmd.Context(), args[0], checkMutable, policySet, validatePolicy)
	},
}

var validateJSON *bool

func init() {
	validateCmd.Flags().StringVar(&validateFormat, "format", "", "Output format: json for machine-readable output")
	validateCmd.Flags().BoolVar(&validateCheckMutableRefs, "check-mutable-refs", false, "Report mutable image tags and unpinned package selectors (advisory, off by default)")
	validateCmd.Flags().StringVar(&validatePolicy, "policy", "", "Evaluate captured declarations against a versioned offline policy file")
	validateJSON = addJSONAlias(validateCmd)
}

func skipHomeForPolicyValidate(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Name() != "validate" {
		return false
	}
	f := cmd.Flags().Lookup("policy")
	return f != nil && f.Changed
}

func runValidate(ctx context.Context, stackPath string, checkMutableRefs, policySet bool, policyPath string) error {
	if policySet {
		return runValidatePolicy(ctx, stackPath, policyPath)
	}
	stack, result, err := config.ValidateStackFile(stackPath)
	if err != nil {
		// File read or YAML parse error — not a validation issue
		if validateFormat == "json" {
			out := map[string]any{
				"valid": false,
				"error": err.Error(),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(out)
		}
		return err
	}

	// Name every active skill the skills: policy would hide. This needs the
	// local registry, which pkg/config deliberately does not read; the cmd
	// layer joins the two so validate and apply warn identically.
	if stack != nil && stack.Skills != nil {
		for _, w := range controller.DeniedActiveSkillWarnings(stack) {
			result.Issues = append(result.Issues, config.ValidationIssue{
				Field:    "skills",
				Message:  w,
				Severity: config.SeverityWarning,
			})
			result.WarningCount++
		}
	}

	// Content-level model preference findings need the registry too;
	// same cmd-layer join, all advisory.
	if stack != nil && stack.ModelPreferences != nil {
		for _, w := range controller.ModelPreferenceWarnings(stack) {
			result.Issues = append(result.Issues, config.ValidationIssue{
				Field:    "model_preferences",
				Message:  w,
				Severity: config.SeverityWarning,
			})
			result.WarningCount++
		}
	}
	indexed, err := config.ParseStackIndex(ctx, stackPath)
	if err != nil {
		return fmt.Errorf("indexing stack declarations: %w", err)
	}
	appendDeclarationValidationIssues(result, declarationDiagnostics(indexed))
	if checkMutableRefs {
		config.AppendMutableRefIssues(result, config.DiagnoseMutableRefs(indexed))
	}

	if validateFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	} else {
		printValidationResultMode(os.Stdout, stackPath, result, checkMutableRefs)
	}

	// Exit codes: 0=valid, 1=errors, 2=warnings only
	if result.ErrorCount > 0 {
		os.Exit(1)
	}
	if result.WarningCount > 0 {
		os.Exit(2)
	}

	return nil
}

func runValidatePolicy(ctx context.Context, stackPath, policyPath string) error {
	if policyPath == "" {
		report := stackpolicy.Evaluate(ctx, stackPath, "")
		return writePolicyReport(report)
	}
	report := stackpolicy.Evaluate(ctx, stackPath, policyPath)
	return writePolicyReport(report)
}

func writePolicyReport(report *stackpolicy.Report) error {
	var buf bytes.Buffer
	var err error
	if validateFormat == "json" {
		err = stackpolicy.FormatJSON(&buf, report)
	} else {
		err = stackpolicy.FormatText(&buf, report)
	}
	if err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, &buf); err != nil {
		return err
	}
	if code := stackpolicy.ExitCode(report); code != 0 {
		os.Exit(code)
	}
	return nil
}

func declarationDiagnostics(stack *config.Stack) []config.DeclarationDiagnostic {
	if stack == nil || len(stack.Variables) == 0 {
		return nil
	}
	metadata := map[string]config.VariableMetadata{}
	locked := true
	if store, err := loadVault(); err == nil && !store.IsLocked() {
		locked = false
		for _, variable := range store.List() {
			metadata[variable.Key] = config.VariableMetadata{Type: string(variable.Type), Secret: variable.IsSecret, Deprecated: variable.Deprecated}
		}
		for key, declaration := range stack.Variables {
			if _, exists := metadata[key]; exists {
				continue
			}
			if _, present := os.LookupEnv(key); present {
				metadata[key] = config.VariableMetadata{Type: declaration.ValueType(), Secret: declaration.IsSecret()}
			}
		}
	}
	return config.DiagnoseDeclarations(stack, metadata, locked)
}

func appendDeclarationValidationIssues(result *config.ValidationResult, diagnostics []config.DeclarationDiagnostic) {
	for _, diagnostic := range diagnostics {
		result.Issues = append(result.Issues, config.ValidationIssue{
			Field:    "variables." + diagnostic.Key,
			Message:  diagnostic.Message,
			Severity: config.SeverityWarning,
		})
		result.WarningCount++
	}
}

func printValidationResult(path string, result *config.ValidationResult) {
	printValidationResultMode(os.Stdout, path, result, false)
}

func printValidationResultMode(w io.Writer, path string, result *config.ValidationResult, showInfoWithoutWarnings bool) {
	okMark, warnMark, errMark, infoMark := "✓", "⚠", "✗", "ℹ"
	if showInfoWithoutWarnings && os.Getenv("NO_COLOR") != "" {
		okMark, warnMark, errMark, infoMark = "OK", "warning", "error", "info"
	}

	switch {
	case result.Valid && result.WarningCount == 0:
		fmt.Fprintf(w, "%s %s is valid\n", okMark, path)
		if !showInfoWithoutWarnings {
			return
		}
	case result.Valid:
		fmt.Fprintf(w, "%s %s is valid with %d warning(s)\n", warnMark, path, result.WarningCount)
	default:
		fmt.Fprintf(w, "%s %s has %d error(s)", errMark, path, result.ErrorCount)
		if result.WarningCount > 0 {
			fmt.Fprintf(w, " and %d warning(s)", result.WarningCount)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w)
	infoOnly := showInfoWithoutWarnings && result.Valid && result.WarningCount == 0
	for _, issue := range result.Issues {
		if infoOnly && issue.Severity != config.SeverityInfo {
			continue
		}
		mark := ""
		switch issue.Severity {
		case config.SeverityError:
			mark = errMark
		case config.SeverityWarning:
			mark = warnMark
		case config.SeverityInfo:
			mark = infoMark
		}
		fmt.Fprintf(w, "  %s %s: %s\n", mark, issue.Field, issue.Message)
	}
	if showInfoWithoutWarnings {
		fmt.Fprintf(w, "  %s coverage: %s\n", infoMark, config.CoverageLimitationMessage())
	}
}
