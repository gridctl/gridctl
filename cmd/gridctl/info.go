package main

import (
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory

	"github.com/spf13/cobra"
)

var infoJSONFlag bool

// infoJSON is the machine-readable shape of `gridctl info --json`.
// The schema is experimental until 1.0.
type infoJSON struct {
	Runtime  string `json:"runtime"`
	Socket   string `json:"socket,omitempty"`
	Version  string `json:"version,omitempty"`
	Host     string `json:"host,omitempty"`
	SELinux  bool   `json:"selinux"`
	Rootless bool   `json:"rootless"`
	Network  string `json:"network,omitempty"`
	Error    string `json:"error,omitempty"`
}

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show runtime and environment information",
	Long: `Displays detected container runtime, socket path, version, and platform
details for diagnostics.

Info reports facts and always exits 0. For pass/fail judgments with
remediation hints, run 'gridctl doctor'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInfo(infoJSONFlag)
	},
}

func init() {
	infoCmd.Flags().BoolVar(&infoJSONFlag, "json", false, "Output as JSON (experimental schema)")
}

func runInfo(asJSON bool) error {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: runtimeFlag})
	if err != nil {
		if asJSON {
			return output.EncodeJSON(os.Stdout, infoJSON{Error: err.Error()})
		}
		fmt.Println("Runtime:  not detected")
		fmt.Printf("Error:    %v\n", err)
		return nil
	}

	if asJSON {
		doc := infoJSON{
			Runtime:  info.DisplayName(),
			Socket:   info.SocketPath,
			Version:  info.Version,
			Host:     info.HostAliasHostname(),
			SELinux:  info.SELinux,
			Rootless: info.IsRootless(),
		}
		if info.IsRootless() {
			doc.Network = rootlessNetworkStack(info)
		}
		return output.EncodeJSON(os.Stdout, doc)
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
		fmt.Printf("Mode:     rootless\n")
		fmt.Printf("Network:  %s\n", rootlessNetworkStack(info))
	}

	return nil
}

// rootlessNetworkStack describes the rootless Podman networking stack.
func rootlessNetworkStack(info *runtime.RuntimeInfo) string {
	switch {
	case info.HasNetavark && info.HasAardvarkDNS:
		return "netavark + aardvark-dns"
	case info.HasNetavark:
		return "netavark (aardvark-dns missing)"
	default:
		return "netavark not found"
	}
}
