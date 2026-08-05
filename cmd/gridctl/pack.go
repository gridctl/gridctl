package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/wiring"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Import and apply team packs (skills + agents + rules + wiring)",
	Long: `A pack is a git repo carrying a ` + pack.ManifestFileName + ` manifest that
selects skills, agents, context rule fragments, and gateway wiring, so
one import configures a whole setup. 'pack add' imports the selection
through the same origin pipeline (security scan, --trust gate,
drift-safe updates) that 'gridctl skill add' uses; 'pack apply'
projects it through the same engines as 'gridctl skill project sync',
'gridctl ctx sync', and 'gridctl project sync --kind wiring', scoped to
the pack; 'pack remove' cascades: projections are unsynced and wiring
records cleaned before the registry entries go.

Packs never introduce hidden state: every projection is tagged with the
pack name in the unified project lockfile, and the underlying verbs keep
working on the same resources.`,
	Example: `  gridctl pack add https://github.com/acme/team-pack
  gridctl pack apply team-pack
  gridctl pack status
  gridctl pack remove team-pack`,
}

// newPackManagers builds the packops engine against the user's home.
func newPackManagers() (*packops.Managers, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	sm, err := newSkillProjectManager()
	if err != nil {
		return nil, err
	}
	am, err := newAgentProjectManager()
	if err != nil {
		return nil, err
	}
	wm, err := wiring.NewManager()
	if err != nil {
		return nil, err
	}
	cm, err := contexts.NewManager()
	if err != nil {
		return nil, err
	}
	return &packops.Managers{Skills: sm, Agents: am, Wiring: wm, Contexts: cm, Home: home}, nil
}

// --- pack add ---

var (
	packAddRef    string
	packAddTrust  bool
	packAddDryRun bool
	packAddFormat string
	packAddJSON   *bool
)

var packAddCmd = &cobra.Command{
	Use:   "add <repo-url>",
	Short: "Import a pack from a git repository",
	Long: `Clones the repository, reads ` + pack.ManifestFileName + ` at its root, and
imports exactly the manifest's selection of skills, agents, and rule
fragments into the local stores (empty skill and agent lists mean
everything discovered; rules are opt-in, an empty list means none).
Nothing touches client files or the gateway; that is 'pack apply'.

Manifest-selected names the repository does not contain are reported as
unresolved (exit 1); the rest of the pack still imports.

Exit codes:
  0  imported cleanly
  1  partial (unresolved selections, or skipped resources)
  2  infrastructure error (clone, auth, missing or invalid manifest)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packAddFormat, cmd.Flags().Changed("format"), *packAddJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		store, err := loadRegistry()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		imp := newImporter(store)
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runPackAdd(cmd.Context(), os.Stdout, os.Stderr, mgrs, imp, args[0], packAddRef, packAddTrust, packAddDryRun, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// runPackAdd clones, resolves the manifest selection, and imports.
func runPackAdd(ctx context.Context, stdout, stderr io.Writer, mgrs *packops.Managers, imp *skills.Importer, repo, ref string, trust, dryRun bool, format string) int {
	res, err := mgrs.Add(ctx, imp, packops.AddOptions{Repo: repo, Ref: ref, Trust: trust, DryRun: dryRun})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := res.Doc

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		for _, n := range res.Notes {
			fmt.Fprintln(stdout, n)
		}
		verb := "Imported"
		if dryRun {
			verb = "Would import"
		}
		wiringLabel := "no"
		if doc.Wiring {
			wiringLabel = "yes"
		}
		if len(doc.Rules) > 0 {
			fmt.Fprintf(stdout, "%s pack %q (%d skills, %d agents, %d rules, wiring: %s) from %s\n",
				verb, doc.Pack, len(doc.Skills), len(doc.Agents), len(doc.Rules), wiringLabel, repo)
		} else {
			fmt.Fprintf(stdout, "%s pack %q (%d skills, %d agents, wiring: %s) from %s\n",
				verb, doc.Pack, len(doc.Skills), len(doc.Agents), wiringLabel, repo)
		}
		for _, w := range doc.Warnings {
			fmt.Fprintf(stdout, "Warning: %s\n", w)
		}
		for _, s := range doc.Skipped {
			fmt.Fprintf(stdout, "Skipped: %s\n", s)
		}
		for _, u := range doc.Unresolved {
			fmt.Fprintf(stdout, "Unresolved: pack selects %q but the repository does not ship it\n", u)
		}
		if !dryRun && len(doc.Unresolved) == 0 && len(doc.Skipped) == 0 {
			fmt.Fprintf(stdout, "Run 'gridctl pack apply %s' to project it.\n", doc.Pack)
		}
	}
	if len(doc.Unresolved) > 0 || len(doc.Skipped) > 0 {
		return ctxExitAttention
	}
	return ctxExitOK
}

// --- pack apply ---

var (
	packApplyForce   bool
	packApplyDryRun  bool
	packApplyClients []string
	packApplyFormat  string
	packApplyJSON    *bool
	packApplyPlain   *bool
)

var packApplyCmd = &cobra.Command{
	Use:   "apply <name>",
	Short: "Project a pack's resources to clients",
	Long: `Projects an imported pack: its skills and agents through the same
engines as 'gridctl skill project sync' (scoped to the pack's
selection), its rule fragments through 'gridctl ctx sync', and, when
the manifest declares wiring, the gateway entry through the same
machinery as 'gridctl project sync --kind wiring'. Every projection is
tagged with the pack name; for rules only pack-shipped fragments carry
the tag.

Apply is additive and never transactional: each resource succeeds or
skips independently, drifted resources are skipped with an adopt or
--force hint, and a resource tagged by a different pack is refused.

Exit codes:
  0  everything applied
  1  a resource was skipped or failed
  2  infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packApplyFormat, cmd.Flags().Changed("format"), *packApplyJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*packApplyPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runPackApply(cmd.Context(), os.Stdout, os.Stderr, mgrs, args[0], packApplyForce, packApplyDryRun, packApplyClients, format, *packApplyPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// runPackApply projects one pack across every kind it selects.
func runPackApply(ctx context.Context, stdout, stderr io.Writer, mgrs *packops.Managers, name string, force, dryRun bool, clientOverride []string, format string, plain bool) int {
	doc, err := mgrs.Apply(ctx, name, packops.ApplyOptions{Force: force, DryRun: dryRun, Clients: clientOverride})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		renderPackRows(stdout, doc.Rows, plain)
		fmt.Fprintf(stdout, "\nApplied %d/%d resources.\n", doc.Applied, doc.Total)
	}
	if doc.Total > doc.Applied {
		return ctxExitAttention
	}
	return ctxExitOK
}

// renderPackRows prints the per-resource table plus detail lines.
func renderPackRows(w io.Writer, rows []packops.Row, plain bool) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "Nothing to do.")
		return
	}
	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"KIND", "NAME", "CLIENT", "ACTION"})
	for _, r := range rows {
		action := r.Action
		if r.State != "" {
			action = r.State
		}
		t.AppendRow(table.Row{r.Kind, r.Name, r.Client, packActionLabel(action)})
	}
	t.Render()
	for _, r := range rows {
		if r.Detail == "" {
			continue
		}
		fmt.Fprintf(w, "\n%s %s: %s.\n", r.Kind, r.Name, r.Detail)
		if r.Remediation != "" {
			fmt.Fprintf(w, "  %s\n", capitalizeFirst(r.Remediation))
		}
	}
}

