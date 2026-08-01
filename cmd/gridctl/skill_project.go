package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/skillsync"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// skillProjectJSONSchemaVersion identifies the shape of the skill
// project JSON documents. Evolution within a version is append-only.
const skillProjectJSONSchemaVersion = 1

var (
	skillProjectSyncClients []string
	skillProjectSyncCopy    bool
	skillProjectSyncDryRun  bool
	skillProjectSyncForce   bool
	skillProjectSyncFormat  string
	skillProjectSyncKind    string
	skillProjectSyncJSON    *bool
	skillProjectSyncPlain   *bool

	skillProjectStatusFormat string
	skillProjectStatusJSON   *bool
	skillProjectStatusPlain  *bool

	skillProjectUnsyncAll     bool
	skillProjectUnsyncClients []string
	skillProjectUnsyncDryRun  bool
	skillProjectUnsyncFormat  string
	skillProjectUnsyncKind    string
	skillProjectUnsyncJSON    *bool

	skillProjectAdoptClient string
	skillProjectAdoptFormat string
	skillProjectAdoptKind   string
	skillProjectAdoptJSON   *bool
)

// skillProjectKindSkill and skillProjectKindAgent are the --kind values
// and the kind tags on JSON rows, so consumers can tell skill and agent
// rows apart as the row vocabulary grows (append-only schema evolution).
const (
	skillProjectKindSkill = "skill"
	skillProjectKindAgent = "agent"
)

// validProjectKind rejects unknown --kind values before any manager is
// built. The default "skill" keeps every existing command line behaving
// exactly as before.
func validProjectKind(kind string) error {
	switch kind {
	case skillProjectKindSkill, skillProjectKindAgent:
		return nil
	}
	return fmt.Errorf("unknown --kind %q (supported: skill, agent)", kind)
}

var skillProjectCmd = &cobra.Command{
	Use:   "project",
	Short: "Project skills into native client skill directories",
	Long: `Project active registry skills into native client skill locations so
they work in clients that never fetch MCP prompts (Antigravity, Grok
Build) and auto-trigger in clients that read skills from disk.

Unlike 'gridctl ctx sync', nothing is projected by default: name the
skills to project explicitly. Projecting all active skills would flood
each client's skill discovery context.

Targets: 'agents' (~/.agents/skills, the vendor-neutral interop dir read
by Zed, Goose, OpenCode, VS Code, and Grok Build), 'claude-code'
(~/.claude/skills), and 'antigravity' (~/.gemini/config/skills, always
copied). Skills are symlinked into the registry by default, so registry
edits propagate without a re-sync; --copy materializes copies instead.

The MCP prompt channel is unchanged: clients that render prompts
(Gemini CLI, Cursor, Windsurf) keep receiving skills that way.`,
	Example: `  gridctl skill project sync my-skill               Project to every available client
  gridctl skill project sync my-skill --clients claude-code
  gridctl skill project sync                        Re-sync the projected set
  gridctl skill project status                      Per-projection state
  gridctl skill project adopt my-skill --client antigravity   Pull a hand edit back
  gridctl skill project unsync --all                Remove every projection`,
}

