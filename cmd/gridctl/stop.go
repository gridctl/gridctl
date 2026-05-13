package main

import (
	"fmt"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/spf13/cobra"
)

// findOrphan is a package-level seam over state.FindOrphan so tests
// can simulate orphan and ambiguous outcomes without depending on
// pgrep matching a real subprocess.
var findOrphan = state.FindOrphan

// stopForce, when true, allows runStop to terminate an orphan daemon
// discovered via the port + process scan that runs when the state
// file is missing.
var stopForce bool

// stopDefaultPort mirrors the default in serveCmd / applyCmd. Phase 3
// of #618 deliberately hardcodes the port for stop fallback; adding a
// --port flag here is out of scope.
const stopDefaultPort = 8180

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

func init() {
	stopCmd.Flags().BoolVar(&stopForce, "force", false,
		"Forcibly terminate an orphan daemon discovered via port and process scan when the state file is missing")
}

func runStop() error {
	const name = "gridctl"

	return state.WithLock(name, 5*time.Second, func() error {
		st, err := state.Load(name)
		if err != nil || st == nil {
			return runStopOrphanFallback()
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

// runStopOrphanFallback handles the missing-state-file case. If we
// can identify a single orphan daemon listening on the default port,
// either point the user at --force or terminate it (with --force).
// Anything ambiguous falls through to the legacy "nothing to stop"
// error so the user never sees us act on guesswork.
func runStopOrphanFallback() error {
	pid, ok, ferr := findOrphan(stopDefaultPort)
	if ferr != nil || !ok {
		return fmt.Errorf("no stackless daemon is running")
	}

	if !stopForce {
		return fmt.Errorf("no state file found, but a gridctl process (pid %d) is listening on :%d — its /health endpoint is responding but it has no managed state; run 'gridctl stop --force' to terminate it", pid, stopDefaultPort)
	}

	orphan := &state.DaemonState{PID: pid, Port: stopDefaultPort, StackName: "gridctl"}
	if err := state.KillDaemon(orphan); err != nil {
		return fmt.Errorf("could not stop orphan daemon (pid %d): %w", pid, err)
	}
	fmt.Printf("Stopped orphan gridctl daemon (pid %d)\n", pid)
	return nil
}
