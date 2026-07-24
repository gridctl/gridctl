package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

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
When called without a stack file, starts the API server and web UI in
stackless mode instead (the same as 'gridctl serve'); a notice is printed
so the fallback is never silent.

Creates a Docker network, pulls/builds images as needed, and starts containers.
The MCP gateway runs as a background daemon by default.

In stackless mode, vault and wizard endpoints are fully functional;
stack-dependent endpoints return 503 until a stack is loaded.

Use --foreground (-f) to run in foreground with verbose output.
Use --flash to auto-link detected LLM clients after apply.

A link: block in the stack file declares clients to connect; apply
reconciles it once the gateway is healthy (--flash is then ignored).`,
	Example: `  gridctl apply stack.yaml             Deploy a stack as a background daemon
  gridctl apply stack.yaml -f          Run in foreground (ctrl-C to stop)
  gridctl apply stack.yaml --watch     Hot reload on stack file changes
  gridctl apply                        Start the API and web UI without a stack`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			if !applyDaemonChild {
				fmt.Fprintln(os.Stderr, "No stack file given; starting stackless API/UI (same as 'gridctl serve').")
			}
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
		Foreground:  applyForeground,
		DaemonChild: applyDaemonChild,
		LogFile:     applyLogFile,
		LogLevel:    logLevel,
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)

	// Cancel ctx on SIGINT/SIGTERM so all ctx-bound goroutines in the gateway
	// (health monitor, autoscaler, file watcher) actually exit; without this
	// the daemon child receives the signal but never exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return ctrl.Serve(ctx)
}

func runApply(stackPath string) error {
	cfg := controller.Config{
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
		LogLevel:    logLevel,
	}

	// Foreground blocks inside Deploy until shutdown, so post-Deploy code
	// never runs; client linking (declared link: entries and --flash) fires
	// via the gateway's post-ready callback instead. The daemon child never
	// links: its parent does, after the health-wait.
	declared := loadDeclaredLinks(stackPath)
	if applyForeground && !applyDaemonChild && (len(declared) > 0 || applyFlash) {
		cfg.OnReady = func(port int) {
			printer := output.New()
			if len(declared) > 0 {
				if applyFlash && !applyQuiet {
					printer.Info("Stack declares link:, --flash ignored")
				}
				reconcileDeclaredLinks(printer, provisioner.NewRegistry(), declared, port, applyQuiet)
				return
			}
			flashLinkClients(port)
		}
	}

	ctrl := controller.New(cfg)
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)

	// Cancel ctx on SIGINT/SIGTERM so daemon goroutines exit cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := ctrl.Deploy(ctx); err != nil {
		return err
	}

	// Post-apply: point at servers waiting on OAuth authorization. Always a
	// printed hint (never an auto-opened browser); interactive login stays
	// an explicit 'gridctl auth login' away.
	if !applyQuiet && !applyDaemonChild && !applyForeground {
		printAuthHints(applyPort, os.Stdout)
	}

	// Post-apply client linking (daemon parent only; foreground handled the
	// same work in the OnReady callback above). A stack with a link: block
	// owns the linking decision, so --flash is ignored with a notice rather
	// than double-writing under a different entry name.
	if !applyDaemonChild && !applyForeground {
		switch {
		case len(declared) > 0:
			printer := output.New()
			if applyFlash && !applyQuiet {
				printer.Info("Stack declares link:, --flash ignored")
			}
			reconcileDeclaredLinks(printer, provisioner.NewRegistry(), declared, applyPort, applyQuiet)
		case applyFlash:
			flashLinkClients(applyPort)
			return nil
		default:
			// Post-apply hint: suggest `gridctl link` if no clients are linked
			if !applyQuiet {
				showLinkHint()
			}
		}
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
		output.New().Hint("Tip: run 'gridctl link' to connect your LLM client, or 'gridctl open' for the web UI")
	}
}
