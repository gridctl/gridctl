package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"

	"github.com/spf13/cobra"
)

var validateFormat string

var validateCmd = &cobra.Command{
	Use:   "validate [stack.yaml]",
	Short: "Validate a stack specification without deploying",
	Long: `Validates the full Stack Spec including config schema, transport rules,
and field-level constraints without deploying any containers.

Exit codes:
  0  Valid (no errors or warnings)
  1  Validation errors found
  2  Warnings only (no errors)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if validateFormat, err = resolveFormat(validateFormat, cmd.Flags().Changed("format"), *validateJSON); err != nil {
			return err
		}
		return runValidate(cmd.Context(), args[0])
	},
}

var validateJSON *bool

func init() {
	validateCmd.Flags().StringVar(&validateFormat, "format", "", "Output format: json for machine-readable output")
	validateJSON = addJSONAlias(validateCmd)
}

func runValidate(ctx context.Context, stackPath string) error {
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

	if validateFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	} else {
		printValidationResult(stackPath, result)
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
	if result.Valid && result.WarningCount == 0 {
		fmt.Printf("✓ %s is valid\n", path)
		return
	}

	if result.Valid && result.WarningCount > 0 {
		fmt.Printf("⚠ %s is valid with %d warning(s)\n", path, result.WarningCount)
	} else {
		fmt.Printf("✗ %s has %d error(s)", path, result.ErrorCount)
		if result.WarningCount > 0 {
			fmt.Printf(" and %d warning(s)", result.WarningCount)
		}
		fmt.Println()
	}

	fmt.Println()
	for _, issue := range result.Issues {
		var prefix string
		switch issue.Severity {
		case config.SeverityError:
			prefix = "  ✗"
		case config.SeverityWarning:
			prefix = "  ⚠"
		case config.SeverityInfo:
			prefix = "  ℹ"
		}
		fmt.Printf("%s %s: %s\n", prefix, issue.Field, issue.Message)
	}
}
