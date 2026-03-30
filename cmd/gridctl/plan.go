package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	planAutoApprove   bool
	planAutoApproveCI bool
	planFormat        string
)

var planCmd = &cobra.Command{
	Use:   "plan [stack.yaml]",
	Short: "Compare stack spec against running state",
	Long: `Loads the stack specification and compares it against the currently
running deployment. Shows a structured diff of what would change:
added, removed, and modified servers, agents, and resources.

Use -y or --auto-approve to auto-approve and apply changes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlan(args[0])
	},
}

func init() {
	planCmd.Flags().BoolVarP(&planAutoApprove, "yes", "y", false, "Auto-approve and apply changes")
	planCmd.Flags().BoolVar(&planAutoApproveCI, "auto-approve", false, "Auto-approve and apply changes (CI/CD equivalent of -y)")
	planCmd.Flags().StringVar(&planFormat, "format", "", "Output format: json for machine-readable output")
}

func runPlan(stackPath string) error {
	// Load and validate the proposed spec
	proposed, result, err := config.ValidateStackFile(stackPath)
	if err != nil {
		return fmt.Errorf("loading proposed spec: %w", err)
	}
	if result.ErrorCount > 0 {
		printValidationResult(stackPath, result)
		return fmt.Errorf("proposed spec has %d validation error(s)", result.ErrorCount)
	}

	// Find the running stack's state
	current, err := loadCurrentStack(proposed.Name)
	if err != nil {
		return err
	}

	// Compute the diff
	diff := config.ComputePlan(proposed, current)

	if planFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(diff)
	}

	printPlanDiff(diff)

	if !diff.HasChanges {
		return nil
	}

	// Confirm or auto-approve
	if !planAutoApprove && !planAutoApproveCI {
		fmt.Print("\nApply these changes? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Apply with Replace to handle running stacks
	fmt.Println("\nApplying changes...")
	ctrl := controller.New(controller.Config{
		StackPath:  stackPath,
		Port:       applyPort,
		BasePort:   applyBasePort,
		Foreground: applyForeground,
		Runtime:    runtimeFlag,
		Replace:    true,
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)

	return ctrl.Deploy(context.Background())
}

// loadCurrentStack finds and loads the currently running stack config.
func loadCurrentStack(stackName string) (*config.Stack, error) {
	st, err := state.Load(stackName)
	if err != nil {
		if os.IsNotExist(err) {
			// No running stack — everything is an add
			return &config.Stack{Name: stackName}, nil
		}
		return nil, fmt.Errorf("loading state for %q: %w", stackName, err)
	}

	if !state.IsRunning(st) {
		// Stale state — treat as no running stack
		return &config.Stack{Name: stackName}, nil
	}

	// Load the running stack's config
	current, _, parseErr := config.ValidateStackFile(st.StackFile)
	if parseErr != nil {
		return nil, fmt.Errorf("loading running stack config from %s: %w", st.StackFile, parseErr)
	}

	return current, nil
}

func printPlanDiff(diff *config.PlanDiff) {
	if !diff.HasChanges {
		fmt.Println("No changes. Stack is up to date.")
		return
	}

	fmt.Printf("Plan: %s\n\n", diff.Summary)

	for _, item := range diff.Items {
		var symbol, label string
		switch item.Action {
		case config.DiffAdd:
			symbol = "+"
			label = "add"
		case config.DiffRemove:
			symbol = "-"
			label = "destroy"
		case config.DiffChange:
			symbol = "~"
			label = "update"
		}

		fmt.Printf("  %s %s %q (%s)\n", symbol, item.Kind, item.Name, label)
		for _, detail := range item.Details {
			fmt.Printf("      %s\n", detail)
		}
	}
}
