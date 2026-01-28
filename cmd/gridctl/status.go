package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var statusStack string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of gateways and containers",
	Long: `Displays the current status of gridctl-managed gateways and containers.

Shows running gateways with their ports, and container states.
Use --stack to filter by a specific stack.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(statusStack)
	},
}

func init() {
	statusCmd.Flags().StringVarP(&statusStack, "stack", "s", "", "Only show containers from this stack")
}

func runStatus(stack string) error {
	printer := output.New()

	// Show gateway status from state files
	states, err := state.List()
	if err != nil && !os.IsNotExist(err) {
		printer.Warn("could not read state files", "error", err)
	}

	// Filter by stack if specified
	var filteredStates []state.DaemonState
	for _, s := range states {
		if stack == "" || s.StackName == stack {
			filteredStates = append(filteredStates, s)
		}
	}

	// Build gateway summaries
	var gateways []output.GatewaySummary
	for _, s := range filteredStates {
		status := "stopped"
		if state.IsRunning(&s) {
			status = "running"
		}
		gateways = append(gateways, output.GatewaySummary{
			Name:    s.StackName,
			Port:    s.Port,
			PID:     s.PID,
			Status:  status,
			Started: formatDuration(time.Since(s.StartedAt)),
		})
	}

	// Show container status
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	ctx := context.Background()
	workloadStatuses, err := rt.Status(ctx, stack)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if len(workloadStatuses) == 0 && len(gateways) == 0 {
		printer.Info("No managed gateways or containers found")
		return nil
	}

	// Build container summaries
	var containers []output.ContainerSummary
	for _, s := range workloadStatuses {
		// Get workload name from labels
		var workloadName string
		if s.Labels != nil {
			if name, ok := s.Labels[runtime.LabelMCPServer]; ok {
				workloadName = name
			} else if name, ok := s.Labels[runtime.LabelResource]; ok {
				workloadName = name
			} else if name, ok := s.Labels[runtime.LabelAgent]; ok {
				workloadName = name
			}
		}
		// Truncate ID for display
		id := string(s.ID)
		if len(id) > 12 {
			id = id[:12]
		}
		containers = append(containers, output.ContainerSummary{
			ID:      id,
			Name:    workloadName,
			Type:    string(s.Type),
			Image:   s.Image,
			State:   string(s.State),
			Message: s.Message,
		})
	}

	// Print tables
	printer.Gateways(gateways)
	printer.Containers(containers)

	return nil
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}
