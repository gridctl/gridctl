package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// ErrSourceConflict is a source-name or source-kind collision. REST maps it
// to 409. The error text names the existing source and, for a directory
// collision, tells the caller to pass --source-name.
var ErrSourceConflict = errors.New("source name conflict")

// SourceConflictError is a source-key collision with an existing record.
type SourceConflictError struct {
	Name string
	Repo string
	Msg  string
}

func (e *SourceConflictError) Error() string {
	if e == nil {
		return ErrSourceConflict.Error()
	}
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("source %q already records %s; pass --source-name to import this directory under another name", e.Name, e.Repo)
}

func (e *SourceConflictError) Unwrap() error { return ErrSourceConflict }

// ErrAmbiguousOwner is returned when more than one lock source owns a name.
// Local update reports it before any mutation.
var ErrAmbiguousOwner = errors.New("ambiguous duplicate owners")

// SourceNameInput selects the lockfile key for one import.
type SourceNameInput struct {
	Kind         string
	Root         string
	Explicit     string
	KnownName    string
	ClientImport bool
}

// ResolveSourceName picks a stable lockfile key. An existing local source
// with the same resolved root is reused. An explicit name that differs from
// that key is an error. Client import suffixes a colliding basename with
// the first eight hex characters of sha256(root); skill add fails instead.
func ResolveSourceName(lf *LockFile, in SourceNameInput) (string, error) {
	if lf == nil {
		lf = &LockFile{}
	}
	if in.Kind == SourceKindLocal {
		if name, ok := findLocalByRepo(lf, in.Root); ok {
			if in.Explicit != "" && in.Explicit != name {
				return "", fmt.Errorf("source %q already tracks %s; --source-name %q would create a second source for the same root", name, in.Root, in.Explicit)
			}
			return name, nil
		}
		candidate := in.Explicit
		if candidate == "" {
			candidate = in.KnownName
		}
		if candidate == "" {
			candidate = filepath.Base(in.Root)
		}
		if existing, ok := lf.Sources[candidate]; ok {
			if in.ClientImport && in.Explicit == "" {
				suffixed := candidate + "-" + rootSuffix(in.Root)
				if taken, exists := lf.Sources[suffixed]; exists {
					return "", &SourceConflictError{Name: suffixed, Repo: taken.Repo}
				}
				return suffixed, nil
			}
			return "", &SourceConflictError{Name: candidate, Repo: existing.Repo}
		}
		return candidate, nil
	}
	if in.Explicit != "" {
		return in.Explicit, nil
	}
	return RepoToName(in.Root), nil
}

func rootSuffix(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])[:8]
}

func findLocalByRepo(lf *LockFile, root string) (string, bool) {
	if lf == nil {
		return "", false
	}
	var names []string
	for name, src := range lf.Sources {
		if src.IsLocal() && src.Repo == root {
			names = append(names, name)
		}
	}
	if len(names) != 1 {
		return "", false
	}
	return names[0], true
}

// GuardSourceKey refuses to replace an existing local source key with a
// different root or source kind. Git-to-git reuse is unchanged.
func GuardSourceKey(lf *LockFile, name string, kind, repo string) error {
	if lf == nil {
		return nil
	}
	existing, ok := lf.Sources[name]
	if !ok {
		return nil
	}
	if kind == SourceKindLocal {
		if existing.IsLocal() && existing.Repo == repo {
			return nil
		}
		if existing.IsLocal() {
			return fmt.Errorf("source %q already tracks %s; --source-name %q would create a second source for the same root", name, existing.Repo, name)
		}
		return &SourceConflictError{
			Name: name,
			Repo: existing.Repo,
			Msg:  fmt.Sprintf("source %q already records %s; pass --source-name to import this directory under another name", name, existing.Repo),
		}
	}
	if existing.IsLocal() {
		return &SourceConflictError{
			Name: name,
			Repo: existing.Repo,
			Msg:  fmt.Sprintf("source %q is a local source at %s; pass --source-name to import this repository under another name", name, existing.Repo),
		}
	}
	return nil
}

func skillOwners(lf *LockFile, name string) []string {
	return ownersOf(lf, name, true)
}

func agentOwners(lf *LockFile, name string) []string {
	return ownersOf(lf, name, false)
}

func ownersOf(lf *LockFile, name string, skill bool) []string {
	if lf == nil {
		return nil
	}
	var owners []string
	for srcName, src := range lf.Sources {
		if skill {
			if _, ok := src.Skills[name]; ok {
				owners = append(owners, srcName)
			}
			continue
		}
		if _, ok := src.Agents[name]; ok {
			owners = append(owners, srcName)
		}
	}
	sort.Strings(owners)
	return owners
}

// AllowOverwrite reports whether an existing resource may be replaced.
// Local re-imports of an unowned name, or a name owned only by sourceName,
// do not need --force. A local owner always requires explicit force from a
// git or pack caller. Git-only selected overwrite is unchanged.
func AllowOverwrite(lf *LockFile, kind, name, sourceName string, selected, explicitForce, agent bool) bool {
	if explicitForce {
		return true
	}
	owners := ownersOf(lf, name, !agent)
	if otherLocalOwner(lf, owners, sourceName) {
		return false
	}
	same := len(owners) == 1 && owners[0] == sourceName
	unowned := len(owners) == 0
	if kind == SourceKindLocal {
		return unowned || same
	}
	if agent {
		return selected && (unowned || same)
	}
	return selected
}

