package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

	ctxListFormat string
	ctxListJSON   *bool

	ctxAdoptInto string
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

Optionally, 'ctx add <name>' activates fragments mode: the store becomes
~/.gridctl/context/fragments/*.md (optional description/paths frontmatter),
composed per client as multi-file passthrough or a compiled document.
Composition order is filename-lexicographic; numeric prefixes (00-, 10-)
are recommended. Fragments mode is strictly opt-in.

Per-project AGENTS.md files stay version-controlled in each repo; ctx
manages only the global layer.`,
	Example: `  gridctl ctx init                   Scan clients and bootstrap the canonical file
  gridctl ctx init --import claude-code   Adopt an existing CLAUDE.md as canon
  gridctl ctx sync --dry-run         Preview what a sync would change
  gridctl ctx sync                   Sync every available client
  gridctl ctx status                 Per-client sync state
  gridctl ctx add style              Opt into fragments; migrate AGENTS.md
  gridctl ctx list                   List fragments (fragments mode)
  gridctl ctx adopt gemini           Pull a hand edit back into the canon`,
}

var ctxInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Bootstrap the canonical global context file",
	Long: `Scans every supported client's global context location and reports what
exists. Nothing is written during the scan.

With --import, an existing client file becomes the canonical context.
With --from, an arbitrary file does. With --template (or when no client
has an existing file), a short commented starter is scaffolded.

This bootstraps the single-file store; 'gridctl ctx add <name>' later
converts it into a fragment library.`,
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

In fragments mode each row also carries a mode: multi-file (one file per
fragment in the client's rules directory) or compiled (all fragments
assembled into the client's single target).

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
	Use:   "diff <client> [fragment]",
	Short: "Diff the canonical context against a client's managed content",
	Long: `Shows a unified diff between the canonical context and the managed
content currently in one client's file. In fragments mode, an optional
fragment name scopes a multi-file client; bare multi-file diff prints a
per-fragment summary.

Exit codes: 0 no differences, 1 differences found, 2 error.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		frag := ""
		if len(args) > 1 {
			frag = args[1]
		}
		if exit := runCtxDiff(cmd.Context(), os.Stdout, os.Stderr, mgr, args[0], frag); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var ctxAdoptCmd = &cobra.Command{
	Use:   "adopt <client> [fragment]",
	Short: "Pull a client's hand edit back into the canonical context",
	Long: `Adopts the managed content currently in a client's file as the new
canonical context, then re-syncs that client. Other synced clients
become stale until the next 'gridctl ctx sync'.

In fragments mode:
  multi-file clients  require a fragment name (lossless per-file adopt)
  compiled clients    refuse by default; pass --into <fragment> to capture
                      the whole managed body into one designated fragment`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		frag := ""
		if len(args) > 1 {
			frag = args[1]
		}
		return runCtxAdopt(cmd.Context(), os.Stdout, mgr, args[0], frag, ctxAdoptInto)
	},
}

var ctxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List rule fragments (fragments mode)",
	Long: `Lists every fragment in filename-lexicographic composition order with
description, paths globs, and size. Requires fragments mode
('gridctl ctx add <name>' activates it).

Exit codes: 0 success, 2 error.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(ctxListFormat, cmd.Flags().Changed("format"), *ctxListJSON)
		if err != nil {
			return err
		}
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		return runCtxList(os.Stdout, mgr, format)
	},
}

var ctxAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a rule fragment (activates fragments mode on first use)",
	Long: `Creates a fragment under ~/.gridctl/context/fragments/<name>.md.
On first use, activates fragments mode: the existing AGENTS.md is backed
up and becomes fragments/00-default.md (an ordinary fragment thereafter).
Composition order is filename-lexicographic; numeric prefixes are
recommended.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		return runCtxAdd(os.Stdout, mgr, args[0])
	},
}

var ctxRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a rule fragment",
	Long: `Deletes a fragment after writing a backup under
~/.gridctl/project-backups/context/. Projected client files for the
fragment are removed on the next 'gridctl ctx sync'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		return runCtxRm(os.Stdout, mgr, args[0])
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
	Use:   "edit [fragment]",
	Short: "Edit the canonical context (or a fragment) in $EDITOR",
	Long: `Opens the canonical global context file in $VISUAL or $EDITOR. After
the editor exits, the per-client sync state is printed; run
'gridctl ctx sync' to propagate changes.

In fragments mode a fragment name is required; bare 'ctx edit' lists
available fragments.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := contexts.NewManager()
		if err != nil {
			return err
		}
		frag := ""
		if len(args) == 1 {
			frag = args[0]
		}
		return runCtxEdit(cmd.Context(), os.Stdout, os.Stderr, mgr, frag)
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

	ctxListCmd.Flags().StringVar(&ctxListFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	ctxListJSON = addJSONAlias(ctxListCmd)

	ctxAdoptCmd.Flags().StringVar(&ctxAdoptInto, "into", "", "In fragments mode, capture a compiled target into this fragment name")

	ctxCmd.AddCommand(ctxInitCmd)
	ctxCmd.AddCommand(ctxStatusCmd)
	ctxCmd.AddCommand(ctxSyncCmd)
	ctxCmd.AddCommand(ctxDiffCmd)
	ctxCmd.AddCommand(ctxAdoptCmd)
	ctxCmd.AddCommand(ctxUnsyncCmd)
	ctxCmd.AddCommand(ctxEditCmd)
	ctxCmd.AddCommand(ctxListCmd)
	ctxCmd.AddCommand(ctxAddCmd)
	ctxCmd.AddCommand(ctxRmCmd)
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
	Fragments     bool                    `json:"fragments,omitempty"`
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
	fragmentsActive := mgr.FragmentsActive()
	doc := ctxStatusDoc{
		SchemaVersion: ctxJSONSchemaVersion,
		Canonical:     ctxCanonicalDoc{Path: mgr.CanonicalPath(), Exists: mgr.HasCanonical()},
		Fragments:     fragmentsActive,
		NeedsSync:     contexts.NeedsSync(statuses),
		Clients:       statuses,
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		switch {
		case fragmentsActive:
			fmt.Fprintf(stdout, "Fragments: %s (composition order is filename-lexicographic)\n\n", mgr.FragmentsDir())
		case !doc.Canonical.Exists:
			fmt.Fprintf(stdout, "No canonical context file yet. Run 'gridctl ctx init' to create one.\n\n")
		default:
			fmt.Fprintf(stdout, "Canonical: %s\n\n", doc.Canonical.Path)
		}
		t := output.NewTableWriter(stdout, plain)
		if fragmentsActive {
			t.AppendHeader(table.Row{"CLIENT", "MODE", "STRATEGY", "STATE", "TARGET"})
			for _, cs := range statuses {
				t.AppendRow(table.Row{cs.Slug, cs.Mode, ctxStrategyLabel(cs), ctxStateLabel(cs), ctxTargetLabel(cs)})
			}
		} else {
			t.AppendHeader(table.Row{"CLIENT", "STRATEGY", "STATE", "TARGET"})
			for _, cs := range statuses {
				t.AppendRow(table.Row{cs.Slug, ctxStrategyLabel(cs), ctxStateLabel(cs), ctxTargetLabel(cs)})
			}
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
			rs, err := mgr.SyncClientDetailed(ctx, slug, opts)
			if err != nil {
				// Usage and infrastructure mistakes abort (a typo must not
				// pass CI); a per-client runtime failure becomes an error
				// row so results already written are still reported.
				if errors.Is(err, contexts.ErrUnknownClient) || errors.Is(err, contexts.ErrUnsupported) ||
					errors.Is(err, contexts.ErrNoCanonical) || errors.Is(err, contexts.ErrNewerLockVersion) {
					fmt.Fprintln(stderr, err)
					return ctxExitInfrastructure
				}
				rs = []contexts.SyncResult{{Slug: slug, Name: slug, Action: contexts.ActionError, Error: err.Error()}}
			}
			results = append(results, rs...)
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
		// Extra columns only when fragments mode produced them, so
		// single-file output stays byte-identical to pre-fragments.
		showFragments := false
		for _, r := range results {
			if r.Mode != "" || r.Fragment != "" {
				showFragments = true
				break
			}
		}
		t := output.NewTableWriter(stdout, plain)
		if showFragments {
			t.AppendHeader(table.Row{"CLIENT", "MODE", "FRAGMENT", "STRATEGY", "ACTION", "TARGET"})
			for _, r := range results {
				t.AppendRow(table.Row{r.Slug, r.Mode, r.Fragment, r.Strategy, ctxActionLabel(r), r.TargetPath})
			}
		} else {
			t.AppendHeader(table.Row{"CLIENT", "STRATEGY", "ACTION", "TARGET"})
			for _, r := range results {
				t.AppendRow(table.Row{r.Slug, r.Strategy, ctxActionLabel(r), r.TargetPath})
			}
		}
		t.Render()
		for _, r := range results {
			label := r.Slug
			if r.Fragment != "" {
				label = r.Slug + "/" + r.Fragment
			}
			if r.Error != "" {
				fmt.Fprintf(stdout, "\n%s: %s\n", label, r.Error)
			}
			if r.Detail != "" && r.Action != contexts.ActionError {
				fmt.Fprintf(stdout, "\n%s: %s\n", label, r.Detail)
			}
			if r.Action == contexts.ActionSkippedDrift && r.Error == "" {
				fmt.Fprintf(stdout, "\n%s: target was hand-edited. Inspect with 'gridctl ctx diff %s', keep the edit with 'gridctl ctx adopt %s', or overwrite with 'gridctl ctx sync --force %s'\n", r.Slug, r.Slug, r.Slug, r.Slug)
			}
			if opts.DryRun && r.Diff != "" {
				fmt.Fprintf(stdout, "\n--- %s ---\n%s", label, r.Diff)
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
func runCtxDiff(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, slug, fragment string) int {
	var diff string
	var err error
	if fragment != "" {
		diff, err = mgr.Diff(ctx, slug, fragment)
	} else {
		diff, err = mgr.Diff(ctx, slug)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	if diff == "" {
		fmt.Fprintf(stdout, "%s matches the canonical context.\n", slug)
		return ctxExitOK
	}
	// Multi-file summary lines are not empty diffs; treat "all in-sync" as clean.
	if fragment == "" && isMultiFileInSyncSummary(diff) {
		fmt.Fprint(stdout, diff)
		return ctxExitOK
	}
	fmt.Fprint(stdout, diff)
	return ctxExitAttention
}

// isMultiFileInSyncSummary reports a bare multi-file summary with no
// missing/differs lines.
func isMultiFileInSyncSummary(diff string) bool {
	if !strings.Contains(diff, ": in-sync") {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(diff), "\n") {
		if strings.HasSuffix(line, ": missing") || strings.HasSuffix(line, ": differs") {
			return false
		}
	}
	return true
}

// runCtxAdopt implements `ctx adopt <client> [fragment]`.
func runCtxAdopt(ctx context.Context, w io.Writer, mgr *contexts.Manager, slug, fragment, into string) error {
	if into != "" && fragment != "" {
		return fmt.Errorf("pass either a fragment name or --into, not both")
	}
	if into != "" {
		if err := mgr.AdoptInto(ctx, slug, into); err != nil {
			return err
		}
		fmt.Fprintf(w, "✓ Captured %s's managed content into fragment %q and re-projected it\n", slug, into)
		fmt.Fprintln(w, "Other synced clients may be stale; run 'gridctl ctx sync' to propagate.")
		return nil
	}
	if fragment != "" {
		if err := mgr.AdoptFragment(ctx, slug, fragment); err != nil {
			return err
		}
		fmt.Fprintf(w, "✓ Adopted %s's %s into fragment %q\n", slug, fragment, fragment)
		fmt.Fprintln(w, "Other synced clients may be stale; run 'gridctl ctx sync' to propagate.")
		return nil
	}
	if err := mgr.Adopt(ctx, slug); err != nil {
		return err
	}
	fmt.Fprintf(w, "✓ Adopted %s's managed content into %s\n", slug, mgr.CanonicalPath())
	fmt.Fprintln(w, "Other synced clients are now stale; run 'gridctl ctx sync' to propagate.")
	return nil
}

// ctxFragmentRow is one fragment in `ctx list --format json`.
type ctxFragmentRow struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Bytes       int      `json:"bytes"`
	Position    int      `json:"position"`
}

// ctxListDoc is the machine-readable `ctx list` document.
type ctxListDoc struct {
	SchemaVersion int              `json:"schema_version"`
	Fragments     []ctxFragmentRow `json:"fragments"`
}

// runCtxList lists fragments in composition order.
func runCtxList(w io.Writer, mgr *contexts.Manager, format string) error {
	frags, err := mgr.ListFragments()
	if err != nil {
		return err
	}
	rows := make([]ctxFragmentRow, 0, len(frags))
	for i, f := range frags {
		rows = append(rows, ctxFragmentRow{
			Name:        f.Name,
			Description: f.Description,
			Paths:       f.Paths,
			Bytes:       len(f.Raw),
			Position:    i + 1,
		})
	}
	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(w, ctxListDoc{SchemaVersion: ctxJSONSchemaVersion, Fragments: rows})
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "No fragments yet. Add one with 'gridctl ctx add <name>'.")
		return nil
	}
	t := output.NewTableWriter(w, false)
	t.AppendHeader(table.Row{"#", "NAME", "DESCRIPTION", "PATHS", "BYTES"})
	for _, r := range rows {
		paths := strings.Join(r.Paths, ", ")
		t.AppendRow(table.Row{r.Position, r.Name, r.Description, paths, r.Bytes})
	}
	t.Render()
	return nil
}

// runCtxAdd creates a fragment, printing migration when it activates the mode.
func runCtxAdd(w io.Writer, mgr *contexts.Manager, name string) error {
	res, err := mgr.AddFragment(name, "")
	if err != nil {
		return err
	}
	if res.Migrated {
		fmt.Fprintf(w, "Activated fragments mode: migrated %s → fragments/00-default.md\n", mgr.CanonicalPath())
		if res.MigratedBackup != "" {
			fmt.Fprintf(w, "Backup: %s\n", res.MigratedBackup)
		}
	}
	fmt.Fprintf(w, "✓ Created fragment %q at %s\n", name, res.CreatedPath)
	fmt.Fprintln(w, "Composition order is filename-lexicographic; use numeric prefixes (00-, 10-) to control order.")
	fmt.Fprintln(w, "Run 'gridctl ctx sync' to project fragments to available clients.")
	return nil
}

// runCtxRm removes a fragment.
func runCtxRm(w io.Writer, mgr *contexts.Manager, name string) error {
	backup, err := mgr.RemoveFragment(name)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "✓ Removed fragment %q (backup: %s)\n", name, backup)
	fmt.Fprintln(w, "Run 'gridctl ctx sync' to drop projected client files for this fragment.")
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
			rs, err := mgr.Unsync(ctx, slug)
			if err != nil {
				return err
			}
			results = append(results, rs...)
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

// runCtxEdit opens the canonical file (or a fragment) in the user's editor.
func runCtxEdit(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, fragment string) error {
	path := mgr.CanonicalPath()
	if mgr.FragmentsActive() {
		if fragment == "" {
			frags, err := mgr.ListFragments()
			if err != nil {
				return err
			}
			names := make([]string, 0, len(frags))
			for _, f := range frags {
				names = append(names, f.Name)
			}
			return fmt.Errorf("fragments mode is active; pass a fragment name (known: %s)", strings.Join(names, ", "))
		}
		f, err := mgr.ReadFragment(fragment)
		if err != nil {
			return err
		}
		path = filepath.Join(mgr.FragmentsDir(), f.FileName)
	} else if !mgr.HasCanonical() {
		return contexts.ErrNoCanonical
	} else if fragment != "" {
		return fmt.Errorf("fragments mode is not active; create one with 'gridctl ctx add %s' or omit the name", fragment)
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("neither $VISUAL nor $EDITOR is set; edit %s directly or use the web UI", path)
	}
	cmd := exec.CommandContext(ctx, editor, path) // #nosec G204 -- the user's own $EDITOR, same trust domain as the shell
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

