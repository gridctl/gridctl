package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"

	"github.com/spf13/cobra"
)

var (
	deployVerbose     bool
	deployQuiet       bool
	deployNoCache     bool
	deployPort        int
	deployBasePort    int
	deployForeground  bool
	deployDaemonChild bool
	deployNoExpand    bool
	deployWatch       bool
	deployFlash       bool
	deployCodeMode    bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy <stack.yaml>",
	Short: "Start MCP servers defined in a stack file",
	Long: `Reads a stack YAML file and starts all defined MCP servers and resources.

Creates a Docker network, pulls/builds images as needed, and starts containers.
The MCP gateway runs as a background daemon by default.

Use --foreground (-f) to run in foreground with verbose output.
Use --flash to auto-link detected LLM clients after deploy.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploy(args[0])
	},
}

func init() {
	deployCmd.Flags().BoolVarP(&deployVerbose, "verbose", "v", false, "Print full stack as JSON")
	deployCmd.Flags().BoolVarP(&deployQuiet, "quiet", "q", false, "Suppress progress output (show only final result)")
	deployCmd.Flags().BoolVar(&deployNoCache, "no-cache", false, "Force rebuild of source-based images")
	deployCmd.Flags().IntVarP(&deployPort, "port", "p", 8180, "Port for MCP gateway")
	deployCmd.Flags().IntVar(&deployBasePort, "base-port", 9000, "Base port for MCP server host port allocation")
	deployCmd.Flags().BoolVarP(&deployForeground, "foreground", "f", false, "Run in foreground (don't daemonize)")
	deployCmd.Flags().BoolVar(&deployDaemonChild, "daemon-child", false, "Internal flag for daemon process")
	_ = deployCmd.Flags().MarkHidden("daemon-child")
	deployCmd.Flags().BoolVar(&deployNoExpand, "no-expand", false, "Disable environment variable expansion in OpenAPI spec files")
	deployCmd.Flags().BoolVarP(&deployWatch, "watch", "w", false, "Watch stack file for changes and hot reload")
	deployCmd.Flags().BoolVar(&deployFlash, "flash", false, "Auto-link detected LLM clients after deploy")
	deployCmd.Flags().BoolVar(&deployCodeMode, "code-mode", false, "Enable gateway code mode (replaces tools with search + execute meta-tools)")
}

func runDeploy(stackPath string) error {
	ctrl := controller.New(controller.Config{
		StackPath:   stackPath,
		Port:        deployPort,
		BasePort:    deployBasePort,
		Verbose:     deployVerbose,
		Quiet:       deployQuiet,
		NoCache:     deployNoCache,
		NoExpand:    deployNoExpand,
		Foreground:  deployForeground,
		Watch:       deployWatch,
		DaemonChild: deployDaemonChild,
		CodeMode:    deployCodeMode,
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)

	if err := ctrl.Deploy(context.Background()); err != nil {
		return err
	}

	// Post-deploy: --flash auto-links all detected clients
	if deployFlash && !deployDaemonChild {
		flashLinkClients(deployPort)
		return nil
	}

	// Post-deploy hint: suggest `gridctl link` if no clients are linked
	if !deployQuiet && !deployFlash && !deployDaemonChild && !deployForeground {
		showLinkHint()
	}

	return nil
}

// flashLinkClients links all detected LLM clients after a successful deploy.
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
