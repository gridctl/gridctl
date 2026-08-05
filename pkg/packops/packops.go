// Package packops orchestrates pack verbs (add, apply, status, remove)
// over the standalone kind managers (skillsync, agentsync, contexts,
// wiring). There is still no pack write path of its own: a pack expands
// into calls against the engines that own every write, and this package
// only owns the expansion. The CLI and the REST layer both drive these
// functions; rendering and exit codes stay with each caller.
package packops

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// SchemaVersion versions every pack document (Article X).
const SchemaVersion = 1

// Sentinel errors callers branch on with errors.Is; the user-facing
// prose rides on the concrete error unchanged.
var (
	// ErrNoManifest marks a repository without a gridctl-pack.yaml at
	// its root.
	ErrNoManifest = errors.New("no pack manifest found")
	// ErrNotImported marks a pack name with no record in the import
	// lockfile.
	ErrNotImported = errors.New("pack is not imported")
	// ErrNameCollision marks a pack name claimed by more than one
	// imported source; operations must refuse rather than pick one.
	ErrNameCollision = errors.New("pack name is claimed by multiple sources")
)

// packError pairs a sentinel with exact user-facing prose, so errors.Is
// dispatch never depends on substring matching.
type packError struct {
	reason error
	msg    string
}

func (e *packError) Error() string { return e.msg }
func (e *packError) Unwrap() error { return e.reason }

// Managers bundles the kind managers pack verbs orchestrate.
type Managers struct {
	Skills   *skillsync.Manager
	Agents   *agentsync.Manager
	Wiring   *wiring.Manager
	Contexts *contexts.Manager
	Home     string
	// LockPath overrides the import lockfile path (skills.lock.yaml).
	// Empty means the HOME-derived default. Callers that inject a custom
	// lockfile path into their importer (the API server) must set the
	// same path here, or the importer and the pack record would write
	// two different files.
	LockPath string
}

// lockPath returns the import lockfile path this engine operates on.
func (m *Managers) lockPath() string {
	if m.LockPath != "" {
		return m.LockPath
	}
	return skills.LockFilePath()
}

// Row is one resource line in pack output.
type Row struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Client      string `json:"client,omitempty"`
	Action      string `json:"action,omitempty"`
	State       string `json:"state,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// packSources returns every imported pack with its source, sorted by
// pack name for deterministic output.
func packSources(lf *skills.LockFile) []packSource {
	var out []packSource
	for srcName, src := range lf.Sources {
		if src.Pack == nil {
			continue
		}
		out = append(out, packSource{SourceName: srcName, Source: src, Pack: src.Pack})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pack.Name != out[j].Pack.Name {
			return out[i].Pack.Name < out[j].Pack.Name
		}
		return out[i].SourceName < out[j].SourceName
	})
	return out
}

type packSource struct {
	SourceName string
	Source     skills.LockedSource
	Pack       *skills.LockedPack
}

// findPack locates one pack by name, refusing on a name collision so a
// caller never acts on a nondeterministic pick.
func findPack(lf *skills.LockFile, name string) (packSource, error) {
	var matches []packSource
	for _, ps := range packSources(lf) {
		if ps.Pack.Name == name {
			matches = append(matches, ps)
		}
	}
	switch len(matches) {
	case 0:
		return packSource{}, &packError{
			reason: ErrNotImported,
			msg:    fmt.Sprintf("pack %q is not imported (run 'gridctl pack add <repo-url>' first; 'gridctl pack status' lists imported packs)", name),
		}
	case 1:
		return matches[0], nil
	default:
		repos := make([]string, 0, len(matches))
		for _, m := range matches {
			repos = append(repos, m.Source.Repo)
		}
		return packSource{}, &packError{
			reason: ErrNameCollision,
			msg:    fmt.Sprintf("pack name %q is claimed by multiple sources (%s); remove or re-add one so a single source owns the name", name, strings.Join(repos, ", ")),
		}
	}
}

// LoadLockedPack finds a pack's record in the default import lockfile.
func LoadLockedPack(name string) (*skills.LockedPack, error) {
	return loadLockedPackFrom(skills.LockFilePath(), name)
}

// LoadLockedPack finds a pack's record in this engine's lockfile.
func (m *Managers) LoadLockedPack(name string) (*skills.LockedPack, error) {
	return loadLockedPackFrom(m.lockPath(), name)
}

func loadLockedPackFrom(path, name string) (*skills.LockedPack, error) {
	lf, err := skills.ReadLockFile(path)
	if err != nil {
		return nil, err
	}
	ps, err := findPack(lf, name)
	if err != nil {
		return nil, err
	}
	return ps.Pack, nil
}

// foreignPackTags returns, per kind, the resource names whose recorded
// projections are tagged by a different pack.
func foreignPackTags(ctx context.Context, home, packName string) (map[string]string, error) {
	l, err := project.NewStore(home).Load(ctx)
	if err != nil {
		return nil, err
	}
	foreign := map[string]string{}
	for _, kind := range []project.Kind{project.KindSkill, project.KindAgent, project.KindWiring, project.KindContextFragment} {
		for _, e := range l.Entries(kind) {
			if e.Pack != "" && e.Pack != packName {
				foreign[string(kind)+"/"+e.Source] = e.Pack
			}
		}
	}
	return foreign, nil
}

// filterForeign splits a selection into syncable names, emitting refusal
// rows for resources tagged by a different pack.
func filterForeign(names []string, kind string, foreign map[string]string, addRow func(Row)) []string {
	var out []string
	for _, n := range names {
		if owner, ok := foreign[kind+"/"+n]; ok {
			addRow(Row{Kind: kind, Name: n, Action: "skipped-foreign-pack",
				Detail:      fmt.Sprintf("already managed by pack %q", owner),
				Remediation: fmt.Sprintf("remove it from one pack ('gridctl pack remove %s' or edit the manifests) so a single pack owns it", owner)})
			continue
		}
		out = append(out, n)
	}
	return out
}

// TallyRows counts clean rows vs rows needing attention.
func TallyRows(rows []Row) (applied, failed int) {
	for _, r := range rows {
		switch {
		case strings.HasPrefix(r.Action, "skipped") || r.Action == "error" || r.Action == "unresolved":
			failed++
		default:
			applied++
		}
	}
	return applied, failed
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// runningGatewayPort reports a running gateway's port, if any.
func runningGatewayPort() (int, bool) {
	states, err := state.List()
	if err != nil {
		return 0, false
	}
	for _, s := range states {
		if state.IsRunning(&s) {
			return s.Port, true
		}
	}
	return 0, false
}
