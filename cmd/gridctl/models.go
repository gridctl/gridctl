package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/gridctl/gridctl/pkg/modelsync"
	"github.com/gridctl/gridctl/pkg/output"
)

// Exit codes shared with the other projection families (Article X).
const (
	modelsExitOK             = 0
	modelsExitAttention      = 1
	modelsExitInfrastructure = 2
)

// modelsJSONSchemaVersion versions every `gridctl models --format json`
// document. Bump only when a field changes meaning or disappears;
// additions ride the same version.
const modelsJSONSchemaVersion = 1

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Declare LLM routing policy and project it to LiteLLM and OpenCode",
	Long: `Manages one model routing policy document (~/.gridctl/models/policy.yaml)
and projects it into a LiteLLM auto-router config fragment (plus the
include: line referencing it from your own LiteLLM config) and an
OpenCode provider stanza.

gridctl never proxies inference: LiteLLM does the routing, gridctl only
compiles and synchronizes configuration. This is not the gateway's MCP
tool router, and it is unrelated to the model_preferences: stack block
(which hints a model per skill; this policy configures which backends
serve which complexity tier in your proxy).

The rendered fragment carries only the router. Backends stay in your
LiteLLM config's own model_list and are referenced by name: LiteLLM's
include directive extends model_list across files, so a re-emitted
backend would silently load-balance against the original.

LiteLLM reads its config only at startup. After a sync, restart it,
then run 'gridctl models ack-restart'; until then status reports
restart-pending.`,
	Example: `  gridctl models init --from-litellm ~/.litellm/config.yaml
  gridctl models edit
  gridctl models sync --dry-run --diff
  gridctl models sync
  gridctl models status`,
}

var (
	modelsInitTemplate    string
	modelsInitFromLiteLLM string
	modelsInitForce       bool

	modelsRenderTarget string
	modelsRenderOutput string

	modelsSyncDryRun bool
	modelsSyncDiff   bool
	modelsSyncCheck  bool
	modelsSyncForce  bool
	modelsSyncFormat string
	modelsSyncJSON   *bool

	modelsStatusFormat string
	modelsStatusJSON   *bool
	modelsStatusPlain  *bool

	modelsValidateFormat string
	modelsValidateJSON   *bool

	modelsUnsyncForce bool
)

var modelsInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold the models policy document",
	Long: `Creates ~/.gridctl/models/policy.yaml from a commented starter template,
or scaffolds it from an existing LiteLLM config with --from-litellm:
its model_list names become backend references (never copied inventory)
and the config becomes the sync target.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if modelsInitTemplate != "" && modelsInitFromLiteLLM != "" {
			return fmt.Errorf("--template and --from-litellm are mutually exclusive")
		}
		mgr, err := modelsync.NewManager()
		if err != nil {
			return err
		}
		if modelsInitFromLiteLLM != "" {
			if err := mgr.InitFromLiteLLM(modelsInitFromLiteLLM, modelsInitForce); err != nil {
				return err
			}
		} else {
			if err := mgr.InitFromTemplate(modelsInitTemplate, modelsInitForce); err != nil {
				return err
			}
		}
		p := output.New()
		p.Info("models policy created", "path", mgr.PolicyPath())
		p.Hint("Try: gridctl models edit   (then 'gridctl models sync')")
		return nil
	},
}

var modelsEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the models policy in $VISUAL/$EDITOR and validate on close",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := modelsync.NewManager()
		if err != nil {
			return err
		}
		if !mgr.HasPolicy() {
			return modelsync.ErrNoPolicy
		}
		path := mgr.PolicyPath()
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			return fmt.Errorf("neither $VISUAL nor $EDITOR is set; edit %s directly", path)
		}
		ed := exec.CommandContext(cmd.Context(), editor, path) // #nosec G204 -- the user's own $EDITOR, same trust domain as the shell
		ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := ed.Run(); err != nil {
			return fmt.Errorf("editor exited with error: %w", err)
		}
		code := runModelsValidate(os.Stdout, os.Stderr, mgr, "", false)
		if code == modelsExitInfrastructure {
			os.Exit(code)
		}
		if code == modelsExitOK {
			output.New().Hint("Try: gridctl models sync --dry-run --diff")
		}
		return nil
	},
}

var modelsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the models policy",
	Long: `Checks the policy for schema errors, undeclared tier backends, literal
