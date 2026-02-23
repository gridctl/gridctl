package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	linkPort   int
	linkAll    bool
	linkName   string
	linkDryRun bool
	linkForce  bool
)

var linkCmd = &cobra.Command{
	Use:   "link [client]",
	Short: "Connect an LLM client to the gridctl gateway",
	Long: `Injects the gridctl gateway into an LLM client's MCP configuration.

Without arguments, detects installed LLM clients and presents a selection list.
With a client name, links that specific client directly.

Supported clients: claude, claude-code, cursor, windsurf, vscode, gemini, opencode, continue, cline, anythingllm, roo, zed, goose`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var client string
		if len(args) > 0 {
			client = args[0]
		}
		return runLink(client)
	},
}

func init() {
	linkCmd.Flags().IntVarP(&linkPort, "port", "p", 0, "Gateway port (default: auto-detect from running stack, else 8180)")
	linkCmd.Flags().BoolVarP(&linkAll, "all", "a", false, "Link all detected clients at once")
	linkCmd.Flags().StringVarP(&linkName, "name", "n", "gridctl", "Server name in client config")
	linkCmd.Flags().BoolVar(&linkDryRun, "dry-run", false, "Show what would change without modifying files")
	linkCmd.Flags().BoolVar(&linkForce, "force", false, "Overwrite existing gridctl entry even if present")
}

func runLink(client string) error {
	printer := output.New()
	registry := provisioner.NewRegistry()

	port := resolveGatewayPort(linkPort)
	gatewayURL := provisioner.GatewayURL(port)

	opts := provisioner.LinkOptions{
		GatewayURL: gatewayURL,
		Port:       port,
		ServerName: linkName,
		Force:      linkForce,
		DryRun:     linkDryRun,
	}

	// Direct client link
	if client != "" {
		return linkSingleClient(printer, registry, client, opts)
	}

	// Link all detected clients
	if linkAll {
		return linkAllClients(printer, registry, opts)
	}

	// Interactive mode
	return linkInteractive(printer, registry, opts)
}

func linkSingleClient(printer *output.Printer, registry *provisioner.Registry, slug string, opts provisioner.LinkOptions) error {
	prov, ok := registry.FindBySlug(slug)
	if !ok {
		printer.Error(fmt.Sprintf("%s is not a supported client", slug))
		printer.Print("Supported clients: %s\n", strings.Join(registry.AllSlugs(), ", "))
		return fmt.Errorf("%s: not a supported client", slug)
	}

	configPath, found := prov.Detect()
	if !found {
		path := configPathForSlug(prov)
		printer.Error(fmt.Sprintf("%s not detected on this system", slug))
		if path != "" {
			printer.Print("Checked: %s\n", path)
		}
		return provisioner.ErrClientNotFound
	}

	// Check npx for bridge clients
	if prov.NeedsBridge() && !provisioner.NpxAvailable() {
		printer.Error("'npx' not found in PATH")
		printer.Print("%s requires the mcp-remote bridge which needs Node.js.\n", prov.Name())
		printer.Print("Install Node.js: https://nodejs.org/\n")
		return provisioner.ErrNpxNotFound
	}

	if opts.DryRun {
		return showDryRun(printer, prov, configPath, opts)
	}

	return doLink(printer, prov, configPath, opts)
}

func linkAllClients(printer *output.Printer, registry *provisioner.Registry, opts provisioner.LinkOptions) error {
	detected := registry.DetectAll()
	if len(detected) == 0 {
		printer.Info("No supported LLM clients detected")
		printer.Print("Supported clients: %s\n", strings.Join(registry.AllSlugs(), ", "))
		return nil
	}

	var needsRestart []string
	hasNpx := provisioner.NpxAvailable()

	for _, dc := range detected {
		if dc.Provisioner.NeedsBridge() && !hasNpx {
			printer.Warn(fmt.Sprintf("Skipped %s: 'npx' not found (mcp-remote bridge requires Node.js)", dc.Provisioner.Name()))
			continue
		}

		if opts.DryRun {
			if err := showDryRun(printer, dc.Provisioner, dc.ConfigPath, opts); err != nil {
				return err
			}
			continue
		}

		if err := doLink(printer, dc.Provisioner, dc.ConfigPath, opts); err != nil {
			if errors.Is(err, provisioner.ErrConflict) {
				printer.Warn(fmt.Sprintf("Skipped %s: existing '%s' entry has unexpected config (use --force to overwrite)",
					dc.Provisioner.Name(), opts.ServerName))
				continue
			}
			return err
		}

		if dc.Provisioner.NeedsBridge() {
			needsRestart = append(needsRestart, dc.Provisioner.Name())
		}
	}

	if len(needsRestart) > 0 && !opts.DryRun {
		printer.Print("\nRestart %s to apply changes.\n", strings.Join(needsRestart, " and "))
	}

	return nil
}

