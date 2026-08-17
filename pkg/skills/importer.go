package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/registry"
)

// AuthConfig carries authentication configuration for a git operation.
// The Token and SSHPassphrase fields are transient — they must never be
// persisted to disk. CredentialRef is the opaque reference string (e.g.
// "${vault:GIT_TOKEN}") that gets stored in Origin/LockFile so that Update
// can re-resolve it later.
type AuthConfig struct {
	Method         string // "", "none", "token", "ssh-agent", "ssh-key"
	Token          string // resolved plaintext — transient, never persisted
	CredentialRef  string // e.g. "${vault:GIT_TOKEN}" — persisted
	SSHUser        string // defaults to "git" when empty
	SSHKeyPath     string // required for method "ssh-key"
	SSHPassphrase  string // transient
	KnownHostsPath string // reserved for future host-key policy work
}

// BuildAuther constructs a git.Auther matching the AuthConfig's Method.
// Returns an error for unknown methods. Individual Auther implementations
// also validate their own inputs (e.g. HTTPSTokenAuth rejects empty tokens).
func BuildAuther(cfg AuthConfig) (gitpkg.Auther, error) {
	switch cfg.Method {
	case "", "none":
		return gitpkg.NoAuth{}, nil
	case "token":
		return gitpkg.HTTPSTokenAuth{Token: cfg.Token}, nil
	case "ssh-agent":
		return gitpkg.SSHAgentAuth{User: cfg.SSHUser}, nil
	case "ssh-key":
		return gitpkg.SSHKeyFileAuth{
			User:           cfg.SSHUser,
			KeyPath:        cfg.SSHKeyPath,
			Passphrase:     cfg.SSHPassphrase,
			KnownHostsPath: cfg.KnownHostsPath,
		}, nil
	default:
		return nil, fmt.Errorf("unknown auth method %q", cfg.Method)
	}
}

// resolveAuther returns the Auther to use for a URL given an AuthConfig.
// Precedence: explicit method > GITHUB_TOKEN env var (HTTPS only) > NoAuth.
// This preserves backward compatibility with the pre-AuthConfig behavior.
func resolveAuther(cfg AuthConfig, url string) (gitpkg.Auther, error) {
	if cfg.Method != "" && cfg.Method != "none" {
		return BuildAuther(cfg)
	}
	// Ambient fallback: GITHUB_TOKEN for HTTPS URLs.
	if gitpkg.DetectProtocol(url) == gitpkg.ProtocolHTTPS {
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			return gitpkg.HTTPSTokenAuth{Token: token}, nil
		}
	}
	// Ambient SSH: go-git dials the agent itself from a nil AuthMethod and
	// reports a raw xanzy/ssh-agent string when it cannot, which tells the
	// user nothing they can act on. Preflight so the failure names the cause.
	//
	// NoAuth is still what we return once an agent is reachable. Substituting
	// SSHAgentAuth here would look tidier but would force the user to "git",
	// discarding an explicit user@host from the URL that go-git's own default
	// builder honors.
	if gitpkg.DetectProtocol(url) == gitpkg.ProtocolSSH {
		if err := gitpkg.SSHAgentAvailable(); err != nil {
			return nil, err
		}
	}
	return gitpkg.NoAuth{}, nil
}

// CredentialResolver resolves an opaque reference like "${vault:GIT_TOKEN}"
// to its raw value. Callers (CLI, HTTP API) register one via
// Importer.SetCredentialResolver so that Update can re-resolve credentials
// recorded in Origin/LockFile without the importer needing to know where
// the values live.
type CredentialResolver func(ref string) (string, error)

// ImportOptions controls the import behavior.
type ImportOptions struct {
	Repo       string
	Ref        string
	Path       string
	Trust      bool     // Skip security scan confirmation
	NoActivate bool     // Import as draft instead of active
	Force      bool     // Overwrite existing skills
	Rename     string   // Rename the skill on import
	Selected   []string // Only import skills with these names (empty = import all)
	// SelectedAgents imports exactly these agent names. When empty, the
	// legacy behavior holds: all agents when Selected is also empty, no
	// agents when a skill selection is present (the web UI picker's
	// contract). Pack imports always pass fully resolved lists.
	SelectedAgents []string
	// Discovered supplies a pre-cloned discovery result so callers that
	// already ran CloneAndDiscover (pack add reads the manifest first)
	// do not clone twice. Nil means Import clones itself.
	Discovered *CloneResult
	Auth       AuthConfig
	// PreserveState carries over the existing skill's State (draft/active/
	// disabled) instead of resetting it. Used by Update so that re-syncing
	// a source does not silently re-activate skills the user disabled.
	PreserveState bool
}

