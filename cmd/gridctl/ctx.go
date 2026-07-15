package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/output"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// Exit codes follow the pins/optimize/validate convention.
const (
	ctxExitOK             = 0
	ctxExitAttention      = 1
	ctxExitInfrastructure = 2
)

// ctxJSONSchemaVersion identifies the shape of the ctx JSON documents.
// Evolution within a version is append-only.
const ctxJSONSchemaVersion = 1

var (
	ctxInitImport   string
	ctxInitFrom     string
	ctxInitTemplate bool
	ctxInitForce    bool

	ctxStatusFormat string
	ctxStatusJSON   *bool
	ctxStatusPlain  *bool

	ctxSyncAll    bool
	ctxSyncDryRun bool
	ctxSyncCheck  bool
	ctxSyncForce  bool
	ctxSyncFormat string
	ctxSyncJSON   *bool
	ctxSyncPlain  *bool

	ctxUnsyncAll    bool
	ctxUnsyncFormat string
	ctxUnsyncJSON   *bool
)

var ctxCmd = &cobra.Command{
	Use:   "ctx",
	Short: "Manage the global agent context across linked clients",
	Long: `Manage one canonical global agent-context file (AGENTS.md) and sync it
to every linked client's global context location.

The canonical file lives at ~/.gridctl/context/AGENTS.md. Each client
receives it through the safest mechanism it supports: a dedicated file in
its rules directory, an @-import line, or a marker-delimited managed
block. Content outside the managed region is never touched.

Per-project AGENTS.md files stay version-controlled in each repo; ctx
manages only the global layer.`,
	Example: `  gridctl ctx init                   Scan clients and bootstrap the canonical file
  gridctl ctx init --import claude-code   Adopt an existing CLAUDE.md as canon
  gridctl ctx sync --dry-run         Preview what a sync would change
  gridctl ctx sync                   Sync every available client
  gridctl ctx status                 Per-client sync state
  gridctl ctx adopt gemini           Pull a hand edit back into the canon`,
}

var ctxInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Bootstrap the canonical global context file",
	Long: `Scans every supported client's global context location and reports what
exists. Nothing is written during the scan.

With --import, an existing client file becomes the canonical context.
With --from, an arbitrary file does. With --template (or when no client
has an existing file), a short commented starter is scaffolded.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		return runCtxInit(os.Stdout, mgr, ctxInitImport, ctxInitFrom, ctxInitTemplate, ctxInitForce)
	},
}

var ctxStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show per-client sync state",
	Long: `Shows every client's global context sync state.

States: in-sync, stale (canonical changed since last sync), drifted
(target was hand-edited), target-missing, never-synced, unsupported.

