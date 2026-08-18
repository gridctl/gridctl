package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/resetops"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/wiring"
)

var (
	resetPurge   bool
	resetForce   bool
	resetDryRun  bool
	resetYes     bool
	resetVerbose bool
	resetFormat  string
	resetJSON    bool
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Remove everything gridctl placed on this machine",
	Long: `Removes every artifact gridctl created outside its own directory:
projected skills, agents, and context rules in client directories,
gateway entries gridctl owns inside shared client MCP configs, and the
containers, networks, and daemons of every stack under the active home.

The default tier PRESERVES ~/.gridctl (vault, oauth grants, pins,
registry, cache, telemetry, logs). --purge deletes ~/.gridctl as well.

Removal is ownership-driven: only lockfile-recorded artifacts are
touched, hand-edited files are kept unless --force, and entries gridctl
never created are never removed. A backup archive is written before
anything is deleted. There is no restore command; the backup is a
manual safety copy, and the forward recovery path is re-running
'gridctl apply', 'gridctl pack add', and 'gridctl link'.

To tear down one stack, use 'gridctl destroy' instead.

Exit codes:
  0  reset cleanly
  1  partial: something failed or was kept (drifted/foreign); re-run
     after addressing it (reset is idempotent)
  2  infrastructure error, or confirmation refused non-interactively`,
	Example: `  gridctl reset --dry-run      Preview the blast radius
  gridctl reset                Remove projections, wiring, containers
  gridctl reset --purge        Also delete ~/.gridctl (vault, pins, ...)
  gridctl reset --yes          Non-interactive (CI) default tier`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(resetFormat, cmd.Flags().Changed("format"), resetJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		return runReset(cmd.Context(), format)
	},
}

func init() {
	resetCmd.Flags().BoolVar(&resetPurge, "purge", false, "Also delete ~/.gridctl (vault, oauth grants, pins, registry, telemetry)")
	resetCmd.Flags().BoolVar(&resetForce, "force", false, "Also remove hand-edited (drifted) artifacts; foreign entries are still never removed")
	resetCmd.Flags().BoolVar(&resetDryRun, "dry-run", false, "Show what would be removed without removing anything")
	resetCmd.Flags().BoolVarP(&resetYes, "yes", "y", false, "Skip confirmation prompts (required non-interactively)")
	resetCmd.Flags().BoolVar(&resetVerbose, "verbose", false, "List every artifact instead of per-client counts")
	resetCmd.Flags().StringVar(&resetFormat, "format", "table", "Output format: table or json")
	resetCmd.Flags().BoolVar(&resetJSON, "json", false, "Shorthand for --format json")
}

// newResetManagers assembles the reset engine from the same kind
// managers every other verb drives. A missing piece degrades to nil
// (reported, not fatal): an unreadable registry must not make reset,
// the recovery tool, unusable.
func newResetManagers(printer *output.Printer) (*resetops.Managers, func(), error) {
	home, err := state.Home()
	if err != nil {
		return nil, nil, err
	}
	m := &resetops.Managers{Home: home}

	if sm, err := newSkillProjectManager(); err != nil {
		printer.Warn("skill projections unavailable; their removal is skipped", "error", err)
		m.Missing = append(m.Missing, "skill")
	} else {
		m.Skills = sm
	}
	if am, err := newAgentProjectManager(); err != nil {
		printer.Warn("agent projections unavailable; their removal is skipped", "error", err)
		m.Missing = append(m.Missing, "agent")
	} else {
		m.Agents = am
	}
	if cm, err := contexts.NewManager(); err != nil {
		printer.Warn("context manager unavailable; context removal is skipped", "error", err)
		m.Missing = append(m.Missing, "context")
	} else {
		m.Contexts = cm
	}
	if wm, err := wiring.NewManager(); err != nil {
		printer.Warn("wiring manager unavailable; client-config removal is skipped", "error", err)
		m.Missing = append(m.Missing, "wiring")
	} else {
		m.Wiring = wm
	}
	cleanup := func() {}
	if rt, err := runtime.New(); err != nil {
		printer.Warn("container runtime unavailable; container teardown is skipped", "error", err)
	} else {
		m.Runtime = rt
		cleanup = func() { rt.Close() }
	}
	return m, cleanup, nil
}