// ImportResult contains the results of an import operation.
type ImportResult struct {
	Imported []ImportedSkill `json:"imported"`
	Skipped  []SkippedSkill  `json:"skipped"`
	Warnings []string        `json:"warnings"`
	// ImportedAgents and SkippedAgents record agent definitions the same
	// import discovered under the agents/*.md convention.
	ImportedAgents []ImportedAgent `json:"importedAgents,omitempty"`
	SkippedAgents  []SkippedAgent  `json:"skippedAgents,omitempty"`
}

// ImportedSkill records a successfully imported skill.
type ImportedSkill struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	Origin *Origin `json:"origin,omitempty"`
	// FilesCopied counts supporting files installed alongside SKILL.md
	// (scripts/, references/, assets/, and package metadata).
	FilesCopied int               `json:"filesCopied"`
	Findings    []SecurityFinding `json:"findings,omitempty"`
}

// SkippedSkill records a skill (or agent) that was skipped during import.
type SkippedSkill struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// SkippedAgent aliases SkippedSkill so agent call sites read as what
// they are; the shape and JSON encoding are identical.
type SkippedAgent = SkippedSkill

// ImportedAgent records a successfully imported agent definition.
type ImportedAgent struct {
	Name     string            `json:"name"`
	Path     string            `json:"path"`
	Origin   *Origin           `json:"origin,omitempty"`
	Findings []SecurityFinding `json:"findings,omitempty"`
}

// Importer orchestrates the skill import process.
type Importer struct {
	store              *registry.Store
	registryDir        string
	lockPath           string
	logger             *slog.Logger
	credentialResolver CredentialResolver
	// lockfileMu serializes read-modify-write windows on skills.lock.yaml.
	// Held only across the file RMW, not the surrounding git work, so
	// concurrent callers (e.g. handleSkillSourcesSyncAll's bounded fan-out)
	// still parallelize their clones.
	lockfileMu sync.Mutex
}

// NewImporter creates a new skill importer.
func NewImporter(store *registry.Store, registryDir, lockPath string, logger *slog.Logger) *Importer {
	return &Importer{
		store:       store,
		registryDir: registryDir,
		lockPath:    lockPath,
		logger:      logger,
	}
}

// SetCredentialResolver registers a resolver used to expand CredentialRef
// values stored in Origin/LockFile when Update fetches the latest state.
// Without a resolver, Update can still run for sources that have no stored
// reference (ambient GITHUB_TOKEN / public repos), but will fail fast for
// sources that do.
func (imp *Importer) SetCredentialResolver(r CredentialResolver) {
	imp.credentialResolver = r
}