// packActionLabel decorates actions/states with the shared glyphs.
func packActionLabel(action string) string {
	switch {
	case strings.HasPrefix(action, "skipped") || action == "error" || action == "unresolved" || action == "drifted" || action == "target-missing" || action == "foreign":
		return "✗ " + action
	case strings.HasPrefix(action, "would-") || action == "stale" || action == "missing":
		return "— " + action
	default:
		return "✓ " + action
	}
}

// --- pack status ---

var (
	packStatusFormat string
	packStatusJSON   *bool
	packStatusPlain  *bool
)

var packStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show per-resource state for imported packs",
	Long: `Shows the projection state of every resource an imported pack selects,
in the shared state vocabulary (in-sync, stale, drifted,
target-missing, foreign, missing), plus unresolved manifest selections.
Rule rows report per-client projection state once applied (per
fragment-file projection; a compiled client's whole-document state
stays in 'gridctl ctx status'); a rule that was imported but never
projected reports store-level presence.

Exit codes:
  0  everything clean
  1  attention needed
  2  infrastructure error`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packStatusFormat, cmd.Flags().Changed("format"), *packStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*packStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		if exit := runPackStatus(cmd.Context(), os.Stdout, os.Stderr, mgrs, name, format, *packStatusPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// packStatusDoc is the machine-readable status document.
type packStatusDoc struct {
	SchemaVersion  int           `json:"schema_version"`
	NeedsAttention bool          `json:"needs_attention"`
	Rows           []packops.Row `json:"rows"`
}

// runPackStatus reports the state matrix for one or all packs.
func runPackStatus(ctx context.Context, stdout, stderr io.Writer, mgrs *packops.Managers, name, format string, plain bool) int {
	statuses, err := mgrs.Statuses(ctx, packops.StatusOptions{Pack: name, GatewayPort: resolveGatewayPort(0)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	if len(statuses) == 0 {
		fmt.Fprintln(stdout, "No packs imported. Run 'gridctl pack add <repo-url>' to import one.")
		return ctxExitOK
	}

	var rows []packops.Row
	attention := false
	for _, ps := range statuses {
		rows = append(rows, ps.Rows...)
		attention = attention || ps.NeedsAttention
	}

	doc := packStatusDoc{SchemaVersion: packops.SchemaVersion, NeedsAttention: attention, Rows: rows}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "Pack imported but nothing projected yet. Run 'gridctl pack apply <name>'.")
			return ctxExitOK
		}
		renderPackRows(stdout, rows, plain)
	}
	if attention {
		return ctxExitAttention
	}
	return ctxExitOK
}