secrets, and LiteLLM keys that must stay in the parent config
(router_settings, fallbacks: an included fragment silently replaces
them). Exit 0 clean, 1 findings, 2 error.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(modelsValidateFormat, cmd.Flags().Changed("format"), *modelsValidateJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		mgr, err := modelsync.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		os.Exit(runModelsValidate(os.Stdout, os.Stderr, mgr, format, false))
		return nil
	},
}

var modelsRenderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render a projection target to stdout or a file",
	Long: `Renders the LiteLLM fragment or the OpenCode provider stanza without
touching sync state. Useful for inspection and for piping to a remote
host yourself.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := modelsync.NewManager()
		if err != nil {
			return err
		}
		return runModelsRender(mgr, modelsRenderTarget, modelsRenderOutput, os.Stdout)
	},
}

var modelsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Project the policy into LiteLLM and OpenCode config",
	Long: `Renders the fragment, keeps the include: line in your LiteLLM config
pointing at it, and writes the OpenCode provider stanza. Every write is
backed up and recorded in the projection lockfile; hand-edited targets
are skipped with guidance unless --force.

Writing the fragment does not make the policy live: LiteLLM reads
config only at startup. Restart it, then run 'gridctl models
ack-restart'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(modelsSyncFormat, cmd.Flags().Changed("format"), *modelsSyncJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		if modelsSyncCheck && (modelsSyncDryRun || modelsSyncForce) {
			fmt.Fprintln(os.Stderr, "--check cannot be combined with --dry-run or --force")
			os.Exit(modelsExitInfrastructure)
		}
		mgr, err := modelsync.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		if modelsSyncCheck {
			os.Exit(runModelsCheck(cmd.Context(), os.Stdout, os.Stderr, mgr, format))
		}
		opts := modelsync.SyncOptions{DryRun: modelsSyncDryRun, Diff: modelsSyncDiff, Force: modelsSyncForce}
		os.Exit(runModelsSync(cmd.Context(), os.Stdout, os.Stderr, mgr, opts, format))
		return nil
	},
}

var modelsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show per-target projection state",
	Long: `Reports each target's state (in-sync, stale, drifted, target-missing,
never-synced) plus the restart-pending annotation on the LiteLLM
fragment. Exit 0 all in-sync (restart-pending alone stays 0), 1
attention needed, 2 error.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(modelsStatusFormat, cmd.Flags().Changed("format"), *modelsStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		if err := resolvePlain(*modelsStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		mgr, err := modelsync.NewManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(modelsExitInfrastructure)
		}
		os.Exit(runModelsStatus(cmd.Context(), os.Stdout, os.Stderr, mgr, format, *modelsStatusPlain))
		return nil
	},
}

var modelsUnsyncCmd = &cobra.Command{
	Use:   "unsync",
	Short: "Remove every projected target and its records",
	Long: `Removes the OpenCode provider stanza, the include: line, and the
rendered fragment, restoring everything outside gridctl's own writes
byte-for-byte. Hand-edited targets are kept unless --force; the policy
document itself is never touched.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := modelsync.NewManager()
		if err != nil {
			return err
		}
		results, err := mgr.Unsync(cmd.Context(), modelsync.UnsyncOptions{Force: modelsUnsyncForce})
		if err != nil {
			return err
		}
		p := output.New()
		if len(results) == 0 {
			p.Info("nothing synced; nothing to remove")
			return nil
		}
		for _, r := range results {
			p.Info("unsync", "target", r.Target, "action", r.Action, "path", r.Path)
			if r.Detail != "" {
				p.Warn(r.Detail, "target", r.Target)
			}
		}
		return nil
	},
}

var modelsAdoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: "Record the current on-disk state as gridctl-owned",
	Long: `Clears drift by accepting hand edits of the fragment or the OpenCode
provider entry as the new owned state. No file is modified.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := modelsync.NewManager()
		if err != nil {
			return err
		}
		results, err := mgr.Adopt(cmd.Context())
		if err != nil {
			return err
		}
		p := output.New()
		for _, r := range results {
			p.Info("adopt", "target", r.Target, "action", r.Action, "path", r.Path)
		}
		return nil
	},
}

var modelsAckRestartCmd = &cobra.Command{
	Use:   "ack-restart",
	Short: "Mark LiteLLM as restarted since the last sync",
	Long: `Clears the restart-pending annotation. Run it after actually
restarting LiteLLM; gridctl never probes the process to guess.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, err := modelsync.NewManager()
		if err != nil {
			return err
		}
		if err := mgr.AckRestart(cmd.Context()); err != nil {
			return err
		}
		output.New().Info("restart acknowledged; the policy is live")
		return nil
	},
}

func init() {
	modelsInitCmd.Flags().StringVar(&modelsInitTemplate, "template", "", "Starter template: local-only, hybrid, or cloud-primary (default hybrid)")
	modelsInitCmd.Flags().StringVar(&modelsInitFromLiteLLM, "from-litellm", "", "Scaffold from an existing LiteLLM config.yaml")
	modelsInitCmd.Flags().BoolVar(&modelsInitForce, "force", false, "Overwrite an existing policy")

	modelsRenderCmd.Flags().StringVar(&modelsRenderTarget, "target", "litellm", "Render target: litellm or opencode")
	modelsRenderCmd.Flags().StringVarP(&modelsRenderOutput, "output", "o", "-", "Output file, or - for stdout")

	modelsSyncCmd.Flags().BoolVar(&modelsSyncDryRun, "dry-run", false, "Preview without writing anything")
	modelsSyncCmd.Flags().BoolVar(&modelsSyncDiff, "diff", false, "With --dry-run, print unified diffs")
	modelsSyncCmd.Flags().BoolVar(&modelsSyncCheck, "check", false, "CI mode: no writes, exit 1 if a sync would change anything")
	modelsSyncCmd.Flags().BoolVar(&modelsSyncForce, "force", false, "Overwrite drifted and foreign targets")
	modelsSyncCmd.Flags().StringVar(&modelsSyncFormat, "format", "table", "Output format: table or json")
	modelsSyncJSON = addJSONAlias(modelsSyncCmd)

	modelsStatusCmd.Flags().StringVar(&modelsStatusFormat, "format", "table", "Output format: table or json")
	modelsStatusJSON = addJSONAlias(modelsStatusCmd)
	modelsStatusPlain = addPlainFlag(modelsStatusCmd)

	modelsValidateCmd.Flags().StringVar(&modelsValidateFormat, "format", "table", "Output format: table or json")
	modelsValidateJSON = addJSONAlias(modelsValidateCmd)

	modelsUnsyncCmd.Flags().BoolVar(&modelsUnsyncForce, "force", false, "Also remove hand-edited (drifted) targets")

	modelsCmd.AddCommand(modelsInitCmd, modelsEditCmd, modelsValidateCmd, modelsRenderCmd,
		modelsSyncCmd, modelsStatusCmd, modelsUnsyncCmd, modelsAdoptCmd, modelsAckRestartCmd)
}

// modelsValidateDoc is the machine-readable validate document.
type modelsValidateDoc struct {
	SchemaVersion int               `json:"schema_version"`
	PolicyPath    string            `json:"policy_path"`
	Valid         bool              `json:"valid"`
	Issues        []modelsync.Issue `json:"issues"`
}

