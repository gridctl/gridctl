package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"

	"github.com/spf13/cobra"
)

var (
	unlinkAll    bool
	unlinkName   string
	unlinkDryRun bool
)

var unlinkCmd = &cobra.Command{
	Use:   "unlink [client]",
	Short: "Remove gridctl from an LLM client's config",
	Long: `Removes the gridctl entry from an LLM client's MCP configuration.

Without arguments, detects linked clients and presents a selection.
With a client name, unlinks that specific client directly.

Supported clients: claude, claude-code, cursor, windsurf, vscode, gemini, opencode, continue, cline, anythingllm, roo, zed, goose`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var client string
		if len(args) > 0 {
			client = args[0]
		}
		return runUnlink(client)
	},
}

func init() {
	unlinkCmd.Flags().BoolVarP(&unlinkAll, "all", "a", false, "Unlink from all clients")
	unlinkCmd.Flags().StringVarP(&unlinkName, "name", "n", "gridctl", "Server name to remove")
	unlinkCmd.Flags().BoolVar(&unlinkDryRun, "dry-run", false, "Show what would change without modifying files")
}

func runUnlink(client string) error {
	printer := output.New()
	registry := provisioner.NewRegistry()

	// Direct unlink
	if client != "" {
		return unlinkSingleClient(printer, registry, client)
	}

	// Unlink all
	if unlinkAll {
		return unlinkAllClients(printer, registry)
	}

	// Interactive: find linked clients
	return unlinkInteractive(printer, registry)
}

func unlinkSingleClient(printer *output.Printer, registry *provisioner.Registry, slug string) error {
	prov, ok := registry.FindBySlug(slug)
	if !ok {
		printer.Error(fmt.Sprintf("%s is not a supported client", slug))
		printer.Print("Supported clients: %s\n", strings.Join(registry.AllSlugs(), ", "))
		return fmt.Errorf("%s: not a supported client", slug)
	}

	configPath, found := prov.Detect()
	if !found {
		printer.Info(fmt.Sprintf("%s not detected on this system", slug))
		return nil
	}

	return doUnlink(printer, prov, configPath)
}

func unlinkAllClients(printer *output.Printer, registry *provisioner.Registry) error {
	detected := registry.DetectAll()
	if len(detected) == 0 {
		printer.Info("No supported LLM clients detected")
		return nil
	}

	for _, dc := range detected {
		if err := doUnlink(printer, dc.Provisioner, dc.ConfigPath); err != nil {
			return err
		}
	}

	return nil
}

func unlinkInteractive(printer *output.Printer, registry *provisioner.Registry) error {
	detected := registry.DetectAll()
	if len(detected) == 0 {
		printer.Info("No supported LLM clients detected")
		return nil
	}

	// Filter to only linked clients
	var linked []provisioner.DetectedClient
	for _, dc := range detected {
		isLinked, err := dc.Provisioner.IsLinked(dc.ConfigPath, unlinkName)
		if err == nil && isLinked {
			linked = append(linked, dc)
		}
	}

	if len(linked) == 0 {
		printer.Info(fmt.Sprintf("No clients linked to '%s'", unlinkName))
		return nil
	}

	// If only one linked client, unlink directly
	if len(linked) == 1 {
		return doUnlink(printer, linked[0].Provisioner, linked[0].ConfigPath)
	}

	// Multiple linked clients — show list and let user choose
	printer.Print("\n  Linked clients:\n\n")
	for i, dc := range linked {
		printer.Print("    %d. %-18s %s\n", i+1, dc.Provisioner.Name(), dc.ConfigPath)
	}
	printer.Print("\n")
	printer.Print("  Select clients to unlink (comma-separated, or 'all'): ")

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
		selected = linked
	} else {
		for _, part := range strings.Split(input, ",") {
			part = strings.TrimSpace(part)
			var idx int
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(linked) {
				continue
			}
			selected = append(selected, linked[idx-1])
		}
	}

	printer.Print("\n")
	for _, dc := range selected {
		if err := doUnlink(printer, dc.Provisioner, dc.ConfigPath); err != nil {
			return err
		}
	}

	return nil
}

func doUnlink(printer *output.Printer, prov provisioner.ClientProvisioner, configPath string) error {
	if unlinkDryRun {
		isLinked, _ := prov.IsLinked(configPath, unlinkName)
		if !isLinked {
			printer.Info(fmt.Sprintf("%s has no '%s' entry", prov.Name(), unlinkName))
			return nil
		}
		printer.Print("  Would remove '%s' entry from: %s\n", unlinkName, configPath)
		printer.Print("  No changes made (dry run).\n")
		return nil
	}

	err := prov.Unlink(configPath, unlinkName)
	if errors.Is(err, provisioner.ErrNotLinked) {
		printer.Info(fmt.Sprintf("%s has no '%s' entry", prov.Name(), unlinkName))
		return nil
	}
	if err != nil {
		return err
	}

	printer.Info(fmt.Sprintf("Unlinked %s", prov.Name()))
	return nil
}
