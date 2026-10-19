package packops

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// StatusOptions parameterizes a status pass.
type StatusOptions struct {
	// Pack restricts the report to one pack; empty means all.
	Pack string
	// GatewayPort feeds the wiring status probe (the caller resolves it:
	// the CLI from running state, the API server from its own listener).
	GatewayPort int
}

// Origin identifies where a pack was imported from, straight off its
// parent lockfile source.
type Origin struct {
	Source    string    `json:"source"`
	Repo      string    `json:"repo"`
	Ref       string    `json:"ref,omitempty"`
	CommitSHA string    `json:"commit_sha,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
}

// Counts summarizes a pack's resolved selection per kind.
type Counts struct {
	Skills int  `json:"skills"`
	Agents int  `json:"agents"`
	Rules  int  `json:"rules"`
	Wiring bool `json:"wiring"`
	// Stack is true when the pack carries a resolved stack record.
	// omitempty keeps packs without a stack on the previous JSON shape.
	Stack bool `json:"stack,omitempty"`
	// Sources is the number of pinned external sources. omitempty keeps
	// packs without sources on the previous JSON shape.
	Sources int `json:"sources,omitempty"`
}

// PackInfo is the identity half of a pack status: everything a list view
// needs without the per-resource rows.
type PackInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Origin      Origin   `json:"origin"`
	Counts      Counts   `json:"counts"`
	Unresolved  []string `json:"unresolved,omitempty"`
	// Applied reports whether any per-client projection exists: an
	// imported-but-never-applied pack is registry-only, and list views
	// surface that as attention rather than reading healthy.
	Applied bool `json:"applied"`
	// Collision marks a pack name claimed by more than one source; the
	// listed repos disambiguate. Detail fetches for a colliding name
	// refuse instead of picking one.
	Collision      bool            `json:"collision,omitempty"`
	CollisionRepos []string        `json:"collision_repos,omitempty"`
	Sources        []SourceSummary `json:"sources,omitempty"`
	// Extends is the base pack a booster composes onto.
	Extends string `json:"extends,omitempty"`
}

// PackStatus is one pack's identity plus its per-resource state rows.
type PackStatus struct {
	Info           PackInfo `json:"info"`
	Rows           []Row    `json:"rows"`
	NeedsAttention bool     `json:"needs_attention"`
}

// Statuses reports the state matrix for one or all imported packs,
// sorted by pack name. Skill, agent, and wiring rows come from the kind
// managers' per-client statuses; rule rows report per-client projection
// state from the context engine (pack-tagged lock entries joined with
// per-fragment status), falling back to a store-presence row for rules
// that were imported but never projected.
func (m *Managers) Statuses(ctx context.Context, opts StatusOptions) ([]PackStatus, error) {
	lf, err := skills.ReadLockFile(m.lockPath())
	if err != nil {
		return nil, err
	}
	sources := packSources(lf)
	if opts.Pack != "" {
		if _, err := findPack(lf, opts.Pack); err != nil {
			// Preserve the status verb's shorter not-imported prose.
			var pe *packError
			if errors.As(err, &pe) && errors.Is(err, ErrNotImported) {
				return nil, &packError{reason: ErrNotImported, msg: fmt.Sprintf("pack %q is not imported", opts.Pack)}
			}
			return nil, err
		}
		var filtered []packSource
		for _, ps := range sources {
			if ps.Pack.Name == opts.Pack {
				filtered = append(filtered, ps)
			}
		}
		sources = filtered
	}
	if len(sources) == 0 {
		return nil, nil
	}

	skillStatuses, err := m.Skills.Statuses(ctx)
	if err != nil {
		return nil, err
	}
	agentStatuses, err := m.Agents.Statuses(ctx)
	if err != nil {
		return nil, err
	}
	wiringRows, err := m.Wiring.Statuses(ctx, wiring.StatusOptions{Port: opts.GatewayPort})
	if err != nil {
		return nil, err
	}
	ruleDeps, err := m.loadRuleStatusDeps(ctx, sources)
	if err != nil {
		return nil, err
	}

	collisions := map[string][]string{}
	for _, ps := range sources {
		collisions[ps.Pack.Name] = append(collisions[ps.Pack.Name], ps.Source.Repo)
	}

	var out []PackStatus
	for _, ps := range sources {
		p := ps.Pack
		members := map[string]skills.LockedSource{}
		for _, key := range lf.MemberSources(p.Name) {
			if src, ok := lf.Sources[key]; ok {
				members[memberSourceName(p.Name, key)] = src
			}
		}
		cache, _ := skills.ReadUpdateCacheAt(skills.UpdateCachePath())
		rows, attention := m.statusRowsFor(p, ps.Source.Repo, members, cache, skillStatuses, agentStatuses, wiringRows, ruleDeps)
		info := PackInfo{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Author:      p.Author,
			Origin: Origin{
				Source:    ps.SourceName,
				Repo:      ps.Source.Repo,
				Ref:       ps.Source.Ref,
				CommitSHA: ps.Source.CommitSHA,
				FetchedAt: ps.Source.FetchedAt,
			},
			Counts:     Counts{Skills: len(p.Skills), Agents: len(p.Agents), Rules: len(p.Rules), Wiring: p.Wiring, Stack: p.Stack != nil || p.Fragment != nil, Sources: len(p.Sources)},
			Extends:    packExtends(p),
			Sources:    statusSourceSummaries(p),
			Unresolved: p.Unresolved,
		}
		for _, r := range rows {
			if r.Client != "" || (r.Kind == "stack" && (r.State == "in-sync" || r.State == "stale")) {
				info.Applied = true
				break
			}
		}
		if repos := collisions[p.Name]; len(repos) > 1 {
			info.Collision = true
			info.CollisionRepos = repos
			attention = true
		}
		out = append(out, PackStatus{Info: info, Rows: rows, NeedsAttention: attention})
	}
	return out, nil
}

// ruleStatusDeps caches the context-engine data rule rows join against.
type ruleStatusDeps struct {
	active bool
	// entries maps fragment name to its pack-tagged projection lock
	// entries (client + pack tag).
	entries map[string][]*project.Entry
	// bySlug indexes the context engine's per-client statuses.
	bySlug map[string]contexts.ClientStatus
}

// loadRuleStatusDeps loads the projection lock entries and per-client
// context statuses once, and only when some pack actually selects rules.
func (m *Managers) loadRuleStatusDeps(ctx context.Context, sources []packSource) (*ruleStatusDeps, error) {
	anyRules := false
	for _, ps := range sources {
		if len(ps.Pack.Rules) > 0 {
			anyRules = true
			break
		}
	}
	deps := &ruleStatusDeps{}
	if !anyRules || m.Contexts == nil || !m.Contexts.FragmentsActive() {
		return deps, nil
	}
	deps.active = true

	l, err := project.NewStore(m.Home).Load(ctx)
	if err != nil {
		return nil, err
	}
	deps.entries = map[string][]*project.Entry{}
	for _, e := range l.Entries(project.KindContextFragment) {
		if e.Pack == "" {
			continue
		}
		deps.entries[e.Source] = append(deps.entries[e.Source], e)
	}

	statuses, err := m.Contexts.Statuses(ctx)
	if err != nil {
		return nil, err
	}
	deps.bySlug = map[string]contexts.ClientStatus{}
	for _, cs := range statuses {
		deps.bySlug[cs.Slug] = cs
	}
	return deps, nil
}

// statusRowsFor builds one pack's rows in kind order: stack, skills,
// agents, rules, wiring, unresolved.
func (m *Managers) statusRowsFor(p *skills.LockedPack, packRepo string, members map[string]skills.LockedSource, cache *skills.UpdateStatus, skillStatuses []skillsync.ProjectionStatus, agentStatuses []agentsync.ProjectionStatus, wiringRows []wiring.Row, ruleDeps *ruleStatusDeps) ([]Row, bool) {
	var rows []Row
	attention := false
	home, homeErr := m.homeDir()
	lf, _ := skills.ReadLockFile(m.lockPath())
	if p.Fragment != nil {
		row, fragmentAttention := statusBoosterStackRow(home, homeErr, p, lf, packRepo)
		rows = append(rows, row)
		attention = attention || fragmentAttention
	} else if p.Stack != nil {
		if homeErr != nil {
			rows = append(rows, Row{Kind: "stack", Name: p.Stack.Name, State: "target-missing", Detail: homeErr.Error(), Remediation: "re-run 'gridctl pack add'"})
			attention = true
		} else {
			target := pinnedStackPath(p.Stack)
			if lf != nil {
				if boosters := attachedBoosters(lf, p.Name); len(boosters) > 0 {
					target = composeTarget(home, p.Name, p.Stack, boosters)
				}
			}
			row, stackAttention := statusStackRow(home, p.Name, p.Stack, target)
			rows = append(rows, row)
			attention = attention || stackAttention
			if lf != nil {
				for _, row := range fragmentStatusRows(home, p.Name, p.Stack, attachedBoosters(lf, p.Name)) {
					rows = append(rows, row)
					if row.State == "stale" || row.State == "missing" {
						attention = true
					}
				}
			}
		}
	}
	if p.Lock != nil {
		row := Row{Kind: "lock", Name: pack.LockFileName, State: p.Lock.State}
		if p.Lock.State == "stale" {
			row.Remediation = fmt.Sprintf("run 'gridctl pack lock' in the pack repository, then re-run 'gridctl pack add %s'", packRepo)
			attention = true
		}
		rows = append(rows, row)
	}
	for _, name := range slices.Sorted(maps.Keys(p.Sources)) {
		row, sourceAttention := sourceStatusRow(packRepo, name, p.Sources[name], members[name], cache)
		rows = append(rows, row)
		attention = attention || sourceAttention
	}
	needsAttention := func(state string) bool {
		switch state {
		case skillsync.StateInSync, "missing":
			return false
		}
		return true
	}
	inSelection := func(names []string, n string) bool {
		for _, s := range names {
			if s == n {
				return true
			}
		}
		return false
	}

	for _, s := range skillStatuses {
		if inSelection(p.Skills, s.Skill) {
			rows = append(rows, Row{Kind: "skill", Name: s.Skill, Client: s.Client, State: s.State, Detail: s.Detail})
			attention = attention || needsAttention(s.State)
		}
	}
	for _, s := range agentStatuses {
		if inSelection(p.Agents, s.Agent) {
			rows = append(rows, Row{Kind: "agent", Name: s.Agent, Client: s.Client, State: s.State, Detail: s.Detail})
			attention = attention || needsAttention(s.State)
		}
	}
	// Rules: per-client projection state joined from the pack-tagged lock
	// entries and the context engine's per-fragment statuses. A rule with
	// no projections yet keeps its store-presence row.
	for _, n := range p.Rules {
		if !ruleDeps.active {
			rows = append(rows, Row{Kind: "rule", Name: n, State: "missing", Detail: "fragments mode is not active"})
			attention = true
			continue
		}
		if _, rerr := m.Contexts.ReadFragment(n); rerr != nil {
			rows = append(rows, Row{Kind: "rule", Name: n, State: "missing", Detail: "not in the fragment store; re-run 'gridctl pack add'"})
			// "missing" is clean for skills (never projected); an absent
			// pack rule is attention: the pack claims it.
			attention = true
			continue
		}
		entries := ruleDeps.entries[n]
		var packEntries []*project.Entry
		for _, e := range entries {
			if e.Pack == p.Name {
				packEntries = append(packEntries, e)
			}
		}
		if len(packEntries) == 0 {
			rows = append(rows, Row{Kind: "rule", Name: n, State: skillsync.StateInSync})
			continue
		}
		for _, e := range packEntries {
			// Unknown must never read as clean: in-sync is asserted only
			// when the client still renders multi-file (its Fragments list
			// names every non-synced fragment, so absence means synced).
			// A client that stopped rendering multi-file, or vanished from
			// the status set, falls back to its aggregate state or
			// target-missing.
			state := "target-missing"
			if cs, ok := ruleDeps.bySlug[e.Client]; ok {
				if cs.Mode == contexts.ModeMultiFile {
					state = skillsync.StateInSync
					for _, f := range cs.Fragments {
						if f.Name == n {
							state = f.State
							break
						}
					}
				} else if cs.State != "" {
					state = cs.State
				}
			}
			rows = append(rows, Row{Kind: "rule", Name: n, Client: e.Client, State: state})
			attention = attention || state != skillsync.StateInSync
		}
	}
	for _, r := range wiringRows {
		if r.Pack == p.Name {
			rows = append(rows, Row{Kind: "wiring", Name: r.Name, Client: r.Client, State: r.State, Detail: r.Detail, Remediation: r.Remediation})
			attention = attention || needsAttention(r.State)
		}
	}
	for _, u := range p.Unresolved {
		detail := "selected by the pack manifest but not shipped by the repository"
		if p.UnresolvedDetails != nil && p.UnresolvedDetails[u] != "" {
			detail = p.UnresolvedDetails[u]
		}
		rows = append(rows, Row{Kind: "unresolved", Name: u, State: "unresolved", Detail: detail})
		attention = true
	}
	return rows, attention
}

func memberSourceName(packName, key string) string {
	prefix := packName + "/"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		return key[len(prefix):]
	}
	return key
}

func statusSourceSummaries(p *skills.LockedPack) []SourceSummary {
	if len(p.Sources) == 0 {
		return nil
	}
	out := make([]SourceSummary, 0, len(p.Sources))
	for _, name := range slices.Sorted(maps.Keys(p.Sources)) {
		ps := p.Sources[name]
		out = append(out, SourceSummary{
			Name:      name,
			Repo:      redactRepo(ps.Repo),
			Ref:       ps.Ref,
			CommitSHA: ps.CommitSHA,
			Skills:    orEmpty(ps.Skills),
			Agents:    orEmpty(ps.Agents),
		})
	}
	return out
}

func sourceStatusRow(packRepo, name string, ps skills.LockedPackSource, member skills.LockedSource, cache *skills.UpdateStatus) (Row, bool) {
	detail := sourceDetail(ps.Repo, ps.Ref, ps.CommitSHA)
	checkable := len(ps.Skills) > 0 && member.CredentialRef == ""
	if !checkable {
		return Row{Kind: "source", Name: name, State: "in-sync", Detail: detail + ", freshness not checked"}, false
	}
	if member.LockedBy != "" {
		detail += ", locked"
		if cache != nil {
			for _, skillName := range ps.Skills {
				upd, ok := cache.Updates[skillName]
				if ok && upd.Repo == ps.Repo && upd.LatestSHA != "" && upd.LatestSHA != ps.CommitSHA {
					detail += ", newer upstream " + skills.ShortSHA(upd.LatestSHA)
					break
				}
			}
		}
		return Row{Kind: "source", Name: name, State: "in-sync", Detail: detail}, false
	}
	if cache != nil {
		for _, skillName := range ps.Skills {
			upd, ok := cache.Updates[skillName]
			if ok && upd.Repo == ps.Repo && upd.LatestSHA != "" && upd.LatestSHA != ps.CommitSHA {
				return Row{
					Kind:        "source",
					Name:        name,
					State:       "stale",
					Detail:      detail,
					Remediation: fmt.Sprintf("re-run 'gridctl pack add %s'", packRepo),
				}, true
			}
		}
	}
	return Row{Kind: "source", Name: name, State: "in-sync", Detail: detail}, false
}

func packExtends(p *skills.LockedPack) string {
	if p == nil || p.Fragment == nil {
		return ""
	}
	return p.Fragment.Base
}

func statusBoosterStackRow(home string, homeErr error, p *skills.LockedPack, lf *skills.LockFile, packRepo string) (Row, bool) {
	name := p.Fragment.Path
	readd := fmt.Sprintf("re-add the base, then re-run 'gridctl pack add %s'", packRepo)
	if homeErr != nil {
		return Row{Kind: "stack", Name: name, State: "target-missing", Detail: homeErr.Error(), Remediation: "re-run 'gridctl pack add'"}, true
	}
	if _, absent := fragmentCheckoutMissing(p.Fragment); absent {
		return Row{
			Kind: "stack", Name: name, State: "target-missing",
			Detail:      fmt.Sprintf("checkout or stack file is missing: %s", p.Fragment.CheckoutDir),
			Remediation: fmt.Sprintf("re-run 'gridctl pack add %s'", packRepo),
		}, true
	}
	if lf == nil {
		return Row{Kind: "stack", Name: name, State: "target-missing", Detail: fmt.Sprintf("base pack %q is not added", p.Fragment.Base), Remediation: readd}, true
	}
	base, err := findPack(lf, p.Fragment.Base)
	if err != nil {
		detail := fmt.Sprintf("base pack %q is not added", p.Fragment.Base)
		return Row{Kind: "stack", Name: name, State: "target-missing", Detail: detail, Remediation: readd}, true
	}
	if base.Pack.Stack == nil {
		return Row{Kind: "stack", Name: name, State: "target-missing", Detail: fmt.Sprintf("base pack %q carries no stack", p.Fragment.Base)}, true
	}
	run, err := classifyRunningStack(home, p.Fragment.Base, base.Pack.Stack.Name, ComposedStackPath(home, p.Fragment.Base))
	if err != nil {
		return Row{Kind: "stack", Name: name, State: "target-missing", Detail: err.Error()}, true
	}
	if !run.running {
		return Row{Kind: "stack", Name: name, State: "missing"}, false
	}
	layers, ok, err := config.ReadComposedHeader(run.stackFile)
	if err != nil || !ok {
		return Row{
			Kind: "stack", Name: name, State: "stale",
			Detail:      fmt.Sprintf("not listed in %s", p.Fragment.Base),
			Remediation: fmt.Sprintf("re-run 'gridctl pack apply %s'", p.Fragment.Base),
		}, true
	}
	for _, layer := range layers {
		if layer.Pack != p.Name {
			continue
		}
		if layer.Commit == baseCommit(p, lf) || layer.Commit == checkoutCommit(home, p.Name, p.Fragment.CheckoutDir) || layer.Commit == fragmentCommit(lf, p.Name) {
			return Row{Kind: "stack", Name: name, State: "composed", Detail: fmt.Sprintf("composed into %s", p.Fragment.Base)}, false
		}
		return Row{
			Kind: "stack", Name: name, State: "stale",
			Detail:      fmt.Sprintf("listed at %s, pinned %s", skills.ShortSHA(layer.Commit), skills.ShortSHA(fragmentCommit(lf, p.Name))),
			Remediation: fmt.Sprintf("re-run 'gridctl pack apply %s'", p.Fragment.Base),
		}, true
	}
	return Row{
		Kind: "stack", Name: name, State: "stale",
		Detail:      fmt.Sprintf("not listed while %s is running", p.Fragment.Base),
		Remediation: fmt.Sprintf("re-run 'gridctl pack apply %s'", p.Fragment.Base),
	}, true
}

func fragmentCommit(lf *skills.LockFile, name string) string {
	if lf == nil {
		return ""
	}
	ps, err := findPack(lf, name)
	if err != nil {
		return ""
	}
	return ps.Source.CommitSHA
}

func baseCommit(p *skills.LockedPack, lf *skills.LockFile) string {
	return fragmentCommit(lf, p.Name)
}

func fragmentCheckoutMissing(frag *skills.LockedFragment) (string, bool) {
	if frag == nil || frag.CheckoutDir == "" {
		return "", true
	}
	info, err := os.Stat(frag.CheckoutDir)
	if err != nil || !info.IsDir() {
		return frag.CheckoutDir, true
	}
	path := filepath.Join(frag.CheckoutDir, filepath.FromSlash(frag.Path))
	info, err = os.Stat(path)
	if err != nil || info.IsDir() {
		return path, true
	}
	return "", false
}

func fragmentStatusRows(home, base string, st *skills.LockedStack, boosters []packSource) []Row {
	if len(boosters) == 0 {
		return nil
	}
	composed := ComposedStackPath(home, base)
	layers, headerOK, _ := config.ReadComposedHeader(composed)
	listed := map[string]string{}
	for _, layer := range layers {
		listed[layer.Pack] = layer.Commit
	}
	var daemonFromPinned bool
	if st != nil {
		run, runErr := classifyRunningStack(home, base, st.Name, composed)
		daemonFromPinned = runErr == nil && run.running && run.owned && !run.pinned
	}
	var rows []Row
	for _, booster := range boosters {
		name := booster.Pack.Name
		file := booster.Pack.Fragment.Path
		short := skills.ShortSHA(booster.Source.CommitSHA)
		if _, absent := fragmentCheckoutMissing(booster.Pack.Fragment); absent {
			rows = append(rows, Row{
				Kind: "fragment", Name: name, State: "missing",
				Detail:      fmt.Sprintf("%s (%s)", file, short),
				Remediation: fmt.Sprintf("re-run 'gridctl pack add %s'", booster.Source.Repo),
			})
			continue
		}
		commit, ok := listed[name]
		if daemonFromPinned || !headerOK || !ok || commit != booster.Source.CommitSHA {
			rows = append(rows, Row{
				Kind: "fragment", Name: name, State: "stale",
				Detail:      fmt.Sprintf("%s (%s)", file, short),
				Remediation: fmt.Sprintf("re-run 'gridctl pack apply %s'", base),
			})
			continue
		}
		rows = append(rows, Row{
			Kind: "fragment", Name: name, State: "in-sync",
			Detail: fmt.Sprintf("%s (%s)", file, short),
		})
	}
	return rows
}



func sourceDetail(repo, ref, sha string) string {
	repo = redactRepo(repo)
	short := skills.ShortSHA(sha)
	if ref == "" {
		return fmt.Sprintf("%s (%s)", repo, short)
	}
	return fmt.Sprintf("%s@%s (%s)", repo, ref, short)
}
