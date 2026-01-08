package main

import (
	"context"
	"fmt"
	"time"

	"agentlab/pkg/config"
	"agentlab/pkg/runtime"
	"agentlab/pkg/state"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy <topology.yaml>",
	Short: "Stop gateway daemon and remove containers",
	Long: `Stops the MCP gateway daemon and removes all containers for a topology.

Requires the topology file to identify which topology to stop.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDestroy(args[0])
	},
}

func runDestroy(topologyPath string) error {
	// Load topology to get its name
	topo, err := config.LoadTopology(topologyPath)
	if err != nil {
		return fmt.Errorf("failed to load topology: %w", err)
	}

	fmt.Printf("Stopping topology '%s'...\n", topo.Name)

	// Check for running daemon
	st, err := state.Load(topo.Name)
	if err == nil && st != nil {
		// Kill daemon process
		if state.IsRunning(st) {
			fmt.Printf("Stopping gateway daemon (PID: %d)...\n", st.PID)
			if err := state.KillDaemon(st); err != nil {
				fmt.Printf("  Warning: could not kill daemon: %v\n", err)
			} else {
				// Give daemon time to shut down gracefully
				time.Sleep(500 * time.Millisecond)
			}
		}

		// Clean up state file
		if err := state.Delete(topo.Name); err != nil {
			fmt.Printf("  Warning: could not delete state file: %v\n", err)
		}
	}

	// Stop containers
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	ctx := context.Background()
	if err := rt.Down(ctx, topo.Name); err != nil {
		return fmt.Errorf("failed to stop containers: %w", err)
	}

	fmt.Printf("Topology '%s' stopped\n", topo.Name)
	return nil
}
