package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skills"

	"github.com/spf13/cobra"
)

var (
	applyVerbose     bool
	applyQuiet       bool
	applyNoCache     bool
	applyPort        int
	applyBasePort    int
	applyForeground  bool
	applyDaemonChild bool
	applyNoExpand    bool
	applyWatch       bool
	applyFlash       bool
	applyCodeMode    bool
	applyLogFile     string
)

var applyCmd = &cobra.Command{
	Use:   "apply [stack.yaml]",
	Short: "Start MCP servers defined in a stack file",
	Long: `Reads a stack YAML file and starts all defined MCP servers and resources.

Creates a Docker network, pulls/builds images as needed, and starts containers.
The MCP gateway runs as a background daemon by default.

When called without a stack file, starts the API server and web UI in stackless
mode. Vault and wizard endpoints are fully functional; stack-dependent endpoints
return 503 until a stack is loaded.

Use --foreground (-f) to run in foreground with verbose output.
Use --flash to auto-link detected LLM clients after apply.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runServeStackless()
		}
		return runApply(args[0])
	},
}

func init() {
	applyCmd.Flags().BoolVarP(&applyVerbose, "verbose", "v", false, "Print full stack as JSON")
	applyCmd.Flags().BoolVarP(&applyQuiet, "quiet", "q", false, "Suppress progress output (show only final result)")
	applyCmd.Flags().BoolVar(&applyNoCache, "no-cache", false, "Force rebuild of source-based images")
	applyCmd.Flags().IntVarP(&applyPort, "port", "p", 8180, "Port for MCP gateway")
	applyCmd.Flags().IntVar(&applyBasePort, "base-port", 9000, "Base port for MCP server host port allocation")
	applyCmd.Flags().BoolVarP(&applyForeground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	applyCmd.Flags().BoolVar(&applyDaemonChild, "daemon-child", false, "Internal flag for daemon process")
	_ = applyCmd.Flags().MarkHidden("daemon-child")
	applyCmd.Flags().BoolVar(&applyNoExpand, "no-expand", false, "Disable environment variable expansion in OpenAPI spec files")
	applyCmd.Flags().BoolVarP(&applyWatch, "watch", "w", false, "Watch stack file for changes and hot reload")
	applyCmd.Flags().BoolVar(&applyFlash, "flash", false, "Auto-link detected LLM clients after apply")
	applyCmd.Flags().BoolVar(&applyCodeMode, "code-mode", false, "Enable gateway code mode (replaces tools with search + execute meta-tools) (experimental)")
	applyCmd.Flags().StringVar(&applyLogFile, "log-file", "", "Path to log file for structured JSON output with automatic rotation")
}

// runServeStackless starts the API server and web UI without a stack file.
// Vault and wizard endpoints are active; stack-dependent endpoints return 503.
func runServeStackless() error {
	ctrl := controller.New(controller.Config{
		Port:        applyPort,
		Foreground:  true,
		DaemonChild: applyDaemonChild,
		LogFile:     applyLogFile,
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)
	return ctrl.Serve(context.Background())
}

func runApply(stackPath string) error {
	ctrl := controller.New(controller.Config{
		StackPath:   stackPath,
		Port:        applyPort,
		BasePort:    applyBasePort,
		Verbose:     applyVerbose,
		Quiet:       applyQuiet,
		NoCache:     applyNoCache,
		NoExpand:    applyNoExpand,
		Foreground:  applyForeground,
		Watch:       applyWatch,
		DaemonChild: applyDaemonChild,
		CodeMode:    applyCodeMode,
		Runtime:     runtimeFlag,
		LogFile:     applyLogFile,
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)

	if err := ctrl.Deploy(context.Background()); err != nil {
		return err
	}

	// Post-apply: --flash auto-links all detected clients
	if applyFlash && !applyDaemonChild {
		flashLinkClients(applyPort)
		return nil
	}

	// Post-apply hint: suggest `gridctl link` if no clients are linked
	if !applyQuiet && !applyFlash && !applyDaemonChild && !applyForeground {
		showLinkHint()
	}

	// Show pending skill update notice (non-blocking read from cache)
	if !applyQuiet && !applyDaemonChild {
		if notice := skills.FormatUpdateNotice(); notice != "" {
			fmt.Print(notice)
		}
	}

	return nil
}

// flashLinkClients links all detected LLM clients after a successful apply.
func flashLinkClients(port int) {
	printer := output.New()
	registry := provisioner.NewRegistry()
	gatewayURL := provisioner.GatewayURL(port)

	opts := provisioner.LinkOptions{
		GatewayURL: gatewayURL,
		Port:       port,
		ServerName: "gridctl",
	}

	detected := registry.DetectAll()
	if len(detected) == 0 {
		printer.Info("No supported LLM clients detected for auto-linking")
		printer.Print("Supported clients: %s\n", strings.Join(registry.AllSlugs(), ", "))
		return
	}

	hasNpx := provisioner.NpxAvailable()
	var needsRestart []string

	for _, dc := range detected {
		if dc.Provisioner.NeedsBridge() && !hasNpx {
			printer.Warn(fmt.Sprintf("Skipped %s: 'npx' not found (mcp-remote bridge requires Node.js)", dc.Provisioner.Name()))
			continue
		}

		err := dc.Provisioner.Link(dc.ConfigPath, opts)
		if errors.Is(err, provisioner.ErrAlreadyLinked) {
			// Silently skip already-linked clients in flash mode
			continue
		}
		if errors.Is(err, provisioner.ErrConflict) {
			printer.Warn(fmt.Sprintf("Skipped %s: existing 'gridctl' entry has unexpected config (use --force to overwrite)", dc.Provisioner.Name()))
			continue
		}
		if err != nil {
			printer.Warn(fmt.Sprintf("Failed to link %s: %s", dc.Provisioner.Name(), err))
			continue
		}

		transport := provisioner.TransportDescriptionFor(dc.Provisioner)
		printer.Info(fmt.Sprintf("Linked %s (via %s)", dc.Provisioner.Name(), transport))

		if dc.Provisioner.NeedsBridge() {
			needsRestart = append(needsRestart, dc.Provisioner.Name())
		}
	}

	if len(needsRestart) > 0 {
		printer.Print("\nRestart %s to apply changes.\n", strings.Join(needsRestart, " and "))
	}
}

// showLinkHint prints a one-line hint about `gridctl link` if no clients are linked.
func showLinkHint() {
	registry := provisioner.NewRegistry()
	if !registry.IsAnyLinked("gridctl") {
		fmt.Println("\n  Tip: Run 'gridctl link' to connect your LLM client to this gateway")
	}
}
