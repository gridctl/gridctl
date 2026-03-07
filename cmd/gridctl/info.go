package main

import (
	"fmt"

	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory

	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show runtime and environment information",
	Long:  "Displays detected container runtime, socket path, version, and platform details for diagnostics.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInfo()
	},
}

func runInfo() error {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: runtimeFlag})
	if err != nil {
		fmt.Println("Runtime:  not detected")
		fmt.Printf("Error:    %v\n", err)
		return nil
	}

	fmt.Printf("Runtime:  %s\n", info.DisplayName())
	fmt.Printf("Socket:   %s\n", info.SocketPath)
	if info.Version != "" {
		fmt.Printf("Version:  %s\n", info.Version)
	}
	fmt.Printf("Host:     %s\n", info.HostAliasHostname())
	if info.SELinux {
		fmt.Println("SELinux:  enforcing")
	}
	if info.IsRootless() {
		fmt.Println("Mode:     rootless (networking limitations may apply)")
	}

	return nil
}
