package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"

	"gopkg.in/yaml.v3"
)

// loadDeclaredLinks reads the stack's link: block with a raw parse. The
// full config.LoadStack pipeline (vault resolution, variable expansion,
// validation) already ran inside Deploy; the reconcile caller only needs
// the declared entries, and a raw read avoids unlocking the vault twice.
// Errors yield nil: an unreadable file at this point means validation
// already failed or the file vanished mid-run, and reconcile is best-effort
// either way.
func loadDeclaredLinks(stackPath string) []config.LinkEntry {
	data, err := os.ReadFile(stackPath)
	if err != nil {
		return nil
	}
	var s config.Stack
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.Link
}

// slugResolver is the slice of provisioner.Registry the reconcile helpers
// need, split out so tests drive them with fakes (the registry's client
// list is not injectable).
type slugResolver interface {
	FindBySlug(slug string) (provisioner.ClientProvisioner, bool)
}

// reconcileDeclaredLinks links every declared client to the gateway on
// port. Additive and idempotent: already-linked entries are silent no-ops,
// clients not installed on this machine warn and skip (stack files travel
// between machines), conflicts warn with a --force hint, and nothing here
// ever fails the apply. quiet suppresses success lines but never warnings
// (Article XI).
func reconcileDeclaredLinks(printer *output.Printer, registry slugResolver, entries []config.LinkEntry, port int, quiet bool) {
	if len(entries) == 0 {
		return
	}

	hasNpx := provisioner.NpxAvailable()
	warnedComments := make(map[string]bool)
	var needsRestart []string

	for _, entry := range entries {
		prov, ok := registry.FindBySlug(entry.Client)
		if !ok {
			// Validation rejects unknown slugs before deploy; this only
			// fires when the binary's registry and config copy drift.
			printer.Warn(fmt.Sprintf("Skipped %s: unknown client", entry.Client))
			continue
		}

		configPath, found := prov.Detect()
		if !found {
			printer.Warn(fmt.Sprintf("Skipped %s: not detected on this system", prov.Name()))
			continue
		}
		if prov.NeedsBridge() && !hasNpx {
			printer.Warn(fmt.Sprintf("Skipped %s: 'npx' not found (mcp-remote bridge requires Node.js)", prov.Name()))
			continue
		}

		if provisioner.HasComments(configPath) && !warnedComments[configPath] {
			printer.Warn(fmt.Sprintf("Comments in %s will not be preserved", configPath))
			warnedComments[configPath] = true
		}

		opts := linkOptionsForEntry(entry, port)
		err := prov.Link(configPath, opts)
		switch {
		case errors.Is(err, provisioner.ErrAlreadyLinked):
			// Idempotent reconcile: declared and already converged.
			continue
		case errors.Is(err, provisioner.ErrConflict):
			printer.Warn(fmt.Sprintf("Skipped %s: existing '%s' entry has unexpected config (run 'gridctl link %s --force' to overwrite)",
				prov.Name(), opts.ServerName, entry.Client))
			continue
		case err != nil:
			printer.Warn(fmt.Sprintf("Failed to link %s: %s", prov.Name(), err))
			continue
		}

		if !quiet {
			printer.Info(fmt.Sprintf("Linked %s (via %s)", prov.Name(), provisioner.TransportDescriptionFor(prov)))
		}
		if prov.NeedsBridge() {
			needsRestart = append(needsRestart, prov.Name())
		}
	}

	if len(needsRestart) > 0 && !quiet {
		printer.Print("\nRestart %s to apply changes.\n", strings.Join(needsRestart, " and "))
	}
}

// linkOptionsForEntry maps a declared entry to LinkOptions exactly as the
// imperative `gridctl link` flags do: group links target the group
// endpoint and default the entry name to gridctl-<group>, and a client_id
// rides the gateway URL as the `client` query parameter.
func linkOptionsForEntry(entry config.LinkEntry, port int) provisioner.LinkOptions {
	baseURL := provisioner.GatewayURL(port)
	if entry.Group != "" {
		baseURL = provisioner.GroupGatewayURL(port, entry.Group)
	}
	return provisioner.LinkOptions{
		GatewayURL: provisioner.AppendClientParam(baseURL, entry.ClientID),
		Port:       port,
		ServerName: entry.EffectiveName(),
		ClientID:   entry.ClientID,
		Group:      entry.Group,
	}
}

// linkAction is one declared entry's pending state for `gridctl plan`.
type linkAction struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Action string `json:"action"` // "link" | "already-linked" | "skip"
}

// computeLinkActions reports what reconcile would do for each declared
// entry without writing anything. IsLinked checks the resolved entry name
// (explicit name, gridctl-<group>, or gridctl), never a hard-coded
// default.
func computeLinkActions(registry slugResolver, entries []config.LinkEntry) []linkAction {
	actions := make([]linkAction, 0, len(entries))
	for _, entry := range entries {
		action := linkAction{Slug: entry.Client, Name: entry.EffectiveName(), Action: "link"}
		prov, ok := registry.FindBySlug(entry.Client)
		if !ok {
			action.Action = "skip"
			actions = append(actions, action)
			continue
		}
		configPath, found := prov.Detect()
		if !found {
			action.Action = "skip"
			actions = append(actions, action)
			continue
		}
		if linked, err := prov.IsLinked(configPath, action.Name); err == nil && linked {
			action.Action = "already-linked"
		}
		actions = append(actions, action)
	}
	return actions
}