var skillProjectSyncCmd = &cobra.Command{
	Use:   "sync [skill...]",
	Short: "Project named skills, or re-sync the projected set",
	Long: `Projects the named active skills into client skill directories and
records them in the projection set. With no skill names, the recorded
set is reconciled instead: dangling links are repaired, stale copies
refreshed, and projections whose skill was deactivated or deleted are
removed.

A destination that gridctl does not manage, or a hand-edited copy, is
skipped with a warning; --force backs it up and replaces it. Every
replacement of a real file or directory is preceded by a timestamped
backup.

Exit codes:
  0  synced cleanly
  1  a projection was skipped (drift or unmanaged path) or failed
  2  infrastructure error (unknown skill or client, lockfile conflict)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillProjectSyncFormat, cmd.Flags().Changed("format"), *skillProjectSyncJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*skillProjectSyncPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := validProjectKind(skillProjectSyncKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if skillProjectSyncKind == skillProjectKindAgent {
			mgr, err := newAgentProjectManager()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(ctxExitInfrastructure)
			}
			opts := agentsync.SyncOptions{
				Clients: skillProjectSyncClients,
				DryRun:  skillProjectSyncDryRun,
				Force:   skillProjectSyncForce,
			}
			if exit := runAgentProjectSync(cmd.Context(), os.Stdout, os.Stderr, mgr, args, opts, format, *skillProjectSyncPlain); exit != ctxExitOK {
				os.Exit(exit)
			}
			return nil
		}
		mgr, err := newSkillProjectManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		opts := skillsync.SyncOptions{
			Clients: skillProjectSyncClients,
			Copy:    skillProjectSyncCopy,
			DryRun:  skillProjectSyncDryRun,
			Force:   skillProjectSyncForce,
		}
		if exit := runSkillProjectSync(cmd.Context(), os.Stdout, os.Stderr, mgr, args, opts, format, *skillProjectSyncPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var skillProjectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show per-projection state",
	Long: `Shows the state of every projected (skill, client) pair.

States: in-sync, stale (registry content changed since the copy was
made, or the skill left the active set), drifted (the projected copy or
link was hand-modified), target-missing. Symlink projections of active
skills are never content-stale: the link references the registry
directly.

Exit codes:
  0  everything clean
  1  drift, staleness, or a missing target detected
  2  infrastructure error`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillProjectStatusFormat, cmd.Flags().Changed("format"), *skillProjectStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*skillProjectStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgr, err := newSkillProjectManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		agentMgr, err := newAgentProjectManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runSkillProjectStatus(cmd.Context(), os.Stdout, os.Stderr, mgr, agentMgr, format, *skillProjectStatusPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

var skillProjectUnsyncCmd = &cobra.Command{
	Use:   "unsync [skill...]",
	Short: "Remove projected skills from client directories",
	Long: `Removes projections gridctl created: symlinks are unlinked and copied
directories removed after a timestamped backup. Files gridctl did not
create are never touched. Removed skills leave the projection set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillProjectUnsyncFormat, cmd.Flags().Changed("format"), *skillProjectUnsyncJSON)
		if err != nil {
			return err
		}
		if err := validProjectKind(skillProjectUnsyncKind); err != nil {
			return err
		}
		if skillProjectUnsyncKind == skillProjectKindAgent {
			mgr, merr := newAgentProjectManager()
			if merr != nil {
				return merr
			}
			opts := agentsync.UnsyncOptions{
				All:     skillProjectUnsyncAll,
				Clients: skillProjectUnsyncClients,
				DryRun:  skillProjectUnsyncDryRun,
			}
			return runAgentProjectUnsync(cmd.Context(), os.Stdout, mgr, args, opts, format)
		}
		mgr, merr := newSkillProjectManager()
		if merr != nil {
			return merr
		}
		opts := skillsync.UnsyncOptions{
			All:     skillProjectUnsyncAll,
			Clients: skillProjectUnsyncClients,
			DryRun:  skillProjectUnsyncDryRun,
		}
		return runSkillProjectUnsync(cmd.Context(), os.Stdout, mgr, args, opts, format)
	},
}