// GuardPackOwnership refuses a pack import that would replace a local-owned
// resource. Packs have no force option, so this is an error before any write.
func GuardPackOwnership(lf *LockFile, sourceName string, result *CloneResult, opts ImportOptions) error {
	if result == nil {
		return nil
	}
	selected := map[string]bool{}
	for _, name := range opts.Selected {
		selected[name] = true
	}
	selectedAgents := map[string]bool{}
	for _, name := range opts.SelectedAgents {
		selectedAgents[name] = true
	}
	if opts.ResourceKind != ResourceKindAgent {
		for _, sk := range result.Skills {
			if len(opts.Selected) > 0 && !selected[sk.Name] {
				continue
			}
			if otherLocalOwner(lf, skillOwners(lf, sk.Name), sourceName) {
				return fmt.Errorf("pack import would overwrite skill %q owned by a local source", sk.Name)
			}
		}
	}
	if opts.ResourceKind == ResourceKindSkill {
		return nil
	}
	if len(opts.Selected) > 0 && len(opts.SelectedAgents) == 0 {
		return nil
	}
	for _, ag := range result.Agents {
		if len(opts.SelectedAgents) > 0 && !selectedAgents[ag.Name] {
			continue
		}
		if otherLocalOwner(lf, agentOwners(lf, ag.Name), sourceName) {
			return fmt.Errorf("pack import would overwrite agent %q owned by a local source", ag.Name)
		}
	}
	return nil
}

func otherLocalOwner(lf *LockFile, owners []string, sourceName string) bool {
	for _, owner := range owners {
		if owner == sourceName {
			continue
		}
		if src, ok := lf.Sources[owner]; ok && src.IsLocal() {
			return true
		}
	}
	return false
}

func copySkills(in map[string]LockedSkill) map[string]LockedSkill {
	out := make(map[string]LockedSkill, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyAgents(in map[string]LockedAgent) map[string]LockedAgent {
	out := make(map[string]LockedAgent, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sourceEmpty(src LockedSource) bool {
	return len(src.Skills) == 0 && len(src.Agents) == 0 && src.Pack == nil
}

// ReleaseInstalledNames removes installed names from other sources when a
// local source is involved. Unrelated skills, agents, and pack metadata
// stay. An emptied source is deleted only when no tracked content or pack
// record remains. Git-only owners are left in place.
func ReleaseInstalledNames(lf *LockFile, destination string, destLocal bool, skills, agents map[string]struct{}) {
	release := func(name string, skill bool) {
		for srcName, src := range lf.Sources {
			if srcName == destination {
				continue
			}
			var ok bool
			if skill {
				_, ok = src.Skills[name]
			} else {
				_, ok = src.Agents[name]
			}
			if !ok {
				continue
			}
			if !destLocal && !src.IsLocal() {
				continue
			}
			if skill {
				delete(src.Skills, name)
			} else {
				delete(src.Agents, name)
			}
			if src.IsLocal() {
				src.ContentHash = CombineTrackedSourceHash(src.Skills, src.Agents)
			}
			if sourceEmpty(src) {
				delete(lf.Sources, srcName)
			} else {
				lf.Sources[srcName] = src
			}
		}
	}
	for name := range skills {
		release(name, true)
	}
	for name := range agents {
		release(name, false)
	}
}

// RecordLocalSource merges installed entries into the previous local maps.
// Absent entries are dropped only when canDrop is set. The source hash is
// recomputed from the merged maps.
func RecordLocalSource(lf *LockFile, sourceName, repo string, fetchedAt time.Time, installedSkills map[string]LockedSkill, installedAgents map[string]LockedAgent, discovered *CloneResult, canDrop bool) error {
	if err := GuardSourceKey(lf, sourceName, SourceKindLocal, repo); err != nil {
		return err
	}
	if name, ok := findLocalByRepo(lf, repo); ok && name != sourceName {
		return fmt.Errorf("source %q already tracks %s", name, repo)
	}
	prev := lf.Sources[sourceName]
	skills := copySkills(prev.Skills)
	agents := copyAgents(prev.Agents)
	for name, entry := range installedSkills {
		skills[name] = entry
	}
	for name, entry := range installedAgents {
		agents[name] = entry
	}
	if canDrop && discovered != nil {
		presentSkills := map[string]bool{}
		for _, sk := range discovered.Skills {
			presentSkills[sk.Name] = true
		}
		presentAgents := map[string]bool{}
		for _, ag := range discovered.Agents {
			presentAgents[ag.Name] = true
		}
		for _, path := range discovered.FilteredSkills {
			presentSkills[filepath.Base(path)] = true
			if path == "." || path == "" {
				presentSkills[filepath.Base(repo)] = true
			}
		}
		for name := range skills {
			if !presentSkills[name] {
				delete(skills, name)
			}
		}
		for name := range agents {
			if !presentAgents[name] {
				delete(agents, name)
			}
		}
	}
	lf.SetSource(sourceName, LockedSource{
		Kind:        SourceKindLocal,
		Repo:        repo,
		FetchedAt:   fetchedAt,
		ContentHash: CombineTrackedSourceHash(skills, agents),
		Skills:      skills,
		Agents:      agents,
		Pack:        prev.Pack,
	})
	installedSkillNames := map[string]struct{}{}
	for name := range installedSkills {
		installedSkillNames[name] = struct{}{}
	}
	installedAgentNames := map[string]struct{}{}
	for name := range installedAgents {
		installedAgentNames[name] = struct{}{}
	}
	ReleaseInstalledNames(lf, sourceName, true, installedSkillNames, installedAgentNames)
	return nil
}
