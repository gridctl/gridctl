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
	"time"

	"maps"

	"github.com/gridctl/gridctl/pkg/config"
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
	// Auth authenticates the pack repository clone. A token or vault
	// reference here never applies to an external source. An ssh-key
	// path is reused for SSH sources that resolved no other auth.
	Auth skills.AuthConfig
	// SourceAuth is an explicit per-source override, keyed by source name.
	// A present key wins even when the value is zero. Omitted keys fall
	// through to stored member auth, the manifest, then ambient.
	SourceAuth map[string]skills.AuthConfig
	// Resolver expands manifest and stored credential references. Nil means
	// a credential_ref on a source is an error for that source.
	Resolver skills.CredentialResolver
}

// orEmpty returns a non-nil slice so encoding/json emits [] rather than null.
// Only needed for fields without omitempty, where null reaches the wire and
// breaks any client that maps over the value its type promises.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// AddDoc is the machine-readable pack add document.
type AddDoc struct {
	SchemaVersion  int                                         `json:"schema_version"`
	DryRun         bool                                        `json:"dry_run,omitempty"`
	Pack           string                                      `json:"pack"`
	Skills         []string                                    `json:"skills"`
	Agents         []string                                    `json:"agents"`
	Rules          []string                                    `json:"rules,omitempty"`
	Wiring         bool                                        `json:"wiring"`
	Unresolved     []string                                    `json:"unresolved,omitempty"`
	Skipped        []string                                    `json:"skipped,omitempty"`
	Warnings       []string                                    `json:"warnings,omitempty"`
	Variables      map[string]skills.LockedVariableDeclaration `json:"variables,omitempty"`
	UnmetVariables []VariableRequirement                       `json:"unmet_variables,omitempty"`
	Stack          *StackSummary                               `json:"stack,omitempty"`
	Sources        []SourceSummary                             `json:"sources,omitempty"`
}