// Import clones a repo, discovers skills and agents, validates, scans,
// and imports.
func (imp *Importer) Import(opts ImportOptions) (*ImportResult, error) {
	if opts.Path != "" {
		if err := SafeRepoPath(opts.Path); err != nil {
			return nil, err
		}
	}
	// Reject a malformed rename before any work: it becomes both the skill
	// name and the destination directory, so an unvalidated value ("../x")
	// would escape the registry root.
	if opts.Rename != "" {
		if err := registry.ValidateSkillName(opts.Rename); err != nil {
			return nil, fmt.Errorf("invalid --rename value %q: %w", opts.Rename, err)
		}
	}

	imp.logger.Info("importing skills", "repo", gitpkg.RedactURL(opts.Repo), "ref", opts.Ref)

	result := opts.Discovered
	if result == nil {
		var err error
		result, err = CloneAndDiscover(opts.Repo, opts.Ref, opts.Path, opts.Auth, imp.logger)
		if err != nil {
			return nil, err
		}
	}

	if len(result.Skills) == 0 && len(result.Agents) == 0 {
		if len(result.Malformed) > 0 {
			return nil, fmt.Errorf("no importable skills found: %s", summarizeMalformed(result.Malformed))
		}
		if len(result.MalformedAgents) > 0 {
			return nil, fmt.Errorf("no importable agents found: %s", summarizeMalformed(result.MalformedAgents))
		}
		return nil, fmt.Errorf("no SKILL.md or agents/*.md files found in repository")
	}

	imp.logger.Info("discovered skills", "count", len(result.Skills), "agents", len(result.Agents))

	importResult := &ImportResult{}
	// Surface parse failures on fresh imports only. Update re-imports with
	// PreserveState, and warning about a permanently broken sibling SKILL.md
	// on every sync would just train users to ignore warnings.
	if !opts.PreserveState {
		for _, m := range result.Malformed {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("%s: failed to parse: %s", m.Path, m.Err))
		}
		for _, m := range result.MalformedAgents {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("%s: failed to parse: %s", m.Path, m.Err))
		}
	}

	// Build selection set for O(1) lookup (empty = import all)
	selectedSet := make(map[string]bool, len(opts.Selected))
	for _, name := range opts.Selected {
		selectedSet[name] = true
	}

	lockedSkills := make(map[string]LockedSkill)

	for _, discovered := range result.Skills {
		skillName := discovered.Name
		if opts.Rename != "" && len(result.Skills) == 1 {
			skillName = opts.Rename
		}

		// Filter to user-selected skills when a selection is provided
		if len(opts.Selected) > 0 && !selectedSet[skillName] {
			continue
		}

		// Check for existing skill; treat explicitly selected skills as force-overwrite
		if _, err := imp.store.GetSkill(skillName); err == nil {
			force := opts.Force || (len(opts.Selected) > 0 && selectedSet[skillName])
			if !force {
				importResult.Skipped = append(importResult.Skipped, SkippedSkill{
					Name:   skillName,
					Reason: fmt.Sprintf("skill %q already exists (use --force to overwrite or --rename to import with a different name)", skillName),
				})
				continue
			}
		}

		// Validate
		vr := registry.ValidateSkillFull(discovered.Skill)
		if !vr.Valid() {
			importResult.Skipped = append(importResult.Skipped, SkippedSkill{
				Name:   skillName,
				Reason: fmt.Sprintf("validation failed: %s", vr.Error()),
			})
			continue
		}
		if len(vr.Warnings) > 0 {
			for _, w := range vr.Warnings {
				importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("%s: %s", skillName, w))
			}
		}

		// Resolve the destination directory up front. SaveSkill defaults Dir
		// (to an existing skill's Dir, else the name), but the supporting-file
		// copy has to know where it is writing before SaveSkill runs, so set
		// Dir explicitly here and let SaveSkill's defaulting become a no-op.
		// Keeping one resolution point stops the two from drifting.
		if discovered.Skill.Dir == "" {
			if existing, err := imp.store.GetSkill(skillName); err == nil && existing.Dir != "" {
				discovered.Skill.Dir = existing.Dir
			} else {
				discovered.Skill.Dir = skillName
			}
		}
		skillsRoot := filepath.Join(imp.registryDir, "skills")
		skillDir := filepath.Join(skillsRoot, discovered.Skill.Dir)
		// Defense in depth behind SaveSkill's name validation: never let a
		// resolved destination land outside the skills root.
		if !withinDir(skillsRoot, skillDir) {
			importResult.Skipped = append(importResult.Skipped, SkippedSkill{
				Name:   skillName,
				Reason: fmt.Sprintf("resolved directory %q escapes the registry", discovered.Skill.Dir),
			})
			continue
		}

		// Gather supporting files from the clone. Nothing is written yet: the
		// scan below has to run against the source so a rejected skill never
		// leaves a partial install behind.
		srcDir := filepath.Join(result.RepoPath, discovered.Path)
		supporting, copyWarnings, err := collectSupportingFiles(srcDir)
		// Surface what was excluded even when collection then failed, so the
		// skip reason is not the only thing the user sees.
		for _, w := range copyWarnings {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("%s: %s", skillName, w))
		}
		if err != nil {
			var le *limitError
			if errors.As(err, &le) {
				importResult.Skipped = append(importResult.Skipped, SkippedSkill{
					Name:   skillName,
					Reason: fmt.Sprintf("supporting files exceed limits: %s", le.reason),
				})
				continue
			}
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("%s: collecting supporting files: %v", skillName, err))
			continue
		}

		// Security scan: body first (any finding blocks, unchanged), then the
		// supporting files (only danger-severity findings block; see
		// scanSupportingFiles for why).
		scanResult := ScanSkill(discovered.Skill)
		treeFindings, treeBlocking := scanSupportingFiles(supporting)
		scanResult.Findings = append(scanResult.Findings, treeFindings...)
		blocked := !scanResult.Safe || treeBlocking
		// Keep Safe consistent with Findings so a later reader of the struct
		// cannot conclude "safe" while findings are attached.
		scanResult.Safe = len(scanResult.Findings) == 0
		if blocked && !opts.Trust {
			importResult.Skipped = append(importResult.Skipped, SkippedSkill{
				Name:   skillName,
				Reason: fmt.Sprintf("security findings detected (use --trust to proceed):\n%s", FormatFindings(scanResult.Findings)),
			})
			continue
		}

		// Set state. PreserveState carries over the existing skill's State
		// across a re-import (used by Update); otherwise NoActivate decides
		// between draft and active.
		discovered.Skill.Name = skillName
		state := registry.StateActive
		if opts.NoActivate {
			state = registry.StateDraft
		}
		if opts.PreserveState {
			if existing, err := imp.store.GetSkill(skillName); err == nil && existing.State != "" {
				state = existing.State
			}
		}
		discovered.Skill.State = state

		// Save to registry first. SaveSkill validates the skill (including its
		// name) before creating any directory, and that validation is the only
		// thing standing between a malformed name and a destructive write, so
		// nothing may touch the filesystem ahead of it.
		if err := imp.store.SaveSkill(discovered.Skill); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to save %s: %v", skillName, err))
			continue
		}

		// Then install supporting files beside the rendered SKILL.md, and
		// refresh the cached count so it reflects what actually landed.
		filesCopied, err := installSupportingFiles(skillDir, supporting)
		if err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to install supporting files for %s: %v", skillName, err))
			continue
		}
		if err := imp.store.RefreshFileCount(skillName); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to refresh file count for %s: %v", skillName, err))
		}

		// Compute fingerprint
		fp := ComputeFingerprint(discovered.Skill)

		// Snapshot the just-written SKILL.md hash so DetectDrift can later
		// distinguish user edits from upstream changes. ContentHash records
		// the upstream file as fetched; InstalledHash records what we wrote.
		// Note: this covers SKILL.md only; edits to installed supporting
		// files are not yet drift-tracked (see CHANGELOG).
		installedHash, _ := ContentHashFile(filepath.Join(skillDir, "SKILL.md"))

		// Write origin sidecar. CredentialRef (if any) is persisted as an
		// opaque reference string — the raw token is never written to disk.
		origin := &Origin{
			Repo:          opts.Repo,
			Ref:           opts.Ref,
			Path:          discovered.Path,
			CommitSHA:     result.CommitSHA,
			ImportedAt:    time.Now().UTC(),
			ContentHash:   discovered.ContentHash,
			InstalledHash: installedHash,
			Fingerprint:   fp,
			CredentialRef: opts.Auth.CredentialRef,
		}

		if err := WriteOrigin(skillDir, origin); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to write origin for %s: %v", skillName, err))
		}

		lockedSkills[skillName] = LockedSkill{
			Path:        discovered.Path,
			ContentHash: discovered.ContentHash,
			Fingerprint: fp,
		}

		imported := ImportedSkill{
			Name:        skillName,
			Path:        discovered.Path,
			Origin:      origin,
			FilesCopied: filesCopied,
		}
		if len(scanResult.Findings) > 0 {
			imported.Findings = scanResult.Findings
		}
		importResult.Imported = append(importResult.Imported, imported)

		imp.logger.Info("imported skill", "name", skillName, "supportingFiles", filesCopied)
	}

	lockedAgents, keptAgents := imp.importAgents(result, opts, importResult)

	// Update lock file. Re-read inside the critical section so concurrent
	// Import calls (e.g. from handleSkillSourcesSyncAll's bounded fan-out)
	// observe each other's writes instead of clobbering them.
	if len(lockedSkills) > 0 || len(lockedAgents) > 0 {
		imp.lockfileMu.Lock()
		defer imp.lockfileMu.Unlock()

		// The cross-process lock covers the whole read-modify-write:
		// the API server builds a fresh Importer per request, so the
		// in-process mutex alone cannot serialize concurrent writers.
		err := MutateLockFile(context.Background(), imp.lockPath, func(lf *LockFile) (bool, error) {
			sourceName := RepoToName(opts.Repo)
			// Carry previously tracked agents forward instead of wiping them:
			// a Selected import never processes agents at all (the web UI's
			// "add more from this source" flow), and an unforced re-import
			// skips agents that already exist in the store, but neither means
			// the source stopped shipping them.
			var prevPack *LockedPack
			if prev, ok := lf.Sources[sourceName]; ok {
				// A source rewrite must never orphan its pack record: pack
				// verbs would report "not imported" while projections still
				// carry the tag, with no cascade-removal path left.
				prevPack = prev.Pack
				if prev.Agents != nil {
					switch {
					case len(opts.SelectedAgents) > 0:
						// An explicit agent selection re-imports those agents
						// only; the source's other agents keep their entries.
						for name, entry := range prev.Agents {
							if _, done := lockedAgents[name]; !done {
								if lockedAgents == nil {
									lockedAgents = make(map[string]LockedAgent)
								}
								lockedAgents[name] = entry
							}
						}
					case len(opts.Selected) > 0:
						lockedAgents = prev.Agents
					default:
						for _, name := range keptAgents {
							if entry, ok := prev.Agents[name]; ok {
								if lockedAgents == nil {
									lockedAgents = make(map[string]LockedAgent)
								}
								lockedAgents[name] = entry
							}
						}
					}
				}
			}
			lf.SetSource(sourceName, LockedSource{
				Repo:          opts.Repo,
				Ref:           opts.Ref,
				CommitSHA:     result.CommitSHA,
				FetchedAt:     time.Now().UTC(),
				ContentHash:   result.CommitSHA,
				Skills:        lockedSkills,
				Agents:        lockedAgents,
				CredentialRef: opts.Auth.CredentialRef,
				Pack:          prevPack,
			})
			return true, nil
		})
		if err != nil {
			return importResult, fmt.Errorf("updating lock file: %w", err)
		}
	}

	return importResult, nil
}

