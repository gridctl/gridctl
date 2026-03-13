package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/config"

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
		return runValidate(args[0])
	},
}

func init() {
	validateCmd.Flags().StringVar(&validateFormat, "format", "", "Output format: json for machine-readable output")
}

func runValidate(stackPath string) error {
	_, result, err := config.ValidateStackFile(stackPath)
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
