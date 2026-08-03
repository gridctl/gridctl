package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/wiring"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

// packJSONSchemaVersion versions every `gridctl pack` JSON document
// (Article X).
const packJSONSchemaVersion = 1

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Import and apply team packs (skills + agents + rules + wiring)",
	Long: `A pack is a git repo carrying a ` + pack.ManifestFileName + ` manifest that
selects skills, agents, context rule fragments, and gateway wiring, so
one import configures a whole setup. 'pack add' imports the selection
through the same origin pipeline (security scan, --trust gate,
drift-safe updates) that 'gridctl skill add' uses; 'pack apply'
projects it through the same engines as 'gridctl skill project sync',
'gridctl ctx sync', and 'gridctl project sync --kind wiring', scoped to
the pack; 'pack remove' cascades: projections are unsynced and wiring
records cleaned before the registry entries go.

Packs never introduce hidden state: every projection is tagged with the
pack name in the unified project lockfile, and the underlying verbs keep
working on the same resources.`,
	Example: `  gridctl pack add https://github.com/acme/team-pack
  gridctl pack apply team-pack
  gridctl pack status
  gridctl pack remove team-pack`,
}

// packRow is one resource line in pack output.
type packRow struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Client      string `json:"client,omitempty"`
	Action      string `json:"action,omitempty"`
	State       string `json:"state,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// --- pack add ---

var (
	packAddRef    string
	packAddTrust  bool
	packAddDryRun bool
	packAddFormat string
	packAddJSON   *bool
)

var packAddCmd = &cobra.Command{
	Use:   "add <repo-url>",
	Short: "Import a pack from a git repository",
	Long: `Clones the repository, reads ` + pack.ManifestFileName + ` at its root, and
imports exactly the manifest's selection of skills, agents, and rule
fragments into the local stores (empty skill and agent lists mean
everything discovered; rules are opt-in, an empty list means none).
Nothing touches client files or the gateway; that is 'pack apply'.

Manifest-selected names the repository does not contain are reported as
unresolved (exit 1); the rest of the pack still imports.

Exit codes:
  0  imported cleanly
  1  partial (unresolved selections, or skipped resources)
  2  infrastructure error (clone, auth, missing or invalid manifest)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packAddFormat, cmd.Flags().Changed("format"), *packAddJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		store, err := loadRegistry()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		imp := newImporter(store)
		if exit := runPackAdd(cmd.Context(), os.Stdout, os.Stderr, imp, args[0], packAddRef, packAddTrust, packAddDryRun, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// packAddDoc is the machine-readable pack add document.
type packAddDoc struct {
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

// runPackAdd clones, resolves the manifest selection, and imports.
func runPackAdd(ctx context.Context, stdout, stderr io.Writer, imp *skills.Importer, repo, ref string, trust, dryRun bool, format string) int {
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	clone, err := skills.CloneAndDiscover(repo, ref, "", skills.AuthConfig{}, slog.Default())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	manifest, err := pack.ParseFile(filepath.Join(clone.RepoPath, pack.ManifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(stderr, "no %s found at the repository root; 'gridctl pack add' imports pack repos (use 'gridctl skill add %s' for plain skill repos)\n", pack.ManifestFileName, repo)
			return ctxExitInfrastructure
		}
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	discoveredRules := discoverPackRules(clone.RepoPath)
	resolved := resolvePackSelection(manifest, clone, discoveredRules)

	doc := packAddDoc{
		SchemaVersion: packJSONSchemaVersion,
		DryRun:        dryRun,
		Pack:          manifest.Name,
		Skills:        resolved.skills,
		Agents:        resolved.agents,
		Rules:         resolved.rules,
		Wiring:        manifest.Wiring,
		Unresolved:    resolved.unresolved,
		Warnings:      manifest.Warnings(),
	}

	if !dryRun && (len(resolved.skills) > 0 || len(resolved.agents) > 0) {
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
			Repo:           repo,
			Ref:            ref,
			Trust:          trust,
			Selected:       selectedSkills,
			SelectedAgents: selectedAgents,
			Discovered:     clone,
		})
		if ierr != nil {
			fmt.Fprintln(stderr, ierr)
			return ctxExitInfrastructure
		}
		for _, s := range result.Skipped {
			doc.Skipped = append(doc.Skipped, fmt.Sprintf("%s: %s", s.Name, s.Reason))
		}
		for _, s := range result.SkippedAgents {
			doc.Skipped = append(doc.Skipped, fmt.Sprintf("%s (agent): %s", s.Name, s.Reason))
		}
		doc.Warnings = append(doc.Warnings, result.Warnings...)

	}
	if !dryRun && len(resolved.rules) > 0 {
		installed, updatedRules, skippedRules, recordedRules, rerr := installPackRules(
			stdout, resolved.rules, discoveredRules, trust, priorPackRules(manifest.Name))
		if rerr != nil {
			fmt.Fprintln(stderr, rerr)
			return ctxExitInfrastructure
		}
		doc.Skipped = append(doc.Skipped, skippedRules...)
		for _, name := range updatedRules {
			fmt.Fprintf(stdout, "Updated rule %s from the pack\n", name)
		}
		// The pack record must claim only what actually installed: a
		// skipped rule recorded as the pack's would let a later remove
		// retract a fragment the pack never delivered.
		resolved.rules = append(installed, updatedRules...)
		slices.Sort(resolved.rules)
		resolved.ruleFiles = recordedRules
		doc.Rules = resolved.rules
	}
	if !dryRun {
		if err := recordLockedPack(manifest, resolved, repo, ref, clone.CommitSHA); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		verb := "Imported"
		if dryRun {
			verb = "Would import"
		}
		wiringLabel := "no"
		if manifest.Wiring {
			wiringLabel = "yes"
		}
		if len(doc.Rules) > 0 {
			fmt.Fprintf(stdout, "%s pack %q (%d skills, %d agents, %d rules, wiring: %s) from %s\n",
				verb, manifest.Name, len(doc.Skills), len(doc.Agents), len(doc.Rules), wiringLabel, repo)
		} else {
			fmt.Fprintf(stdout, "%s pack %q (%d skills, %d agents, wiring: %s) from %s\n",
				verb, manifest.Name, len(doc.Skills), len(doc.Agents), wiringLabel, repo)
		}
		for _, w := range doc.Warnings {
			fmt.Fprintf(stdout, "Warning: %s\n", w)
		}
		for _, s := range doc.Skipped {
			fmt.Fprintf(stdout, "Skipped: %s\n", s)
		}
		for _, u := range doc.Unresolved {
			fmt.Fprintf(stdout, "Unresolved: pack selects %q but the repository does not ship it\n", u)
		}
		if !dryRun && len(doc.Unresolved) == 0 && len(doc.Skipped) == 0 {
			fmt.Fprintf(stdout, "Run 'gridctl pack apply %s' to project it.\n", manifest.Name)
		}
	}
	if len(doc.Unresolved) > 0 || len(doc.Skipped) > 0 {
		return ctxExitAttention
	}
	return ctxExitOK
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

// packRuleFile is one discovered rule fragment in a pack repo.
type packRuleFile struct {
	Name string
	Path string // absolute path on disk in the clone
	// Rel is the path within the pack repo, recorded as provenance so a
	// later install can name where the rule came from. Clone paths are
	// temporary and must never be persisted.
	Rel string
}

// discoverPackRules finds rules/*.md and fragments/*.md under the pack
// repo root (and one level of subdirs, matching agents discovery breadth).
func discoverPackRules(repoPath string) map[string]packRuleFile {
	out := map[string]packRuleFile{}
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
				out[name] = packRuleFile{Name: name, Path: path, Rel: filepath.ToSlash(rel)}
			}
			return nil
		})
	}
	return out
}

