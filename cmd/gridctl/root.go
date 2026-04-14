package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var runtimeFlag string

var rootCmd = &cobra.Command{
	Use:   "gridctl",
	Short: "MCP orchestration tool",
	Long: `Gridctl is an MCP (Model Context Protocol) orchestration tool.

It allows you to define a stack of MCP servers, tools, and resources
in a simple YAML file, then spins up, wires together, and exposes
them via a single MCP gateway.`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&runtimeFlag, "runtime", "", "Container runtime to use (docker, podman). Auto-detected if not set.")

	initHelp()

	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(destroyCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(unlinkCmd)
	rootCmd.AddCommand(vaultCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(pinsCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(activateCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
}