var skillProjectAdoptCmd = &cobra.Command{
	Use:   "adopt <skill>",
	Short: "Pull a hand-edited projected copy back into the registry skill",
	Long: `Adopts the files of one projected copy back into the registry skill,
then re-syncs that (skill, client) pair so it returns to in-sync. Other
clients projecting the skill become stale until the next
'gridctl skill project sync'.

Only copy projections can be adopted: a symlinked projection references
the registry directly, so edits made through it already live in the
registry. The prior registry SKILL.md is backed up as SKILL.md.pre-<sha>
and the adopted files count as local edits, so 'gridctl skill update'
will not overwrite them without --force.

The flag is singular --client: adopt operates on exactly one
(skill, client) pair, unlike sync/unsync's --clients.

Exit codes:
  0  adopted
  1  nothing to adopt (not projected, symlinked, or empty/invalid content)
  2  infrastructure error (unknown skill or client, lockfile conflict)`,
	Example: `  gridctl skill project adopt my-skill --client antigravity`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillProjectAdoptFormat, cmd.Flags().Changed("format"), *skillProjectAdoptJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := validProjectKind(skillProjectAdoptKind); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if skillProjectAdoptKind == skillProjectKindAgent {
			mgr, err := newAgentProjectManager()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(ctxExitInfrastructure)
			}
			if exit := runAgentProjectAdopt(cmd.Context(), os.Stdout, os.Stderr, mgr, args[0], skillProjectAdoptClient, format); exit != ctxExitOK {
				os.Exit(exit)
			}
			return nil
		}
		mgr, err := newSkillProjectManager()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runSkillProjectAdopt(cmd.Context(), os.Stdout, os.Stderr, mgr, args[0], skillProjectAdoptClient, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

func init() {
	skillProjectSyncCmd.Flags().StringSliceVar(&skillProjectSyncClients, "clients", nil, "Target client slugs (default: every available target)")
	skillProjectSyncCmd.Flags().BoolVar(&skillProjectSyncCopy, "copy", false, "Copy skill directories instead of symlinking (copy-forced targets copy regardless)")
	skillProjectSyncCmd.Flags().BoolVar(&skillProjectSyncDryRun, "dry-run", false, "Show what would change without writing")
	skillProjectSyncCmd.Flags().BoolVar(&skillProjectSyncForce, "force", false, "Overwrite drifted copies and unmanaged destination paths (after a backup)")
	skillProjectSyncCmd.Flags().StringVar(&skillProjectSyncFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	skillProjectSyncCmd.Flags().StringVar(&skillProjectSyncKind, "kind", "skill", "Resource kind to sync: skill or agent (agents are experimental and always copied)")
	skillProjectSyncJSON = addJSONAlias(skillProjectSyncCmd)
	skillProjectSyncPlain = addPlainFlag(skillProjectSyncCmd)

	skillProjectStatusCmd.Flags().StringVar(&skillProjectStatusFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	skillProjectStatusJSON = addJSONAlias(skillProjectStatusCmd)
	skillProjectStatusPlain = addPlainFlag(skillProjectStatusCmd)

	skillProjectUnsyncCmd.Flags().BoolVar(&skillProjectUnsyncAll, "all", false, "Remove every projection")
	skillProjectUnsyncCmd.Flags().StringSliceVar(&skillProjectUnsyncClients, "clients", nil, "Only remove projections for these client slugs")
	skillProjectUnsyncCmd.Flags().BoolVar(&skillProjectUnsyncDryRun, "dry-run", false, "Show what would be removed without writing")
	skillProjectUnsyncCmd.Flags().StringVar(&skillProjectUnsyncFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	skillProjectUnsyncCmd.Flags().StringVar(&skillProjectUnsyncKind, "kind", "skill", "Resource kind to unsync: skill or agent")
	skillProjectUnsyncJSON = addJSONAlias(skillProjectUnsyncCmd)

	skillProjectAdoptCmd.Flags().StringVar(&skillProjectAdoptClient, "client", "", "Client slug whose projected copy to adopt (required)")
	_ = skillProjectAdoptCmd.MarkFlagRequired("client")
	skillProjectAdoptCmd.Flags().StringVar(&skillProjectAdoptFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	skillProjectAdoptCmd.Flags().StringVar(&skillProjectAdoptKind, "kind", "skill", "Resource kind to adopt: skill or agent")
	skillProjectAdoptJSON = addJSONAlias(skillProjectAdoptCmd)

	skillProjectCmd.AddCommand(skillProjectSyncCmd)
	skillProjectCmd.AddCommand(skillProjectStatusCmd)
	skillProjectCmd.AddCommand(skillProjectUnsyncCmd)
	skillProjectCmd.AddCommand(skillProjectAdoptCmd)
	skillCmd.AddCommand(skillProjectCmd)
}

// newSkillProjectManager loads the registry and builds the projection
// manager rooted at the user's home.
func newSkillProjectManager() (*skillsync.Manager, error) {
	store, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	return skillsync.NewManager(store)
}

// newAgentProjectManager builds the agent projection manager rooted at
// the user's home, reading the canonical agent store under the registry
// directory.
func newAgentProjectManager() (*agentsync.Manager, error) {
	return agentsync.NewManager(registryDir())
}

// skillSyncRow is one skill sync result tagged with its kind.
type skillSyncRow struct {
	Kind string `json:"kind"`
	skillsync.SyncResult
}

// agentSyncRow is one agent sync result tagged with its kind.
type agentSyncRow struct {
	Kind string `json:"kind"`
	agentsync.SyncResult
}

func skillSyncRows(results []skillsync.SyncResult) []skillSyncRow {
	rows := make([]skillSyncRow, len(results))
	for i, r := range results {
		rows[i] = skillSyncRow{Kind: skillProjectKindSkill, SyncResult: r}
	}
	return rows
}

func agentSyncRows(results []agentsync.SyncResult) []agentSyncRow {
	rows := make([]agentSyncRow, len(results))
	for i, r := range results {
		rows[i] = agentSyncRow{Kind: skillProjectKindAgent, SyncResult: r}
	}
	return rows
}

// skillProjectSyncDoc is the machine-readable sync document.
type skillProjectSyncDoc struct {
	SchemaVersion int            `json:"schema_version"`
	DryRun        bool           `json:"dry_run"`
	HasFailures   bool           `json:"has_failures"`
	Results       []skillSyncRow `json:"results"`
}

// runSkillProjectSync performs the sync and returns the exit code.
func runSkillProjectSync(ctx context.Context, stdout, stderr io.Writer, mgr *skillsync.Manager, names []string, opts skillsync.SyncOptions, format string, plain bool) int {
	warnCoScannedDuplicates(stderr, names, opts.Clients)
	results, err := mgr.Sync(ctx, names, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := skillProjectSyncDoc{
		SchemaVersion: skillProjectJSONSchemaVersion,
		DryRun:        opts.DryRun,
		HasFailures:   skillsync.HasFailures(results),
		Results:       skillSyncRows(results),
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(results) == 0 {
			fmt.Fprintln(stdout, "Nothing projected yet. Run 'gridctl skill project sync <skill>' to project a skill.")
			return ctxExitOK
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"SKILL", "CLIENT", "CHANNEL", "ACTION", "TARGET"})
		for _, r := range results {
			t.AppendRow(table.Row{r.Skill, r.Client, r.Channel, skillProjectActionLabel(r.Action), r.Target})
		}
		t.Render()
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", r.Skill, r.Client, r.Error)
			}
			if r.Action == skillsync.ActionSkippedDrift && r.Error == "" {
				fmt.Fprintf(stdout, "\n%s → %s: projected copy was hand-edited. Overwrite with 'gridctl skill project sync %s --clients %s --force', or remove it with 'gridctl skill project unsync %s --clients %s'\n",
					r.Skill, r.Client, r.Skill, r.Client, r.Skill, r.Client)
			}
		}
	}
	if doc.HasFailures {
		return ctxExitAttention
	}
	return ctxExitOK
}

// warnCoScannedDuplicates warns when one sync projects the same skills
// into both ~/.claude/skills and ~/.agents/skills: Goose, OpenCode, and
// VS Code scan both roots and will discover each skill twice.
func warnCoScannedDuplicates(stderr io.Writer, names, clients []string) {
	if len(names) == 0 || len(clients) == 0 {
		return
	}
	var hasClaude, hasAgents bool
	for _, c := range clients {
		switch c {
		case "claude-code":
			hasClaude = true
		case "agents":
			hasAgents = true
		}
	}
	if hasClaude && hasAgents {
		fmt.Fprintln(stderr, "warning: projecting to both claude-code and agents; clients that scan both roots (Goose, OpenCode, VS Code) will discover these skills twice")
	}
}

// skillStatusRow is one skill status row tagged with its kind.
type skillStatusRow struct {
	Kind string `json:"kind"`
	skillsync.ProjectionStatus
}

// agentStatusRow is one agent status row tagged with its kind. Agent
// rows carry an "agent" name field instead of "skill".
type agentStatusRow struct {
	Kind string `json:"kind"`
	agentsync.ProjectionStatus
}

// skillProjectStatusDoc is the machine-readable status document. The
// projections array mixes skill and agent rows; the kind field on each
// row tells them apart.
type skillProjectStatusDoc struct {
	SchemaVersion  int   `json:"schema_version"`
	NeedsAttention bool  `json:"needs_attention"`
	Projections    []any `json:"projections"`
}

// runSkillProjectStatus renders per-projection state for both kinds and
// returns the exit code. agentMgr may be nil (skill rows only).
func runSkillProjectStatus(ctx context.Context, stdout, stderr io.Writer, mgr *skillsync.Manager, agentMgr *agentsync.Manager, format string, plain bool) int {
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	var agentStatuses []agentsync.ProjectionStatus
	if agentMgr != nil {
		agentStatuses, err = agentMgr.Statuses(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	}
	rows := make([]any, 0, len(statuses)+len(agentStatuses))
	for _, s := range statuses {
		rows = append(rows, skillStatusRow{Kind: skillProjectKindSkill, ProjectionStatus: s})
	}
	for _, s := range agentStatuses {
		rows = append(rows, agentStatusRow{Kind: skillProjectKindAgent, ProjectionStatus: s})
	}
	doc := skillProjectStatusDoc{
		SchemaVersion:  skillProjectJSONSchemaVersion,
		NeedsAttention: skillsync.NeedsAttention(statuses) || agentsync.NeedsAttention(agentStatuses),
		Projections:    rows,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "Nothing projected yet. Run 'gridctl skill project sync <skill>' to project a skill.")
			return ctxExitOK
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"SKILL", "CLIENT", "CHANNEL", "RENDER", "STATE", "TARGET"})
		for _, s := range statuses {
			t.AppendRow(table.Row{s.Skill, s.Client, s.Channel, s.Render, skillProjectStateLabel(s), s.Target})
		}
		for _, s := range agentStatuses {
			t.AppendRow(table.Row{s.Agent + " (agent)", s.Client, s.Channel, s.Render, agentProjectStateLabel(s), s.Target})
		}
		t.Render()
		for _, s := range statuses {
			if s.Detail != "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", s.Skill, s.Client, s.Detail)
			}
		}
		for _, s := range agentStatuses {
			if s.Detail != "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", s.Agent, s.Client, s.Detail)
			}
		}
	}
	if doc.NeedsAttention {
		return ctxExitAttention
	}
	return ctxExitOK
}