// importAgents installs the agent definitions a clone discovered. Agents
// are written verbatim (identity render): the fetched bytes become
// ~/.gridctl/registry/agents/<name>/AGENT.md unchanged, so ContentHash
// and InstalledHash coincide at import time. Explicit skill selection
// (the web UI picker) skips agents entirely: the user chose specific
// skills, and agents were not on offer. The second return lists agents
// skipped as already-existing conflicts; their prior lock entries must
// survive the source rewrite.
func (imp *Importer) importAgents(result *CloneResult, opts ImportOptions, importResult *ImportResult) (map[string]LockedAgent, []string) {
	// Legacy contract: a skill selection alone skips agents (the web UI
	// picker chose specific skills; agents were not on offer). An
	// explicit agent selection overrides that and imports exactly those.
	if len(result.Agents) == 0 || (len(opts.Selected) > 0 && len(opts.SelectedAgents) == 0) {
		return nil, nil
	}
	selectedAgents := make(map[string]bool, len(opts.SelectedAgents))
	for _, name := range opts.SelectedAgents {
		selectedAgents[name] = true
	}
	// Selected-implies-overwrite is scoped to this source: an agent the
	// lockfile attributes to a different source is another import's
	// resource and still needs --force.
	sameSource := func(name string) bool {
		lf, err := ReadLockFile(imp.lockPath)
		if err != nil {
			return false
		}
		srcName, _, found := lf.FindAgentSource(name)
		return !found || srcName == RepoToName(opts.Repo)
	}

	// Duplicate names inside one batch fail every carrier: Claude Code
	// resolves same-named agents by undefined read order, so importing
	// either would be a coin flip.
	nameSources := make(map[string][]string, len(result.Agents))
	for _, a := range result.Agents {
		nameSources[a.Name] = append(nameSources[a.Name], a.Path)
	}

	lockedAgents := make(map[string]LockedAgent)
	var kept []string
	for _, discovered := range result.Agents {
		if len(opts.SelectedAgents) > 0 && !selectedAgents[discovered.Name] {
			continue
		}
		if paths := nameSources[discovered.Name]; len(paths) > 1 {
			importResult.SkippedAgents = append(importResult.SkippedAgents, SkippedAgent{
				Name:   discovered.Name,
				Reason: fmt.Sprintf("duplicate agent name %q in %s (Claude Code resolves duplicates by undefined read order; rename one)", discovered.Name, strings.Join(paths, " and ")),
			})
			continue
		}
		if err := ValidateAgentName(discovered.Name); err != nil {
			importResult.SkippedAgents = append(importResult.SkippedAgents, SkippedAgent{
				Name:   discovered.Name,
				Reason: fmt.Sprintf("%s: %v", discovered.Path, err),
			})
			continue
		}
		agentForce := opts.Force || (len(opts.SelectedAgents) > 0 && selectedAgents[discovered.Name] && sameSource(discovered.Name))
		if _, err := GetAgent(imp.registryDir, discovered.Name); err == nil && !agentForce {
			importResult.SkippedAgents = append(importResult.SkippedAgents, SkippedAgent{
				Name:   discovered.Name,
				Reason: fmt.Sprintf("agent %q already exists (use --force to overwrite)", discovered.Name),
			})
			kept = append(kept, discovered.Name)
			continue
		}

		scanResult := ScanAgent(discovered.Definition)
		if !scanResult.Safe && !opts.Trust {
			importResult.SkippedAgents = append(importResult.SkippedAgents, SkippedAgent{
				Name:   discovered.Name,
				Reason: fmt.Sprintf("security findings detected (use --trust to proceed):\n%s", FormatFindings(scanResult.Findings)),
			})
			continue
		}

		agentDir := AgentDir(imp.registryDir, discovered.Name)
		agentsRoot := AgentsRoot(imp.registryDir)
		// Defense in depth behind ValidateAgentName: never let a resolved
		// destination land outside the agents root.
		if !withinDir(agentsRoot, agentDir) {
			importResult.SkippedAgents = append(importResult.SkippedAgents, SkippedAgent{
				Name:   discovered.Name,
				Reason: fmt.Sprintf("resolved directory %q escapes the registry", discovered.Name),
			})
			continue
		}
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to save agent %s: %v", discovered.Name, err))
			continue
		}
		agentFile := filepath.Join(agentDir, "AGENT.md")
		if err := atomicWriteBytes(agentFile, discovered.Definition.Raw); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to save agent %s: %v", discovered.Name, err))
			continue
		}

		installedHash, _ := ContentHashFile(agentFile)
		origin := &Origin{
			Repo:          opts.Repo,
			Ref:           opts.Ref,
			Path:          discovered.Path,
			CommitSHA:     result.CommitSHA,
			ImportedAt:    time.Now().UTC(),
			ContentHash:   discovered.ContentHash,
			InstalledHash: installedHash,
			CredentialRef: opts.Auth.CredentialRef,
		}
		if err := WriteOrigin(agentDir, origin); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to write origin for agent %s: %v", discovered.Name, err))
		}

		lockedAgents[discovered.Name] = LockedAgent{
			Path:        discovered.Path,
			ContentHash: discovered.ContentHash,
		}

		imported := ImportedAgent{
			Name:   discovered.Name,
			Path:   discovered.Path,
			Origin: origin,
		}
		if len(scanResult.Findings) > 0 {
			imported.Findings = scanResult.Findings
		}
		importResult.ImportedAgents = append(importResult.ImportedAgents, imported)

		imp.logger.Info("imported agent", "name", discovered.Name)
	}
	return lockedAgents, kept
}

