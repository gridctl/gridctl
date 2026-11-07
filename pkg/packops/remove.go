package packops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skillpins"
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
// itself. A drifted resource is kept unless forced. A hook is also kept
// when any unsync row is not removed or already-gone, including under
// force. Rule store files follow the pack record, not the projection
// list. A locally modified or unknown-provenance rule is kept unless
// forced. An unreadable rule is kept even under force. The trimmed pack
// record stays truthful about what remains.
func (m *Managers) Remove(ctx context.Context, imp *skills.Importer, name string, opts RemoveOptions) (*RemoveDoc, error) {
	locked, err := m.LoadLockedPack(name)
	if err != nil {
		return nil, err
	}

	// Drift pre-check: a hand-edited projection means the user changed
	// something gridctl would destroy; without force the whole resource
	// (projections + registry entry) is kept.
	driftedSkills, driftedAgents, driftedHooks, err := m.driftedPackResources(ctx)
	if err != nil {
		return nil, err
	}

	var rows []Row
	var kept []string
	lf, lerr := skills.ReadLockFile(m.lockPath())
	if lerr != nil {
		return nil, lerr
	}
	if locked.Stack != nil {
		boosters := attachedBoosters(lf, name)
		if len(boosters) > 0 && !opts.Force {
			names := make([]string, len(boosters))
			for i, b := range boosters {
				names[i] = b.Pack.Name
			}
			return nil, &packError{
				reason: ErrBoostersAttached,
				msg:    fmt.Sprintf("pack '%s' has attached boosters (%s); remove them first or pass --force", name, strings.Join(names, ", ")),
			}
		}
	}
	if locked.Fragment != nil {
		row, block, rerr := m.removeBooster(ctx, name, locked, lf, opts)
		if rerr != nil {
			return nil, rerr
		}
		if row != nil {
			rows = append(rows, *row)
		}
		if block {
			return &RemoveDoc{SchemaVersion: SchemaVersion, Pack: name, DryRun: opts.DryRun, Rows: rows, Kept: kept}, nil
		}
	} else if locked.Stack != nil {
		row, block, rerr := m.removeStack(ctx, name, locked.Stack, opts.DryRun)
		if rerr != nil {
			return nil, rerr
		}
		if row != nil {
			rows = append(rows, *row)
		}
		if block {
			return &RemoveDoc{SchemaVersion: SchemaVersion, Pack: name, DryRun: opts.DryRun, Rows: rows, Kept: kept}, nil
		}
	}
	memberSkills, memberAgents := memberOwnedNames(lf, name)
	removableSkills := splitKept(unionNames(locked.Skills, memberSkills), driftedSkills, opts.Force, "skill", &rows, &kept)
	removableAgents := splitKept(unionNames(locked.Agents, memberAgents), driftedAgents, opts.Force, "agent", &rows, &kept)
	removableHooks := splitKept(locked.Hooks, driftedHooks, opts.Force, "hook", &rows, &kept)

	if opts.DryRun {
		for _, n := range removableSkills {
			rows = append(rows, Row{Kind: "skill", Name: n, Action: "would-remove", Detail: skillPinDryRunDetail(imp, n)})
		}
		for _, n := range removableAgents {
			rows = append(rows, Row{Kind: "agent", Name: n, Action: "would-remove"})
		}
		// Same classification as the real run, so a preview that says
		// would-remove is a preview that would delete the store file.
		// UnsyncPackFragments is not called here: it mutates the
		// projection lockfile. A nil contexts manager emits no rule
		// rows, matching the real run.
		if len(locked.Rules) > 0 && m.Contexts != nil {
			appendRuleStoreRows(name, packSourceRepo(lf, name), m.classifyPackRules(locked), opts.Force, true, nil, &rows, &kept)
		}
		// A dry-run unsync rewrites a retiring row from removed to
		// would-remove. Count that as retired here only; the real run
		// must not, or a healthy preview fills kept and exits 1.
		if err := m.previewPackHookRemoval(ctx, removableHooks, opts.Force, &rows, &kept); err != nil {
			return nil, err
		}
		if locked.Wiring {
			rows = append(rows, Row{Kind: "wiring", Name: "gridctl", Action: "would-remove"})
		}
		if len(kept) == 0 {
			for _, key := range lf.MemberSources(name) {
				rows = append(rows, Row{Kind: "source", Name: memberSourceName(name, key), Action: "would-remove"})
			}
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

	// 2. Pack-tagged rule projections retract by tag. Store files follow
	// the pack record: a rule that was never projected, or whose
	// projections were already unsynced, is still removed when the
	// recorded hash matches. Classify before any write. An unreadable
	// store file is kept, including under --force, because there is
	// nothing to compare and RemoveFragment must not be called for it.
	if len(locked.Rules) > 0 && m.Contexts != nil {
		classified := m.classifyPackRules(locked)
		results, _, uerr := m.Contexts.UnsyncPackFragments(ctx, name)
		if uerr != nil {
			return nil, uerr
		}
		for _, r := range results {
			rows = append(rows, Row{Kind: "rule", Name: r.Fragment, Client: r.Slug, Action: r.Action})
		}
		appendRuleStoreRows(name, packSourceRepo(lf, name), classified, opts.Force, false, func(n string) error {
			_, rerr := m.Contexts.RemoveFragment(n)
			return rerr
		}, &rows, &kept)
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
		res, rerr := imp.RemoveSkill(ctx, n)
		if rerr != nil {
			rows = append(rows, Row{Kind: "skill", Name: n, Action: "error", Detail: rerr.Error()})
			continue
		}
		detail := "registry entry removed"
		if res != nil && len(res.PinsReset) > 0 {
			stacks := append([]string(nil), res.PinsReset...)
			slices.Sort(stacks)
			detail = fmt.Sprintf("registry entry removed, pin reset (%s)", strings.Join(stacks, ", "))
		}
		rows = append(rows, Row{Kind: "skill", Name: n, Action: "removed", Detail: detail})
	}
	for _, n := range removableAgents {
		if rerr := imp.RemoveAgent(n); rerr != nil {
			rows = append(rows, Row{Kind: "agent", Name: n, Action: "error", Detail: rerr.Error()})
			continue
		}
		rows = append(rows, Row{Kind: "agent", Name: n, Action: "removed", Detail: "registry entry removed"})
	}
	home, herr := m.homeDir()
	if herr != nil {
		return nil, herr
	}
	retiredHooks, err := m.unsyncPackHooks(ctx, removableHooks, opts.Force, &rows, &kept)
	if err != nil {
		return nil, err
	}
	if err := removePackHooks(ctx, home, retiredHooks, &rows); err != nil {
		return nil, err
	}

	// Partial removal keeps a truthful pack record covering what stayed.
	if err := trimLockedPack(ctx, m.lockPath(), name, kept); err != nil {
		return nil, err
	}

	return &RemoveDoc{SchemaVersion: SchemaVersion, Pack: name, Rows: rows, Kept: kept}, nil
}

// skillPinDryRunDetail reports pin files that hold name, without writing.
// A bare importer (no resetter) returns an empty detail and reads no pin
// file. Files that fail to load are skipped. A directory listing error is
// logged and treated as no preview, matching a real removal, which logs
// the same class of error and still removes the skill.
func skillPinDryRunDetail(imp *skills.Importer, name string) string {
	if imp == nil || !imp.HasSkillPinResetter() {
		return ""
	}
	stacks, err := skillpins.ListStacks()
	if err != nil {
		slog.Warn("skill pin dry-run listing failed", "skill", name, "error", err)
		return ""
	}
	var present []string
	for _, stack := range stacks {
		ps := skillpins.New(stack)
		if err := ps.Load(); err != nil {
			continue
		}
		if _, ok := ps.Get(name); ok {
			present = append(present, stack)
		}
	}
	if len(present) == 0 {
		return ""
	}
	return fmt.Sprintf("would reset pin (%s)", strings.Join(present, ", "))
}

// driftedPackResources reports which resources have a drifted projection
// anywhere.
func (m *Managers) driftedPackResources(ctx context.Context) (map[string]bool, map[string]bool, map[string]bool, error) {
	driftedSkills := map[string]bool{}
	driftedAgents := map[string]bool{}
	driftedHooks := map[string]bool{}
	sst, err := m.Skills.Statuses(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, s := range sst {
		if s.State == skillsync.StateDrifted {
			driftedSkills[s.Skill] = true
		}
	}
	ast, err := m.Agents.Statuses(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, s := range ast {
		if s.State == agentsync.StateDrifted {
			driftedAgents[s.Agent] = true
		}
	}
	if m.Hooks != nil {
		hst, herr := m.Hooks.Statuses(ctx)
		if herr != nil {
			return nil, nil, nil, herr
		}
		for _, s := range hst {
			if s.State == hooksync.StateDrifted {
				driftedHooks[s.Hook] = true
			}
		}
	}
	return driftedSkills, driftedAgents, driftedHooks, nil
}

// unsyncPackHooks retracts pack-tagged hook projections before the
// registry entries are deleted. It returns the names that may proceed:
// hooks that were never projected, and hooks whose unsync rows all
// retired. A hook with any row that hooksync.BlocksRemoval rejects is
// kept whole (one refusal row, a hook/<name> kept entry, no registry
// delete). Drifted projections are not in names unless the caller
// passed --force. Force still reaches the unsync, so hand-edited groups
// can clear; a parse error or a retired client still blocks.
func (m *Managers) unsyncPackHooks(ctx context.Context, names []string, force bool, rows *[]Row, kept *[]string) ([]string, error) {
	if len(names) == 0 || m.Hooks == nil {
		return names, nil
	}
	results, err := m.hookUnsyncResults(ctx, names, force, false)
	if err != nil {
		return nil, err
	}
	blocked := classifyHookUnsync(results, false)
	for _, r := range results {
		if _, ok := blocked[r.Hook]; ok {
			continue
		}
		*rows = append(*rows, Row{Kind: "hook", Name: r.Hook, Client: r.Client, Action: r.Action, Detail: firstNonEmpty(r.Error, r.Detail)})
	}
	var removable []string
	for _, name := range names {
		if row, ok := blocked[name]; ok {
			keepBlockedHook(name, row, rows, kept)
			continue
		}
		removable = append(removable, name)
	}
	return removable, nil
}

// previewPackHookRemoval reports hook removal without writing. A hook
// whose dry-run rows are all would-remove or already-gone is reported as
// would-remove, matching a preview that would succeed. Any other row is
// kept and reported with the same refusal as a real remove. A hook that
// was never projected stays would-remove. Member-source would-remove rows
// stay the caller's concern: they appear only when kept is empty.
func (m *Managers) previewPackHookRemoval(ctx context.Context, names []string, force bool, rows *[]Row, kept *[]string) error {
	if len(names) == 0 {
		return nil
	}
	if m.Hooks == nil {
		for _, n := range names {
			*rows = append(*rows, Row{Kind: "hook", Name: n, Action: "would-remove"})
		}
		return nil
	}
	results, err := m.hookUnsyncResults(ctx, names, force, true)
	if err != nil {
		return err
	}
	blocked := classifyHookUnsync(results, true)
	for _, name := range names {
		if row, ok := blocked[name]; ok {
			keepBlockedHook(name, row, rows, kept)
			continue
		}
		*rows = append(*rows, Row{Kind: "hook", Name: name, Action: "would-remove"})
	}
	return nil
}

// hookUnsyncResults runs hook unsync for the projected subset of names.
// dryRun is passed through so a preview writes nothing. Never-projected
// names are absent from the results; the caller treats that absence as
// removable.
func (m *Managers) hookUnsyncResults(ctx context.Context, names []string, force, dryRun bool) ([]hooksync.UnsyncResult, error) {
	projected, err := projectedNames(ctx, m.Home, project.KindHook, names)
	if err != nil {
		return nil, err
	}
	if len(projected) == 0 {
		return nil, nil
	}
	return m.Hooks.Unsync(ctx, projected, hooksync.UnsyncOptions{Force: force, DryRun: dryRun})
}

// classifyHookUnsync groups unsync rows by hook and returns the first
// blocking row for each hook that cannot be deleted. dryRun counts
// would-remove as retired, because that is what a dry-run removed row
// becomes. The real run does not: blockingHookUnsync then delegates to
// hooksync.BlocksRemoval, which treats would-remove as blocking. A hook
// with two client rows is blocked if either row blocks.
func classifyHookUnsync(results []hooksync.UnsyncResult, dryRun bool) map[string]hooksync.UnsyncResult {
	byName := map[string][]hooksync.UnsyncResult{}
	for _, r := range results {
		byName[r.Hook] = append(byName[r.Hook], r)
	}
	blocked := map[string]hooksync.UnsyncResult{}
	for name, hookRows := range byName {
		if row, ok := blockingHookUnsync(hookRows, dryRun); ok {
			blocked[name] = row
		}
	}
	return blocked
}

// blockingHookUnsync reports the first row that keeps the hook. A real
// run uses hooksync.BlocksRemoval, which continues only on removed and
// already-gone. A dry run must not call that helper unchanged: Unsync
// rewrites a row that would retire from removed to would-remove, and
// BlocksRemoval treats would-remove as blocking.
func blockingHookUnsync(rows []hooksync.UnsyncResult, dryRun bool) (hooksync.UnsyncResult, bool) {
	if !dryRun {
		return hooksync.BlocksRemoval(rows)
	}
	for _, r := range rows {
		switch r.Action {
		case hooksync.ActionWouldRemove, hooksync.ActionAlreadyGone:
			continue
		default:
			return r, true
		}
	}
	return hooksync.UnsyncResult{}, false
}

func keepBlockedHook(name string, row hooksync.UnsyncResult, rows *[]Row, kept *[]string) {
	*rows = append(*rows, Row{
		Kind:        "hook",
		Name:        name,
		Client:      row.Client,
		Action:      row.Action,
		Detail:      firstNonEmpty(row.Error, row.Detail),
		Remediation: hooksync.RemovalRefusal(name, row),
	})
	*kept = append(*kept, "hook/"+name)
}

// ruleClass is the store state of one name in a pack's rule list,
// read before any remove write.
type ruleClass int

const (
	ruleClassAbsent ruleClass = iota
	ruleClassUnreadable
	ruleClassModified
	ruleClassRemovable
)

// classifiedRule is one pack-listed fragment. Unreadable never becomes
// removable: there is no hash to compare, and RemoveFragment must not
// be called for it. Modified becomes removable only when the caller
// passes --force.
type classifiedRule struct {
	name  string
	class ruleClass
	err   error
	prior skills.LockedRule
}

// classifyPackRules reads each name in locked.Rules. Callers must check
// m.Contexts != nil first. ErrNoFragment and ErrFragmentsInactive are
// absent, so a home that deactivated fragments mode does not block pack
// remove. Any other read error, including a frontmatter parse error
// (ReadFragment returns no fragment), is unreadable and is kept.
func (m *Managers) classifyPackRules(locked *skills.LockedPack) []classifiedRule {
	if locked == nil {
		return nil
	}
	out := make([]classifiedRule, 0, len(locked.Rules))
	for _, name := range locked.Rules {
		var prior skills.LockedRule
		if locked.RuleFiles != nil {
			prior = locked.RuleFiles[name]
		}
		frag, err := m.Contexts.ReadFragment(name)
		switch {
		case errors.Is(err, contexts.ErrNoFragment), errors.Is(err, contexts.ErrFragmentsInactive):
			out = append(out, classifiedRule{name: name, class: ruleClassAbsent})
		case err != nil:
			out = append(out, classifiedRule{name: name, class: ruleClassUnreadable, err: err})
		case ruleIsUnmodified(prior, frag.Raw):
			out = append(out, classifiedRule{name: name, class: ruleClassRemovable, prior: prior})
		default:
			out = append(out, classifiedRule{name: name, class: ruleClassModified, prior: prior})
		}
	}
	return out
}

// appendRuleStoreRows emits one store row per classified rule, in record
// order. dryRun writes nothing. A kept name is appended as rule/<name>
// so trimLockedPack retains the pack record and a retry is possible.
// An error row without that kept entry would drop the record and let
// the CLI print a success line for a file that is still on disk.
func appendRuleStoreRows(packName, repo string, classified []classifiedRule, force, dryRun bool, remove func(string) error, rows *[]Row, kept *[]string) {
	for _, c := range classified {
		removable := c.class == ruleClassRemovable || (c.class == ruleClassModified && force)
		switch {
		case c.class == ruleClassAbsent:
			*rows = append(*rows, Row{Kind: "rule", Name: c.name, Action: "already-gone", Detail: "not in the fragment store"})
		case c.class == ruleClassUnreadable:
			detail := "fragment could not be read"
			if c.err != nil {
				detail = c.err.Error()
			}
			*rows = append(*rows, Row{
				Kind:        "rule",
				Name:        c.name,
				Action:      "error",
				Detail:      detail,
				Remediation: fmt.Sprintf("fix or remove the fragment with 'gridctl ctx rm %s', then re-run 'gridctl pack remove %s'", c.name, packName),
			})
			*kept = append(*kept, "rule/"+c.name)
		case c.class == ruleClassModified && !force:
			*rows = append(*rows, Row{
				Kind:        "rule",
				Name:        c.name,
				Action:      "skipped-drift",
				Detail:      "the fragment store copy differs from what the pack installed",
				Remediation: ruleDriftRemediation(packName, repo, c.name, c.prior),
			})
			*kept = append(*kept, "rule/"+c.name)
		case removable && dryRun:
			*rows = append(*rows, Row{Kind: "rule", Name: c.name, Action: "would-remove"})
		case removable:
			if remove == nil {
				*rows = append(*rows, Row{Kind: "rule", Name: c.name, Action: "error", Detail: "fragment store is not available"})
				*kept = append(*kept, "rule/"+c.name)
				continue
			}
			if err := remove(c.name); err != nil {
				if errors.Is(err, contexts.ErrNoFragment) {
					*rows = append(*rows, Row{Kind: "rule", Name: c.name, Action: "already-gone", Detail: "not in the fragment store"})
					continue
				}
				*rows = append(*rows, Row{Kind: "rule", Name: c.name, Action: "error", Detail: err.Error()})
				*kept = append(*kept, "rule/"+c.name)
				continue
			}
			*rows = append(*rows, Row{
				Kind:        "rule",
				Name:        c.name,
				Action:      "removed",
				Detail:      "fragment store entry removed",
				Remediation: "run 'gridctl ctx sync' to drop projected client files for this fragment",
			})
		}
	}
}

// ruleDriftRemediation names both ways out of a local edit, or the
// re-add path when the lockfile has no recorded hash. An empty hash is
// unknown provenance, which ruleIsUnmodified never treats as a match.
func ruleDriftRemediation(packName, repo, name string, prior skills.LockedRule) string {
	if prior.ContentHash == "" {
		target := repo
		if target == "" {
			target = packName
		}
		return fmt.Sprintf("re-run 'gridctl pack add %s' to record provenance, or force removal with --force", target)
	}
	return fmt.Sprintf("keep the edit by renaming the fragment, or discard it with 'gridctl ctx rm %s' before removing, or force removal with --force", name)
}

func packSourceRepo(lf *skills.LockFile, name string) string {
	if lf == nil {
		return ""
	}
	_, src, ok := lf.FindPackSource(name)
	if !ok || src == nil {
		return ""
	}
	return src.Repo
}

// splitKept separates removable names from drift-kept ones, emitting
// skip rows for the latter.
func splitKept(names []string, drifted map[string]bool, force bool, kind string, rows *[]Row, kept *[]string) []string {
	var removable []string
	for _, n := range names {
		if drifted[n] && !force {
			remediation := fmt.Sprintf("keep the edit with 'gridctl skill project adopt --kind %s %s --client <slug>' before removing, or force removal with --force", kind, n)
			if kind == "hook" {
				remediation = fmt.Sprintf("force removal with --force, or rewrite with 'gridctl skill project sync --kind hook %s --force'", n)
			}
			*rows = append(*rows, Row{Kind: kind, Name: n, Action: "skipped-drift",
				Detail:      "a projection of this " + kind + " was hand-edited",
				Remediation: remediation})
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

// removePackHooks deletes imported hook directories, sidecars, and pin
// records. names are the hooks whose projections retired, or that were
// never projected. A kept hook must not be passed here.
func removePackHooks(ctx context.Context, home string, names []string, rows *[]Row) error {
	if len(names) == 0 {
		return nil
	}
	registryDir := filepath.Join(home, ".gridctl", "registry")
	pins := hookpins.New()
	if err := pins.Load(); err != nil && !errors.Is(err, hookpins.ErrCorrupt) {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := skills.DeleteHook(registryDir, name); err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "not found") {
			*rows = append(*rows, Row{Kind: "hook", Name: name, Action: "error", Detail: err.Error()})
			continue
		}
		if err := pins.Reset(ctx, name); err != nil && !errors.Is(err, hookpins.ErrNotPinned) {
			*rows = append(*rows, Row{Kind: "hook", Name: name, Action: "error", Detail: err.Error()})
			continue
		}
		*rows = append(*rows, Row{Kind: "hook", Name: name, Action: "removed", Detail: "registry entry removed"})
	}
	return nil
}

// trimLockedPack drops the pack record, or shrinks it to the resources
// a partial removal kept. The read-modify-write cycle holds the import
// lockfile's cross-process lock.
func trimLockedPack(ctx context.Context, lockPath, name string, kept []string) error {
	return skills.MutateLockFile(ctx, lockPath, func(lf *skills.LockFile) (bool, error) {
		if len(kept) == 0 {
			for _, key := range lf.MemberSources(name) {
				lf.RemoveSource(key)
			}
		}
		srcName, src, ok := lf.FindPackSource(name)
		if !ok {
			return len(kept) == 0, nil // source already GC'd; members were dropped above
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
		var hooksKept []string
		for _, n := range src.Pack.Hooks {
			if keptSet["hook/"+n] {
				hooksKept = append(hooksKept, n)
			}
		}
		src.Pack.Skills = skillsKept
		src.Pack.Agents = agentsKept
		src.Pack.Hooks = hooksKept
		// Rules and RuleFiles are trimmed together. migrateRuleFiles
		// backfills only from Rules, so a dropped name cannot return.
		// The field is omitempty, so an empty map is omitted on write
		// and reads back as nil; the trimmed Rules list is the guard.
		var rulesKept []string
		for _, n := range src.Pack.Rules {
			if keptSet["rule/"+n] {
				rulesKept = append(rulesKept, n)
			}
		}
		src.Pack.Rules = rulesKept
		if src.Pack.RuleFiles != nil {
			for n := range src.Pack.RuleFiles {
				if !keptSet["rule/"+n] {
					delete(src.Pack.RuleFiles, n)
				}
			}
		}
		if len(src.Pack.Sources) > 0 {
			for sourceName, ps := range src.Pack.Sources {
				ps.Skills = filterKept(ps.Skills, "skill", keptSet)
				ps.Agents = filterKept(ps.Agents, "agent", keptSet)
				src.Pack.Sources[sourceName] = ps
			}
		}
		lf.SetSource(srcName, *src)
		return true, nil
	})
}

func memberOwnedNames(lf *skills.LockFile, packName string) (skillNames, agentNames []string) {
	if lf == nil {
		return nil, nil
	}
	skillsSet := map[string]struct{}{}
	agentsSet := map[string]struct{}{}
	for _, key := range lf.MemberSources(packName) {
		src := lf.Sources[key]
		for name := range src.Skills {
			skillsSet[name] = struct{}{}
		}
		for name := range src.Agents {
			agentsSet[name] = struct{}{}
		}
	}
	return sortedSet(skillsSet), sortedSet(agentsSet)
}

func sortedSet(in map[string]struct{}) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for name := range in {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func unionNames(primary, extra []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(primary)+len(extra))
	for _, name := range primary {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, name := range extra {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func filterKept(names []string, kind string, kept map[string]bool) []string {
	var out []string
	for _, n := range names {
		if kept[kind+"/"+n] {
			out = append(out, n)
		}
	}
	return out
}
