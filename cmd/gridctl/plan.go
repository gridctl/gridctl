package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/output"
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
		var err error
		if planFormat, err = resolveFormat(planFormat, cmd.Flags().Changed("format"), *planJSON); err != nil {
			return err
		}
		return runPlan(args[0])
	},
}

var planJSON *bool

func init() {
	planCmd.Flags().BoolVarP(&planAutoApprove, "yes", "y", false, "Auto-approve and apply changes")
	planCmd.Flags().BoolVar(&planAutoApproveCI, "auto-approve", false, "Auto-approve and apply changes (CI/CD equivalent of -y)")
	planCmd.Flags().StringVar(&planFormat, "format", "", "Output format: json for machine-readable output")
	planJSON = addJSONAlias(planCmd)
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
		return output.EncodeJSON(os.Stdout, diff)
	}

	printPlanDiff(os.Stdout, diff)

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

// printPlanDiff renders the human plan view. Symbols mirror the Terraform
// convention: + add (green), - destroy (red), ~ update (amber). Colors
// follow the color contract, so piped output stays plain.
func printPlanDiff(w io.Writer, diff *config.PlanDiff) {
	if !diff.HasChanges {
		fmt.Fprintln(w, "No changes. Stack is up to date.")
		return
	}

	color := output.ColorEnabled(w)
	header := fmt.Sprintf("Plan: %s", diff.Summary)
	if color {
		header = lipgloss.NewStyle().Foreground(output.ColorAmber).Bold(true).Render(header)
	}
	fmt.Fprintf(w, "%s\n\n", header)

	muted := lipgloss.NewStyle().Foreground(output.ColorMuted)
	for _, item := range diff.Items {
		var symbol, label string
		var symbolColor lipgloss.Color
		switch item.Action {
		case config.DiffAdd:
			symbol, label, symbolColor = "+", "add", output.ColorGreen
		case config.DiffRemove:
			symbol, label, symbolColor = "-", "destroy", output.ColorRed
		case config.DiffChange:
			symbol, label, symbolColor = "~", "update", output.ColorAmber
		}

		line := fmt.Sprintf("%s %s %q (%s)", symbol, item.Kind, item.Name, label)
		if color {
			line = lipgloss.NewStyle().Foreground(symbolColor).Render(line)
		}
		fmt.Fprintf(w, "  %s\n", line)
		for _, detail := range item.Details {
			if color {
				detail = muted.Render(detail)
			}
			fmt.Fprintf(w, "      %s\n", detail)
		}
	}
}
