package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	linkPort     int
	linkAll      bool
	linkName     string
	linkClientID string
	linkGroup    string
	linkDryRun   bool
	linkForce    bool
)

var linkCmd = &cobra.Command{
	Use:   "link [client]",
	Short: "Connect an LLM client to the gridctl gateway",
	Long: `Injects the gridctl gateway into an LLM client's MCP configuration.

Without arguments, detects installed LLM clients and presents a selection list.
With a client name, links that specific client directly.

Supported clients: claude, claude-code, cursor, windsurf, vscode, gemini, antigravity, opencode, grok, continue, cline, anythingllm, roo, zed, goose`,
	Example: `  gridctl link                 Pick from detected clients interactively
  gridctl link claude          Link Claude Desktop directly
  gridctl link --all           Link every detected client
  gridctl link claude --dry-run  Preview the config change`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var client string
		if len(args) > 0 {
			client = args[0]
		}
		// A group-scoped link defaults its entry name to gridctl-<group> so
		// several groups can be linked into one client side by side.
		if linkGroup != "" && !cmd.Flags().Changed("name") {
			linkName = "gridctl-" + linkGroup
		}
		return runLink(client)
	},
}

func init() {
	linkCmd.Flags().IntVarP(&linkPort, "port", "p", 0, "Gateway port (default: auto-detect from running stack, else 8180)")
	linkCmd.Flags().BoolVarP(&linkAll, "all", "a", false, "Link all detected clients at once")
	linkCmd.Flags().StringVarP(&linkName, "name", "n", "gridctl", "Server name in client config")
	linkCmd.Flags().StringVar(&linkClientID, "client-id", "", "Stable client identifier for per-client access scoping (matches a stack.yaml clients: profile)")
	linkCmd.Flags().StringVar(&linkGroup, "group", "", "Tool group whose endpoint to link (matches a stack.yaml groups: entry)")
	linkCmd.Flags().BoolVar(&linkDryRun, "dry-run", false, "Show what would change without modifying files")
	linkCmd.Flags().BoolVar(&linkForce, "force", false, "Overwrite existing gridctl entry even if present")
}

func runLink(client string) error {
	printer := output.New()
	registry := provisioner.NewRegistry()

	port := resolveGatewayPort(linkPort)
	// A group link targets the group's endpoint; the check against the
	// running daemon is best-effort (the daemon may be down or older).
	baseURL := provisioner.GatewayURL(port)
	if linkGroup != "" {
		baseURL = provisioner.GroupGatewayURL(port, linkGroup)
		warnUnknownGroup(printer, port, linkGroup)
	}
	// Embed the stable client identifier (when set) on the gateway URL so the
	// gateway resolves the connecting client's access scope from the wire rather
	// than from the clientInfo.name normalization heuristic alone.
	gatewayURL := provisioner.AppendClientParam(baseURL, linkClientID)

	opts := provisioner.LinkOptions{
		GatewayURL: gatewayURL,
		Port:       port,
		ServerName: linkName,
		ClientID:   linkClientID,
		Group:      linkGroup,
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

	// Interactive mode. The selector guards against non-terminal stdin
	// itself, so the zero-clients no-op below stays script-safe.
	return linkInteractive(printer, registry, opts)
}

func linkSingleClient(printer *output.Printer, registry *provisioner.Registry, slug string, opts provisioner.LinkOptions) error {
	prov, ok := registry.FindBySlug(slug)
	if !ok {
		return unknownClientError(registry, slug)
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

	return linkSelected(printer, detected, opts)
}

// linkSelected prompts for a subset of detected clients and links each
// selection. Split from linkInteractive so tests can drive it with fake
// clients and a swapped selector.
func linkSelected(printer *output.Printer, detected []provisioner.DetectedClient, opts provisioner.LinkOptions) error {
	selected, err := clientSelector("link", detected)
	if err != nil {
		return err
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

// unknownClientError builds the single-print error for an unrecognized
// client slug, including a "did you mean" suggestion within edit distance
// two. The suggestion is never auto-run.
func unknownClientError(registry *provisioner.Registry, slug string) error {
	msg := fmt.Sprintf("unknown client %q", slug)
	if s := output.Suggest(slug, registry.AllSlugs()); s != "" {
		msg += fmt.Sprintf("\nDid you mean %q?", s)
	}
	msg += "\nSupported clients: " + strings.Join(registry.AllSlugs(), ", ")
	return errors.New(msg)
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
