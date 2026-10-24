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
// The schema is backward compatible within 0.x: fields may be added,
// never removed or retyped without a clearly-labeled release.
type infoJSON struct {
	Runtime  string `json:"runtime"`
	Socket   string `json:"socket,omitempty"`
	Version  string `json:"version,omitempty"`
	Host     string `json:"host,omitempty"`
	SELinux  bool   `json:"selinux"`
	Rootless bool   `json:"rootless"`
	Network  string `json:"network,omitempty"`
	Error    string `json:"error,omitempty"`
	// Schemas advertises the on-disk schema versions this binary reads.
	// Additive (Article VIII). Present on both the success and
	// runtime-detection-error paths. Pin fields are strings because the
	// on-disk constants are strings.
	Schemas *infoSchemas `json:"schemas"`
}

// infoSchemas is the schemas object of `gridctl info --json`.
type infoSchemas struct {
	DaemonState int    `json:"daemon_state"`
	ProjectLock int    `json:"project_lock"`
	ImportLock  int    `json:"import_lock"`
	ToolPins    string `json:"tool_pins"`
	SkillPins   string `json:"skill_pins"`
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
	infoCmd.Flags().BoolVar(&infoJSONFlag, "json", false, "Output as JSON")
}

func runInfo(asJSON bool) error {
	schemas := advertisedInfoSchemas()
	info, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: runtimeFlag})
	if err != nil {
		if asJSON {
			return output.EncodeJSON(os.Stdout, infoJSON{Error: err.Error(), Schemas: schemas})
		}
		fmt.Println("Runtime:  not detected")
		fmt.Printf("Error:    %v\n", err)
		fmt.Println(formatInfoSchemasLine(schemas))
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
			Schemas:  schemas,
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
	fmt.Println(formatInfoSchemasLine(schemas))

	return nil
}

// advertisedInfoSchemas is the schemas object this binary advertises.
// It is filled from the package constants, not from a subprocess.
func advertisedInfoSchemas() *infoSchemas {
	s := currentSchemas()
	return &infoSchemas{
		DaemonState: s.DaemonState,
		ProjectLock: s.ProjectLock,
		ImportLock:  s.ImportLock,
		ToolPins:    s.ToolPins,
		SkillPins:   s.SkillPins,
	}
}

func formatInfoSchemasLine(s *infoSchemas) string {
	if s == nil {
		return "Schemas:  unknown"
	}
	return fmt.Sprintf("Schemas:  daemon_state=%d project_lock=%d import_lock=%d tool_pins=%s skill_pins=%s",
		s.DaemonState, s.ProjectLock, s.ImportLock, s.ToolPins, s.SkillPins)
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
