package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var statusTopology string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of gateways and containers",
	Long: `Displays the current status of gridctl-managed gateways and containers.

Shows running gateways with their ports, and container states.
Use --topology to filter by a specific topology.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(statusTopology)
	},
}

func init() {
	statusCmd.Flags().StringVarP(&statusTopology, "topology", "t", "", "Only show containers from this topology")
}

func runStatus(topology string) error {
	// Show gateway status from state files
	states, err := state.List()
	if err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: could not read state files: %v\n", err)
	}

	// Filter by topology if specified
	var filteredStates []state.DaemonState
	for _, s := range states {
		if topology == "" || s.TopologyName == topology {
			filteredStates = append(filteredStates, s)
		}
	}

	// Print gateways
	if len(filteredStates) > 0 {
		fmt.Println("GATEWAYS")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPORT\tPID\tSTATUS\tSTARTED")
		for _, s := range filteredStates {
			status := "stopped"
			if state.IsRunning(&s) {
				status = "running"
			}
			started := formatDuration(time.Since(s.StartedAt))
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n",
				s.TopologyName, s.Port, s.PID, status, started)
		}
		w.Flush()
		fmt.Println()
	}

	// Show container status
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	ctx := context.Background()
	workloadStatuses, err := rt.Status(ctx, topology)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if len(workloadStatuses) == 0 && len(filteredStates) == 0 {
		fmt.Println("No managed gateways or containers found.")
		return nil
	}

	if len(workloadStatuses) > 0 {
		fmt.Println("CONTAINERS")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tIMAGE\tSTATE\tSTATUS")
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
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				id, workloadName, s.Type, s.Image, s.State, s.Message)
		}
		w.Flush()
	}

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