// summarizeMalformed renders malformed SKILL.md entries for the zero-skills
// error, capped so a repository full of bad files stays readable.
func summarizeMalformed(malformed []MalformedSkill) string {
	const maxShown = 3
	shown := malformed
	suffix := ""
	if len(malformed) > maxShown {
		shown = malformed[:maxShown]
		suffix = "; ..."
	}
	parts := make([]string, len(shown))
	for i, m := range shown {
		parts[i] = fmt.Sprintf("%s: %s", m.Path, m.Err)
	}
	return fmt.Sprintf("%d SKILL.md file(s) failed to parse (%s%s)", len(malformed), strings.Join(parts, "; "), suffix)
}

// Remove removes an imported skill and cleans up origin and lock entries.
func (imp *Importer) Remove(skillName string) error {
	skillDir := imp.skillDir(skillName)

	// Delete origin file
	_ = DeleteOrigin(skillDir)

	// Delete from registry
	if err := imp.store.DeleteSkill(skillName); err != nil {
		return fmt.Errorf("deleting skill: %w", err)
	}

	// Update lock file under the cross-process import lock.
	if err := MutateLockFile(context.Background(), imp.lockPath, func(lf *LockFile) (bool, error) {
		lf.RemoveSkill(skillName)
		return true, nil
	}); err != nil {
		return fmt.Errorf("updating lock file: %w", err)
	}

	return nil
}

