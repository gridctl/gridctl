package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/wiring"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// projectJSONSchemaVersion versions every `gridctl project` JSON
// document (Article X).
const projectJSONSchemaVersion = 1

// projectKindWiring is the only kind served by the top-level project
// verb today. The dispatch is structured so skill, agent, and context
// migrate under it later without reshaping the CLI.
const projectKindWiring = "wiring"

// validTopProjectKind rejects unknown --kind values before any manager
// is built.
func validTopProjectKind(kind string) error {
	if kind == projectKindWiring {
		return nil
	}
	return fmt.Errorf("unknown --kind %q (supported: wiring; skill and agent live under 'gridctl skill project' for now)", kind)
}

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage recorded projections (wiring ownership)",
	Long: `Manages gridctl's recorded projections. The wiring kind records
ownership of the gateway entries 'gridctl link' merges into client MCP
configs: every link stores the config path, entry name, and a canonical
hash of the written value in ~/.gridctl/project.lock.yaml, so unlink,
drift detection, and adoption are decided from recorded state instead
of guessing from entry shape.

'gridctl link' and 'gridctl unlink' remain the everyday verbs; they
record ownership through the same machinery. Skill and agent
projections stay under 'gridctl skill project' for now.`,
	Example: `  gridctl project sync --kind wiring                Link every detected client
  gridctl project status --kind wiring              Per-entry ownership state
  gridctl project adopt --kind wiring --client cursor   Take ownership of an edit
  gridctl project unsync --kind wiring --client cursor  Remove the entry and record`,
}

var (
	projectSyncKind     string
	projectSyncClients  []string
	projectSyncName     string
	projectSyncGroup    string
	projectSyncClientID string
	projectSyncPort     int
	projectSyncForce    bool
	projectSyncDryRun   bool
	projectSyncFormat   string
	projectSyncJSON     *bool
	projectSyncPlain    *bool
)

var projectSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Link detected clients with ownership recorded",
	Long: `Links every detected client (or --clients subsets) to the gateway and
records ownership of each written entry. An entry gridctl never
recorded is skipped unless its value already matches what gridctl would
write (then it is adopted silently) or --force is given. A hand-edited
recorded entry is skipped with an adopt/--force hint.

Exit codes:
  0  synced cleanly
  1  an entry was skipped (foreign or drifted) or failed
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(projectSyncFormat, cmd.Flags().Changed("format"), *projectSyncJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*projectSyncPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := validTopProjectKind(projectSyncKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := wiring.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		port := resolveGatewayPort(projectSyncPort)
		name := projectSyncName
		if projectSyncGroup != "" && !cmd.Flags().Changed("name") {
			name = "gridctl-" + projectSyncGroup
		}
		baseURL := provisioner.GatewayHTTPURL(port)
		if projectSyncGroup != "" {
			baseURL = provisioner.GroupGatewayHTTPURL(port, projectSyncGroup)
		}
		opts := wiring.SyncOptions{
			Clients:    projectSyncClients,
			ServerName: name,
			GatewayURL: provisioner.AppendClientParam(baseURL, projectSyncClientID),
			Port:       port,
			Group:      projectSyncGroup,
			ClientID:   projectSyncClientID,
			Force:      projectSyncForce,
			DryRun:     projectSyncDryRun,
		}
		if exit := runWiringSync(cmd.Context(), os.Stdout, os.Stderr, mgr, opts, format, *projectSyncPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var (
	projectStatusKind   string
	projectStatusPort   int
	projectStatusFormat string
	projectStatusJSON   *bool
	projectStatusPlain  *bool
)

var projectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show per-entry wiring ownership state",
	Long: `Shows the ownership state of every recorded client entry, plus foreign
rows (gridctl-named entries never recorded, e.g. links written before
ownership recording existed) and missing rows (clients detected but not
linked).

States: in-sync, stale (the entry differs from what gridctl would write
now, e.g. after a gateway port change), drifted (edited since gridctl
wrote it), target-missing (entry or whole config file gone), foreign,
missing.

