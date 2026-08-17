package packops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

// AddOptions parameterizes a pack import.
type AddOptions struct {
	Repo   string
	Ref    string
	Path   string
	Trust  bool
	DryRun bool
	// BlockOnFindings refuses the whole import with a *FindingsError
	// before any write when the resolved selection carries security
	// findings and Trust is false. The CLI leaves it false (partial
	// import with per-resource skips, its documented contract); the REST
	// layer sets it so a 409 can never follow a half-done import.
	BlockOnFindings bool
	// Auth authenticates the clone and, when it carries a CredentialRef,
	// is persisted by reference so a later update re-resolves without the
	// caller re-supplying credentials. A zero value keeps the ambient
	// behavior (ssh-agent for SSH, GITHUB_TOKEN for HTTPS, else anonymous).
	Auth skills.AuthConfig
}

// AddDoc is the machine-readable pack add document.
type AddDoc struct {
	SchemaVersion int      `json:"schema_version"`
	DryRun        bool     `json:"dry_run,omitempty"`
	Pack          string   `json:"pack"`
	Skills        []string `json:"skills"`
	Agents        []string `json:"agents"`
	Rules         []string `json:"rules,omitempty"`
	Wiring        bool     `json:"wiring"`
	Unresolved    []string `json:"unresolved,omitempty"`
	Skipped       []string `json:"skipped,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

// AddResult pairs the document with progress notes the CLI prints in
// order (rule updates, fragments-mode migration). Notes are caller-facing
// prose, not part of the versioned document.
type AddResult struct {
	Doc   AddDoc
	Notes []string
}

// Add clones, resolves the manifest selection, and imports.
func (m *Managers) Add(ctx context.Context, imp *skills.Importer, opts AddOptions) (*AddResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clone, err := skills.CloneAndDiscover(opts.Repo, opts.Ref, opts.Path, opts.Auth, slog.Default())
	if err != nil {
		return nil, err
	}

	manifest, err := pack.ParseFile(filepath.Join(clone.RepoPath, pack.ManifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &packError{
				reason: ErrNoManifest,
				msg:    fmt.Sprintf("no %s found at the repository root; 'gridctl pack add' imports pack repos (use 'gridctl skill add %s' for plain skill repos)", pack.ManifestFileName, opts.Repo),
			}
		}
		return nil, err
	}

	discoveredRules := discoverPackRules(clone.RepoPath)
	resolved := resolvePackSelection(manifest, clone, discoveredRules)

	if opts.BlockOnFindings && !opts.Trust {
		if flagged := scanSelection(clone, resolved, discoveredRules); len(flagged) > 0 {
			return nil, &FindingsError{Pack: manifest.Name, Resources: flagged}
		}
	}

	res := &AddResult{Doc: AddDoc{
		SchemaVersion: SchemaVersion,
		DryRun:        opts.DryRun,
		Pack:          manifest.Name,
		Skills:        resolved.skills,
		Agents:        resolved.agents,
		Rules:         resolved.rules,
		Wiring:        manifest.Wiring,
		Unresolved:    resolved.unresolved,
		Warnings:      manifest.Warnings(),
	}}

	if !opts.DryRun && (len(resolved.skills) > 0 || len(resolved.agents) > 0) {
		// Selection lists ride to the importer as the manifest wrote them
		// (unresolved names match nothing, so exactly the resolved subset
		// imports); an empty agents list expands to the discovered set so
		// the importer's legacy skip-agents-on-skill-selection contract
		// never hides a pack's agents.
		selectedSkills := manifest.Skills
		selectedAgents := manifest.Agents
		if len(selectedAgents) == 0 {
			selectedAgents = resolved.agents
		}
		result, ierr := imp.Import(skills.ImportOptions{
			Repo:           opts.Repo,
			Ref:            opts.Ref,
			Path:           opts.Path,
			Trust:          opts.Trust,
			Selected:       selectedSkills,
			SelectedAgents: selectedAgents,
			Discovered:     clone,
			// Discovered is set, so the importer does not re-clone and Auth
			// buys no network access here. It is what persists the
			// CredentialRef into the origin sidecars and the lockfile
			// source, which is what lets a later update re-resolve.
			Auth: opts.Auth,
		})
		if ierr != nil {
			return nil, ierr
		}
		for _, s := range result.Skipped {
			res.Doc.Skipped = append(res.Doc.Skipped, fmt.Sprintf("%s: %s", s.Name, s.Reason))
		}
		for _, s := range result.SkippedAgents {
			res.Doc.Skipped = append(res.Doc.Skipped, fmt.Sprintf("%s (agent): %s", s.Name, s.Reason))
		}
		res.Doc.Warnings = append(res.Doc.Warnings, result.Warnings...)
	}
	if !opts.DryRun && len(resolved.rules) > 0 {
		installed, updatedRules, skippedRules, recordedRules, rerr := m.installPackRules(
			&res.Notes, resolved.rules, discoveredRules, opts.Trust, priorPackRules(m.lockPath(), manifest.Name))
		if rerr != nil {
			return nil, rerr
		}
		res.Doc.Skipped = append(res.Doc.Skipped, skippedRules...)
		for _, name := range updatedRules {
			res.Notes = append(res.Notes, fmt.Sprintf("Updated rule %s from the pack", name))
		}
		// The pack record must claim only what actually installed: a
		// skipped rule recorded as the pack's would let a later remove
		// retract a fragment the pack never delivered.
		resolved.rules = append(installed, updatedRules...)
		slices.Sort(resolved.rules)
		resolved.ruleFiles = recordedRules
		res.Doc.Rules = resolved.rules
	}
	if !opts.DryRun {
		if err := recordLockedPack(ctx, m.lockPath(), manifest, resolved, opts.Repo, opts.Ref, clone.CommitSHA, opts.Auth.CredentialRef); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// resolvedSelection is a manifest selection resolved against discovery:
// concrete name lists, never empty-means-all.
type resolvedSelection struct {
	skills []string
	agents []string
	rules  []string
	// ruleFiles carries per-rule provenance for what actually installed,
	// populated by installPackRules and persisted alongside rules.
	ruleFiles  map[string]skills.LockedRule
	unresolved []string
}

// PackRuleFile is one discovered rule fragment in a pack repo.
type PackRuleFile struct {
	Name string
	Path string // absolute path on disk in the clone
	// Rel is the path within the pack repo, recorded as provenance so a
	// later install can name where the rule came from. Clone paths are
	// temporary and must never be persisted.
	Rel string
}

// discoverPackRules finds rules/*.md and fragments/*.md under the pack
// repo root (and one level of subdirs, matching agents discovery breadth).
func discoverPackRules(repoPath string) map[string]PackRuleFile {
	out := map[string]PackRuleFile{}
	for _, dirName := range []string{"rules", "fragments"} {
		_ = filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) != dirName {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(d.Name()), ".md") || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			if err := contexts.ValidateFragmentName(name); err != nil {
				return nil
			}
			// First discovery wins; rules/ preferred over fragments/ by walk order
			// only when both exist for the same name — keep the first.
			if _, exists := out[name]; !exists {
				rel, rerr := filepath.Rel(repoPath, path)
				if rerr != nil {
					rel = d.Name()
				}
				out[name] = PackRuleFile{Name: name, Path: path, Rel: filepath.ToSlash(rel)}
			}
			return nil
		})
	}
	return out
}

// installPackRules copies selected rule files into the local fragment
// store behind the same gates the importer applies to skills and agents:
// a blocking security scan (bypassed only by trust) and a refusal to
// overwrite a fragment the user has edited.
//
// The prior hash (what gridctl recorded installing last time) is what
// makes that refusal accurate. Comparing incoming bytes against disk alone
// cannot distinguish "the pack changed this rule upstream" from "the user
// edited it," so an upstream change was refused exactly like a local edit
// and the documented update path (re-run 'pack add') could not update
// anything. With the recorded hash the three cases separate: unchanged,
// upstream-changed-and-untouched (update), and locally modified (skip).
//
// A rule with no recorded hash (installed before provenance existed)
// falls back to the byte comparison, so behavior is unchanged until its
// next install records one. A first install that activates fragments mode
// reports the migration as a note.
func (m *Managers) installPackRules(notes *[]string, names []string, discovered map[string]PackRuleFile, trust bool, prior map[string]skills.LockedRule) (installed, updated, skipped []string, recorded map[string]skills.LockedRule, err error) {
	mgr := m.Contexts
	if mgr == nil {
		var merr error
		mgr, merr = contexts.NewManager()
		if merr != nil {
			return nil, nil, nil, nil, merr
		}
	}
	recorded = make(map[string]skills.LockedRule, len(names))
	for _, name := range names {
		rf, ok := discovered[name]
		if !ok {
			continue
		}
		data, rerr := os.ReadFile(rf.Path) // #nosec G304 -- path from pack clone discovery
		if rerr != nil {
			return installed, updated, skipped, recorded, fmt.Errorf("reading pack rule %s: %w", name, rerr)
		}
		if scan := skills.ScanFragment(name, data); !scan.Safe && !trust {
			skipped = append(skipped, fmt.Sprintf("%s (rule): security findings; re-run with --trust to accept\n%s", name, skills.FormatFindings(scan.Findings)))
			continue
		}
		entry := skills.LockedRule{Path: rf.Rel, ContentHash: contexts.FragmentContentHash(data)}
		wasUpdate := false
		if mgr.FragmentsActive() {
			existing, rerr := mgr.ReadFragment(name)
			switch {
			case rerr == nil && bytes.Equal(existing.Raw, data):
				installed = append(installed, name)
				recorded[name] = entry
				continue
			case rerr == nil && ruleIsUnmodified(prior[name], existing.Raw):
				// gridctl installed the current content and the user has
				// not touched it, so the difference is upstream's.
				wasUpdate = true
			case rerr == nil:
				skipped = append(skipped, fmt.Sprintf("%s (rule): locally modified; 'gridctl ctx rm %s' to discard your copy and take the pack's, or rename the pack rule", name, name))
				continue
			case !errors.Is(rerr, contexts.ErrNoFragment):
				return installed, updated, skipped, recorded, rerr
			}
		}
		res, ierr := mgr.InstallFragmentBytes(name, data)
		if ierr != nil {
			return installed, updated, skipped, recorded, fmt.Errorf("installing pack rule %s: %w", name, ierr)
		}
		if res.Migrated {
			*notes = append(*notes, fmt.Sprintf("Activated fragments mode: migrated %s to fragments/00-default.md (backup: %s)", mgr.CanonicalPath(), res.MigratedBackup))
		}
		recorded[name] = entry
		if wasUpdate {
			updated = append(updated, name)
			continue
		}
		installed = append(installed, name)
	}
	return installed, updated, skipped, recorded, nil
}

// ruleIsUnmodified reports whether on-disk content still matches what
// gridctl recorded installing. An empty recorded hash means provenance is
// unknown (a pre-provenance lockfile), which must never match — otherwise
// migration would silently license overwriting user edits.
func ruleIsUnmodified(prior skills.LockedRule, onDisk []byte) bool {
	if prior.ContentHash == "" {
		return false
	}
	return prior.ContentHash == contexts.FragmentContentHash(onDisk)
}

// resolvePackSelection expands the manifest's selection against the
// clone's discovery. Empty skill/agent lists select everything discovered;
// rules are opt-in (empty means none). Named selections must resolve or
// land in unresolved.
func resolvePackSelection(m *pack.Manifest, clone *skills.CloneResult, discoveredRules map[string]PackRuleFile) resolvedSelection {
	discoveredSkills := map[string]bool{}
	for _, s := range clone.Skills {
		discoveredSkills[s.Name] = true
	}
	discoveredAgents := map[string]bool{}
	for _, a := range clone.Agents {
		discoveredAgents[a.Name] = true
	}

	var out resolvedSelection
	if len(m.Skills) == 0 {
		for _, s := range clone.Skills {
			out.skills = append(out.skills, s.Name)
		}
	} else {
		for _, name := range m.Skills {
			if discoveredSkills[name] {
				out.skills = append(out.skills, name)
			} else {
				out.unresolved = append(out.unresolved, name)
			}
		}
	}
	if len(m.Agents) == 0 {
		for _, a := range clone.Agents {
			out.agents = append(out.agents, a.Name)
		}
	} else {
		for _, name := range m.Agents {
			if discoveredAgents[name] {
				out.agents = append(out.agents, name)
			} else {
				out.unresolved = append(out.unresolved, name)
			}
		}
	}
	// Rules: empty means none (opt-in). Named selections must resolve.
	for _, name := range m.Rules {
		if _, ok := discoveredRules[name]; ok {
			out.rules = append(out.rules, name)
		} else {
			out.unresolved = append(out.unresolved, "rules:"+name)
		}
	}
	return out
}

// priorPackRules returns what a previous install recorded for this pack's
// rules, keyed by fragment name. An unreadable or absent lockfile yields an
// empty map, which reads as "provenance unknown" and keeps the install path
// on its pre-provenance behavior rather than failing the run.
func priorPackRules(lockPath, packName string) map[string]skills.LockedRule {
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		return nil
	}
	_, src, ok := lf.FindPackSource(packName)
	if !ok || src.Pack == nil {
		return nil
	}
	return src.Pack.RuleFiles
}

// recordLockedPack stamps the pack record onto the imported source,
// keyed exactly as Import keys it (RepoToName), creating the source
// when the import wrote nothing (wiring-only packs, or a fully skipped
// selection). The whole read-modify-write cycle holds the import
// lockfile's cross-process lock.
//
// credentialRef is carried onto a source this function creates. On the
// normal path Import has already written it; this branch runs precisely
// when Import wrote nothing, and without it a wiring-only private pack
// would record no way to authenticate its next update.
func recordLockedPack(ctx context.Context, lockPath string, m *pack.Manifest, resolved resolvedSelection, repo, ref, commitSHA, credentialRef string) error {
	return skills.MutateLockFile(ctx, lockPath, func(lf *skills.LockFile) (bool, error) {
		sourceName := skills.RepoToName(repo)
		src, ok := lf.Sources[sourceName]
		if !ok {
			src = skills.LockedSource{Repo: repo, Ref: ref, CommitSHA: commitSHA, CredentialRef: credentialRef}
		}
		src.Pack = &skills.LockedPack{
			Name:        m.Name,
			Version:     m.Version,
			Description: m.Description,
			Author:      m.Author.Name,
			Wiring:      m.Wiring,
			Clients:     m.Clients,
			Skills:      resolved.skills,
			Agents:      resolved.agents,
			Rules:       resolved.rules,
			RuleFiles:   resolved.ruleFiles,
			Unresolved:  resolved.unresolved,
		}
		lf.SetSource(sourceName, src)
		return true, nil
	})
}