func runReset(ctx context.Context, format string) error {
	printer := output.New()
	asJSON := strings.EqualFold(format, "json")
	if asJSON {
		printer = output.NewWithWriter(os.Stderr)
	}

	mgrs, cleanup, err := newResetManagers(printer)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ctxExitInfrastructure)
	}
	defer cleanup()
	opts := resetops.Options{Purge: resetPurge, Force: resetForce}

	preview, err := mgrs.Preview(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ctxExitInfrastructure)
	}

	if resetDryRun {
		if asJSON {
			return output.EncodeJSON(os.Stdout, preview)
		}
		renderResetPreview(os.Stdout, mgrs, preview)
		fmt.Println("\nNo changes made (dry run).")
		return nil
	}

	// Confirmation. Non-interactive without --yes exits 2 with guidance;
	// --yes covers the default tier, and --purge is authorized only by
	// the flag itself being typed (never implied by config or env).
	if !resetYes {
		if !output.IsTerminal(os.Stdin) {
			fmt.Fprintln(os.Stderr, "refusing to prompt in a non-interactive session\nPass --yes (default tier) or --yes --purge, or run 'gridctl reset --dry-run' to preview")
			os.Exit(ctxExitInfrastructure)
		}
		renderResetPreview(os.Stdout, mgrs, preview)
		fmt.Println()
		if resetPurge {
			if !confirmResetPurge(mgrs.GridctlDir()) {
				fmt.Println("Cancelled")
				return nil
			}
		} else if !confirmResetDefault() {
			fmt.Println("Cancelled")
			return nil
		}
	}

	// Streaming per-phase text lines: no spinner, no cursor tricks, and
	// word-prefixed statuses so a piped log reads the same as a TTY.
	progressOut := os.Stdout
	if asJSON {
		progressOut = os.Stderr
	}
	phaseTitles := map[string]string{
		"backup": "Writing backup", "daemons": "Stopping daemons",
		"projections": "Removing skill and agent projections",
		"contexts":    "Removing context rules", "wiring": "Removing gateway entries from client configs",
		"containers": "Removing containers and networks", "state": "Removing state files",
		"purge": "Deleting " + mgrs.GridctlDir(),
	}
	progress := func(phase string, row *resetops.Row) {
		if row == nil {
			if title, ok := phaseTitles[phase]; ok {
				fmt.Fprintf(progressOut, "%s...\n", title)
			}
			return
		}
		line := row.Action + "  " + row.Kind
		if row.Name != "" {
			line += " " + row.Name
		}
		if row.Client != "" && row.Client != row.Name {
			line += " (" + row.Client + ")"
		}
		if row.Error != "" {
			line = "FAILED  " + row.Kind + " " + row.Name + ": " + row.Error
		}
		fmt.Fprintf(progressOut, "  %s\n", line)
		if row.Kind == "backup" {
			fmt.Fprintf(progressOut, "  (the backup is a safety copy, not an undo)\n")
		}
	}

	doc, err := mgrs.Execute(ctx, opts, progress)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ctxExitInfrastructure)
	}

	if asJSON {
		if err := output.EncodeJSON(os.Stdout, doc); err != nil {
			return err
		}
	} else {
		renderResetResult(os.Stdout, doc)
	}

	if doc.Failed > 0 || len(doc.Kept) > 0 {
		os.Exit(ctxExitAttention)
	}
	return nil
}

