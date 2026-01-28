package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy <stack.yaml>",
	Short: "Stop gateway daemon and remove containers",
	Long: `Stops the MCP gateway daemon and removes all containers for a stack.

Requires the stack file to identify which stack to stop.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDestroy(args[0])
	},
}

func runDestroy(stackPath string) error {
	printer := output.New()

	// Load stack to get its name
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		return fmt.Errorf("failed to load stack: %w", err)
	}

	printer.Info("Stopping stack", "name", stack.Name)

	// Check for running daemon (with lock to prevent races with deploy)
	err = state.WithLock(stack.Name, 5*time.Second, func() error {
		st, loadErr := state.Load(stack.Name)
		if loadErr != nil || st == nil {
			return nil // No state file, nothing to kill
		}

		// Kill daemon process (SIGTERM, wait 5s, SIGKILL if needed)
		if state.IsRunning(st) {
			printer.Info("Stopping gateway daemon", "pid", st.PID)
			if killErr := state.KillDaemon(st); killErr != nil {
				printer.Warn("could not kill daemon", "error", killErr)
			}
		}

		// Clean up state file
		if delErr := state.Delete(stack.Name); delErr != nil {
			printer.Warn("could not delete state file", "error", delErr)
		}
		return nil
	})
	if err != nil {
		printer.Warn("could not acquire lock", "error", err)
	}

	// Stop containers
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Close()

	ctx := context.Background()
	if err := rt.Down(ctx, stack.Name); err != nil {
		return fmt.Errorf("failed to stop containers: %w", err)
	}

	printer.Info("Stack stopped", "name", stack.Name)
	return nil
}
