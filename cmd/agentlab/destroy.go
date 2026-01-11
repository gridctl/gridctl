package main

import (
	"context"
	"fmt"
	"time"

	"agentlab/pkg/config"
	"agentlab/pkg/runtime"
	_ "agentlab/pkg/runtime/docker" // Register DockerRuntime factory
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

	// Check for running daemon (with lock to prevent races with deploy)
	err = state.WithLock(topo.Name, 5*time.Second, func() error {
		st, loadErr := state.Load(topo.Name)
		if loadErr != nil || st == nil {
			return nil // No state file, nothing to kill
		}

		// Kill daemon process (SIGTERM, wait 5s, SIGKILL if needed)
		if state.IsRunning(st) {
			fmt.Printf("Stopping gateway daemon (PID: %d)...\n", st.PID)
			if killErr := state.KillDaemon(st); killErr != nil {
				fmt.Printf("  Warning: could not kill daemon: %v\n", killErr)
			}
		}

		// Clean up state file
		if delErr := state.Delete(topo.Name); delErr != nil {
			fmt.Printf("  Warning: could not delete state file: %v\n", delErr)
		}
		return nil
	})
	if err != nil {
		fmt.Printf("  Warning: could not acquire lock: %v\n", err)
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
