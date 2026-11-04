package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"

	"github.com/jedib0t/go-pretty/v6/table"
)

func newHookProjectManager() (*hooksync.Manager, error) {
	return hooksync.NewManager(registryDir())
}

type hookStatusRow struct {
	Kind string `json:"kind"`
	hooksync.ProjectionStatus
}

type hookSyncRow struct {
	Kind string `json:"kind"`
	hooksync.SyncResult
}

type hookProjectSyncDoc struct {
	SchemaVersion int           `json:"schema_version"`
	DryRun        bool          `json:"dry_run"`
	HasFailures   bool          `json:"has_failures"`
	Results       []hookSyncRow `json:"results"`
}

type hookProjectUnsyncDoc struct {
	SchemaVersion int                     `json:"schema_version"`
	DryRun        bool                    `json:"dry_run"`
	Results       []hooksync.UnsyncResult `json:"results"`
}

func runHookProjectSync(ctx context.Context, stdout, stderr io.Writer, mgr *hooksync.Manager, names []string, opts hooksync.SyncOptions, format string, plain bool) int {
	results, err := mgr.Sync(ctx, names, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, hooksync.ErrUnknownClient) || errors.Is(err, hooksync.ErrUnknownHook) || errors.Is(err, hooksync.ErrNewerLockVersion) {
			return ctxExitInfrastructure
		}
		return ctxExitInfrastructure
	}
	if len(opts.Clients) == 0 {
		for _, c := range mgr.DetectedUnsupported() {
			fmt.Fprintf(stderr, "note: %s (%s) has a hook mechanism but is not a projection target in this release\n", c.Slug, c.Location)
		}
	}
	rows := make([]hookSyncRow, len(results))
	for i, r := range results {
		rows[i] = hookSyncRow{Kind: skillKindHook, SyncResult: r}
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, hookProjectSyncDoc{
			SchemaVersion: skillProjectJSONSchemaVersion,
			DryRun:        opts.DryRun,
			HasFailures:   hooksync.HasFailures(results),
			Results:       rows,
		}); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else if len(results) == 0 {
		fmt.Fprintln(stdout, "No hooks imported yet. Run 'gridctl pack add <repo>' to import hooks.")
		return ctxExitOK
	} else {
		t := output.NewTableWriter(stdout, plain)
		t.AppendHeader(table.Row{"HOOK", "CLIENT", "CHANNEL", "ACTION", "TARGET"})
		for _, r := range results {
			t.AppendRow(table.Row{r.Hook, r.Client, r.Channel, hookProjectActionLabel(r.Action), r.Target})
		}
		t.Render()
		for _, r := range results {
			if r.Error != "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", r.Hook, r.Client, r.Error)
			}
			if r.Detail != "" && r.Error == "" {
				fmt.Fprintf(stdout, "\n%s → %s: %s\n", r.Hook, r.Client, r.Detail)
			}
			if r.Remediation != "" {
				fmt.Fprintf(stdout, "  %s\n", r.Remediation)
			}
		}
	}
	if hooksync.HasFailures(results) {
		return ctxExitAttention
	}
	return ctxExitOK
}

func runHookProjectUnsync(ctx context.Context, w io.Writer, mgr *hooksync.Manager, names []string, opts hooksync.UnsyncOptions, format string) error {
	results, err := mgr.Unsync(ctx, names, opts)
	if err != nil {
		if errors.Is(err, hooksync.ErrNotProjected) {
			return fmt.Errorf("%w (check 'gridctl skill project status')", err)
		}
		return err
	}
	if strings.EqualFold(format, "json") {
		return output.EncodeJSON(w, hookProjectUnsyncDoc{
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
		fmt.Fprintf(w, "%s %-24s %-12s %s (%s)\n", hookProjectActionLabel(r.Action), r.Hook, r.Client, r.Target, r.Action)
		if r.Detail != "" {
			fmt.Fprintf(w, "  %s\n", r.Detail)
		}
	}
	return nil
}

func hookProjectActionLabel(action string) string {
	switch action {
	case hooksync.ActionCopied, hooksync.ActionUpdated, hooksync.ActionUnchanged, hooksync.ActionRemoved:
		return "✓ " + action
	case hooksync.ActionSkippedDrift, hooksync.ActionSkippedForeign, hooksync.ActionSkippedPinDrift,
		hooksync.ActionSkippedUnmanaged, hooksync.ActionError:
		return "✗ " + action
	default:
		return "— " + action
	}
}

func hookProjectStateLabel(s hooksync.ProjectionStatus) string {
	switch s.State {
	case hooksync.StateInSync:
		return "✓ " + s.State
	case hooksync.StateDrifted, hooksync.StateTargetMissing:
		return "✗ " + s.State
	case hooksync.StateStale:
		return "~ " + s.State
	default:
		return "— " + s.State
	}
}

// requiresCache reads metadata.requires.hooks once per command. A nil
// cache, or one with enabled false, skips annotation. That is how a
// failed hook-status read avoids reporting every declared hook as
// "not projected".
type requiresCache struct {
	skills   map[string][]string
	loaded   bool
	statuses []hooksync.ProjectionStatus
	enabled  bool
}

func newRequiresCache(statuses []hooksync.ProjectionStatus) *requiresCache {
	return &requiresCache{
		skills:   map[string][]string{},
		statuses: statuses,
		enabled:  true,
	}
}

// loadRequiresCache reads hook projection status for a sync annotation.
// On error it prints one stderr line and returns nil so the sync exit
// code is unchanged and no false "not projected" lines are emitted.
func loadRequiresCache(ctx context.Context, stderr io.Writer) *requiresCache {
	statuses, err := hookMgrForSync(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "warning: hook projection status unavailable: %s\n", err)
		return nil
	}
	return newRequiresCache(statuses)
}

// annotate fills requires_hooks and unenforced_hooks for one projected
// skill or agent. Missing declarations are ignored. The returned lines
// are appended to the text output and do not change state.
func (c *requiresCache) annotate(lines []string, kind, name, client string) ([]string, []hooksync.UnenforcedHook, []string) {
	if c == nil || !c.enabled || name == "" {
		return nil, nil, lines
	}
	required := c.hooks(kind, name)
	if len(required) == 0 {
		return nil, nil, lines
	}
	unenforced := hooksync.Unenforced(client, required, c.statuses)
	for _, gap := range unenforced {
		lines = append(lines, fmt.Sprintf("%s → %s: requires hook %s: not enforced on %s (%s)", name, client, gap.Name, client, gap.Reason))
	}
	return required, unenforced, lines
}

func (c *requiresCache) hooks(kind, name string) []string {
	switch kind {
	case "skill":
		if !c.loaded {
			c.loaded = true
			store := registry.NewStore(registryDir())
			if err := store.Load(); err != nil {
				return nil
			}
			for _, sk := range store.ListSkills() {
				if sk == nil {
					continue
				}
				c.skills[sk.Name] = skills.RequiredHooksFromSkill(sk.Metadata)
			}
		}
		return c.skills[name]
	case "agent":
		agent, err := skills.GetAgent(registryDir(), name)
		if err != nil || agent == nil || agent.Definition == nil {
			return nil
		}
		return skills.RequiredHooksFromAgent(agent.Definition)
	default:
		return nil
	}
}

func hookMgrForSync(ctx context.Context) ([]hooksync.ProjectionStatus, error) {
	mgr, err := newHookProjectManager()
	if err != nil {
		return nil, err
	}
	return mgr.Statuses(ctx)
}