Exit codes:
  0  everything clean
  1  drift, staleness, or a missing target detected
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxStatusFormat, cmd.Flags().Changed("format"), *ctxStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*ctxStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := contexts.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runCtxStatus(cmd.Context(), os.Stdout, os.Stderr, mgr, format, *ctxStatusPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var ctxSyncCmd = &cobra.Command{
	Use:   "sync [client...]",
	Short: "Project the canonical context into client files",
	Long: `Projects the canonical global context into each client's global context
location. With no arguments every available client is synced; name
clients to sync a subset.

A drifted target (hand-edited since the last sync) is skipped with a
warning; use --force to overwrite it, or 'gridctl ctx adopt' to pull the
edit back into the canon instead. Every write is preceded by a
timestamped backup.

Exit codes:
  0  synced cleanly
  1  drift skipped, sync failed for a client, or --check found pending work
  2  infrastructure error`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxSyncFormat, cmd.Flags().Changed("format"), *ctxSyncJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*ctxSyncPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if ctxSyncAll && len(args) > 0 {
			fmt.Fprintln(os.Stderr, "cannot combine --all with named clients")
			os.Exit(ctxExitInfrastructure)
		}
		if ctxSyncCheck && (len(args) > 0 || ctxSyncForce || ctxSyncDryRun) {
			fmt.Fprintln(os.Stderr, "--check inspects all clients and performs no writes; it cannot be combined with named clients, --force, or --dry-run")
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := contexts.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		var exit int
		if ctxSyncCheck {
			exit = runCtxCheck(cmd.Context(), os.Stdout, os.Stderr, mgr, format)
		} else {
			opts := contexts.SyncOptions{Force: ctxSyncForce, DryRun: ctxSyncDryRun}
			exit = runCtxSync(cmd.Context(), os.Stdout, os.Stderr, mgr, args, opts, format, *ctxSyncPlain)
		}
		if exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var ctxDiffCmd = &cobra.Command{
	Use:   "diff <client>",
	Short: "Diff the canonical context against a client's managed content",
	Long: `Shows a unified diff between the canonical context and the managed
content currently in one client's file.

Exit codes: 0 no differences, 1 differences found, 2 error.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runCtxDiff(cmd.Context(), os.Stdout, os.Stderr, mgr, args[0]); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var ctxAdoptCmd = &cobra.Command{
	Use:   "adopt <client>",
	Short: "Pull a client's hand edit back into the canonical context",
	Long: `Adopts the managed content currently in a client's file as the new
canonical context, then re-syncs that client. Other synced clients
become stale until the next 'gridctl ctx sync'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		return runCtxAdopt(cmd.Context(), os.Stdout, mgr, args[0])
	},
}

var ctxUnsyncCmd = &cobra.Command{
	Use:   "unsync [client...]",
	Short: "Remove gridctl-managed context from client files",
	Long: `Removes the managed artifact from client files: dedicated files are
deleted, shim lines and managed blocks are stripped, and files gridctl
created are removed entirely. Content the user owns is preserved.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxUnsyncFormat, cmd.Flags().Changed("format"), *ctxUnsyncJSON)
		if err != nil {
			return err
		}
		mgr, merr := contexts.NewManager()
		if merr != nil {
			return merr
		}
		return runCtxUnsync(cmd.Context(), os.Stdout, mgr, args, ctxUnsyncAll, format)
	},
}

var ctxEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit the canonical context in $EDITOR",
	Long: `Opens the canonical global context file in $VISUAL or $EDITOR. After
the editor exits, the per-client sync state is printed; run
'gridctl ctx sync' to propagate changes.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		return runCtxEdit(cmd.Context(), os.Stdout, os.Stderr, mgr)
	},
}

func init() {
	ctxInitCmd.Flags().StringVar(&ctxInitImport, "import", "", "Adopt an existing client file as the canonical context (client slug)")
	ctxInitCmd.Flags().StringVar(&ctxInitFrom, "from", "", "Adopt an arbitrary file as the canonical context (path)")
	ctxInitCmd.Flags().BoolVar(&ctxInitTemplate, "template", false, "Scaffold the starter template even when client files exist")
	ctxInitCmd.Flags().BoolVar(&ctxInitForce, "force", false, "Overwrite an existing canonical file")

	ctxStatusCmd.Flags().StringVar(&ctxStatusFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	ctxStatusJSON = addJSONAlias(ctxStatusCmd)
	ctxStatusPlain = addPlainFlag(ctxStatusCmd)

	ctxSyncCmd.Flags().BoolVar(&ctxSyncAll, "all", false, "Sync every available client (the default when no client is named)")
	ctxSyncCmd.Flags().BoolVar(&ctxSyncDryRun, "dry-run", false, "Show what would change without writing")
	ctxSyncCmd.Flags().BoolVar(&ctxSyncCheck, "check", false, "CI mode: no writes, exit 1 on drift or pending sync")
	ctxSyncCmd.Flags().BoolVar(&ctxSyncForce, "force", false, "Overwrite drifted targets and repair corrupt managed blocks")
	ctxSyncCmd.Flags().StringVar(&ctxSyncFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	ctxSyncJSON = addJSONAlias(ctxSyncCmd)
	ctxSyncPlain = addPlainFlag(ctxSyncCmd)

	ctxUnsyncCmd.Flags().BoolVar(&ctxUnsyncAll, "all", false, "Unsync every synced client")
	ctxUnsyncCmd.Flags().StringVar(&ctxUnsyncFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	ctxUnsyncJSON = addJSONAlias(ctxUnsyncCmd)

	ctxCmd.AddCommand(ctxInitCmd)
	ctxCmd.AddCommand(ctxStatusCmd)
	ctxCmd.AddCommand(ctxSyncCmd)
	ctxCmd.AddCommand(ctxDiffCmd)
	ctxCmd.AddCommand(ctxAdoptCmd)
	ctxCmd.AddCommand(ctxUnsyncCmd)
	ctxCmd.AddCommand(ctxEditCmd)
}

// runCtxInit implements `ctx init`. The scan always runs and never
// writes; only an explicit source choice (or a clean slate) scaffolds.
func runCtxInit(w io.Writer, mgr *contexts.Manager, importSlug, fromPath string, useTemplate, force bool) error {
	if importSlug != "" && fromPath != "" {
		return fmt.Errorf("--import and --from are mutually exclusive")
	}

	printer := output.NewWithWriter(w)
	entries := mgr.Scan()
	existing := 0
	fmt.Fprintln(w, "Existing global context files:")
	for _, e := range entries {
		if e.Exists {
			existing++
			fmt.Fprintf(w, "  %-14s %s (%d bytes)\n", e.Slug, e.Path, e.Size)
		}
	}
	if existing == 0 {
		fmt.Fprintln(w, "  (none found)")
	}
	fmt.Fprintln(w)

	switch {
	case importSlug != "":
		if err := mgr.InitFromClient(importSlug, force); err != nil {
			return err
		}
		printer.Info("Imported " + importSlug + " global context as " + mgr.CanonicalPath())
	case fromPath != "":
		if err := mgr.InitFromFile(fromPath, force); err != nil {
			return err
		}
		printer.Info("Imported " + fromPath + " as " + mgr.CanonicalPath())
	case useTemplate || existing == 0:
		if err := mgr.InitFromTemplate(force); err != nil {
			return err
		}
		printer.Info("Wrote starter template to " + mgr.CanonicalPath())
		fmt.Fprintln(w, "\nThe template is a draft. Trim it to durable cross-project preferences.")
	default:
		fmt.Fprintln(w, "Found existing files. Choose a source before anything is written:")
		fmt.Fprintln(w, "  gridctl ctx init --import <client>   adopt one client's file as canon")
		fmt.Fprintln(w, "  gridctl ctx init --from <path>       adopt an arbitrary file")
		fmt.Fprintln(w, "  gridctl ctx init --template          start fresh from the starter template")
		return nil
	}

	fmt.Fprintln(w, "\nNext steps:")
	fmt.Fprintln(w, "  1. gridctl ctx edit                Review the canonical file")
	fmt.Fprintln(w, "  2. gridctl ctx sync --dry-run      Preview per-client changes")
	fmt.Fprintln(w, "  3. gridctl ctx sync                Propagate to available clients")
	return nil
}

// ctxCanonicalDoc describes the canonical file in JSON documents.
type ctxCanonicalDoc struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// ctxStatusDoc is the machine-readable `ctx status --format json` document.
type ctxStatusDoc struct {
	SchemaVersion int                     `json:"schema_version"`
	Canonical     ctxCanonicalDoc         `json:"canonical"`
	NeedsSync     bool                    `json:"needs_sync"`
	Clients       []contexts.ClientStatus `json:"clients"`
}

// runCtxStatus renders per-client state and returns the exit code.
func runCtxStatus(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, format string, plain bool) int {
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := ctxStatusDoc{
		SchemaVersion: ctxJSONSchemaVersion,
		Canonical:     ctxCanonicalDoc{Path: mgr.CanonicalPath(), Exists: mgr.HasCanonical()},
		NeedsSync:     contexts.NeedsSync(statuses),
		Clients:       statuses,
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if !doc.Canonical.Exists {
			fmt.Fprintf(stdout, "No canonical context file yet. Run 'gridctl ctx init' to create one.\n\n")
		} else {
			fmt.Fprintf(stdout, "Canonical: %s\n\n", doc.Canonical.Path)
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"CLIENT", "STRATEGY", "STATE", "TARGET"})
		for _, cs := range statuses {
			t.AppendRow(table.Row{cs.Slug, ctxStrategyLabel(cs), ctxStateLabel(cs), ctxTargetLabel(cs)})
		}
		t.Render()
	}

	if doc.NeedsSync {
		return ctxExitAttention
	}
	return ctxExitOK
}

// ctxSyncDoc is the machine-readable `ctx sync --format json` document.
type ctxSyncDoc struct {
	SchemaVersion int                   `json:"schema_version"`
	DryRun        bool                  `json:"dry_run"`
	HasFailures   bool                  `json:"has_failures"`
	Results       []contexts.SyncResult `json:"results"`
}

// runCtxSync performs the sync and returns the exit code.
func runCtxSync(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, args []string, opts contexts.SyncOptions, format string, plain bool) int {
	var results []contexts.SyncResult
	if len(args) == 0 {
		all, err := mgr.SyncAll(ctx, opts)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
		results = all
	} else {
		for _, slug := range args {
			res, err := mgr.SyncClient(ctx, slug, opts)
			if err != nil {
				// Usage and infrastructure mistakes abort (a typo must not
				// pass CI); a per-client runtime failure becomes an error
				// row so results already written are still reported.
				if errors.Is(err, contexts.ErrUnknownClient) || errors.Is(err, contexts.ErrUnsupported) ||
					errors.Is(err, contexts.ErrNoCanonical) || errors.Is(err, contexts.ErrNewerLockVersion) {
					fmt.Fprintln(stderr, err)
					return ctxExitInfrastructure
				}
				res = contexts.SyncResult{Slug: slug, Name: slug, Action: contexts.ActionError, Error: err.Error()}
			}
			results = append(results, res)
		}
	}

	doc := ctxSyncDoc{
		SchemaVersion: ctxJSONSchemaVersion,
		DryRun:        opts.DryRun,
		HasFailures:   contexts.HasFailures(results),
		Results:       results,
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"CLIENT", "STRATEGY", "ACTION", "TARGET"})
		for _, r := range results {
			t.AppendRow(table.Row{r.Slug, r.Strategy, ctxActionLabel(r), r.TargetPath})
		}
		t.Render()
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(stdout, "\n%s: %s\n", r.Slug, r.Error)
			}
			if r.Action == contexts.ActionSkippedDrift && r.Error == "" {
				fmt.Fprintf(stdout, "\n%s: target was hand-edited. Inspect with 'gridctl ctx diff %s', keep the edit with 'gridctl ctx adopt %s', or overwrite with 'gridctl ctx sync --force %s'\n", r.Slug, r.Slug, r.Slug, r.Slug)
			}
			if opts.DryRun && r.Diff != "" {
				fmt.Fprintf(stdout, "\n--- %s ---\n%s", r.Slug, r.Diff)
			}
		}
	}

	if doc.HasFailures {
		return ctxExitAttention
	}
	return ctxExitOK
}

