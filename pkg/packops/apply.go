package packops

import (
	"context"
	"strings"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/wiring"
	"fmt"
)

// ApplyOptions parameterizes a pack projection, mirroring the CLI flags
// one to one (--force, --dry-run, --clients).
type ApplyOptions struct {
	Force  bool
	DryRun bool
	// Clients restricts wiring to these client slugs.
	Clients []string
}

// ApplyDoc is the machine-readable apply document.
type ApplyDoc struct {
	SchemaVersion int    `json:"schema_version"`
	Pack          string `json:"pack"`
	DryRun        bool   `json:"dry_run,omitempty"`
	Applied       int    `json:"applied"`
	Total         int    `json:"total"`
	Rows          []Row  `json:"rows"`
}

// Apply projects one pack across every kind it selects. Apply is
// additive and never transactional: each resource succeeds or skips
// independently, and per-resource outcomes become rows, not errors.
func (m *Managers) Apply(ctx context.Context, name string, opts ApplyOptions) (*ApplyDoc, error) {
	locked, err := m.LoadLockedPack(name)
	if err != nil {
		return nil, err
	}
	foreign, err := foreignPackTags(ctx, m.Home, name)
	if err != nil {
		return nil, err
	}

	var rows []Row
	addRow := func(r Row) { rows = append(rows, r) }

	// Skills and agents: exclude foreign-tagged resources up front, then
	// hand the rest to the same engines the standalone verbs drive.
	skillNames := filterForeign(locked.Skills, "skill", foreign, addRow)
	agentNames := filterForeign(locked.Agents, "agent", foreign, addRow)

	if len(skillNames) > 0 {
		results, serr := m.Skills.Sync(ctx, skillNames, skillsync.SyncOptions{Force: opts.Force, DryRun: opts.DryRun, Pack: name})
		if serr != nil {
			// Apply is additive: a kind-level failure (a disabled skill,
			// say) becomes rows, not a whole-command abort.
			addRow(Row{Kind: "skill", Name: strings.Join(skillNames, ","), Action: "error", Detail: serr.Error()})
			results = nil
		}
		for _, r := range results {
			if r.Action == skillsync.ActionSkippedUnavailable {
				continue
			}
			addRow(Row{Kind: "skill", Name: r.Skill, Client: r.Client, Action: r.Action, Detail: r.Error})
		}
	}
	if len(agentNames) > 0 {
		results, aerr := m.Agents.Sync(ctx, agentNames, agentsync.SyncOptions{Force: opts.Force, DryRun: opts.DryRun, Pack: name})
		if aerr != nil {
			addRow(Row{Kind: "agent", Name: strings.Join(agentNames, ","), Action: "error", Detail: aerr.Error()})
			results = nil
		}
		for _, r := range results {
			if r.Action == agentsync.ActionSkippedUnavailable {
				continue
			}
			addRow(Row{Kind: "agent", Name: r.Agent, Client: r.Client, Action: r.Action, Detail: firstNonEmpty(r.Error, r.Detail)})
		}
	}

	if locked.Wiring {
		if _, ok := foreign["wiring/gridctl"]; ok {
			addRow(Row{Kind: "wiring", Name: "gridctl", Action: wiring.ActionSkippedForeign,
				Detail: fmt.Sprintf("the gridctl wiring entry is managed by pack %q", foreign["wiring/gridctl"])})
		} else if port, running := runningGatewayPort(); !running {
			addRow(Row{Kind: "wiring", Name: "gridctl", Action: wiring.ActionSkippedUnavailable,
				Detail:      "no running gateway detected",
				Remediation: fmt.Sprintf("start one with 'gridctl serve' or 'gridctl apply', then re-run 'gridctl pack apply %s'", name)})
		} else {
			clients := locked.Clients
			if len(opts.Clients) > 0 {
				clients = opts.Clients
			}
			results, werr := m.Wiring.Sync(ctx, wiring.SyncOptions{
				Clients:    clients,
				ServerName: "gridctl",
				GatewayURL: provisioner.GatewayHTTPURL(port),
				Port:       port,
				Force:      opts.Force,
				DryRun:     opts.DryRun,
				Pack:       name,
			})
			if werr != nil {
				addRow(Row{Kind: "wiring", Name: "gridctl", Action: "error", Detail: werr.Error()})
				results = nil
			}
			for _, r := range results {
				addRow(Row{Kind: "wiring", Name: r.Name, Client: r.Client, Action: r.Action,
					Detail: firstNonEmpty(r.Error, r.Detail), Remediation: r.Remediation})
			}
		}
	}

	// Rules: project every available client with the pack tag so lock
	// entries cascade-remove by tag. Only the pack's fragments need to
	// exist in the store (installed at pack add); SyncAll projects the
	// whole fragment set and tags new multi-file writes with Pack.
	if len(locked.Rules) > 0 {
		if m.Contexts != nil && m.Contexts.FragmentsActive() {
			results, rerr := m.Contexts.SyncAll(ctx, contexts.SyncOptions{Force: opts.Force, DryRun: opts.DryRun, Pack: name, PackRules: locked.Rules})
			if rerr != nil {
				addRow(Row{Kind: "rule", Name: strings.Join(locked.Rules, ","), Action: "error", Detail: rerr.Error()})
			} else {
				for _, r := range results {
					if r.Action == contexts.ActionSkippedUnavailable {
						continue
					}
					// Keep rows that touch pack-selected fragments (or compiled
					// clients that receive the whole compose).
					if r.Fragment != "" && !containsString(locked.Rules, r.Fragment) {
						continue
					}
					addRow(Row{Kind: "rule", Name: firstNonEmpty(r.Fragment, strings.Join(locked.Rules, ",")), Client: r.Slug, Action: r.Action, Detail: firstNonEmpty(r.Error, r.Detail)})
				}
			}
		} else {
			addRow(Row{Kind: "rule", Name: strings.Join(locked.Rules, ","), Action: "error",
				Detail: "fragments mode is not active; re-run 'gridctl pack add' for this pack"})
		}
	}

	for _, u := range locked.Unresolved {
		addRow(Row{Kind: "unresolved", Name: u, Action: "unresolved",
			Detail: "selected by the pack manifest but not shipped by the repository"})
	}

	applied, failed := TallyRows(rows)
	return &ApplyDoc{SchemaVersion: SchemaVersion, Pack: name, DryRun: opts.DryRun, Applied: applied, Total: applied + failed, Rows: rows}, nil
}
