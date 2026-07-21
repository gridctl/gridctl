package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	skillProjectSyncJSON    *bool
	skillProjectSyncPlain   *bool

	skillProjectStatusFormat string
	skillProjectStatusJSON   *bool
	skillProjectStatusPlain  *bool

	skillProjectUnsyncAll     bool
	skillProjectUnsyncClients []string
	skillProjectUnsyncDryRun  bool
	skillProjectUnsyncFormat  string
	skillProjectUnsyncJSON    *bool
)

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
		if exit := runSkillProjectStatus(cmd.Context(), os.Stdout, os.Stderr, mgr, format, *skillProjectStatusPlain); exit != ctxExitOK {
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

func init() {
	skillProjectSyncCmd.Flags().StringSliceVar(&skillProjectSyncClients, "clients", nil, "Target client slugs (default: every available target)")
	skillProjectSyncCmd.Flags().BoolVar(&skillProjectSyncCopy, "copy", false, "Copy skill directories instead of symlinking (copy-forced targets copy regardless)")
	skillProjectSyncCmd.Flags().BoolVar(&skillProjectSyncDryRun, "dry-run", false, "Show what would change without writing")
	skillProjectSyncCmd.Flags().BoolVar(&skillProjectSyncForce, "force", false, "Overwrite drifted copies and unmanaged destination paths (after a backup)")
	skillProjectSyncCmd.Flags().StringVar(&skillProjectSyncFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	skillProjectSyncJSON = addJSONAlias(skillProjectSyncCmd)
	skillProjectSyncPlain = addPlainFlag(skillProjectSyncCmd)

	skillProjectStatusCmd.Flags().StringVar(&skillProjectStatusFormat, "format", "", "Output format: 'json' for machine-readable output (default: table)")
	skillProjectStatusJSON = addJSONAlias(skillProjectStatusCmd)
	skillProjectStatusPlain = addPlainFlag(skillProjectStatusCmd)

	skillProjectUnsyncCmd.Flags().BoolVar(&skillProjectUnsyncAll, "all", false, "Remove every projection")
	skillProjectUnsyncCmd.Flags().StringSliceVar(&skillProjectUnsyncClients, "clients", nil, "Only remove projections for these client slugs")
	skillProjectUnsyncCmd.Flags().BoolVar(&skillProjectUnsyncDryRun, "dry-run", false, "Show what would be removed without writing")
	skillProjectUnsyncCmd.Flags().StringVar(&skillProjectUnsyncFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	skillProjectUnsyncJSON = addJSONAlias(skillProjectUnsyncCmd)

	skillProjectCmd.AddCommand(skillProjectSyncCmd)
	skillProjectCmd.AddCommand(skillProjectStatusCmd)
	skillProjectCmd.AddCommand(skillProjectUnsyncCmd)
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

// skillProjectSyncDoc is the machine-readable sync document.
type skillProjectSyncDoc struct {
	SchemaVersion int                    `json:"schema_version"`
	DryRun        bool                   `json:"dry_run"`
	HasFailures   bool                   `json:"has_failures"`
	Results       []skillsync.SyncResult `json:"results"`
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
		Results:       results,
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

// skillProjectStatusDoc is the machine-readable status document.
type skillProjectStatusDoc struct {
	SchemaVersion  int                          `json:"schema_version"`
	NeedsAttention bool                         `json:"needs_attention"`
	Projections    []skillsync.ProjectionStatus `json:"projections"`
}

// runSkillProjectStatus renders per-projection state and returns the
// exit code.
func runSkillProjectStatus(ctx context.Context, stdout, stderr io.Writer, mgr *skillsync.Manager, format string, plain bool) int {
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	doc := skillProjectStatusDoc{
		SchemaVersion:  skillProjectJSONSchemaVersion,
		NeedsAttention: skillsync.NeedsAttention(statuses),
		Projections:    statuses,
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(statuses) == 0 {
			fmt.Fprintln(stdout, "Nothing projected yet. Run 'gridctl skill project sync <skill>' to project a skill.")
			return ctxExitOK
		}
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"SKILL", "CLIENT", "CHANNEL", "STATE", "TARGET"})
		for _, s := range statuses {
			t.AppendRow(table.Row{s.Skill, s.Client, s.Channel, skillProjectStateLabel(s), s.Target})
		}
		t.Render()
		for _, s := range statuses {
			if s.Detail != "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", s.Skill, s.Client, s.Detail)
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