// skillProjectUnsyncDoc is the machine-readable unsync document.
type skillProjectUnsyncDoc struct {
	SchemaVersion int                      `json:"schema_version"`
	DryRun        bool                     `json:"dry_run"`
	Results       []skillsync.UnsyncResult `json:"results"`
}

// runSkillProjectUnsync implements `skill project unsync`.
func runSkillProjectUnsync(ctx context.Context, w io.Writer, mgr *skillsync.Manager, names []string, opts skillsync.UnsyncOptions, format string) error {
	results, err := mgr.Unsync(ctx, names, opts)
	if err != nil {
		if errors.Is(err, skillsync.ErrNotProjected) {
			return fmt.Errorf("%w (check 'gridctl skill project status')", err)
		}
		return err
	}
	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(w, skillProjectUnsyncDoc{
			SchemaVersion: skillProjectJSONSchemaVersion,
			DryRun:        opts.DryRun,
			Results:       results,
		})
	}
	if len(results) == 0 {
		fmt.Fprintln(w, "Nothing to unsync.")
		return nil
	}
	for _, r := range results {
		fmt.Fprintf(w, "✓ %-24s %-12s %s (%s)\n", r.Skill, r.Client, r.Target, r.Action)
	}
	return nil
}

// skillProjectAdoptDoc is the machine-readable adopt document.
type skillProjectAdoptDoc struct {
	SchemaVersion int                    `json:"schema_version"`
	Result        *skillsync.AdoptResult `json:"result"`
}

