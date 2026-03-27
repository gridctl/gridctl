package main

import (
	"fmt"
	"os"

	"github.com/gridctl/gridctl/internal/server"

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
	rootCmd.PersistentFlags().StringVar(&runtimeFlag, "runtime", "", "Container runtime to use (docker, podman) (experimental for podman). Auto-detected if not set.")

	initHelp()

	rootCmd.AddCommand(deployCmd)
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
	Short: "Start the web UI server",
	Long:  "Starts the Gridctl web UI server without managing any stack.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServe()
	},
}

func runServe() error {
	addr := ":8180"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	webFS, err := WebFS()
	if err != nil {
		return fmt.Errorf("failed to load embedded web files: %w", err)
	}

	srv := server.New(addr, webFS)
	fmt.Printf("Gridctl UI starting on http://localhost%s\n", addr)
	return srv.ListenAndServe()
}