// Update fetches latest for a skill and applies changes.
//
// trust forwards to ImportOptions.Trust. It defaults to false at every caller:
// a sync that surfaces new security findings is skipped with the finding text
// rather than applied silently. Previously this was hardcoded true, which meant
// every sync refreshed upstream content with the scan gate disabled, harmless
// while only the SKILL.md body was scanned, but not once supporting files are
// installed too.
func (imp *Importer) Update(skillName string, dryRun, force, trust bool) (*ImportResult, error) {
	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		// The name may be an imported agent: agents share the drift-safe
		// update flow, and the re-import below refreshes every kind the
		// source ships anyway.
		if agentOrigin, aerr := ReadOrigin(AgentDir(imp.registryDir, skillName)); aerr == nil {
			origin = agentOrigin
		} else {
			return nil, fmt.Errorf("skill %q has no origin (not an imported skill): %w", skillName, err)
		}
	}

	// Re-resolve any CredentialRef stored at import time.
	auth, err := imp.authFromOrigin(origin)
	if err != nil {
		return nil, err
	}

	imp.logger.Info("checking for updates", "skill", skillName, "repo", gitpkg.RedactURL(origin.Repo))

	newSHA, changed, err := FetchAndCompare(origin.Repo, origin.Ref, origin.CommitSHA, auth, imp.logger)
	if err != nil {
		return nil, fmt.Errorf("checking updates: %w", err)
	}

	// force re-installs from upstream even when the commit is unchanged, so a
	// caller can discard local edits and restore the tracked version (reset).
	// Without force, an unchanged commit is a no-op.
	if !changed && !force {
		return &ImportResult{
			Warnings: []string{fmt.Sprintf("%s is already up to date", skillName)},
		}, nil
	}

	imp.logger.Info("update available", "skill", skillName, "current", ShortSHA(origin.CommitSHA), "latest", ShortSHA(newSHA))

	if dryRun {
		return &ImportResult{
			Warnings: []string{fmt.Sprintf("%s: update available (%s → %s)", skillName, ShortSHA(origin.CommitSHA), ShortSHA(newSHA))},
		}, nil
	}

	// Store old fingerprint for comparison
	oldFingerprint := origin.Fingerprint

	result, err := imp.Import(ImportOptions{
		Repo:          origin.Repo,
		Ref:           origin.Ref,
		Path:          origin.Path,
		Trust:         trust,
		Force:         true,
		Auth:          auth,
		PreserveState: true,
	})
	if err != nil {
		return result, err
	}

	// Check for behavioral changes
	if oldFingerprint != nil && len(result.Imported) > 0 {
		for _, imported := range result.Imported {
			if imported.Origin != nil && imported.Origin.Fingerprint != nil {
				changes := BehavioralChanges(oldFingerprint, imported.Origin.Fingerprint)
				for _, c := range changes {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s: behavioral change — %s", imported.Name, c))
				}
			}
		}
	}

	return result, nil
}