// runSkillProjectAdopt implements `skill project adopt` and returns the
// exit code: 0 adopted, 1 nothing to adopt, 2 infrastructure.
func runSkillProjectAdopt(ctx context.Context, stdout, stderr io.Writer, mgr *skillsync.Manager, skill, client, format string) int {
	res, err := mgr.Adopt(ctx, skill, client)
	if err != nil {
		var refusal *skillsync.AdoptRefusal
		switch {
		case errors.As(err, &refusal):
			fmt.Fprintln(stderr, err)
			return ctxExitAttention
		case errors.Is(err, skillsync.ErrNotProjected):
			fmt.Fprintf(stderr, "%v (check 'gridctl skill project status')\n", err)
			return ctxExitAttention
		default:
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, skillProjectAdoptDoc{
			SchemaVersion: skillProjectJSONSchemaVersion,
			Result:        res,
		}); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
		return ctxExitOK
	}
	if len(res.ChangedFiles) == 0 {
		fmt.Fprintf(stdout, "✓ %s's copy of %s already matches the registry; hashes refreshed\n", client, skill)
		return ctxExitOK
	}
	fmt.Fprintf(stdout, "✓ Adopted %s's copy of %s into %s\n", client, skill, res.RegistryDir)
	for _, f := range res.ChangedFiles {
		fmt.Fprintf(stdout, "  updated: %s\n", f)
	}
	if res.BackupFile != "" {
		fmt.Fprintf(stdout, "  previous SKILL.md kept as %s\n", res.BackupFile)
	}
	fmt.Fprintln(stdout, "Other clients projecting this skill are now stale; run 'gridctl skill project sync' to propagate.")
	fmt.Fprintln(stdout, "'gridctl skill update' now treats these files as local edits and will not overwrite them without --force.")
	return ctxExitOK
}

