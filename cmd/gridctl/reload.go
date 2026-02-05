package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var reloadCmd = &cobra.Command{
	Use:   "reload [stack-name]",
	Short: "Reload configuration for a running stack",
	Long: `Triggers a hot reload of the stack configuration.

The stack must be running with the --watch flag, or you can call
this command to manually trigger a reload.

If no stack name is provided, reloads all running stacks.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return reloadStack(args[0])
		}
		return reloadAllStacks()
	},
}

func init() {
	rootCmd.AddCommand(reloadCmd)
}

func reloadStack(name string) error {
	// Try to find by stack name first
	st, err := state.Load(name)
	if err != nil {
		// Try to load as a file path
		stack, loadErr := config.LoadStack(name)
		if loadErr != nil {
			return fmt.Errorf("stack '%s' not found (not a running stack name or valid file path)", name)
		}
		// Found as file path, now look up by stack name
		st, err = state.Load(stack.Name)
		if err != nil {
			return fmt.Errorf("stack '%s' is not running", stack.Name)
		}
	}

	if !state.IsRunning(st) {
		return fmt.Errorf("stack '%s' is not running", st.StackName)
	}

	return callReloadAPI(st)
}

func reloadAllStacks() error {
	states, err := state.List()
	if err != nil {
		return fmt.Errorf("listing stacks: %w", err)
	}

	if len(states) == 0 {
		fmt.Println("No running stacks found")
		return nil
	}

	var lastErr error
	for _, st := range states {
		if !state.IsRunning(&st) {
			continue
		}

		fmt.Printf("Reloading stack '%s'...\n", st.StackName)
		if err := callReloadAPI(&st); err != nil {
			fmt.Printf("  Error: %v\n", err)
			lastErr = err
		}
	}

	return lastErr
}

func callReloadAPI(st *state.DaemonState) error {
	url := fmt.Sprintf("http://localhost:%d/api/reload", st.Port)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("calling reload API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var result reload.ReloadResult
	if err := json.Unmarshal(body, &result); err != nil {
		// Try to read as error message
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("reload failed: %s", string(body))
		}
		return fmt.Errorf("parsing response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("reload failed: %s", result.Message)
	}

	// Print results
	fmt.Printf("Stack '%s' reloaded successfully\n", st.StackName)
	if len(result.Added) > 0 {
		fmt.Printf("  Added: %v\n", result.Added)
	}
	if len(result.Removed) > 0 {
		fmt.Printf("  Removed: %v\n", result.Removed)
	}
	if len(result.Modified) > 0 {
		fmt.Printf("  Modified: %v\n", result.Modified)
	}
	if len(result.Errors) > 0 {
		fmt.Printf("  Errors: %v\n", result.Errors)
	}
	if result.Message != "" && len(result.Added)+len(result.Removed)+len(result.Modified) == 0 {
		fmt.Printf("  %s\n", result.Message)
	}

	return nil
}