// runCtxCheck implements `ctx sync --check`: CI mode, no writes. The
// JSON mode is exactly the status document, so it delegates.
func runCtxCheck(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, format string) int {
	if strings.EqualFold(format, "json") {
		return runCtxStatus(ctx, stdout, stderr, mgr, format, false)
	}
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	needs := contexts.NeedsSync(statuses)
	for _, cs := range statuses {
		switch cs.State {
		case contexts.StateDrifted, contexts.StateStale, contexts.StateTargetMissing:
			fmt.Fprintf(stdout, "  ✗ %-14s %s\n", cs.Slug, cs.State)
		}
	}
	if !needs {
		fmt.Fprintln(stdout, "All synced clients are clean.")
		return ctxExitOK
	}
	return ctxExitAttention
}

// runCtxDiff prints the diff and returns 0 (clean), 1 (differs), 2 (error).
func runCtxDiff(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, slug string) int {
	diff, err := mgr.Diff(ctx, slug)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	if diff == "" {
		fmt.Fprintf(stdout, "%s matches the canonical context.\n", slug)
		return ctxExitOK
	}
	fmt.Fprint(stdout, diff)
	return ctxExitAttention
}

// runCtxAdopt implements `ctx adopt <client>`.
func runCtxAdopt(ctx context.Context, w io.Writer, mgr *contexts.Manager, slug string) error {
	if err := mgr.Adopt(ctx, slug); err != nil {
		return err
	}
	fmt.Fprintf(w, "✓ Adopted %s's managed content into %s\n", slug, mgr.CanonicalPath())
	fmt.Fprintln(w, "Other synced clients are now stale; run 'gridctl ctx sync' to propagate.")
	return nil
}

