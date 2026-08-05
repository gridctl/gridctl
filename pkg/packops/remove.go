package packops

import (
	"context"
	"errors"
	"fmt"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// RemoveOptions parameterizes a cascade removal.
type RemoveOptions struct {
	Force  bool
	DryRun bool
	// GatewayPort feeds the wiring status probe used to find pack-tagged
	// wiring records.
	GatewayPort int
}

// RemoveDoc is the machine-readable remove document.
type RemoveDoc struct {
	SchemaVersion int      `json:"schema_version"`
	Pack          string   `json:"pack"`
	DryRun        bool     `json:"dry_run,omitempty"`
	Rows          []Row    `json:"rows"`
	Kept          []string `json:"kept,omitempty"`
}

// Remove cascades one pack's removal in dependency order: pack-tagged
// projections are unsynced, pack-tagged wiring records removed through
// the ownership manager, then the registry entries and the pack record
// itself. A drifted resource is kept unless forced; the trimmed pack
// record stays truthful about what remains.
func (m *Managers) Remove(ctx context.Context, imp *skills.Importer, name string, opts RemoveOptions) (*RemoveDoc, error) {
	locked, err := m.LoadLockedPack(name)
	if err != nil {
		return nil, err
	}

	// Drift pre-check: a hand-edited projection means the user changed
	// something gridctl would destroy; without force the whole resource
	// (projections + registry entry) is kept.
	driftedSkills, driftedAgents, err := m.driftedPackResources(ctx)
	if err != nil {
		return nil, err
	}

	var rows []Row
	var kept []string
	removableSkills := splitKept(locked.Skills, driftedSkills, opts.Force, "skill", &rows, &kept)
	removableAgents := splitKept(locked.Agents, driftedAgents, opts.Force, "agent", &rows, &kept)

	if opts.DryRun {
		for _, n := range removableSkills {
			rows = append(rows, Row{Kind: "skill", Name: n, Action: "would-remove"})
		}
		for _, n := range removableAgents {
			rows = append(rows, Row{Kind: "agent", Name: n, Action: "would-remove"})
		}
		for _, n := range locked.Rules {
			rows = append(rows, Row{Kind: "rule", Name: n, Action: "would-remove"})
		}
		if locked.Wiring {
			rows = append(rows, Row{Kind: "wiring", Name: "gridctl", Action: "would-remove"})
		}
		return &RemoveDoc{SchemaVersion: SchemaVersion, Pack: name, DryRun: true, Rows: rows, Kept: kept}, nil
	}

	// 1. Unsync projections (files leave client trees before the registry
	// entries they came from).
	if len(removableSkills) > 0 {
		projected, perr := projectedNames(ctx, m.Home, project.KindSkill, removableSkills)
		if perr != nil {
			return nil, perr
		}
		if len(projected) > 0 {
			results, uerr := m.Skills.Unsync(ctx, projected, skillsync.UnsyncOptions{})
			if uerr != nil {
				return nil, uerr
			}
			for _, r := range results {
				rows = append(rows, Row{Kind: "skill", Name: r.Skill, Client: r.Client, Action: r.Action})
			}
		}
	}
	if len(removableAgents) > 0 {
		projected, perr := projectedNames(ctx, m.Home, project.KindAgent, removableAgents)
		if perr != nil {
			return nil, perr
		}
		if len(projected) > 0 {
			results, uerr := m.Agents.Unsync(ctx, projected, agentsync.UnsyncOptions{})
			if uerr != nil {
				return nil, uerr
			}
			for _, r := range results {
				rows = append(rows, Row{Kind: "agent", Name: r.Agent, Client: r.Client, Action: r.Action})
			}
		}
	}

	// 2. Pack-tagged rule fragment projections (by tag, never by name).
	if len(locked.Rules) > 0 && m.Contexts != nil {
		results, ruleNames, uerr := m.Contexts.UnsyncPackFragments(ctx, name)
		if uerr != nil {
			return nil, uerr
		}
		for _, r := range results {
			rows = append(rows, Row{Kind: "rule", Name: r.Fragment, Client: r.Slug, Action: r.Action})
		}
		// Drop store files only for fragments the pack listed and that
		// lost their pack projections; a user fragment of the same name
		// created afterward is not in locked.Rules at remove time only
		// if they renamed — we only remove names still listed on the pack.
		for _, n := range ruleNames {
			if !containsString(locked.Rules, n) {
				continue
			}
			if _, rerr := m.Contexts.RemoveFragment(n); rerr != nil && !errors.Is(rerr, contexts.ErrNoFragment) {
				rows = append(rows, Row{Kind: "rule", Name: n, Action: "error", Detail: rerr.Error()})
				continue
			}
			rows = append(rows, Row{Kind: "rule", Name: n, Action: "removed", Detail: "fragment store entry removed"})
		}
	}

	// 3. Wiring records: delete only what ownership proves is ours;
	// undetected clients drop the record alone.
	wr, wkept, werr := m.removePackWiring(ctx, name, opts.Force, opts.GatewayPort)
	if werr != nil {
		return nil, werr
	}
	rows = append(rows, wr...)
	kept = append(kept, wkept...)

	// 4. Registry entries, then the pack record (which the source GC
	// drops automatically when its last resource goes).
	for _, n := range removableSkills {
		if rerr := imp.Remove(n); rerr != nil {
			rows = append(rows, Row{Kind: "skill", Name: n, Action: "error", Detail: rerr.Error()})
			continue
		}
		rows = append(rows, Row{Kind: "skill", Name: n, Action: "removed", Detail: "registry entry removed"})
	}
	for _, n := range removableAgents {
		if rerr := imp.RemoveAgent(n); rerr != nil {
			rows = append(rows, Row{Kind: "agent", Name: n, Action: "error", Detail: rerr.Error()})
			continue
		}
		rows = append(rows, Row{Kind: "agent", Name: n, Action: "removed", Detail: "registry entry removed"})
	}

	// Partial removal keeps a truthful pack record covering what stayed.
	if err := trimLockedPack(ctx, m.lockPath(), name, kept); err != nil {
		return nil, err
	}

	return &RemoveDoc{SchemaVersion: SchemaVersion, Pack: name, Rows: rows, Kept: kept}, nil
}

// driftedPackResources reports which resources have a drifted projection
// anywhere.
func (m *Managers) driftedPackResources(ctx context.Context) (map[string]bool, map[string]bool, error) {
	driftedSkills := map[string]bool{}
	driftedAgents := map[string]bool{}
	sst, err := m.Skills.Statuses(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range sst {
		if s.State == skillsync.StateDrifted {
			driftedSkills[s.Skill] = true
		}
	}
	ast, err := m.Agents.Statuses(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range ast {
		if s.State == agentsync.StateDrifted {
			driftedAgents[s.Agent] = true
		}
	}
	return driftedSkills, driftedAgents, nil
}

// splitKept separates removable names from drift-kept ones, emitting
// skip rows for the latter.
func splitKept(names []string, drifted map[string]bool, force bool, kind string, rows *[]Row, kept *[]string) []string {
	var removable []string
	for _, n := range names {
		if drifted[n] && !force {
			*rows = append(*rows, Row{Kind: kind, Name: n, Action: "skipped-drift",
				Detail:      "a projection of this " + kind + " was hand-edited",
				Remediation: fmt.Sprintf("keep the edit with 'gridctl skill project adopt --kind %s %s --client <slug>' before removing, or force removal with --force", kind, n)})
			*kept = append(*kept, kind+"/"+n)
			continue
		}
		removable = append(removable, n)
	}
	return removable
}

// projectedNames filters names to those with recorded projections of a
// kind (Unsync errors on never-projected names). A store read failure
// propagates: the cascade must not proceed to registry deletion with
// the unsync step silently skipped.
func projectedNames(ctx context.Context, home string, kind project.Kind, names []string) ([]string, error) {
	l, err := project.NewStore(home).Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading projection lockfile: %w", err)
	}
	recorded := map[string]bool{}
	for _, e := range l.Entries(kind) {
		recorded[e.Source] = true
	}
	var out []string
	for _, n := range names {
		if recorded[n] {
			out = append(out, n)
		}
	}
	return out, nil
}

// removePackWiring removes every wiring record tagged by the pack.
func (m *Managers) removePackWiring(ctx context.Context, packName string, force bool, gatewayPort int) ([]Row, []string, error) {
	rows := []Row{}
	var kept []string
	wiringRows, err := m.Wiring.Statuses(ctx, wiring.StatusOptions{Port: gatewayPort})
	if err != nil {
		return nil, nil, err
	}
	for _, r := range wiringRows {
		if r.Pack != packName {
			continue
		}
		prov, ok := m.Wiring.Registry().FindBySlug(r.Client)
		if !ok {
			res, derr := m.Wiring.DropRecord(ctx, r.Client, r.Name)
			if derr != nil && !errors.Is(derr, wiring.ErrNotRecorded) {
				return nil, nil, derr
			}
			if res.Action != "" {
				rows = append(rows, Row{Kind: "wiring", Name: r.Name, Client: r.Client, Action: res.Action, Detail: res.Detail})
			}
			continue
		}
		configPath, found := prov.Detect()
		if !found {
			res, derr := m.Wiring.DropRecord(ctx, r.Client, r.Name)
			if derr != nil && !errors.Is(derr, wiring.ErrNotRecorded) {
				return nil, nil, derr
			}
			rows = append(rows, Row{Kind: "wiring", Name: r.Name, Client: r.Client, Action: res.Action, Detail: res.Detail})
			continue
		}
		res, uerr := m.Wiring.UnlinkClient(ctx, prov, configPath, r.Name, force, false)
		if uerr != nil {
			return nil, nil, uerr
		}
		row := Row{Kind: "wiring", Name: r.Name, Client: r.Client, Action: res.Action, Detail: res.Detail, Remediation: res.Remediation}
		rows = append(rows, row)
		if res.Action == wiring.ActionSkippedDrift || res.Action == wiring.ActionSkippedForeign {
			kept = append(kept, "wiring/"+r.Client)
		}
	}
	return rows, kept, nil
}

// trimLockedPack drops the pack record, or shrinks it to the resources
// a partial removal kept. The read-modify-write cycle holds the import
// lockfile's cross-process lock.
func trimLockedPack(ctx context.Context, lockPath, name string, kept []string) error {
	return skills.MutateLockFile(ctx, lockPath, func(lf *skills.LockFile) (bool, error) {
		srcName, src, ok := lf.FindPackSource(name)
		if !ok {
			return false, nil // source already GC'd by the last resource removal
		}
		if len(kept) == 0 {
			src.Pack = nil
			if len(src.Skills) == 0 && len(src.Agents) == 0 {
				lf.RemoveSource(srcName)
			} else {
				lf.SetSource(srcName, *src)
			}
			return true, nil
		}
		keptSet := map[string]bool{}
		for _, k := range kept {
			keptSet[k] = true
		}
		var skillsKept, agentsKept []string
		for _, n := range src.Pack.Skills {
			if keptSet["skill/"+n] {
				skillsKept = append(skillsKept, n)
			}
		}
		for _, n := range src.Pack.Agents {
			if keptSet["agent/"+n] {
				agentsKept = append(agentsKept, n)
			}
		}
		src.Pack.Skills = skillsKept
		src.Pack.Agents = agentsKept
		lf.SetSource(srcName, *src)
		return true, nil
	})
}