// RemoveAgent removes an imported agent and cleans up origin and lock
// entries.
func (imp *Importer) RemoveAgent(agentName string) error {
	agentDir := AgentDir(imp.registryDir, agentName)
	_ = DeleteOrigin(agentDir)

	if err := DeleteAgent(imp.registryDir, agentName); err != nil {
		return fmt.Errorf("deleting agent: %w", err)
	}

	if err := MutateLockFile(context.Background(), imp.lockPath, func(lf *LockFile) (bool, error) {
		lf.RemoveAgent(agentName)
		return true, nil
	}); err != nil {
		return fmt.Errorf("updating lock file: %w", err)
	}
	return nil
}

// AgentInfo returns details about an imported agent's origin.
func (imp *Importer) AgentInfo(agentName string) (*SkillInfo, error) {
	if _, err := GetAgent(imp.registryDir, agentName); err != nil {
		return nil, err
	}
	info := &SkillInfo{Name: agentName}
	origin, err := ReadOrigin(AgentDir(imp.registryDir, agentName))
	if err != nil {
		return info, nil
	}
	info.Origin = origin
	info.IsRemote = true
	lf, _ := ReadLockFile(imp.lockPath)
	if lf != nil {
		if _, src, found := lf.FindAgentSource(agentName); found {
			info.LastChecked = src.FetchedAt
		}
	}
	return info, nil
}