// ctxUnsyncDoc is the machine-readable `ctx unsync --format json` document.
type ctxUnsyncDoc struct {
	SchemaVersion int                     `json:"schema_version"`
	Results       []contexts.UnsyncResult `json:"results"`
}

// runCtxUnsync implements `ctx unsync`.
func runCtxUnsync(ctx context.Context, w io.Writer, mgr *contexts.Manager, args []string, all bool, format string) error {
	if len(args) == 0 && !all {
		return fmt.Errorf("name at least one client or pass --all (known clients: %s)", strings.Join(contexts.SupportedSlugs(), ", "))
	}
	var results []contexts.UnsyncResult
	if all {
		rs, err := mgr.UnsyncAll(ctx)
		if err != nil {
			return err
		}
		results = rs
	} else {
		for _, slug := range args {
			res, err := mgr.Unsync(ctx, slug)
			if err != nil {
				return err
			}
			results = append(results, res)
		}
	}
	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(w, ctxUnsyncDoc{SchemaVersion: ctxJSONSchemaVersion, Results: results})
	}
	if len(results) == 0 {
		fmt.Fprintln(w, "Nothing to unsync.")
		return nil
	}
	for _, r := range results {
		fmt.Fprintf(w, "✓ %-14s %s (%s)\n", r.Slug, r.TargetPath, r.Action)
	}
	return nil
}