// runModelsValidate validates the policy and returns the exit code.
func runModelsValidate(stdout, stderr io.Writer, mgr *modelsync.Manager, format string, quietOK bool) int {
	p, err := mgr.LoadPolicy()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return modelsExitInfrastructure
	}
	issues := mgr.Validate(p)
	doc := modelsValidateDoc{
		SchemaVersion: modelsJSONSchemaVersion,
		PolicyPath:    mgr.PolicyPath(),
		Valid:         !modelsync.HasErrors(issues),
		Issues:        issues,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return modelsExitInfrastructure
		}
	} else {
		for _, i := range issues {
			fmt.Fprintf(stdout, "%s: %s: %s\n", i.Severity, i.Field, i.Message)
		}
		if len(issues) == 0 && !quietOK {
			fmt.Fprintf(stdout, "policy is valid: %s\n", mgr.PolicyPath())
		}
	}
	if modelsync.HasErrors(issues) {
		return modelsExitAttention
	}
	return modelsExitOK
}

// runModelsRender renders one target without touching sync state.
func runModelsRender(mgr *modelsync.Manager, target, out string, stdout io.Writer) error {
	p, err := mgr.LoadPolicy()
	if err != nil {
		return err
	}
	if issues := mgr.Validate(p); modelsync.HasErrors(issues) {
		return fmt.Errorf("policy is invalid; run 'gridctl models validate'")
	}
	var data []byte
	switch target {
	case "litellm":
		if data, err = modelsync.RenderLiteLLM(p, p.Hash()); err != nil {
			return err
		}
	case "opencode":
		if p.Clients.OpenCode == nil {
			return fmt.Errorf("policy has no clients.opencode block")
		}
		schema := p.Clients.OpenCode.Schema
		if schema == "" || schema == modelsync.SchemaDetect {
			schema = modelsync.SchemaV1
		}
		render, rerr := modelsync.RenderOpenCode(p, schema)
		if rerr != nil {
			return rerr
		}
		if data, err = json.MarshalIndent(render.Value, "", "  "); err != nil {
			return err
		}
		data = append(data, '\n')
	default:
		return fmt.Errorf("unknown --target %q (supported: litellm, opencode)", target)
	}
	if out == "" || out == "-" {
		_, err = stdout.Write(data)
		return err
	}
	return os.WriteFile(out, data, 0644)
}

// modelsSyncDoc is the machine-readable sync document.
type modelsSyncDoc struct {
	SchemaVersion int                    `json:"schema_version"`
	DryRun        bool                   `json:"dry_run"`
	HasFailures   bool                   `json:"has_failures"`
	Results       []modelsync.SyncResult `json:"results"`
}

// runModelsSync performs the sync and returns the exit code.
func runModelsSync(ctx context.Context, stdout, stderr io.Writer, mgr *modelsync.Manager, opts modelsync.SyncOptions, format string) int {
	results, err := mgr.Sync(ctx, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return modelsExitInfrastructure
	}
	doc := modelsSyncDoc{
		SchemaVersion: modelsJSONSchemaVersion,
		DryRun:        opts.DryRun,
		HasFailures:   modelsync.HasFailures(results),
		Results:       results,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return modelsExitInfrastructure
		}
	} else {
		t := output.NewTableWriter(stdout, false)
		t.AppendHeader(table.Row{"TARGET", "ACTION", "PATH"})
		for _, r := range results {
			t.AppendRow(table.Row{r.Target, r.Action, r.Path})
		}
		t.Render()
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(stdout, "\n%s: %s\n", r.Target, r.Error)
			}
			if r.Detail != "" && r.Action != modelsync.ActionError {
				fmt.Fprintf(stdout, "\n%s: %s\n", r.Target, r.Detail)
			}
			if opts.DryRun && r.Diff != "" {
				fmt.Fprintf(stdout, "\n--- %s ---\n%s", r.Target, r.Diff)
			}
		}
		printModelsSyncHints(stdout, mgr, results, opts.DryRun)
	}
	if doc.HasFailures {
		return modelsExitAttention
	}
	return modelsExitOK
}