func linkInteractive(printer *output.Printer, registry *provisioner.Registry, opts provisioner.LinkOptions) error {
	detected := registry.DetectAll()
	if len(detected) == 0 {
		printer.Info("No supported LLM clients detected")
		printer.Print("Supported clients: %s\n", strings.Join(registry.AllSlugs(), ", "))
		return nil
	}

	printer.Print("\n  Detected LLM clients:\n\n")
	for i, dc := range detected {
		printer.Print("    %d. %-18s %s\n", i+1, dc.Provisioner.Name(), dc.ConfigPath)
	}
	printer.Print("\n")

	// Read selection
	printer.Print("  Select clients to link (comma-separated, or 'all'): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" {
		return nil
	}

	var selected []provisioner.DetectedClient
	if input == "all" {
		selected = detected
	} else {
		indices := parseSelection(input, len(detected))
		for _, idx := range indices {
			selected = append(selected, detected[idx])
		}
	}

	if len(selected) == 0 {
		printer.Info("No clients selected")
		return nil
	}

	printer.Print("\n")
	hasNpx := provisioner.NpxAvailable()
	var needsRestart []string

	for _, dc := range selected {
		if dc.Provisioner.NeedsBridge() && !hasNpx {
			printer.Warn(fmt.Sprintf("Skipped %s: 'npx' not found (mcp-remote bridge requires Node.js)", dc.Provisioner.Name()))
			continue
		}

		if err := doLink(printer, dc.Provisioner, dc.ConfigPath, opts); err != nil {
			return err
		}

		if dc.Provisioner.NeedsBridge() {
			needsRestart = append(needsRestart, dc.Provisioner.Name())
		}
	}

	port := resolveGatewayPort(linkPort)
	printer.Print("\n  Gateway: http://localhost:%d\n", port)
	if len(needsRestart) > 0 {
		printer.Print("  Restart %s to apply changes.\n", strings.Join(needsRestart, " and "))
	}

	return nil
}

func doLink(printer *output.Printer, prov provisioner.ClientProvisioner, configPath string, opts provisioner.LinkOptions) error {
	// Warn about JSONC comment loss before modifying
	if provisioner.HasComments(configPath) {
		printer.Warn(fmt.Sprintf("Comments in %s will not be preserved", configPath))
	}

	err := prov.Link(configPath, opts)
	if errors.Is(err, provisioner.ErrAlreadyLinked) {
		port := portFromURL(opts.GatewayURL)
		printer.Info(fmt.Sprintf("%s is already linked to %s on port %s", prov.Name(), opts.ServerName, port))
		return nil
	}
	if err != nil {
		return err
	}

	transport := provisioner.TransportDescriptionFor(prov)
	printer.Info(fmt.Sprintf("Linked %s (via %s)", prov.Name(), transport))
	return nil
}

func showDryRun(printer *output.Printer, prov provisioner.ClientProvisioner, configPath string, opts provisioner.LinkOptions) error {
	before, after, err := provisioner.DryRunDiff(configPath, prov, opts)
	if err != nil {
		return err
	}

	printer.Print("\n  Would modify: %s\n\n", configPath)
	printer.Print("  --- Current ---\n")
	printer.Print("  %s\n\n", strings.ReplaceAll(before, "\n", "\n  "))
	printer.Print("  --- After ---\n")
	printer.Print("  %s\n\n", strings.ReplaceAll(after, "\n", "\n  "))
	printer.Print("  No changes made (dry run).\n\n")
	return nil
}

// resolveGatewayPort auto-detects the port from a running stack, falls back to 8180.
func resolveGatewayPort(flagPort int) int {
	if flagPort != 0 {
		return flagPort
	}
	// Try to detect from running gateway state
	states, err := state.List()
	if err == nil {
		for _, s := range states {
			if state.IsRunning(&s) {
				return s.Port
			}
		}
	}
	return 8180
}

// configPathForSlug returns the expected config path for a client (for error messages).
func configPathForSlug(prov provisioner.ClientProvisioner) string {
	path, _ := prov.Detect()
	return path
}

// portFromURL extracts the port from a URL like "http://localhost:8180/sse".
func portFromURL(url string) string {
	// Find the port between : and /
	start := strings.LastIndex(url, ":")
	if start < 0 {
		return ""
	}
	end := strings.Index(url[start:], "/")
	if end < 0 {
		return url[start+1:]
	}
	return url[start+1 : start+end]
}

// parseSelection parses comma-separated 1-based indices.
func parseSelection(input string, max int) []int {
	var indices []int
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > max {
			continue
		}
		indices = append(indices, idx-1)
	}
	return indices
}
