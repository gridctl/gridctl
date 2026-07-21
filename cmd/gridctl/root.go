package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/output"

	"github.com/spf13/cobra"
)

var (
	runtimeFlag  string
	noColorFlag  bool
	logLevelFlag string
	// logLevel is the parsed global --log-level, consumed by subcommands
	// that configure their own slog handlers (apply, plan, serve).
	logLevel slog.Level
)

// Command group IDs for the grouped root help (see help.go).
const (
	groupStack   = "stack"
	groupCatalog = "catalog"
	groupClients = "clients"
	groupSkills  = "skills"
	groupConfig  = "config"
	groupObserve = "observability"
	groupSystem  = "system"
)

var rootCmd = &cobra.Command{
	Use:   "gridctl",
	Short: "MCP orchestration tool",
	Long: `Gridctl is an MCP (Model Context Protocol) orchestration tool.

It allows you to define a stack of MCP servers, tools, and resources
in a simple YAML file, then spins up, wires together, and exposes
them via a single MCP gateway.`,
	Example: `  gridctl apply stack.yaml     Deploy a stack of MCP servers
  gridctl link claude          Connect Claude Desktop to the gateway
  gridctl status               Show gateways and containers
  gridctl destroy stack.yaml   Stop the stack and clean up`,
	// Runtime errors are printed exactly once by Execute; usage dumps are
	// reserved for flag and argument mistakes (see SetFlagErrorFunc below).
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		output.SetNoColor(noColorFlag)
		lvl, err := parseLogLevelFlag(logLevelFlag)
		if err != nil {
			return err
		}
		logLevel = lvl
		output.SetDefaultLevel(lvl)
		// Re-level the default slog logger only when the user asked for a
		// non-default level, so the stock output format stays untouched on
		// the happy path. Logs go to stderr; --json stdout stays clean.
		if lvl != slog.LevelInfo {
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
		}
		return nil
	},
}

// parseLogLevelFlag validates the global --log-level value. Unknown values
// are rejected rather than silently defaulting to info.
func parseLogLevelFlag(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug", "info", "warn", "warning", "error":
		return logging.ParseLevel(s), nil
	}
	return 0, fmt.Errorf("invalid --log-level %q (allowed: debug, info, warn, error)\nRun 'gridctl --help' for usage", s)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&runtimeFlag, "runtime", "", "Container runtime to use (docker, podman). Auto-detected if not set.")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "Disable colored output (also honors NO_COLOR and TERM=dumb)")
	rootCmd.PersistentFlags().StringVar(&logLevelFlag, "log-level", "info", "Minimum log level: debug, info, warn, error")

	initHelp()

	rootCmd.AddGroup(
		&cobra.Group{ID: groupStack, Title: "STACK"},
		&cobra.Group{ID: groupCatalog, Title: "CATALOG"},
		&cobra.Group{ID: groupClients, Title: "CLIENTS"},
		&cobra.Group{ID: groupSkills, Title: "SKILLS"},
		&cobra.Group{ID: groupConfig, Title: "VARIABLES & PINS"},
		&cobra.Group{ID: groupObserve, Title: "OBSERVABILITY"},
		&cobra.Group{ID: groupSystem, Title: "SYSTEM"},
	)
	rootCmd.SetHelpCommandGroupID(groupSystem)
	rootCmd.SetCompletionCommandGroupID(groupSystem)

	// Flag mistakes keep a short usage pointer; runtime errors do not.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage", err, cmd.CommandPath())
	})

	for cmd, group := range map[*cobra.Command]string{
		initCmd:      groupStack,
		applyCmd:     groupStack,
		planCmd:      groupStack,
		validateCmd:  groupStack,
		reloadCmd:    groupStack,
		destroyCmd:   groupStack,
		exportCmd:    groupStack,
		statusCmd:    groupStack,
		serveCmd:     groupStack,
		stopCmd:      groupStack,
		logsCmd:      groupStack,
		searchCmd:    groupCatalog,
		addCmd:       groupCatalog,
		linkCmd:      groupClients,
		groupsCmd:    groupClients,
		unlinkCmd:    groupClients,
		importCmd:    groupClients,
		ctxCmd:       groupClients,
		skillCmd:     groupSkills,
		activateCmd:  groupSkills,
		varCmd:       groupConfig,
		vaultCmd:     groupConfig, // hidden; grouped for completeness
		pinsCmd:      groupConfig,
		authCmd:      groupConfig,
		tracesCmd:    groupObserve,
		telemetryCmd: groupObserve,
		optimizeCmd:  groupObserve,
		limitsCmd:    groupObserve,
		infoCmd:      groupSystem,
		doctorCmd:    groupSystem,
		openCmd:      groupSystem,
		versionCmd:   groupSystem,
		upgradeCmd:   groupSystem,
	} {
		cmd.GroupID = group
		rootCmd.AddCommand(cmd)
	}
}

func Execute() {
	// ExecuteC returns the command that was (or would have been) executed,
	// so help pointers name the right command path.
	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		printCLIError(os.Stderr, cmd, err)
		os.Exit(1)
	}
}

// printCLIError writes the single user-facing error line(s) for a failed
// command. Cobra's own printing is silenced globally, so this is the only
// place errors reach the terminal.
func printCLIError(w io.Writer, cmd *cobra.Command, err error) {
	prefix := "Error:"
	if output.ColorEnabled(w) {
		prefix = lipgloss.NewStyle().Foreground(output.ColorRed).Bold(true).Render(prefix)
	}
	fmt.Fprintf(w, "%s %s\n", prefix, err)

	// A stack file that does not exist is the classic first-run dead end;
	// point at the scaffold instead of leaving the user to guess.
	if isMissingStackFile(err) {
		fmt.Fprintln(w, "Run 'gridctl init' to scaffold a stack.yaml, or check the path")
	}

	// Unknown-command and argument-count mistakes lose cobra's own usage
	// output once SilenceUsage/SilenceErrors are set; restore a short help
	// pointer for them (flag mistakes carry theirs via SetFlagErrorFunc,
	// and suggestions stay embedded in the error itself).
	if !isUsageMistake(err) || strings.Contains(err.Error(), "--help' for usage") {
		return
	}
	path := "gridctl"
	if cmd != nil {
		path = cmd.CommandPath()
	}
	fmt.Fprintf(w, "Run '%s --help' for usage\n", path)
}

// isMissingStackFile reports whether err is a failed stack-file read on a
// path that does not exist (pkg/config wraps the os error, so errors.Is
// sees through the chain).
func isMissingStackFile(err error) bool {
	return errors.Is(err, fs.ErrNotExist) && strings.Contains(err.Error(), "reading stack file")
}

// isUsageMistake reports whether err is an invocation mistake (unknown
// command or wrong argument count) rather than a runtime failure. The
// prefixes match cobra's args.go error strings.
func isUsageMistake(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"accepts ",
		"requires at least",
		"requires exactly",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server and web UI without a stack",
	Long: `Starts the gridctl API server and web UI in stackless mode.

Vault and wizard endpoints are fully functional. Stack-dependent endpoints
return 503 until a stack is loaded via 'gridctl apply <stack.yaml>'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServeStackless()
	},
}

func init() {
	serveCmd.Flags().IntVarP(&applyPort, "port", "p", 8180, "Port for the API server and web UI")
	serveCmd.Flags().BoolVarP(&applyForeground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	serveCmd.Flags().BoolVar(&applyDaemonChild, "daemon-child", false, "Internal flag for daemon process")
	_ = serveCmd.Flags().MarkHidden("daemon-child")
}