// printModelsSyncHints prints the honest next steps: the file is
// written, the policy is not live until LiteLLM restarts, and any env
// var the rendered config references but the environment lacks.
func printModelsSyncHints(stdout io.Writer, mgr *modelsync.Manager, results []modelsync.SyncResult, dryRun bool) {
	if dryRun {
		return
	}
	updated := false
	for _, r := range results {
		if r.Target == "litellm-fragment" && r.Action == modelsync.ActionUpdated {
			updated = true
		}
	}
	if updated {
		fmt.Fprintf(stdout, "\nFiles are written, but the policy is not live yet: LiteLLM reads its\n")
		fmt.Fprintf(stdout, "config only at startup (reload is not enough). Restart it, e.g.:\n")
		fmt.Fprintf(stdout, "  systemctl restart litellm   # or: docker restart litellm\n")
		fmt.Fprintf(stdout, "then confirm with: gridctl models ack-restart\n")
	}
	if p, err := mgr.LoadPolicy(); err == nil && p.Clients.OpenCode != nil {
		if name := p.Clients.OpenCode.APIKeyEnv; name != "" && os.Getenv(name) == "" {
			fmt.Fprintf(stdout, "\n%s is not set in this environment; OpenCode resolves it at run time:\n", name)
			fmt.Fprintf(stdout, "  export %s=...\n", name)
		}
	}
}

// modelsStatusDoc is the machine-readable status document.
type modelsStatusDoc struct {
	SchemaVersion  int                `json:"schema_version"`
	PolicyPath     string             `json:"policy_path"`
	PolicyExists   bool               `json:"policy_exists"`
	NeedsAttention bool               `json:"needs_attention"`
	Targets        []modelsync.Status `json:"targets"`
}

// runModelsStatus renders per-target state and returns the exit code.
func runModelsStatus(ctx context.Context, stdout, stderr io.Writer, mgr *modelsync.Manager, format string, plain bool) int {
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return modelsExitInfrastructure
	}
	doc := modelsStatusDoc{
		SchemaVersion:  modelsJSONSchemaVersion,
		PolicyPath:     mgr.PolicyPath(),
		PolicyExists:   mgr.HasPolicy(),
		NeedsAttention: modelsync.NeedsAttention(statuses),
		Targets:        statuses,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return modelsExitInfrastructure
		}
	} else {
		if !doc.PolicyExists {
			fmt.Fprintf(stdout, "No models policy yet. Run 'gridctl models init' to create one.\n\n")
		} else {
			fmt.Fprintf(stdout, "Policy: %s\n\n", doc.PolicyPath)
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"TARGET", "STATE", "PATH"})
		for _, s := range statuses {
			state := s.State
			if s.RestartPending {
				state += " (restart-pending)"
			}
			t.AppendRow(table.Row{s.Target, state, s.Path})
		}
		t.Render()
		for _, s := range statuses {
			if s.Detail != "" {
				fmt.Fprintf(stdout, "\n%s: %s\n", s.Target, s.Detail)
			}
		}
	}
	if doc.NeedsAttention {
		return modelsExitAttention
	}
	return modelsExitOK
}

// runModelsCheck implements `models sync --check`: CI mode, no writes.
// The JSON mode is exactly the status document.
func runModelsCheck(ctx context.Context, stdout, stderr io.Writer, mgr *modelsync.Manager, format string) int {
	if strings.EqualFold(format, "json") {
		return runModelsStatus(ctx, stdout, stderr, mgr, format, false)
	}
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return modelsExitInfrastructure
	}
	var dirty []modelsync.Status
	for _, s := range statuses {
		if s.State != modelsync.StateInSync {
			dirty = append(dirty, s)
		}
	}
	if len(dirty) == 0 {
		fmt.Fprintln(stdout, "models projections are in sync")
		return modelsExitOK
	}
	for _, s := range dirty {
		fmt.Fprintf(stdout, "- %s: %s", s.Target, s.State)
		if s.Detail != "" {
			fmt.Fprintf(stdout, " (%s)", s.Detail)
		}
		fmt.Fprintln(stdout)
	}
	return modelsExitAttention
}