// VariableRequirement is a value-free prerequisite reported after import.
type VariableRequirement struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Secret      bool   `json:"secret"`
	Description string `json:"description,omitempty"`
	Docs        string `json:"docs,omitempty"`
	Command     string `json:"command"`
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

	// A second pack with the same name would reuse member keys only by
	// coincidence. Refuse before cloning sources. Packs without sources
	// keep the previous add behavior: both records land and findPack refuses.
	if len(manifest.Sources) > 0 {
		if err := guardPackName(m.lockPath(), manifest.Name, opts.Repo); err != nil {
			return nil, err
		}
	}
	sourceClones, sourceWarnings, err := cloneDeclaredSources(ctx, manifest, opts, m.lockPath())
	if err != nil {
		return nil, err
	}
	discoveredRules := discoverPackRules(clone.RepoPath)
	resolved := resolvePackSelection(manifest, clone, sourceClones, discoveredRules)
	stackSummary, stackWarnings := m.resolveCarriedStack(ctx, clone, manifest, &resolved, opts.DryRun)
	stackWarnings = append(stackWarnings, sourceWarnings...)
	stackWarnings = append(stackWarnings, unknownSourceAuthWarnings(manifest, opts.SourceAuth)...)

	if opts.BlockOnFindings && !opts.Trust {
		if flagged := scanSelection(clone, resolved, discoveredRules); len(flagged) > 0 {
			return nil, &FindingsError{Pack: manifest.Name, Resources: flagged}
		}
	}

	// Skills, Agents, and Notes carry no omitempty, so a nil slice would
	// marshal as JSON null while every client is typed for an array. Preview
	// already initializes its lists for the same reason; these did not.
	res := &AddResult{
		Doc: AddDoc{
			SchemaVersion: SchemaVersion,
			DryRun:        opts.DryRun,
			Pack:          manifest.Name,
			Skills:        orEmpty(resolved.skills),
			Agents:        orEmpty(resolved.agents),
			Rules:         resolved.rules,
			Wiring:        manifest.Wiring,
			Unresolved:    resolved.unresolved,
			Warnings:      append(manifest.Warnings(), stackWarnings...),
			Variables:     lockedVariableDeclarations(manifest.Variables),
			Stack:         stackSummary,
			Sources:       sourceSummaries(resolved, false),
		},
		Notes: []string{},
	}

	if !opts.DryRun && len(manifest.Sources) == 0 && (len(resolved.skills) > 0 || len(resolved.agents) > 0) {
		// Selection lists ride to the importer as the manifest wrote them
		// (unresolved names match nothing, so exactly the resolved subset
		// imports); an empty agents list expands to the discovered set so
		// the importer's legacy skip-agents-on-skill-selection contract
		// never hides a pack's agents. Packs without sources keep this path
		// unchanged.
		selectedSkills := manifest.SkillNames()
		selectedAgents := manifest.AgentNames()
		if len(manifest.Agents) == 0 {
			selectedAgents = resolved.agents
		}
		result, ierr := imp.Import(ctx, skills.ImportOptions{
			Repo:           opts.Repo,
			Ref:            opts.Ref,
			Path:           opts.Path,
			Trust:          opts.Trust,
			Selected:       selectedSkills,
			SelectedAgents: selectedAgents,
			Discovered:     clone,
			PackImport:     true,
			// Discovered is set, so the importer does not re-clone and Auth
			// buys no network access here. It is what persists the
			// CredentialRef into the origin sidecars and the lockfile
			// source, which is what lets a later update re-resolve.
			Auth: opts.Auth,
		})
		if ierr != nil {
			return nil, ierr
		}
		appendImportSkips(&res.Doc, result)
	}
	if !opts.DryRun && len(manifest.Sources) > 0 {
		if err := importPackAndSources(ctx, imp, opts, manifest, clone, &resolved, &res.Doc); err != nil {
			return nil, err
		}
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
		if extra := m.materializeCarriedStack(ctx, &resolved); len(extra) > 0 {
			res.Doc.Warnings = append(res.Doc.Warnings, extra...)
			res.Doc.Stack = nil
			res.Doc.Unresolved = append([]string(nil), resolved.unresolved...)
		}
		res.Doc.Skills = orEmpty(resolved.skills)
		res.Doc.Agents = orEmpty(resolved.agents)
		res.Doc.Unresolved = resolved.unresolved
		res.Doc.Sources = sourceSummaries(resolved, true)
		if err := recordLockedPack(ctx, m.lockPath(), manifest, resolved, opts.Repo, opts.Ref, clone.CommitSHA, opts.Auth); err != nil {
			discardCheckout(resolved.createdCheckout)
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
	ruleFiles         map[string]skills.LockedRule
	unresolved        []string
	stack             *skills.LockedStack
	unresolvedDetails map[string]string
	// skillOwner and agentOwner map a resolved name to its source.
	// Empty means the pack repository. sources holds each declared source.
	skillOwner map[string]string
	agentOwner map[string]string
	sources    map[string]*resolvedSource
	// pendingStack is a validated checkout that Add materializes only
	// after the findings gate and the import succeed.
	pendingStack *pendingStack
	// createdCheckout is a directory this add created. A later lockfile
	// write failure deletes it. A reused checkout is left alone.
	createdCheckout string
}

// pendingStack is a carried stack waiting for SnapshotWorktree.
type pendingStack struct {
	repoPath      string
	dest          string
	rel           string
	stackPath     string
	name          string
	manifestStack string
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
func resolvePackSelection(m *pack.Manifest, clone *skills.CloneResult, sources map[string]*resolvedSource, discoveredRules map[string]PackRuleFile) resolvedSelection {
	discoveredSkills := map[string]bool{}
	for _, s := range clone.Skills {
		discoveredSkills[s.Name] = true
	}
	discoveredAgents := map[string]bool{}
	for _, a := range clone.Agents {
		discoveredAgents[a.Name] = true
	}

	var out resolvedSelection
	out.sources = sources
	out.skillOwner = map[string]string{}
	out.agentOwner = map[string]string{}
	if len(m.Skills) == 0 {
		for _, s := range clone.Skills {
			out.skills = append(out.skills, s.Name)
		}
	} else {
		for _, sel := range m.Skills {
			resolveOne(&out, "skill", sel, discoveredSkills, sources)
		}
	}
	if len(m.Agents) == 0 {
		for _, a := range clone.Agents {
			out.agents = append(out.agents, a.Name)
		}
	} else {
		for _, sel := range m.Agents {
			resolveOne(&out, "agent", sel, discoveredAgents, sources)
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

func resolveOne(out *resolvedSelection, kind string, sel pack.Selection, discovered map[string]bool, sources map[string]*resolvedSource) {
	if sel.Source == "" {
		if discovered[sel.Name] {
			if kind == "skill" {
				out.skills = append(out.skills, sel.Name)
			} else {
				out.agents = append(out.agents, sel.Name)
			}
			return
		}
		out.unresolved = append(out.unresolved, sel.Name)
		return
	}
	rs := sources[sel.Source]
	token := kind + "/" + sel.Name
	if rs == nil || rs.err != nil {
		out.unresolved = append(out.unresolved, token)
		if rs != nil && rs.err != nil {
			if out.unresolvedDetails == nil {
				out.unresolvedDetails = map[string]string{}
			}
			out.unresolvedDetails[token] = redactSourceErr(rs.err)
		}
		return
	}
	found := false
	if kind == "skill" {
		for _, s := range rs.clone.Skills {
			if s.Name == sel.Name {
				found = true
				break
			}
		}
	} else {
		for _, a := range rs.clone.Agents {
			if a.Name == sel.Name {
				found = true
				break
			}
		}
	}
	if !found {
		out.unresolved = append(out.unresolved, token)
		return
	}
	if kind == "skill" {
		out.skills = append(out.skills, sel.Name)
		out.skillOwner[sel.Name] = sel.Source
		rs.skills = append(rs.skills, sel.Name)
		return
	}
	out.agents = append(out.agents, sel.Name)
	out.agentOwner[sel.Name] = sel.Source
	rs.agents = append(rs.agents, sel.Name)
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
// auth is carried onto a source this function creates. On the normal
// path Import has already written the vault reference or ssh-key path;
// this branch runs precisely when Import wrote nothing, and without it a
// wiring-only private pack would record no way to authenticate its next
// update. Token values and passphrases are not written.
func recordLockedPack(ctx context.Context, lockPath string, m *pack.Manifest, resolved resolvedSelection, repo, ref, commitSHA string, auth skills.AuthConfig) error {
	return skills.MutateLockFile(ctx, lockPath, func(lf *skills.LockFile) (bool, error) {
		sourceName := skills.RepoToName(repo)
		src, ok := lf.Sources[sourceName]
		if ok && src.IsLocal() {
			return false, fmt.Errorf("source %q is a local source at %s; packs cannot replace a local source", sourceName, src.Repo)
		}
		if !ok {
			method, user, path := persistedPackSSH(auth)
			src = skills.LockedSource{
				Repo:          repo,
				Ref:           ref,
				CommitSHA:     commitSHA,
				CredentialRef: auth.CredentialRef,
				AuthMethod:    method,
				SSHUser:       user,
				SSHKeyPath:    path,
			}
		}
		packRecord := &skills.LockedPack{
			Name:              m.Name,
			Version:           m.Version,
			Description:       m.Description,
			Author:            m.Author.Name,
			Wiring:            m.Wiring,
			Clients:           m.Clients,
			Skills:            resolved.skills,
			Agents:            resolved.agents,
			Rules:             resolved.rules,
			RuleFiles:         resolved.ruleFiles,
			Unresolved:        resolved.unresolved,
			Variables:         lockedVariableDeclarations(m.Variables),
			Stack:             resolved.stack,
			UnresolvedDetails: resolved.unresolvedDetails,
		}
		if len(resolved.sources) > 0 {
			packRecord.Sources = lockedPackSources(m.Name, resolved)
			for _, sourceName := range slices.Sorted(maps.Keys(resolved.sources)) {
				rs := resolved.sources[sourceName]
				if err := ensureMemberSource(lf, m.Name, sourceName, rs); err != nil {
					return false, err
				}
			}
		}
		src.Pack = packRecord
		lf.SetSource(sourceName, src)
		return true, nil
	})
}

func lockedPackSources(packName string, resolved resolvedSelection) map[string]skills.LockedPackSource {
	out := make(map[string]skills.LockedPackSource, len(resolved.sources))
	now := time.Now().UTC()
	for _, name := range slices.Sorted(maps.Keys(resolved.sources)) {
		rs := resolved.sources[name]
		entry := skills.LockedPackSource{
			Repo:      rs.spec.Repo,
			Ref:       rs.spec.Ref,
			Path:      rs.spec.Path,
			CommitSHA: rs.sha,
			FetchedAt: now,
			SourceKey: memberKey(packName, name),
		}
		if rs.err == nil {
			entry.Skills = append([]string(nil), rs.importedSkills...)
			entry.Agents = append([]string(nil), rs.importedAgents...)
		}
		out[name] = entry
	}
	return out
}

func ensureMemberSource(lf *skills.LockFile, packName, sourceName string, rs *resolvedSource) error {
	key := memberKey(packName, sourceName)
	member, ok := lf.Sources[key]
	if ok && member.IsLocal() {
		return fmt.Errorf("source %q is a local source at %s; packs cannot replace a local source", key, member.Repo)
	}
	if !ok {
		method, user, path := persistedPackSSH(rs.auth)
		member = skills.LockedSource{
			Repo:          rs.spec.Repo,
			Ref:           rs.spec.Ref,
			CommitSHA:     rs.sha,
			CredentialRef: rs.auth.CredentialRef,
			AuthMethod:    method,
			SSHUser:       user,
			SSHKeyPath:    path,
		}
	}
	member.PackMember = packName
	if member.Repo == "" {
		member.Repo = rs.spec.Repo
	}
	lf.SetSource(key, member)
	return nil
}

func appendImportSkips(doc *AddDoc, result *skills.ImportResult) {
	if result == nil {
		return
	}
	for _, s := range result.Skipped {
		doc.Skipped = append(doc.Skipped, fmt.Sprintf("%s: %s", s.Name, s.Reason))
	}
	for _, s := range result.SkippedAgents {
		doc.Skipped = append(doc.Skipped, fmt.Sprintf("%s (agent): %s", s.Name, s.Reason))
	}
	doc.Warnings = append(doc.Warnings, result.Warnings...)
}

func importPackAndSources(ctx context.Context, imp *skills.Importer, opts AddOptions, manifest *pack.Manifest, clone *skills.CloneResult, resolved *resolvedSelection, doc *AddDoc) error {
	packSkills, packAgents := packLocalNames(resolved)
	if len(packSkills) > 0 || len(packAgents) > 0 {
		result, err := imp.Import(ctx, skills.ImportOptions{
			Repo:           opts.Repo,
			Ref:            opts.Ref,
			Path:           opts.Path,
			Trust:          opts.Trust,
			Selected:       nonNilNames(packSkills),
			SelectedAgents: nonNilNames(packAgents),
			Discovered:     clone,
			PackImport:     true,
			ExactSelection: true,
			Auth:           opts.Auth,
		})
		if err != nil {
			return err
		}
		appendImportSkips(doc, result)
	}
	for _, name := range slices.Sorted(maps.Keys(resolved.sources)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		rs := resolved.sources[name]
		if rs.err != nil {
			continue
		}
		if beforeExternalImport != nil {
			beforeExternalImport(name, rs.clone)
		}
		if err := recheckSourceCommit(rs); err != nil {
			failSource(resolved, name, err)
			doc.Warnings = append(doc.Warnings, sourceWarning(name, err))
			continue
		}
		result, err := imp.Import(ctx, skills.ImportOptions{
			Repo:           rs.spec.Repo,
			Ref:            rs.spec.Ref,
			Path:           rs.spec.Path,
			Trust:          opts.Trust,
			Selected:       nonNilNames(rs.skills),
			SelectedAgents: nonNilNames(rs.agents),
			Discovered:     rs.clone,
			PackImport:     true,
			ExactSelection: true,
			SourceName:     memberKey(manifest.Name, name),
			Auth:           rs.auth,
		})
		if err != nil {
			failSource(resolved, name, err)
			doc.Warnings = append(doc.Warnings, sourceWarning(name, err))
			continue
		}
		for _, s := range result.Imported {
			rs.importedSkills = append(rs.importedSkills, s.Name)
		}
		for _, a := range result.ImportedAgents {
			rs.importedAgents = append(rs.importedAgents, a.Name)
		}
		appendImportSkips(doc, result)
	}
	return nil
}

func packLocalNames(resolved *resolvedSelection) (skills, agents []string) {
	for _, name := range resolved.skills {
		if resolved.skillOwner[name] == "" {
			skills = append(skills, name)
		}
	}
	for _, name := range resolved.agents {
		if resolved.agentOwner[name] == "" {
			agents = append(agents, name)
		}
	}
	return skills, agents
}

// persistedPackSSH mirrors skills.persistedAuth. The helper is unexported
// in pkg/skills, so the wiring-only create path applies the same rule here:
// only an ssh-key method with a path is stored, never a passphrase.
func persistedPackSSH(cfg skills.AuthConfig) (method, user, path string) {
	if cfg.Method == "ssh-key" && cfg.SSHKeyPath != "" {
		return cfg.Method, cfg.SSHUser, cfg.SSHKeyPath
	}
	return "", "", ""
}

// resolveCarriedStack validates a manifest stack entry against the clone
// root (not the --path subdirectory). A failure is an unresolved selection,
// not an import abort. Dry-run and a failed read never materialize a checkout.
// A successful read records a pending checkout; Add copies it only after
// the findings gate and the import succeed.
func (m *Managers) resolveCarriedStack(ctx context.Context, clone *skills.CloneResult, manifest *pack.Manifest, resolved *resolvedSelection, dryRun bool) (*StackSummary, []string) {
	if manifest.Stack == "" {
		return nil, nil
	}
	token := "stack:" + manifest.Stack
	rel := filepath.FromSlash(manifest.Stack)
	stackPath := filepath.Join(clone.RepoPath, rel)
	var warnings []string
	fail := func(detail string) (*StackSummary, []string) {
		resolved.unresolved = append(resolved.unresolved, token)
		if resolved.unresolvedDetails == nil {
			resolved.unresolvedDetails = map[string]string{}
		}
		resolved.unresolvedDetails[token] = detail
		warnings = append(warnings, detail)
		return nil, warnings
	}
	info, err := os.Stat(stackPath)
	if err != nil || info.IsDir() {
		return fail(fmt.Sprintf("stack file %q not found in the pack repository", manifest.Stack))
	}
	exported, sources, err := config.ExportStack(ctx, stackPath)
	if err != nil {
		return fail(err.Error())
	}
	if exported.Name == "" {
		return fail("stack file has no name:")
	}
	if len(exported.Link) > 0 {
		warnings = append(warnings, stackLinkWarning)
	}
	if escaping, outside := stackEscapesClone(clone.RepoPath, sources); outside {
		return fail(fmt.Sprintf("stack path escapes the pack repository: %s", escaping))
	}
	configFiles, err := config.ReferencedConfigFiles(exported, filepath.Dir(stackPath))
	if err != nil {
		return fail(err.Error())
	}
	if escaping, outside := stackEscapesClone(clone.RepoPath, configFiles); outside {
		return fail(fmt.Sprintf("stack config file escapes the pack repository: %s", escaping))
	}
	for _, configFile := range configFiles {
		info, statErr := os.Stat(configFile)
		if statErr != nil || info.IsDir() {
			rel, relErr := filepath.Rel(clone.RepoPath, configFile)
			if relErr != nil || rel == "" {
				rel = configFile
			}
			return fail(fmt.Sprintf("stack config file not found in the pack repository: %s", rel))
		}
	}
	summary := &StackSummary{Path: manifest.Stack, Name: exported.Name, Servers: len(exported.MCPServers)}
	if dryRun {
		return summary, warnings
	}
	home, err := m.homeDir()
	if err != nil {
		return fail(err.Error())
	}
	dest, err := filepath.Abs(PackCheckoutDir(home, manifest.Name, clone.CommitSHA))
	if err != nil {
		return fail(err.Error())
	}
	resolved.pendingStack = &pendingStack{
		repoPath:      clone.RepoPath,
		dest:          dest,
		rel:           rel,
		stackPath:     stackPath,
		name:          exported.Name,
		manifestStack: manifest.Stack,
	}
	return summary, warnings
}

// materializeCarriedStack copies a validated stack into the pinned checkout.
// A snapshot failure, or a stack file the copy skipped, is an unresolved
// selection. The returned warnings are empty when the checkout is recorded.
func (m *Managers) materializeCarriedStack(ctx context.Context, resolved *resolvedSelection) []string {
	pending := resolved.pendingStack
	resolved.pendingStack = nil
	if pending == nil {
		return nil
	}
	fail := func(detail string) []string {
		token := "stack:" + pending.manifestStack
		resolved.unresolved = append(resolved.unresolved, token)
		if resolved.unresolvedDetails == nil {
			resolved.unresolvedDetails = map[string]string{}
		}
		resolved.unresolvedDetails[token] = detail
		resolved.stack = nil
		resolved.createdCheckout = ""
		return []string{detail}
	}
	pinned := filepath.Join(pending.dest, pending.rel)
	if info, statErr := os.Stat(pinned); statErr != nil || info.IsDir() {
		discardCheckout(pending.dest)
		result, snapErr := skills.SnapshotWorktree(ctx, pending.repoPath, pending.dest)
		if snapErr != nil {
			discardCheckout(pending.dest)
			return fail(snapErr.Error())
		}
		if !result.Reused {
			resolved.createdCheckout = pending.dest
		}
	}
	if info, statErr := os.Stat(pinned); statErr != nil || info.IsDir() {
		discardCheckout(pending.dest)
		return fail(fmt.Sprintf("stack file %q is a symlink or was not copied into the checkout", pending.manifestStack))
	}
	hash, err := skills.ContentHashFile(pending.stackPath)
	if err != nil {
		discardCheckout(resolved.createdCheckout)
		return fail(err.Error())
	}
	resolved.stack = &skills.LockedStack{
		Path:        pending.manifestStack,
		Name:        pending.name,
		ContentHash: hash,
		CheckoutDir: pending.dest,
	}
	return nil
}

// discardCheckout removes a checkout this add created. An empty pack root
// left behind by the copy is removed too. A root that still holds another
// commit is left alone.
func discardCheckout(dest string) {
	if dest == "" {
		return
	}
	_ = os.RemoveAll(dest)
	parent := filepath.Dir(dest)
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		return
	}
	_ = os.Remove(parent)
}

// stackEscapesClone reports the first source path that resolves outside
// the clone root. ExportStack's source list includes the extends chain.
func stackEscapesClone(cloneRoot string, sources []string) (string, bool) {
	root, err := filepath.Abs(cloneRoot)
	if err != nil {
		return cloneRoot, true
	}
	if evaluated, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = evaluated
	}
	for _, source := range sources {
		abs, absErr := filepath.Abs(source)
		if absErr != nil {
			return source, true
		}
		checked := abs
		if evaluated, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
			checked = evaluated
		}
		rel, relErr := filepath.Rel(root, checked)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return checked, true
		}
	}
	return "", false
}

func lockedVariableDeclarations(in map[string]pack.VariableDeclaration) map[string]skills.LockedVariableDeclaration {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]skills.LockedVariableDeclaration, len(in))
	for key, d := range in {
		out[key] = skills.LockedVariableDeclaration{
			Required: d.Required, Secret: d.Secret, Type: d.Type,
			Description: d.Description, Docs: d.Docs,
		}
	}
	return out
}