// installPackRules copies selected rule files into the local fragment
// store behind the same gates the importer applies to skills and agents:
// a blocking security scan (bypassed only by --trust) and a refusal to
// overwrite a fragment the user has edited.
//
// The prior hash — what gridctl recorded installing last time — is what
// makes that refusal accurate. Comparing incoming bytes against disk alone
// cannot distinguish "the pack changed this rule upstream" from "the user
// edited it," so an upstream change was refused exactly like a local edit
// and the documented update path (re-run 'pack add') could not update
// anything. With the recorded hash the three cases separate: unchanged,
// upstream-changed-and-untouched (update), and locally modified (skip).
//
// A rule with no recorded hash — installed before provenance existed —
// falls back to the byte comparison, so behavior is unchanged until its
// next install records one. A first install that activates fragments mode
// prints the migration.
func installPackRules(stdout io.Writer, names []string, discovered map[string]packRuleFile, trust bool, prior map[string]skills.LockedRule) (installed, updated, skipped []string, recorded map[string]skills.LockedRule, err error) {
	mgr, merr := contexts.NewManager()
	if merr != nil {
		return nil, nil, nil, nil, merr
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
			fmt.Fprintf(stdout, "Activated fragments mode: migrated %s to fragments/00-default.md (backup: %s)\n", mgr.CanonicalPath(), res.MigratedBackup)
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
func resolvePackSelection(m *pack.Manifest, clone *skills.CloneResult, discoveredRules map[string]packRuleFile) resolvedSelection {
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

// recordLockedPack stamps the pack record onto the imported source,
// keyed exactly as Import keys it (RepoToName), creating the source
// when the import wrote nothing (wiring-only packs, or a fully skipped
// selection).
// priorPackRules returns what a previous install recorded for this pack's
// rules, keyed by fragment name. An unreadable or absent lockfile yields an
// empty map, which reads as "provenance unknown" and keeps the install path
// on its pre-provenance behavior rather than failing the run.
func priorPackRules(packName string) map[string]skills.LockedRule {
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		return nil
	}
	_, src, ok := lf.FindPackSource(packName)
	if !ok || src.Pack == nil {
		return nil
	}
	return src.Pack.RuleFiles
}

func recordLockedPack(m *pack.Manifest, resolved resolvedSelection, repo, ref, commitSHA string) error {
	lockPath := skills.LockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("reading import lockfile: %w", err)
	}
	sourceName := skills.RepoToName(repo)
	src, ok := lf.Sources[sourceName]
	if !ok {
		src = skills.LockedSource{Repo: repo, Ref: ref, CommitSHA: commitSHA}
	}
	src.Pack = &skills.LockedPack{
		Name:       m.Name,
		Version:    m.Version,
		Wiring:     m.Wiring,
		Clients:    m.Clients,
		Skills:     resolved.skills,
		Agents:     resolved.agents,
		Rules:      resolved.rules,
		RuleFiles:  resolved.ruleFiles,
		Unresolved: resolved.unresolved,
	}
	lf.SetSource(sourceName, src)
	return skills.WriteLockFile(lockPath, lf)
}

// --- pack apply ---

var (
	packApplyForce   bool
	packApplyDryRun  bool
	packApplyClients []string
	packApplyFormat  string
	packApplyJSON    *bool
	packApplyPlain   *bool
)

var packApplyCmd = &cobra.Command{
	Use:   "apply <name>",
	Short: "Project a pack's resources to clients",
	Long: `Projects an imported pack: its skills and agents through the same
engines as 'gridctl skill project sync' (scoped to the pack's
selection), its rule fragments through 'gridctl ctx sync', and, when
the manifest declares wiring, the gateway entry through the same
machinery as 'gridctl project sync --kind wiring'. Every projection is
tagged with the pack name; for rules only pack-shipped fragments carry
the tag.

Apply is additive and never transactional: each resource succeeds or
skips independently, drifted resources are skipped with an adopt or
--force hint, and a resource tagged by a different pack is refused.

Exit codes:
  0  everything applied
  1  a resource was skipped or failed
  2  infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packApplyFormat, cmd.Flags().Changed("format"), *packApplyJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*packApplyPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if exit := runPackApply(cmd.Context(), os.Stdout, os.Stderr, mgrs, args[0], packApplyForce, packApplyDryRun, packApplyClients, format, *packApplyPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// packManagers bundles the kind managers pack verbs orchestrate. No
// pack engine exists: these are the same managers the standalone verbs
// drive.
type packManagers struct {
	skills   *skillsync.Manager
	agents   *agentsync.Manager
	wiring   *wiring.Manager
	contexts *contexts.Manager
	home     string
}

// newPackManagers builds the managers against the user's home.
func newPackManagers() (*packManagers, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	sm, err := newSkillProjectManager()
	if err != nil {
		return nil, err
	}
	am, err := newAgentProjectManager()
	if err != nil {
		return nil, err
	}
	wm, err := wiring.NewManager()
	if err != nil {
		return nil, err
	}
	cm, err := contexts.NewManager()
	if err != nil {
		return nil, err
	}
	return &packManagers{skills: sm, agents: am, wiring: wm, contexts: cm, home: home}, nil
}

// loadLockedPack finds a pack's record in the import lockfile.
func loadLockedPack(name string) (*skills.LockedPack, error) {
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		return nil, err
	}
	if _, src, ok := lf.FindPackSource(name); ok {
		return src.Pack, nil
	}
	return nil, fmt.Errorf("pack %q is not imported (run 'gridctl pack add <repo-url>' first; 'gridctl pack status' lists imported packs)", name)
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

// packApplyDoc is the machine-readable apply document.
type packApplyDoc struct {
	SchemaVersion int       `json:"schema_version"`
	Pack          string    `json:"pack"`
	DryRun        bool      `json:"dry_run,omitempty"`
	Applied       int       `json:"applied"`
	Total         int       `json:"total"`
	Rows          []packRow `json:"rows"`
}

// runPackApply projects one pack across every kind it selects.
func runPackApply(ctx context.Context, stdout, stderr io.Writer, mgrs *packManagers, name string, force, dryRun bool, clientOverride []string, format string, plain bool) int {
	locked, err := loadLockedPack(name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	foreign, err := foreignPackTags(ctx, mgrs.home, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	var rows []packRow
	addRow := func(r packRow) { rows = append(rows, r) }

	// Skills and agents: exclude foreign-tagged resources up front, then
	// hand the rest to the same engines the standalone verbs drive.
	skillNames := filterForeign(locked.Skills, "skill", foreign, name, addRow)
	agentNames := filterForeign(locked.Agents, "agent", foreign, name, addRow)

	if len(skillNames) > 0 {
		results, serr := mgrs.skills.Sync(ctx, skillNames, skillsync.SyncOptions{Force: force, DryRun: dryRun, Pack: name})
		if serr != nil {
			// Apply is additive: a kind-level failure (a disabled skill,
			// say) becomes rows, not a whole-command abort.
			addRow(packRow{Kind: "skill", Name: strings.Join(skillNames, ","), Action: "error", Detail: serr.Error()})
			results = nil
		}
		for _, r := range results {
			if r.Action == skillsync.ActionSkippedUnavailable {
				continue
			}
			addRow(packRow{Kind: "skill", Name: r.Skill, Client: r.Client, Action: r.Action, Detail: r.Error})
		}
	}
	if len(agentNames) > 0 {
		results, aerr := mgrs.agents.Sync(ctx, agentNames, agentsync.SyncOptions{Force: force, DryRun: dryRun, Pack: name})
		if aerr != nil {
			addRow(packRow{Kind: "agent", Name: strings.Join(agentNames, ","), Action: "error", Detail: aerr.Error()})
			results = nil
		}
		for _, r := range results {
			if r.Action == agentsync.ActionSkippedUnavailable {
				continue
			}
			addRow(packRow{Kind: "agent", Name: r.Agent, Client: r.Client, Action: r.Action, Detail: firstNonEmpty(r.Error, r.Detail)})
		}
	}

	if locked.Wiring {
		if _, ok := foreign["wiring/gridctl"]; ok {
			addRow(packRow{Kind: "wiring", Name: "gridctl", Action: wiring.ActionSkippedForeign,
				Detail: fmt.Sprintf("the gridctl wiring entry is managed by pack %q", foreign["wiring/gridctl"])})
		} else if port, running := runningGatewayPort(); !running {
			addRow(packRow{Kind: "wiring", Name: "gridctl", Action: wiring.ActionSkippedUnavailable,
				Detail:      "no running gateway detected",
				Remediation: fmt.Sprintf("start one with 'gridctl serve' or 'gridctl apply', then re-run 'gridctl pack apply %s'", name)})
		} else {
			clients := locked.Clients
			if len(clientOverride) > 0 {
				clients = clientOverride
			}
			results, werr := mgrs.wiring.Sync(ctx, wiring.SyncOptions{
				Clients:    clients,
				ServerName: "gridctl",
				GatewayURL: provisioner.GatewayHTTPURL(port),
				Port:       port,
				Force:      force,
				DryRun:     dryRun,
				Pack:       name,
			})
			if werr != nil {
				addRow(packRow{Kind: "wiring", Name: "gridctl", Action: "error", Detail: werr.Error()})
				results = nil
			}
			for _, r := range results {
				addRow(packRow{Kind: "wiring", Name: r.Name, Client: r.Client, Action: r.Action,
					Detail: firstNonEmpty(r.Error, r.Detail), Remediation: r.Remediation})
			}
		}
	}

	// Rules: project every available client with the pack tag so lock
	// entries cascade-remove by tag. Only the pack's fragments need to
	// exist in the store (installed at pack add); SyncAll projects the
	// whole fragment set and tags new multi-file writes with Pack.
	if len(locked.Rules) > 0 {
		if mgrs.contexts != nil && mgrs.contexts.FragmentsActive() {
			results, rerr := mgrs.contexts.SyncAll(ctx, contexts.SyncOptions{Force: force, DryRun: dryRun, Pack: name, PackRules: locked.Rules})
			if rerr != nil {
				addRow(packRow{Kind: "rule", Name: strings.Join(locked.Rules, ","), Action: "error", Detail: rerr.Error()})
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
					addRow(packRow{Kind: "rule", Name: firstNonEmpty(r.Fragment, strings.Join(locked.Rules, ",")), Client: r.Slug, Action: r.Action, Detail: firstNonEmpty(r.Error, r.Detail)})
				}
			}
		} else {
			addRow(packRow{Kind: "rule", Name: strings.Join(locked.Rules, ","), Action: "error",
				Detail: "fragments mode is not active; re-run 'gridctl pack add' for this pack"})
		}
	}

	for _, u := range locked.Unresolved {
		addRow(packRow{Kind: "unresolved", Name: u, Action: "unresolved",
			Detail: "selected by the pack manifest but not shipped by the repository"})
	}

	applied, failed := tallyPackRows(rows)
	doc := packApplyDoc{SchemaVersion: packJSONSchemaVersion, Pack: name, DryRun: dryRun, Applied: applied, Total: applied + failed, Rows: rows}

	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		renderPackRows(stdout, rows, plain)
		fmt.Fprintf(stdout, "\nApplied %d/%d resources.\n", applied, doc.Total)
	}
	if failed > 0 {
		return ctxExitAttention
	}
	return ctxExitOK
}

// filterForeign splits a selection into syncable names, emitting refusal
// rows for resources tagged by a different pack.
func filterForeign(names []string, kind string, foreign map[string]string, packName string, addRow func(packRow)) []string {
	var out []string
	for _, n := range names {
		if owner, ok := foreign[kind+"/"+n]; ok {
			addRow(packRow{Kind: kind, Name: n, Action: "skipped-foreign-pack",
				Detail:      fmt.Sprintf("already managed by pack %q", owner),
				Remediation: fmt.Sprintf("remove it from one pack ('gridctl pack remove %s' or edit the manifests) so a single pack owns it", owner)})
			continue
		}
		out = append(out, n)
	}
	return out
}

// tallyPackRows counts clean rows vs rows needing attention.
func tallyPackRows(rows []packRow) (applied, failed int) {
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

// renderPackRows prints the per-resource table plus detail lines.
func renderPackRows(w io.Writer, rows []packRow, plain bool) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "Nothing to do.")
		return
	}
	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"KIND", "NAME", "CLIENT", "ACTION"})
	for _, r := range rows {
		action := r.Action
		if r.State != "" {
			action = r.State
		}
		t.AppendRow(table.Row{r.Kind, r.Name, r.Client, packActionLabel(action)})
	}
	t.Render()
	for _, r := range rows {
		if r.Detail == "" {
			continue
		}
		fmt.Fprintf(w, "\n%s %s: %s.\n", r.Kind, r.Name, r.Detail)
		if r.Remediation != "" {
			fmt.Fprintf(w, "  %s\n", capitalizeFirst(r.Remediation))
		}
	}
}

// packActionLabel decorates actions/states with the shared glyphs.
func packActionLabel(action string) string {
	switch {
	case strings.HasPrefix(action, "skipped") || action == "error" || action == "unresolved" || action == "drifted" || action == "target-missing" || action == "foreign":
		return "✗ " + action
	case strings.HasPrefix(action, "would-") || action == "stale" || action == "missing":
		return "— " + action
	default:
		return "✓ " + action
	}
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

// --- pack status ---

var (
	packStatusFormat string
	packStatusJSON   *bool
	packStatusPlain  *bool
)

var packStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show per-resource state for imported packs",
	Long: `Shows the projection state of every resource an imported pack selects,
in the shared state vocabulary (in-sync, stale, drifted,
target-missing, foreign, missing), plus unresolved manifest selections.
Rule rows report store-level presence; per-client fragment state lives
in 'gridctl ctx status'.

Exit codes:
  0  everything clean
  1  attention needed
  2  infrastructure error`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packStatusFormat, cmd.Flags().Changed("format"), *packStatusJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		if err := resolvePlain(*packStatusPlain, format); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		if exit := runPackStatus(cmd.Context(), os.Stdout, os.Stderr, mgrs, name, format, *packStatusPlain); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// packStatusDoc is the machine-readable status document.
type packStatusDoc struct {
	SchemaVersion  int       `json:"schema_version"`
	NeedsAttention bool      `json:"needs_attention"`
	Rows           []packRow `json:"rows"`
}

// runPackStatus reports the state matrix for one or all packs.
func runPackStatus(ctx context.Context, stdout, stderr io.Writer, mgrs *packManagers, name, format string, plain bool) int {
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	var packs []*skills.LockedPack
	for _, src := range lf.Sources {
		if src.Pack == nil {
			continue
		}
		if name != "" && src.Pack.Name != name {
			continue
		}
		packs = append(packs, src.Pack)
	}
	if len(packs) == 0 {
		if name != "" {
			fmt.Fprintf(stderr, "pack %q is not imported\n", name)
			return ctxExitInfrastructure
		}
		fmt.Fprintln(stdout, "No packs imported. Run 'gridctl pack add <repo-url>' to import one.")
		return ctxExitOK
	}

	skillStatuses, err := mgrs.skills.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	agentStatuses, err := mgrs.agents.Statuses(ctx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}
	wiringRows, err := mgrs.wiring.Statuses(ctx, wiring.StatusOptions{Port: resolveGatewayPort(0)})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	var rows []packRow
	attention := false
	needsAttention := func(state string) bool {
		switch state {
		case skillsync.StateInSync, "missing":
			return false
		}
		return true
	}

	for _, p := range packs {
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
				rows = append(rows, packRow{Kind: "skill", Name: s.Skill, Client: s.Client, State: s.State, Detail: s.Detail})
				attention = attention || needsAttention(s.State)
			}
		}
		for _, s := range agentStatuses {
			if inSelection(p.Agents, s.Agent) {
				rows = append(rows, packRow{Kind: "agent", Name: s.Agent, Client: s.Client, State: s.State, Detail: s.Detail})
				attention = attention || needsAttention(s.State)
			}
		}
		// Rules report store-level presence; per-client projection state
		// lives in 'gridctl ctx status' (fragment granularity in detail).
		for _, n := range p.Rules {
			state := skillsync.StateInSync
			detail := ""
			if mgrs.contexts == nil || !mgrs.contexts.FragmentsActive() {
				state, detail = "missing", "fragments mode is not active"
			} else if _, rerr := mgrs.contexts.ReadFragment(n); rerr != nil {
				state, detail = "missing", "not in the fragment store; re-run 'gridctl pack add'"
			}
			rows = append(rows, packRow{Kind: "rule", Name: n, State: state, Detail: detail})
			// "missing" is clean for skills (never projected); an absent
			// pack rule is attention: the pack claims it.
			attention = attention || state != skillsync.StateInSync
		}
		for _, r := range wiringRows {
			if r.Pack == p.Name {
				rows = append(rows, packRow{Kind: "wiring", Name: r.Name, Client: r.Client, State: r.State, Detail: r.Detail, Remediation: r.Remediation})
				attention = attention || needsAttention(r.State)
			}
		}
		for _, u := range p.Unresolved {
			rows = append(rows, packRow{Kind: "unresolved", Name: u, State: "unresolved",
				Detail: "selected by the pack manifest but not shipped by the repository"})
			attention = true
		}
	}

	doc := packStatusDoc{SchemaVersion: packJSONSchemaVersion, NeedsAttention: attention, Rows: rows}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "Pack imported but nothing projected yet. Run 'gridctl pack apply <name>'.")
			return ctxExitOK
		}
		renderPackRows(stdout, rows, plain)
	}
	if attention {
		return ctxExitAttention
	}
	return ctxExitOK
}

// --- pack remove ---

var (
	packRemoveForce  bool
	packRemoveDryRun bool
	packRemoveFormat string
	packRemoveJSON   *bool
)

var packRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a pack: projections, wiring records, and registry entries",
	Long: `Cascade removal in dependency order: pack-tagged projections are
unsynced from client directories, pack-tagged wiring records are
removed through the ownership manager (the entry is deleted only when
its value is one gridctl recorded), and only then do the pack's skills,
agents, and installed rule fragments leave the local stores, followed
by the pack record itself.

A resource whose projection was hand-edited (drifted) is skipped with a
remediation hint unless --force; everything else still removes, and the
skipped resources stay imported so nothing is lost. Removing a skill
removes all of its projections, including any made outside the pack.

Exit codes:
  0  removed cleanly
  1  partial (drifted resources kept)
  2  infrastructure error`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(packRemoveFormat, cmd.Flags().Changed("format"), *packRemoveJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		mgrs, err := newPackManagers()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		store, err := loadRegistry()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ctxExitInfrastructure)
		}
		imp := newImporter(store)
		if exit := runPackRemove(cmd.Context(), os.Stdout, os.Stderr, mgrs, imp, args[0], packRemoveForce, packRemoveDryRun, format); exit != ctxExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

// packRemoveDoc is the machine-readable remove document.
type packRemoveDoc struct {
	SchemaVersion int       `json:"schema_version"`
	Pack          string    `json:"pack"`
	DryRun        bool      `json:"dry_run,omitempty"`
	Rows          []packRow `json:"rows"`
	Kept          []string  `json:"kept,omitempty"`
}

// runPackRemove cascades one pack's removal.
func runPackRemove(ctx context.Context, stdout, stderr io.Writer, mgrs *packManagers, imp *skills.Importer, name string, force, dryRun bool, format string) int {
	locked, err := loadLockedPack(name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	// Drift pre-check: a hand-edited projection means the user changed
	// something gridctl would destroy; without --force the whole resource
	// (projections + registry entry) is kept.
	driftedSkills, driftedAgents, err := driftedPackResources(ctx, mgrs, locked)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	var rows []packRow
	var kept []string
	removableSkills := splitKept(locked.Skills, driftedSkills, force, "skill", &rows, &kept)
	removableAgents := splitKept(locked.Agents, driftedAgents, force, "agent", &rows, &kept)

	if dryRun {
		for _, n := range removableSkills {
			rows = append(rows, packRow{Kind: "skill", Name: n, Action: "would-remove"})
		}
		for _, n := range removableAgents {
			rows = append(rows, packRow{Kind: "agent", Name: n, Action: "would-remove"})
		}
		for _, n := range locked.Rules {
			rows = append(rows, packRow{Kind: "rule", Name: n, Action: "would-remove"})
		}
		if locked.Wiring {
			rows = append(rows, packRow{Kind: "wiring", Name: "gridctl", Action: "would-remove"})
		}
		return finishPackRemove(stdout, stderr, name, rows, kept, true, format)
	}

	// 1. Unsync projections (files leave client trees before the registry
	// entries they came from).
	if len(removableSkills) > 0 {
		projected, perr := projectedNames(ctx, mgrs.home, project.KindSkill, removableSkills)
		if perr != nil {
			fmt.Fprintln(stderr, perr)
			return ctxExitInfrastructure
		}
		if len(projected) > 0 {
			results, uerr := mgrs.skills.Unsync(ctx, projected, skillsync.UnsyncOptions{})
			if uerr != nil {
				fmt.Fprintln(stderr, uerr)
				return ctxExitInfrastructure
			}
			for _, r := range results {
				rows = append(rows, packRow{Kind: "skill", Name: r.Skill, Client: r.Client, Action: r.Action})
			}
		}
	}
	if len(removableAgents) > 0 {
		projected, perr := projectedNames(ctx, mgrs.home, project.KindAgent, removableAgents)
		if perr != nil {
			fmt.Fprintln(stderr, perr)
			return ctxExitInfrastructure
		}
		if len(projected) > 0 {
			results, uerr := mgrs.agents.Unsync(ctx, projected, agentsync.UnsyncOptions{})
			if uerr != nil {
				fmt.Fprintln(stderr, uerr)
				return ctxExitInfrastructure
			}
			for _, r := range results {
				rows = append(rows, packRow{Kind: "agent", Name: r.Agent, Client: r.Client, Action: r.Action})
			}
		}
	}

	// 2. Pack-tagged rule fragment projections (by tag, never by name).
	if len(locked.Rules) > 0 && mgrs.contexts != nil {
		results, ruleNames, uerr := mgrs.contexts.UnsyncPackFragments(ctx, name)
		if uerr != nil {
			fmt.Fprintln(stderr, uerr)
			return ctxExitInfrastructure
		}
		for _, r := range results {
			rows = append(rows, packRow{Kind: "rule", Name: r.Fragment, Client: r.Slug, Action: r.Action})
		}
		// Drop store files only for fragments the pack listed and that
		// lost their pack projections; a user fragment of the same name
		// created afterward is not in locked.Rules at remove time only
		// if they renamed — we only remove names still listed on the pack.
		for _, n := range ruleNames {
			if !containsString(locked.Rules, n) {
				continue
			}
			if _, rerr := mgrs.contexts.RemoveFragment(n); rerr != nil && !errors.Is(rerr, contexts.ErrNoFragment) {
				rows = append(rows, packRow{Kind: "rule", Name: n, Action: "error", Detail: rerr.Error()})
				continue
			}
			rows = append(rows, packRow{Kind: "rule", Name: n, Action: "removed", Detail: "fragment store entry removed"})
		}
	}

	// 3. Wiring records: delete only what ownership proves is ours;
	// undetected clients drop the record alone.
	wr, wkept, werr := removePackWiring(ctx, mgrs.wiring, name, force)
	if werr != nil {
		fmt.Fprintln(stderr, werr)
		return ctxExitInfrastructure
	}
	rows = append(rows, wr...)
	kept = append(kept, wkept...)

	// 4. Registry entries, then the pack record (which the source GC
	// drops automatically when its last resource goes).
	for _, n := range removableSkills {
		if rerr := imp.Remove(n); rerr != nil {
			rows = append(rows, packRow{Kind: "skill", Name: n, Action: "error", Detail: rerr.Error()})
			continue
		}
		rows = append(rows, packRow{Kind: "skill", Name: n, Action: "removed", Detail: "registry entry removed"})
	}
	for _, n := range removableAgents {
		if rerr := imp.RemoveAgent(n); rerr != nil {
			rows = append(rows, packRow{Kind: "agent", Name: n, Action: "error", Detail: rerr.Error()})
			continue
		}
		rows = append(rows, packRow{Kind: "agent", Name: n, Action: "removed", Detail: "registry entry removed"})
	}

	// Partial removal keeps a truthful pack record covering what stayed.
	if err := trimLockedPack(name, kept); err != nil {
		fmt.Fprintln(stderr, err)
		return ctxExitInfrastructure
	}

	return finishPackRemove(stdout, stderr, name, rows, kept, false, format)
}

// driftedPackResources reports which pack resources have a drifted
// projection anywhere.
func driftedPackResources(ctx context.Context, mgrs *packManagers, locked *skills.LockedPack) (map[string]bool, map[string]bool, error) {
	driftedSkills := map[string]bool{}
	driftedAgents := map[string]bool{}
	sst, err := mgrs.skills.Statuses(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range sst {
		if s.State == skillsync.StateDrifted {
			driftedSkills[s.Skill] = true
		}
	}
	ast, err := mgrs.agents.Statuses(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range ast {
		if s.State == agentsync.StateDrifted {
			driftedAgents[s.Agent] = true
		}
	}
	_ = locked
	return driftedSkills, driftedAgents, nil
}

// splitKept separates removable names from drift-kept ones, emitting
// skip rows for the latter.
func splitKept(names []string, drifted map[string]bool, force bool, kind string, rows *[]packRow, kept *[]string) []string {
	var removable []string
	for _, n := range names {
		if drifted[n] && !force {
			*rows = append(*rows, packRow{Kind: kind, Name: n, Action: "skipped-drift",
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
func removePackWiring(ctx context.Context, wm *wiring.Manager, packName string, force bool) ([]packRow, []string, error) {
	rows := []packRow{}
	var kept []string
	wiringRows, err := wm.Statuses(ctx, wiring.StatusOptions{Port: resolveGatewayPort(0)})
	if err != nil {
		return nil, nil, err
	}
	for _, r := range wiringRows {
		if r.Pack != packName {
			continue
		}
		prov, ok := wm.Registry().FindBySlug(r.Client)
		if !ok {
			res, derr := wm.DropRecord(ctx, r.Client, r.Name)
			if derr != nil && !errors.Is(derr, wiring.ErrNotRecorded) {
				return nil, nil, derr
			}
			if res.Action != "" {
				rows = append(rows, packRow{Kind: "wiring", Name: r.Name, Client: r.Client, Action: res.Action, Detail: res.Detail})
			}
			continue
		}
		configPath, found := prov.Detect()
		if !found {
			res, derr := wm.DropRecord(ctx, r.Client, r.Name)
			if derr != nil && !errors.Is(derr, wiring.ErrNotRecorded) {
				return nil, nil, derr
			}
			rows = append(rows, packRow{Kind: "wiring", Name: r.Name, Client: r.Client, Action: res.Action, Detail: res.Detail})
			continue
		}
		res, uerr := wm.UnlinkClient(ctx, prov, configPath, r.Name, force, false)
		if uerr != nil {
			return nil, nil, uerr
		}
		row := packRow{Kind: "wiring", Name: r.Name, Client: r.Client, Action: res.Action, Detail: res.Detail, Remediation: res.Remediation}
		rows = append(rows, row)
		if res.Action == wiring.ActionSkippedDrift || res.Action == wiring.ActionSkippedForeign {
			kept = append(kept, "wiring/"+r.Client)
		}
	}
	return rows, kept, nil
}

// trimLockedPack drops the pack record, or shrinks it to the resources
// a partial removal kept.
func trimLockedPack(name string, kept []string) error {
	lockPath := skills.LockFilePath()
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		return err
	}
	srcName, src, ok := lf.FindPackSource(name)
	if !ok {
		return nil // source already GC'd by the last resource removal
	}
	if len(kept) == 0 {
		src.Pack = nil
		if len(src.Skills) == 0 && len(src.Agents) == 0 {
			lf.RemoveSource(srcName)
		} else {
			lf.SetSource(srcName, *src)
		}
		return skills.WriteLockFile(lockPath, lf)
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
	return skills.WriteLockFile(lockPath, lf)
}

// finishPackRemove renders the remove report and picks the exit code.
func finishPackRemove(stdout, stderr io.Writer, name string, rows []packRow, kept []string, dryRun bool, format string) int {
	doc := packRemoveDoc{SchemaVersion: packJSONSchemaVersion, Pack: name, DryRun: dryRun, Rows: rows, Kept: kept}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return ctxExitInfrastructure
		}
	} else {
		renderPackRows(stdout, rows, false)
		if len(kept) > 0 {
			fmt.Fprintf(stdout, "\nKept (drifted, re-run with --force to remove): %s\n", strings.Join(kept, ", "))
		} else if !dryRun {
			fmt.Fprintf(stdout, "\nPack %q removed.\n", name)
		}
	}
	for _, r := range rows {
		if r.Action == "error" {
			return ctxExitAttention
		}
	}
	if len(kept) > 0 {
		return ctxExitAttention
	}
	return ctxExitOK
}

func init() {
	packAddCmd.Flags().StringVar(&packAddRef, "ref", "", "Git ref to import (branch, tag, or commit; default: the default branch)")
	packAddCmd.Flags().BoolVar(&packAddTrust, "trust", false, "Proceed despite security findings")
	packAddCmd.Flags().BoolVar(&packAddDryRun, "dry-run", false, "Resolve and report without importing")
	packAddCmd.Flags().StringVar(&packAddFormat, "format", "text", "Output format: text or json")
	packAddJSON = addJSONAlias(packAddCmd)

	packApplyCmd.Flags().BoolVar(&packApplyForce, "force", false, "Overwrite drifted or foreign resources (after backup)")
	packApplyCmd.Flags().BoolVar(&packApplyDryRun, "dry-run", false, "Show what would change without modifying files")
	packApplyCmd.Flags().StringSliceVar(&packApplyClients, "clients", nil, "Restrict wiring to these client slugs")
	packApplyCmd.Flags().StringVar(&packApplyFormat, "format", "text", "Output format: text or json")
	packApplyJSON = addJSONAlias(packApplyCmd)
	packApplyPlain = addPlainFlag(packApplyCmd)

	packStatusCmd.Flags().StringVar(&packStatusFormat, "format", "text", "Output format: text or json")
	packStatusJSON = addJSONAlias(packStatusCmd)
	packStatusPlain = addPlainFlag(packStatusCmd)

	packRemoveCmd.Flags().BoolVar(&packRemoveForce, "force", false, "Remove even when projections were hand-edited")
	packRemoveCmd.Flags().BoolVar(&packRemoveDryRun, "dry-run", false, "Show what would be removed without removing")
	packRemoveCmd.Flags().StringVar(&packRemoveFormat, "format", "text", "Output format: text or json")
	packRemoveJSON = addJSONAlias(packRemoveCmd)

	packCmd.AddCommand(packAddCmd, packApplyCmd, packStatusCmd, packRemoveCmd)
}
