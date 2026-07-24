package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var destroyUnlink bool

var destroyCmd = &cobra.Command{
	Use:   "destroy <stack.yaml|stack-name>",
	Short: "Stop gateway daemon and remove containers",
	Long: `Stops the MCP gateway daemon and removes all containers for a stack.

Accepts either the stack YAML file or the stack name shown by
'gridctl status', so a moved or renamed file never blocks a teardown.

By default client configs are left untouched (linked clients simply point
at a stopped gateway). With --unlink, the entries declared in the stack's
link: block are also removed from their client configs.`,
	Example: `  gridctl destroy stack.yaml            Destroy by file
  gridctl destroy mystack               Destroy by stack name (see 'gridctl status')
  gridctl destroy stack.yaml --unlink   Also unlink clients declared in link:`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDestroy(args[0])
	},
}

func init() {
	destroyCmd.Flags().BoolVar(&destroyUnlink, "unlink", false, "Also remove the stack's declared link: entries from client configs")
}

// resolveDestroyTarget resolves the destroy argument to a stack name and,
// when available, the loaded spec. A readable stack file takes precedence
// (the original behavior); anything else falls back to a stack name from
// state, so an unrelated file or directory with the same name never blocks
// a by-name teardown. When resolved by name, the spec is loaded best-effort
// from the recorded stack file so container-runtime warnings keep their
// gating.
func resolveDestroyTarget(arg string) (string, *config.Stack, error) {
	var fileErr error
	if info, err := os.Stat(arg); err == nil && info.Mode().IsRegular() {
		stack, loadErr := config.LoadStack(arg)
		if loadErr == nil {
			return stack.Name, stack, nil
		}
		fileErr = loadErr
	}

	st, err := state.Load(arg)
	if err != nil {
		if fileErr != nil {
			return "", nil, fmt.Errorf("failed to load stack: %w", fileErr)
		}
		states, _ := state.List()
		var names []string
		for _, s := range states {
			names = append(names, s.StackName)
		}
		if len(names) > 0 {
			return "", nil, fmt.Errorf("stack %q not found; known stacks: %s", arg, strings.Join(names, ", "))
		}
		return "", nil, fmt.Errorf("stack %q not found (not a stack file or a known stack name)", arg)
	}

	// Best-effort spec load; the recorded file may have moved since apply.
	if stack, loadErr := config.LoadStack(st.StackFile); loadErr == nil {
		return st.StackName, stack, nil
	}
	return st.StackName, nil, nil
}

func runDestroy(target string) error {
	printer := output.New()

	name, stack, err := resolveDestroyTarget(target)
	if err != nil {
		return err
	}

	printer.Info("Stopping stack", "name", name)

	// Check for running daemon (with lock to prevent races with deploy)
	err = state.WithLock(name, 5*time.Second, func() error {
		st, loadErr := state.Load(name)
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
		if delErr := state.Delete(name); delErr != nil {
			printer.Warn("could not delete state file", "error", delErr)
		}
		return nil
	})
	if err != nil {
		printer.Warn("could not acquire lock", "error", err)
	}

	// Without a loaded spec (destroy by name with a moved stack file) we
	// cannot tell whether the stack needed a container runtime; warn anyway.
	needsRuntime := stack == nil || stack.NeedsContainerRuntime()

	// Stop containers (best-effort when Docker is unavailable)
	rt, err := runtime.New()
	if err != nil {
		if needsRuntime {
			printer.Warn("could not initialize runtime — container cleanup skipped", "error", err)
		}
	} else {
		defer rt.Close()
		ctx := context.Background()
		if err := rt.Down(ctx, name); err != nil {
			if needsRuntime {
				printer.Warn("container runtime unavailable — could not remove containers", "error", err)
			}
		}
	}

	if destroyUnlink {
		unlinkDeclaredClients(printer, provisioner.NewRegistry(), stack)
	}

	printer.Info("Stack stopped", "name", name)
	printer.Hint("Verify with 'gridctl status'")
	return nil
}

// unlinkDeclaredClients removes the destroyed stack's declared link:
// entries from their client configs. Unlink resolves the same entry names
// reconcile writes (explicit name, gridctl-<group>, or gridctl); entries
// that are not linked are silent no-ops. Without a loadable spec (destroy
// by name after the stack file moved) the declared set is unknown, so we
// warn and skip rather than guess.
func unlinkDeclaredClients(printer *output.Printer, registry slugResolver, stack *config.Stack) {
	if stack == nil {
		printer.Warn("--unlink skipped: stack file not loadable, declared clients unknown (use 'gridctl unlink')")
		return
	}
	if len(stack.Link) == 0 {
		printer.Info("--unlink: stack declares no link: entries, nothing to remove")
		return
	}

	for _, entry := range stack.Link {
		prov, ok := registry.FindBySlug(entry.Client)
		if !ok {
			printer.Warn(fmt.Sprintf("Skipped %s: unknown client", entry.Client))
			continue
		}
		configPath, found := prov.Detect()
		if !found {
			continue // Nothing on this machine to unlink
		}
		serverName := entry.EffectiveName()
		err := prov.Unlink(configPath, serverName)
		switch {
		case errors.Is(err, provisioner.ErrNotLinked):
			// Declared but never linked here; nothing to do.
		case err != nil:
			printer.Warn(fmt.Sprintf("Failed to unlink %s: %s", prov.Name(), err))
		default:
			printer.Info(fmt.Sprintf("Unlinked %s (%s)", prov.Name(), serverName))
		}
	}
}