Exit codes:
  0  everything clean (missing rows are advisory)
  1  drift, staleness, a foreign entry, or a missing target detected
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(projectStatusFormat, cmd.Flags().Changed("format"), *projectStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*projectStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := validTopProjectKind(projectStatusKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := wiring.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		opts := wiring.StatusOptions{Port: resolveGatewayPort(projectStatusPort)}
		if exit := runWiringStatus(cmd.Context(), os.Stdout, os.Stderr, mgr, opts, format, *projectStatusPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var (
	projectUnsyncKind   string
	projectUnsyncClient string
	projectUnsyncName   string
	projectUnsyncForce  bool
	projectUnsyncDryRun bool
	projectUnsyncFormat string
	projectUnsyncJSON   *bool
)

var projectUnsyncCmd = &cobra.Command{
	Use:   "unsync",
	Short: "Remove a recorded entry and its ownership record",
	Long: `Removes one client's recorded gateway entry and purges its ownership
record. The entry is deleted only when its current value is one gridctl
recorded (or --force); entries gridctl never recorded are never
deleted, with or without --force. When the client is no longer detected
on this machine, only the record is dropped.

Exit codes:
  0  removed (or already gone)
  1  refused (foreign or drifted entry)
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(projectUnsyncFormat, cmd.Flags().Changed("format"), *projectUnsyncJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := validTopProjectKind(projectUnsyncKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if projectUnsyncClient == "" {
			fmt.Fprintln(os.Stderr, "--client is required")
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := wiring.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runWiringUnsync(cmd.Context(), os.Stdout, os.Stderr, mgr, projectUnsyncClient, projectUnsyncName, projectUnsyncForce, projectUnsyncDryRun, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var (
	projectAdoptKind   string
	projectAdoptClient string
	projectAdoptName   string
	projectAdoptFormat string
	projectAdoptJSON   *bool
)

var projectAdoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: "Take ownership of an entry's current value",
	Long: `Records ownership of a client entry's current value without rewriting
it: the hand-edited (or pre-lockfile) value becomes a recognized gridctl
hash, so sync and unlink stop refusing. Nothing on disk changes.

Exit codes:
  0  adopted
  1  nothing to adopt
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(projectAdoptFormat, cmd.Flags().Changed("format"), *projectAdoptJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := validTopProjectKind(projectAdoptKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if projectAdoptClient == "" {
			fmt.Fprintln(os.Stderr, "--client is required")
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := wiring.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runWiringAdopt(cmd.Context(), os.Stdout, os.Stderr, mgr, projectAdoptClient, projectAdoptName, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

func init() {
	projectSyncCmd.Flags().StringVar(&projectSyncKind, "kind", projectKindWiring, "Projection kind (wiring)")
	projectSyncCmd.Flags().StringSliceVar(&projectSyncClients, "clients", nil, "Restrict to these client slugs")
	projectSyncCmd.Flags().StringVarP(&projectSyncName, "name", "n", "gridctl", "Entry name to write")
	projectSyncCmd.Flags().StringVar(&projectSyncGroup, "group", "", "Tool group whose endpoint to link")
	projectSyncCmd.Flags().StringVar(&projectSyncClientID, "client-id", "", "Stable client identifier for access scoping")
	projectSyncCmd.Flags().IntVarP(&projectSyncPort, "port", "p", 0, "Gateway port (auto-detected from a running stack)")
	projectSyncCmd.Flags().BoolVar(&projectSyncForce, "force", false, "Overwrite foreign or drifted entries (after backup)")
	projectSyncCmd.Flags().BoolVar(&projectSyncDryRun, "dry-run", false, "Show what would change without modifying files")
	projectSyncCmd.Flags().StringVar(&projectSyncFormat, "format", "text", "Output format: text or json")
	projectSyncJSON = addJSONAlias(projectSyncCmd)
	projectSyncPlain = addPlainFlag(projectSyncCmd)

	projectStatusCmd.Flags().StringVar(&projectStatusKind, "kind", projectKindWiring, "Projection kind (wiring)")
	projectStatusCmd.Flags().IntVarP(&projectStatusPort, "port", "p", 0, "Gateway port for staleness comparison")
	projectStatusCmd.Flags().StringVar(&projectStatusFormat, "format", "text", "Output format: text or json")
	projectStatusJSON = addJSONAlias(projectStatusCmd)
	projectStatusPlain = addPlainFlag(projectStatusCmd)

	projectUnsyncCmd.Flags().StringVar(&projectUnsyncKind, "kind", projectKindWiring, "Projection kind (wiring)")
	projectUnsyncCmd.Flags().StringVar(&projectUnsyncClient, "client", "", "Client slug to unsync (required)")
	projectUnsyncCmd.Flags().StringVarP(&projectUnsyncName, "name", "n", "gridctl", "Entry name to remove")
	projectUnsyncCmd.Flags().BoolVar(&projectUnsyncForce, "force", false, "Remove even when the recorded entry was edited")
	projectUnsyncCmd.Flags().BoolVar(&projectUnsyncDryRun, "dry-run", false, "Show what would change without modifying files")
	projectUnsyncCmd.Flags().StringVar(&projectUnsyncFormat, "format", "text", "Output format: text or json")
	projectUnsyncJSON = addJSONAlias(projectUnsyncCmd)

	projectAdoptCmd.Flags().StringVar(&projectAdoptKind, "kind", projectKindWiring, "Projection kind (wiring)")
	projectAdoptCmd.Flags().StringVar(&projectAdoptClient, "client", "", "Client slug whose entry to adopt (required)")
	projectAdoptCmd.Flags().StringVarP(&projectAdoptName, "name", "n", "gridctl", "Entry name to adopt")
	projectAdoptCmd.Flags().StringVar(&projectAdoptFormat, "format", "text", "Output format: text or json")
	projectAdoptJSON = addJSONAlias(projectAdoptCmd)

	projectCmd.AddCommand(projectSyncCmd, projectStatusCmd, projectUnsyncCmd, projectAdoptCmd)
}

// wiringStateLabel renders a status glyph + state, same glyph vocabulary
// as the skill and agent kinds.
func wiringStateLabel(state string) string {
	switch state {
	case wiring.StateInSync:
		return "✓ " + state
	case wiring.StateDrifted, wiring.StateTargetMissing, wiring.StateForeign:
		return "✗ " + state
	case wiring.StateStale:
		return "~ " + state
	default:
		return "— " + state
	}
}

// wiringActionLabel decorates sync/unsync actions with glyphs.
func wiringActionLabel(action string) string {
	switch action {
	case wiring.ActionLinked, wiring.ActionUpdated, wiring.ActionUnchanged, wiring.ActionAdopted, wiring.ActionRemoved, wiring.ActionAlreadyGone:
		return "✓ " + action
	case wiring.ActionSkippedForeign, wiring.ActionSkippedDrift, wiring.ActionError:
		return "✗ " + action
	default:
		return "— " + action
	}
}

// wiringSyncDoc is the machine-readable wiring sync document.
type wiringSyncDoc struct {
	SchemaVersion int             `json:"schema_version"`
	DryRun        bool            `json:"dry_run"`
	HasFailures   bool            `json:"has_failures"`
	Results       []wiring.Result `json:"results"`
}

// runWiringSync performs a wiring sync pass and returns the exit code.
func runWiringSync(ctx context.Context, stdout, stderr io.Writer, mgr *wiring.Manager, opts wiring.SyncOptions, format string, plain bool) int {
	results, err := mgr.Sync(ctx, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := wiringSyncDoc{
		SchemaVersion: projectJSONSchemaVersion,
		DryRun:        opts.DryRun,
		HasFailures:   wiring.HasFailures(results),
		Results:       results,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(results) == 0 {
			fmt.Fprintln(stdout, "No supported LLM clients detected.")
			return ctxExitOK
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"CLIENT", "NAME", "ACTION", "TARGET"})
		for _, r := range results {
			t.AppendRow(table.Row{r.Client, r.Name, wiringActionLabel(r.Action), r.Target})
		}
		t.Render()
		printWiringRemediation(stdout, results)
	}
	if doc.HasFailures {
		return ctxExitAttention
	}
	return ctxExitOK
}

// printWiringRemediation prints per-result detail and remediation lines
// after a table, skipping clean rows.
func printWiringRemediation(w io.Writer, results []wiring.Result) {
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(w, "\n%s / %s: %s\n", r.Client, r.Name, r.Error)
		}
		if r.Detail != "" && (r.Action == wiring.ActionSkippedForeign || r.Action == wiring.ActionSkippedDrift) {
			fmt.Fprintf(w, "\n%s / %s: %s.\n", r.Client, r.Name, r.Detail)
			if r.Remediation != "" {
				fmt.Fprintf(w, "  %s\n", strings.ToUpper(r.Remediation[:1])+r.Remediation[1:])
			}
		}
	}
}

// wiringStatusDoc is the machine-readable wiring status document.
type wiringStatusDoc struct {
	SchemaVersion  int          `json:"schema_version"`
	NeedsAttention bool         `json:"needs_attention"`
	Rows           []wiring.Row `json:"rows"`
}

// runWiringStatus renders the wiring state matrix and returns the exit
// code.
func runWiringStatus(ctx context.Context, stdout, stderr io.Writer, mgr *wiring.Manager, opts wiring.StatusOptions, format string, plain bool) int {
	rows, err := mgr.Statuses(ctx, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := wiringStatusDoc{
		SchemaVersion:  projectJSONSchemaVersion,
		NeedsAttention: wiring.NeedsAttention(rows),
		Rows:           rows,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "No wiring recorded and no clients detected.")
			return ctxExitOK
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"KIND", "CLIENT", "NAME", "CHANNEL", "STATE", "TARGET"})
		for _, r := range rows {
			t.AppendRow(table.Row{projectKindWiring, r.Client, r.Name, r.Channel, wiringStateLabel(r.State), r.Target})
		}
		t.Render()
		for _, r := range rows {
			if r.Detail == "" || r.State == wiring.StateInSync {
				continue
			}
			fmt.Fprintf(stdout, "\n%s / %s: %s.\n", r.Client, r.Name, r.Detail)
			if r.Remediation != "" {
				fmt.Fprintf(stdout, "  %s\n", strings.ToUpper(r.Remediation[:1])+r.Remediation[1:])
			}
		}
	}
	if doc.NeedsAttention {
		return ctxExitAttention
	}
	return ctxExitOK
}

// wiringResultDoc wraps a single wiring result for JSON output.
type wiringResultDoc struct {
	SchemaVersion int           `json:"schema_version"`
	DryRun        bool          `json:"dry_run,omitempty"`
	Result        wiring.Result `json:"result"`
}

// runWiringUnsync removes one recorded entry and returns the exit code.
func runWiringUnsync(ctx context.Context, stdout, stderr io.Writer, mgr *wiring.Manager, client, name string, force, dryRun bool, format string) int {
	prov, ok := mgr.Registry().FindBySlug(client)
	if !ok {
		// A record can outlive its client's registry membership; dropping
		// it must stay reachable or the remediation text lies.
		res, derr := mgr.DropRecord(ctx, client, name)
		if errors.Is(derr, wiring.ErrNotRecorded) {
			fmt.Fprintln(stderr, unknownClientError(mgr.Registry(), client))
			return ctxExitInfrastructure
		}
		if derr != nil {
			fmt.Fprintln(stderr, derr)
			return ctxExitInfrastructure
		}
		fmt.Fprintf(stdout, "%s %s / %s (record dropped; client is no longer supported)\n", wiringActionLabel(res.Action), res.Client, res.Name)
		return ctxExitOK
	}

	var res wiring.Result
	var err error
	if configPath, found := prov.Detect(); found {
		res, err = mgr.UnlinkClient(ctx, prov, configPath, name, force, dryRun)
	} else {
		// Client gone from this machine: drop the record only.
		res, err = mgr.DropRecord(ctx, client, name)
		if errors.Is(err, wiring.ErrNotRecorded) {
			fmt.Fprintf(stderr, "%s is not detected and has no recorded entry named '%s'\n", client, name)
			return ctxExitAttention
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, wiringResultDoc{SchemaVersion: projectJSONSchemaVersion, DryRun: dryRun, Result: res}); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		fmt.Fprintf(stdout, "%s %s / %s (%s)\n", wiringActionLabel(res.Action), res.Client, res.Name, res.Target)
		printWiringRemediation(stdout, []wiring.Result{res})
	}
	if wiring.HasFailures([]wiring.Result{res}) {
		return ctxExitAttention
	}
	return ctxExitOK
}

// runWiringAdopt takes ownership of an entry's current value and
// returns the exit code.
func runWiringAdopt(ctx context.Context, stdout, stderr io.Writer, mgr *wiring.Manager, client, name, format string) int {
	res, err := mgr.Adopt(ctx, client, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, wiring.ErrNothingToAdopt) {
			return ctxExitAttention
		}
		return ctxExitInfrastructure
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, wiringResultDoc{SchemaVersion: projectJSONSchemaVersion, Result: res}); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
		return ctxExitOK
	}
	fmt.Fprintf(stdout, "✓ Adopted %s's '%s' entry (%s)\n", client, name, res.Target)
	fmt.Fprintln(stdout, "Its current value is now recorded as gridctl's own; sync and unlink will no longer refuse it.")
	return ctxExitOK
}