// runCtxEdit opens the canonical file in the user's editor, then prints
// the resulting per-client state.
func runCtxEdit(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager) error {
	if !mgr.HasCanonical() {
		return contexts.ErrNoCanonical
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("neither $VISUAL nor $EDITOR is set; edit %s directly or use the web UI", mgr.CanonicalPath())
	}
	cmd := exec.CommandContext(ctx, editor, mgr.CanonicalPath()) // #nosec G204 -- the user's own $EDITOR, same trust domain as the shell
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}
	fmt.Fprintln(stdout)
	runCtxStatus(ctx, stdout, stderr, mgr, "", false)
	fmt.Fprintln(stdout, "\nRun 'gridctl ctx sync' to propagate changes.")
	return nil
}

// ctxStateLabel renders a status glyph + state, with detail for the
// states a user must act on.
func ctxStateLabel(cs contexts.ClientStatus) string {
	label := cs.State
	if cs.Experimental && cs.Supported {
		label += " (experimental)"
	}
	switch cs.State {
	case contexts.StateInSync:
		return "✓ " + label
	case contexts.StateDrifted, contexts.StateTargetMissing:
		return "✗ " + label
	case contexts.StateStale:
		return "~ " + label
	default:
		return "— " + label
	}
}

// ctxStrategyLabel is empty for unsupported clients instead of a bogus value.
func ctxStrategyLabel(cs contexts.ClientStatus) string {
	if !cs.Supported {
		return ""
	}
	return cs.Strategy
}

// ctxTargetLabel shows the target path, or the reason for unsupported
// clients so the table explains itself.
func ctxTargetLabel(cs contexts.ClientStatus) string {
	if !cs.Supported {
		return cs.Detail
	}
	if !cs.Available && cs.State == contexts.StateNeverSynced {
		return cs.TargetPath + " (client not detected)"
	}
	return cs.TargetPath
}

// ctxActionLabel decorates sync actions with glyphs.
func ctxActionLabel(r contexts.SyncResult) string {
	switch r.Action {
	case contexts.ActionCreated, contexts.ActionUpdated, contexts.ActionUnchanged:
		return "✓ " + r.Action
	case contexts.ActionSkippedDrift, contexts.ActionError:
		return "✗ " + r.Action
	default:
		return "— " + r.Action
	}
}