// confirmResetDefault is the moderate-tier [y/N], matching telemetry wipe.
func confirmResetDefault() bool {
	fmt.Print("Proceed? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	return ans == "y" || ans == "yes"
}

// confirmResetPurge is the severe-tier typed confirmation: the exact
// resolved path RemoveAll will receive, produced by the same resolver,
// never a literal "~/.gridctl", which under a non-default home names
// the wrong tree.
func confirmResetPurge(gridctlDir string) bool {
	var typed string
	input := huh.NewInput().
		Title(fmt.Sprintf("Type %s to confirm permanent deletion", gridctlDir)).
		Description("This deletes the vault, oauth grants, pins, registry, and telemetry. There is no restore command.").
		Value(&typed)
	form := huh.NewForm(huh.NewGroup(input)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if !output.ColorEnabled(os.Stdout) {
		form = form.WithTheme(huh.ThemeBase())
	}
	if err := form.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(typed) == gridctlDir
}

// renderResetPreview prints the blast radius grouped by consequence
// class. Summarized by kind and client; --verbose lists every row.
func renderResetPreview(w *os.File, mgrs *resetops.Managers, doc *resetops.Doc) {
	fmt.Fprintf(w, "About to reset gridctl (home: %s)\n\n", doc.Home)

	recreatable := map[string]int{}
	perClient := map[string]int{}
	var wiringRows, keptRows, verboseRows []resetops.Row
	for _, r := range doc.Rows {
		switch r.Action {
		case resetops.ActionKeptDrift, resetops.ActionKeptForeign:
			keptRows = append(keptRows, r)
			continue
		}
		verboseRows = append(verboseRows, r)
		if r.Kind == "wiring" {
			wiringRows = append(wiringRows, r)
			continue
		}
		recreatable[r.Kind]++
		if r.Client != "" {
			perClient[r.Client]++
		}
	}

	fmt.Fprintln(w, "  RECREATABLE  (a later 'gridctl apply' / 'gridctl pack apply' rebuilds these)")
	if len(recreatable) == 0 {
		fmt.Fprintln(w, "    nothing")
	}
	for _, kind := range sortedKeys(recreatable) {
		fmt.Fprintf(w, "    %-12s %d\n", kind, recreatable[kind])
	}
	if len(perClient) > 0 {
		fmt.Fprintf(w, "    across clients: %s\n", clientSummary(perClient))
	}

	fmt.Fprintln(w, "\n  REMOVED FROM SHARED CLIENT CONFIGS  (backed up first)")
	if len(wiringRows) == 0 {
		fmt.Fprintln(w, "    nothing")
	} else {
		var clients []string
		for _, r := range wiringRows {
			clients = append(clients, r.Client)
		}
		sort.Strings(clients)
		fmt.Fprintf(w, "    gateway entry in %d client config(s): %s\n", len(clients), strings.Join(clients, ", "))
	}

	if len(keptRows) > 0 {
		fmt.Fprintf(w, "\n  KEPT (YOUR EDITS ARE SAFE) (%d)\n", len(keptRows))
		for _, r := range keptRows {
			fmt.Fprintf(w, "    %s %s: %s\n", r.Kind, r.Name, r.Detail)
		}
	}

	if doc.Purge {
		fmt.Fprintln(w, "\n  UNRECOVERABLE  (deleted with "+mgrs.GridctlDir()+")")
		if s := doc.Stats; s != nil {
			fmt.Fprintf(w, "    vault variables  %s\n", countOrUnknown(s.VaultVariables))
			fmt.Fprintf(w, "    oauth grants     %d\n", s.OAuthServers)
			fmt.Fprintf(w, "    pin files        %d\n", s.PinFiles)
			fmt.Fprintf(w, "    telemetry        %s\n", formatBytes(s.TelemetryBytes))
		}
	} else {
		fmt.Fprintln(w, "\n  PRESERVED")
		fmt.Fprintf(w, "    %s is kept: vault, oauth grants, pins, registry, cache, telemetry, logs.\n", mgrs.GridctlDir())
		fmt.Fprintln(w, "    Run 'gridctl reset --purge' to remove it as well.")
	}

	if resetVerbose && len(verboseRows) > 0 {
		fmt.Fprintln(w, "\n  FULL LISTING")
		tw := output.NewTableWriter(w, false)
		tw.AppendHeader(table.Row{"KIND", "NAME", "CLIENT", "PATH", "ACTION"})
		for _, r := range verboseRows {
			tw.AppendRow(table.Row{r.Kind, r.Name, r.Client, r.Path, r.Action})
		}
		tw.Render()
	} else if len(verboseRows) > 0 {
		fmt.Fprintf(w, "\n%d items. Full listing: --verbose or --format json.\n", len(verboseRows))
	}
}

// renderResetResult prints the final summary with the honest no-restore
// wording and the forward recovery path.
func renderResetResult(w *os.File, doc *resetops.Doc) {
	removed := 0
	for _, r := range doc.Rows {
		switch r.Action {
		case resetops.ActionRemoved, resetops.ActionStopped, "removed-file", "removed-region":
			removed++
		}
	}
	fmt.Fprintln(w)
	if doc.Failed > 0 {
		fmt.Fprintf(w, "%d removed · %d failed. Reset is idempotent; run it again to retry the failures.\n", removed, doc.Failed)
	} else {
		fmt.Fprintf(w, "%d removed.\n", removed)
	}
	if len(doc.Kept) > 0 {
		fmt.Fprintf(w, "Kept (hand-edited or foreign): %s\n", strings.Join(doc.Kept, ", "))
	}
	if doc.BackupPath != "" {
		fmt.Fprintf(w, "\nBackup saved: %s\n", doc.BackupPath)
		fmt.Fprintln(w, "This is a safety copy, not an undo. gridctl has no restore command.")
		fmt.Fprintln(w, "To recover, re-run the imports that created this state (gridctl apply,")
		fmt.Fprintln(w, "gridctl pack add, gridctl link), or copy files back by hand; see")
		fmt.Fprintln(w, "docs/troubleshooting.md#recovering-from-reset.")
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func clientSummary(perClient map[string]int) string {
	keys := sortedKeys(perClient)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, perClient[k]))
	}
	return strings.Join(parts, ", ")
}

func countOrUnknown(n int) string {
	if n < 0 {
		return "unknown (vault locked)"
	}
	return fmt.Sprintf("%d", n)
}
