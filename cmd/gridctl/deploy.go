package main

import (
	"context"

	"github.com/gridctl/gridctl/pkg/controller"

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
)

var deployCmd = &cobra.Command{
	Use:   "deploy <stack.yaml>",
	Short: "Start MCP servers defined in a stack file",
	Long: `Reads a stack YAML file and starts all defined MCP servers and resources.

Creates a Docker network, pulls/builds images as needed, and starts containers.
The MCP gateway runs as a background daemon by default.

Use --foreground (-f) to run in foreground with verbose output.`,
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
	})
	ctrl.SetVersion(version)
	ctrl.SetWebFS(WebFS)
	return ctrl.Deploy(context.Background())
}
