package main

import (
	"fmt"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the stackless gridctl daemon",
	Long: `Stops a daemon started with 'gridctl serve'.

For stacks started with 'gridctl apply', use 'gridctl destroy <stack.yaml>' instead.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStop()
	},
}

func runStop() error {
	const name = "gridctl"

	return state.WithLock(name, 5*time.Second, func() error {
		st, err := state.Load(name)
		if err != nil || st == nil {
			return fmt.Errorf("no stackless daemon is running")
		}

		if !state.IsRunning(st) {
			_ = state.Delete(name)
			return fmt.Errorf("no stackless daemon is running")
		}

		fmt.Printf("Stopping gridctl daemon (pid %d)...\n", st.PID)
		if err := state.KillDaemon(st); err != nil {
			return fmt.Errorf("could not stop daemon: %w", err)
		}

		_ = state.Delete(name)
		fmt.Println("gridctl stopped")
		return nil
	})
}