// skillProjectActionLabel decorates sync actions with glyphs.
func skillProjectActionLabel(action string) string {
	switch action {
	case skillsync.ActionLinked, skillsync.ActionCopied, skillsync.ActionUpdated, skillsync.ActionUnchanged, skillsync.ActionRemoved:
		return "✓ " + action
	case skillsync.ActionSkippedDrift, skillsync.ActionSkippedUnmanaged, skillsync.ActionError:
		return "✗ " + action
	default:
		return "— " + action
	}
}

// skillProjectStateLabel renders a status glyph + state.
func skillProjectStateLabel(s skillsync.ProjectionStatus) string {
	label := s.State
	if s.Experimental {
		label += " (experimental)"
	}
	switch s.State {
	case skillsync.StateInSync:
		return "✓ " + label
	case skillsync.StateDrifted, skillsync.StateTargetMissing:
		return "✗ " + label
	case skillsync.StateStale:
		return "~ " + label
	default:
		return "— " + label
	}
}

// agentProjectStateLabel renders a status glyph + state for agent rows,
// same vocabulary and glyphs as skills.
func agentProjectStateLabel(s agentsync.ProjectionStatus) string {
	label := s.State
	if s.Experimental {
		label += " (experimental)"
	}
	switch s.State {
	case agentsync.StateInSync:
		return "✓ " + label
	case agentsync.StateDrifted, agentsync.StateTargetMissing:
		return "✗ " + label
	case agentsync.StateStale:
		return "~ " + label
	default:
		return "— " + label
	}
}

// agentProjectActionLabel decorates agent sync actions with glyphs.
func agentProjectActionLabel(action string) string {
	switch action {
	case agentsync.ActionCopied, agentsync.ActionUpdated, agentsync.ActionUnchanged, agentsync.ActionRemoved:
		return "✓ " + action
	case agentsync.ActionSkippedDrift, agentsync.ActionSkippedUnmanaged, agentsync.ActionError:
		return "✗ " + action
	default:
		return "— " + action
	}
}

// agentProjectSyncDoc is the machine-readable agent sync document.
type agentProjectSyncDoc struct {
	SchemaVersion int            `json:"schema_version"`
	DryRun        bool           `json:"dry_run"`
	HasFailures   bool           `json:"has_failures"`
	Results       []agentSyncRow `json:"results"`
}