// Pin updates a skill's ref and disables auto-update.
func (imp *Importer) Pin(skillName, ref string) error {
	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		return fmt.Errorf("skill %q has no origin: %w", skillName, err)
	}

	origin.Ref = ref
	if err := WriteOrigin(skillDir, origin); err != nil {
		return fmt.Errorf("writing origin: %w", err)
	}

	// Update lock file under the cross-process import lock.
	_ = MutateLockFile(context.Background(), imp.lockPath, func(lf *LockFile) (bool, error) {
		srcName, src, found := lf.FindSkillSource(skillName)
		if !found {
			return false, nil
		}
		src.Ref = ref
		lf.SetSource(srcName, *src)
		return true, nil
	})

	return nil
}

// SkillInfo returns details about an imported skill.
type SkillInfo struct {
	Name        string    `json:"name"`
	Origin      *Origin   `json:"origin,omitempty"`
	IsRemote    bool      `json:"isRemote"`
	UpdateAvail bool      `json:"updateAvailable"`
	LatestSHA   string    `json:"latestSha,omitempty"`
	LastChecked time.Time `json:"lastChecked,omitempty"`
}

// Info returns details about a skill's origin and update status.
func (imp *Importer) Info(skillName string) (*SkillInfo, error) {
	if _, err := imp.store.GetSkill(skillName); err != nil {
		return nil, fmt.Errorf("skill %q not found: %w", skillName, err)
	}

	info := &SkillInfo{Name: skillName}

	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		// Local skill, no origin
		return info, nil
	}

	info.Origin = origin
	info.IsRemote = true

	// Check lock file for last checked time
	lf, _ := ReadLockFile(imp.lockPath)
	if _, src, found := lf.FindSkillSource(skillName); found {
		info.LastChecked = src.FetchedAt
	}

	return info, nil
}

func (imp *Importer) skillDir(skillName string) string {
	sk, err := imp.store.GetSkill(skillName)
	if err != nil || sk.Dir == "" {
		return filepath.Join(imp.registryDir, "skills", skillName)
	}
	return filepath.Join(imp.registryDir, "skills", sk.Dir)
}

// authFromOrigin builds an AuthConfig from a stored Origin. If the origin
// carries a CredentialRef, the configured CredentialResolver is invoked
// to obtain the raw token. Without a resolver, a stored CredentialRef is
// a hard failure — we never silently fall through to an unauth clone.
func (imp *Importer) authFromOrigin(origin *Origin) (AuthConfig, error) {
	if origin.CredentialRef == "" {
		return AuthConfig{}, nil
	}
	if imp.credentialResolver == nil {
		return AuthConfig{}, fmt.Errorf("%w: credential %q requires a resolver; vault not available", gitpkg.ErrAuthFailed, origin.CredentialRef)
	}
	token, err := imp.credentialResolver(origin.CredentialRef)
	if err != nil {
		return AuthConfig{}, fmt.Errorf("%w: resolving %q: %w", gitpkg.ErrAuthFailed, origin.CredentialRef, err)
	}
	if token == "" {
		return AuthConfig{}, fmt.Errorf("%w: %q resolved to empty value", gitpkg.ErrEmptyToken, origin.CredentialRef)
	}
	return AuthConfig{
		Method:        "token",
		Token:         token,
		CredentialRef: origin.CredentialRef,
	}, nil
}