// --- pack remove ---

var (
	packRemoveForce  bool
	packRemoveDryRun bool
	packRemoveFormat string
	packRemoveJSON   *bool
)

var packRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a pack: projections, wiring records, and registry entries",
	Long: `Cascade removal in dependency order: pack-tagged projections are
unsynced from client directories, pack-tagged wiring records are
removed through the ownership manager (the entry is deleted only when
its value is one gridctl recorded), and only then do the pack's skills,
agents, and installed rule fragments leave the local stores, followed
by the pack record itself.

A resource whose projection was hand-edited (drifted) is skipped with a
remediation hint unless --force; everything else still removes, and the
skipped resources stay imported so nothing is lost. Removing a skill
removes all of its projections, including any made outside the pack.

Exit codes:
  0  removed cleanly
  1  partial (drifted resources kept)
  2  infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packRemoveFormat, cmd.Flags().Changed("format"), *packRemoveJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		store, err := loadRegistry()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		imp := newImporter(store)
		if exit := runPackRemove(cmd.Context(), os.Stdout, os.Stderr, mgrs, imp, args[0], packRemoveForce, packRemoveDryRun, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// runPackRemove cascades one pack's removal.
func runPackRemove(ctx context.Context, stdout, stderr io.Writer, mgrs *packops.Managers, imp *skills.Importer, name string, force, dryRun bool, format string) int {
	doc, err := mgrs.Remove(ctx, imp, name, packops.RemoveOptions{Force: force, DryRun: dryRun, GatewayPort: resolveGatewayPort(0)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		renderPackRows(stdout, doc.Rows, false)
		if len(doc.Kept) > 0 {
			fmt.Fprintf(stdout, "\nKept (drifted, re-run with --force to remove): %s\n", strings.Join(doc.Kept, ", "))
		} else if !dryRun {
			fmt.Fprintf(stdout, "\nPack %q removed.\n", name)
		}
	}
	for _, r := range doc.Rows {
		if r.Action == "error" {
			return ctxExitAttention
		}
	}
	if len(doc.Kept) > 0 {
		return ctxExitAttention
	}
	return ctxExitOK
}

// loadLockedPack finds a pack's record in the import lockfile.
func loadLockedPack(name string) (*skills.LockedPack, error) {
	return packops.LoadLockedPack(name)
}

func init() {
	packAddCmd.Flags().StringVar(&packAddRef, "ref", "", "Git ref to import (branch, tag, or commit; default: the default branch)")
	packAddCmd.Flags().BoolVar(&packAddTrust, "trust", false, "Proceed despite security findings")
	packAddCmd.Flags().BoolVar(&packAddDryRun, "dry-run", false, "Resolve and report without importing")
	packAddCmd.Flags().StringVar(&packAddFormat, "format", "text", "Output format: text or json")
	packAddJSON = addJSONAlias(packAddCmd)

	packApplyCmd.Flags().BoolVar(&packApplyForce, "force", false, "Overwrite drifted or foreign resources (after backup)")
	packApplyCmd.Flags().BoolVar(&packApplyDryRun, "dry-run", false, "Show what would change without modifying files")
	packApplyCmd.Flags().StringSliceVar(&packApplyClients, "clients", nil, "Restrict wiring to these client slugs")
	packApplyCmd.Flags().StringVar(&packApplyFormat, "format", "text", "Output format: text or json")
	packApplyJSON = addJSONAlias(packApplyCmd)
	packApplyPlain = addPlainFlag(packApplyCmd)

	packStatusCmd.Flags().StringVar(&packStatusFormat, "format", "text", "Output format: text or json")
	packStatusJSON = addJSONAlias(packStatusCmd)
	packStatusPlain = addPlainFlag(packStatusCmd)

	packRemoveCmd.Flags().BoolVar(&packRemoveForce, "force", false, "Remove even when projections were hand-edited")
	packRemoveCmd.Flags().BoolVar(&packRemoveDryRun, "dry-run", false, "Show what would be removed without removing")
	packRemoveCmd.Flags().StringVar(&packRemoveFormat, "format", "text", "Output format: text or json")
	packRemoveJSON = addJSONAlias(packRemoveCmd)

	packCmd.AddCommand(packAddCmd, packApplyCmd, packStatusCmd, packRemoveCmd)
}