// runAgentProjectSync performs an agent-kind sync and returns the exit
// code. With no names, every imported agent is projected.
func runAgentProjectSync(ctx context.Context, stdout, stderr io.Writer, mgr *agentsync.Manager, names []string, opts agentsync.SyncOptions, format string, plain bool) int {
	results, err := mgr.Sync(ctx, names, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := agentProjectSyncDoc{
		SchemaVersion: skillProjectJSONSchemaVersion,
		DryRun:        opts.DryRun,
		HasFailures:   agentsync.HasFailures(results),
		Results:       agentSyncRows(results),
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(results) == 0 {
			fmt.Fprintln(stdout, "No agents imported yet. Run 'gridctl skill add <repo-url>' to import agents.")
			return ctxExitOK
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"AGENT", "CLIENT", "CHANNEL", "ACTION", "TARGET"})
		for _, r := range results {
			t.AppendRow(table.Row{r.Agent, r.Client, r.Channel, agentProjectActionLabel(r.Action), r.Target})
		}
		t.Render()
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", r.Agent, r.Client, r.Error)
			}
			if r.Action == agentsync.ActionSkippedDrift && r.Error == "" {
				fmt.Fprintf(stdout, "\n%s → %s: projected file was hand-edited. Keep the edit with 'gridctl skill project adopt --kind agent %s --client %s', or overwrite with 'gridctl skill project sync --kind agent %s --clients %s --force'\n",
					r.Agent, r.Client, r.Agent, r.Client, r.Agent, r.Client)
			}
			if r.Detail != "" && r.Error == "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", r.Agent, r.Client, r.Detail)
			}
		}
	}
	if doc.HasFailures {
		return ctxExitAttention
	}
	return ctxExitOK
}

// agentProjectUnsyncDoc is the machine-readable agent unsync document.
type agentProjectUnsyncDoc struct {
	SchemaVersion int                      `json:"schema_version"`
	DryRun        bool                     `json:"dry_run"`
	Results       []agentsync.UnsyncResult `json:"results"`
}

// runAgentProjectUnsync implements `skill project unsync --kind agent`.
func runAgentProjectUnsync(ctx context.Context, w io.Writer, mgr *agentsync.Manager, names []string, opts agentsync.UnsyncOptions, format string) error {
	results, err := mgr.Unsync(ctx, names, opts)
	if err != nil {
		if errors.Is(err, agentsync.ErrNotProjected) {
			return fmt.Errorf("%w (check 'gridctl skill project status')", err)
		}
		return err
	}
	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(w, agentProjectUnsyncDoc{
			SchemaVersion: skillProjectJSONSchemaVersion,
			DryRun:        opts.DryRun,
			Results:       results,
		})
	}
	if len(results) == 0 {
		fmt.Fprintln(w, "Nothing to unsync.")
		return nil
	}
	for _, r := range results {
		fmt.Fprintf(w, "✓ %-24s %-12s %s (%s)\n", r.Agent, r.Client, r.Target, r.Action)
	}
	return nil
}

// agentProjectAdoptDoc is the machine-readable agent adopt document.
type agentProjectAdoptDoc struct {
	SchemaVersion int                    `json:"schema_version"`
	Result        *agentsync.AdoptResult `json:"result"`
}

// runAgentProjectAdopt implements `skill project adopt --kind agent` and
// returns the exit code: 0 adopted, 1 nothing to adopt, 2
// infrastructure.
func runAgentProjectAdopt(ctx context.Context, stdout, stderr io.Writer, mgr *agentsync.Manager, agent, client, format string) int {
	res, err := mgr.Adopt(ctx, agent, client)
	if err != nil {
		var refusal *agentsync.AdoptRefusal
		switch {
		case errors.As(err, &refusal):
			fmt.Fprintln(stderr, err)
			return ctxExitAttention
		case errors.Is(err, agentsync.ErrNotProjected):
			fmt.Fprintf(stderr, "%v (check 'gridctl skill project status')\n", err)
			return ctxExitAttention
		default:
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, agentProjectAdoptDoc{
			SchemaVersion: skillProjectJSONSchemaVersion,
			Result:        res,
		}); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
		return ctxExitOK
	}
	if !res.Changed {
		fmt.Fprintf(stdout, "✓ %s's copy of %s already matches the store; hashes refreshed\n", client, agent)
		return ctxExitOK
	}
	fmt.Fprintf(stdout, "✓ Adopted %s's copy of %s into %s\n", client, agent, res.CanonicalFile)
	if res.BackupFile != "" {
		fmt.Fprintf(stdout, "  previous AGENT.md kept as %s\n", res.BackupFile)
	}
	fmt.Fprintln(stdout, "'gridctl skill update' now treats this file as a local edit and will not overwrite it without --force.")
	return ctxExitOK
}
