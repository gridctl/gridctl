package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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

// persistedAuth returns the ssh-key method, user, and path to store.
// Every other method, including token and ssh-agent, persists nothing here.
// The passphrase is never returned.
func persistedAuth(cfg AuthConfig) (method, user, path string) {
	if cfg.Method == "ssh-key" && cfg.SSHKeyPath != "" {
		return cfg.Method, cfg.SSHUser, cfg.SSHKeyPath
	}
	return "", "", ""
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
	// Kind is SourceKindLocal for a directory import. Empty keeps git import.
	Kind string
	// SourceName overrides RepoToName. Required when a derived name collides.
	SourceName string
	// KnownName is the client-location source name for a new local root.
	KnownName string
	// Provenance maps a discovered relative path to client import metadata.
	// skill add leaves it nil.
	Provenance map[string]EntryProvenance
	// ResourceKind, when set to skill or agent, imports only that kind.
	ResourceKind string
	// PackImport fails before any registry write when a local source key or
	// a local-owned resource would be replaced. Packs have no force option.
	PackImport bool
	// ExactSelection makes Selected and SelectedAgents authoritative. An
	// empty Selected imports no skills and an empty SelectedAgents imports
	// no agents, regardless of ResourceKind and of the legacy empty-means-all
	// rules. Unset preserves those rules.
	ExactSelection bool
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
// and imports. A local directory import sets Kind to SourceKindLocal and
// skips git. ctx is checked before any registry write and between entries.
func (imp *Importer) Import(ctx context.Context, opts ImportOptions) (*ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Path != "" && opts.Kind != SourceKindLocal {
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

	imp.logger.Info("importing skills", "repo", gitpkg.RedactURL(opts.Repo), "ref", opts.Ref, "kind", opts.Kind)

	var discoverWarnings []string
	result := opts.Discovered
	if opts.Kind == SourceKindLocal {
		prepared, warnings, err := prepareLocalImport(ctx, &opts, imp.logger)
		if err != nil {
			return nil, err
		}
		discoverWarnings = warnings
		result = prepared
	} else if result == nil {
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
		if opts.Kind == SourceKindLocal {
			return nil, fmt.Errorf("no importable skills or agents found in %s", opts.Repo)
		}
		return nil, fmt.Errorf("no SKILL.md or agents/*.md files found in repository")
	}

	imp.logger.Info("discovered skills", "count", len(result.Skills), "agents", len(result.Agents))

	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return nil, err
	}
	sourceName, err := ResolveSourceName(lf, SourceNameInput{
		Kind:      opts.Kind,
		Root:      opts.Repo,
		Explicit:  opts.SourceName,
		KnownName: opts.KnownName,
	})
	if err != nil {
		return nil, err
	}
	if err := GuardSourceKey(lf, sourceName, opts.Kind, opts.Repo); err != nil {
		return nil, err
	}
	if prev, ok := lf.Sources[sourceName]; ok && prev.PackMember != "" && !opts.ExactSelection && opts.Selected == nil && opts.SelectedAgents == nil {
		// A selection-less rewrite of a member (skill update) refreshes the
		// recorded names only. A primary pack source is not a member and is
		// unchanged. Explicit ExactSelection, including an empty list from
		// pack add, stays authoritative.
		opts.ExactSelection = true
		opts.Selected = sortedSkillNames(prev.Skills)
		opts.SelectedAgents = sortedAgentNames(prev.Agents)
	}
	if opts.PackImport {
		if err := GuardPackOwnership(lf, sourceName, result, opts); err != nil {
			return nil, err
		}
	}
	opts.SourceName = sourceName

	importResult := &ImportResult{Warnings: discoverWarnings}
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

	if opts.ResourceKind != ResourceKindAgent && (!opts.ExactSelection || len(opts.Selected) > 0) {
		for _, discovered := range result.Skills {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			skillName := discovered.Name
			if opts.Rename != "" && len(result.Skills) == 1 {
				skillName = opts.Rename
			}

			// Filter to user-selected skills when a selection is provided
			if len(opts.Selected) > 0 && !selectedSet[skillName] {
				continue
			}

			// Check for existing skill. Git selection still implies overwrite
			// unless a local source owns the name. Local re-import of an unowned
			// or same-source name does not need --force.
			if _, err := imp.store.GetSkill(skillName); err == nil {
				selected := len(opts.Selected) > 0 && selectedSet[skillName]
				if !AllowOverwrite(lf, opts.Kind, skillName, sourceName, selected, opts.Force, false) {
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

			var treeHash string
			if opts.Kind == SourceKindLocal {
				var hashErr error
				treeHash, hashErr = SkillTreeHash(ctx, srcDir)
				if hashErr != nil {
					importResult.Skipped = append(importResult.Skipped, SkippedSkill{
						Name:   skillName,
						Reason: fmt.Sprintf("hashing skill tree: %v", hashErr),
					})
					continue
				}
			}

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

			// Write origin sidecar. A vault reference and an ssh-key path may be
			// persisted. The raw token, key material, and passphrase are not.
			// Local origins record none of those fields.
			authMethod, sshUser, sshKeyPath := persistedAuth(opts.Auth)
			client, location := provenanceFor(opts, discovered.Path, skillDir)
			origin := &Origin{
				Repo:                     opts.Repo,
				Ref:                      opts.Ref,
				Path:                     discovered.Path,
				CommitSHA:                result.CommitSHA,
				ImportedAt:               time.Now().UTC(),
				ContentHash:              discovered.ContentHash,
				InstalledHash:            installedHash,
				Fingerprint:              fp,
				SupportingFilesInstalled: true,
				CredentialRef:            opts.Auth.CredentialRef,
				AuthMethod:               authMethod,
				SSHUser:                  sshUser,
				SSHKeyPath:               sshKeyPath,
				Client:                   client,
				Location:                 location,
			}
			if opts.Kind == SourceKindLocal {
				origin.Kind = SourceKindLocal
				origin.Ref = ""
				origin.CommitSHA = ""
				origin.CredentialRef = ""
				origin.AuthMethod = ""
				origin.SSHUser = ""
				origin.SSHKeyPath = ""
			}

			if err := WriteOrigin(skillDir, origin); err != nil {
				importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to write origin for %s: %v", skillName, err))
			}

			lockedSkills[skillName] = LockedSkill{
				Path:        discovered.Path,
				ContentHash: discovered.ContentHash,
				TreeHash:    treeHash,
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
	}

	lockedAgents, keptAgents := imp.importAgents(ctx, result, opts, sourceName, lf, importResult)

	// Update lock file. Re-read inside the critical section so concurrent
	// Import calls (e.g. from handleSkillSourcesSyncAll's bounded fan-out)
	// observe each other's writes instead of clobbering them.
	if len(lockedSkills) > 0 || len(lockedAgents) > 0 {
		imp.lockfileMu.Lock()
		defer imp.lockfileMu.Unlock()

		// The cross-process lock covers the whole read-modify-write:
		// the API server builds a fresh Importer per request, so the
		// in-process mutex alone cannot serialize concurrent writers.
		err := MutateLockFile(ctx, imp.lockPath, func(lf *LockFile) (bool, error) {
			if err := GuardSourceKey(lf, sourceName, opts.Kind, opts.Repo); err != nil {
				return false, err
			}
			if opts.Kind == SourceKindLocal {
				canDrop := opts.Path == "" && len(opts.Selected) == 0 && len(opts.SelectedAgents) == 0 && opts.ResourceKind == "" &&
					len(importResult.Skipped) == 0 && len(importResult.SkippedAgents) == 0 &&
					len(result.Malformed) == 0 && len(result.MalformedAgents) == 0
				return true, RecordLocalSource(lf, sourceName, opts.Repo, time.Now().UTC(), lockedSkills, lockedAgents, result, canDrop)
			}
			// Carry previously tracked agents forward instead of wiping them:
			// a Selected import never processes agents at all (the web UI's
			// "add more from this source" flow), and an unforced re-import
			// skips agents that already exist in the store, but neither means
			// the source stopped shipping them.
			var prevPack *LockedPack
			var prevPackMember string
			if prev, ok := lf.Sources[sourceName]; ok {
				// A source rewrite must never orphan its pack record: pack
				// verbs would report "not imported" while projections still
				// carry the tag, with no cascade-removal path left. PackMember
				// is carried the same way so a member rewrite keeps its marker.
				prevPack = prev.Pack
				prevPackMember = prev.PackMember
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
			authMethod, sshUser, sshKeyPath := persistedAuth(opts.Auth)
			lf.SetSource(sourceName, LockedSource{
				Repo:          opts.Repo,
				Ref:           opts.Ref,
				CommitSHA:     result.CommitSHA,
				FetchedAt:     time.Now().UTC(),
				ContentHash:   result.CommitSHA,
				Skills:        lockedSkills,
				Agents:        lockedAgents,
				CredentialRef: opts.Auth.CredentialRef,
				AuthMethod:    authMethod,
				SSHUser:       sshUser,
				SSHKeyPath:    sshKeyPath,
				Pack:          prevPack,
				PackMember:    prevPackMember,
			})
			installedSkills := map[string]struct{}{}
			for name := range lockedSkills {
				installedSkills[name] = struct{}{}
			}
			installedAgents := map[string]struct{}{}
			for name := range lockedAgents {
				installedAgents[name] = struct{}{}
			}
			ReleaseInstalledNames(lf, sourceName, false, installedSkills, installedAgents)
			return true, nil
		})
		if err != nil {
			return importResult, fmt.Errorf("updating lock file: %w", err)
		}
	}

	return importResult, nil
}

func sortedSkillNames(in map[string]LockedSkill) []string {
	names := make([]string, 0, len(in))
	for name := range in {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedAgentNames(in map[string]LockedAgent) []string {
	names := make([]string, 0, len(in))
	for name := range in {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// importAgents installs the agent definitions a clone discovered. Agents
// are written verbatim (identity render): the fetched bytes become
// ~/.gridctl/registry/agents/<name>/AGENT.md unchanged, so ContentHash
// and InstalledHash coincide at import time. Explicit skill selection
// (the web UI picker) skips agents entirely: the user chose specific
// skills, and agents were not on offer. The second return lists agents
// skipped as already-existing conflicts; their prior lock entries must
// survive the source rewrite.
func (imp *Importer) importAgents(ctx context.Context, result *CloneResult, opts ImportOptions, sourceName string, lf *LockFile, importResult *ImportResult) (map[string]LockedAgent, []string) {
	// Legacy contract: a skill selection alone skips agents (the web UI
	// picker chose specific skills; agents were not on offer). An
	// explicit agent selection overrides that and imports exactly those.
	// A skill-only batch never imports agents.
	if opts.ExactSelection && len(opts.SelectedAgents) == 0 {
		return nil, nil
	}
	if opts.ResourceKind == ResourceKindSkill || len(result.Agents) == 0 || (len(opts.Selected) > 0 && len(opts.SelectedAgents) == 0 && opts.ResourceKind != ResourceKindAgent) {
		return nil, nil
	}
	selectedAgents := make(map[string]bool, len(opts.SelectedAgents))
	for _, name := range opts.SelectedAgents {
		selectedAgents[name] = true
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
		if err := ctx.Err(); err != nil {
			return nil, nil
		}
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
		selected := len(opts.SelectedAgents) > 0 && selectedAgents[discovered.Name]
		agentForce := AllowOverwrite(lf, opts.Kind, discovered.Name, sourceName, selected, opts.Force, true)
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
		authMethod, sshUser, sshKeyPath := persistedAuth(opts.Auth)
		client, location := provenanceFor(opts, discovered.Path, agentDir)
		origin := &Origin{
			Repo:          opts.Repo,
			Ref:           opts.Ref,
			Path:          discovered.Path,
			CommitSHA:     result.CommitSHA,
			ImportedAt:    time.Now().UTC(),
			Kind:          opts.Kind,
			Client:        client,
			Location:      location,
			ContentHash:   discovered.ContentHash,
			InstalledHash: installedHash,
			CredentialRef: opts.Auth.CredentialRef,
			AuthMethod:    authMethod,
			SSHUser:       sshUser,
			SSHKeyPath:    sshKeyPath,
		}
		if opts.Kind == SourceKindLocal {
			origin.Ref = ""
			origin.CommitSHA = ""
			origin.CredentialRef = ""
			origin.AuthMethod = ""
			origin.SSHUser = ""
			origin.SSHKeyPath = ""
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

func provenanceFor(opts ImportOptions, relPath, destDir string) (string, string) {
	existing, err := ReadOrigin(destDir)
	if err == nil && existing != nil && existing.Repo == opts.Repo && existing.Path == relPath && (existing.Client != "" || existing.Location != "") {
		return existing.Client, existing.Location
	}
	if prov, ok := opts.Provenance[relPath]; ok {
		return prov.Client, prov.Location
	}
	return "", ""
}

// Update fetches latest for a skill and applies changes.
//
// trust forwards to ImportOptions.Trust. It defaults to false at every caller:
// a sync that surfaces new security findings is skipped with the finding text
// rather than applied silently. Previously this was hardcoded true, which meant
// every sync refreshed upstream content with the scan gate disabled, harmless
// while only the SKILL.md body was scanned, but not once supporting files are
// installed too.
func (imp *Importer) Update(ctx context.Context, skillName string, dryRun, force, trust bool) (*ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	isSkill := err == nil
	if err != nil {
		// The name may be an imported agent: agents share the drift-safe
		// update flow, and the re-import below refreshes every kind the
		// source ships anyway.
		if agentOrigin, aerr := ReadOrigin(AgentDir(imp.registryDir, skillName)); aerr == nil {
			origin = agentOrigin
			skillDir = AgentDir(imp.registryDir, skillName)
		} else {
			return nil, fmt.Errorf("skill %q has no origin (not an imported skill): %w", skillName, err)
		}
	}
	if origin.IsLocal() {
		return imp.updateLocal(ctx, skillName, skillDir, origin, isSkill, dryRun, force, trust)
	}

	// Rebuild the stored auth (vault reference or ssh-key path).
	auth, err := imp.authFromOrigin(origin)
	if err != nil {
		return nil, err
	}

	imp.logger.Info("checking for updates", "skill", skillName, "repo", gitpkg.RedactURL(origin.Repo))

	newSHA, changed, err := FetchAndCompare(origin.Repo, origin.Ref, origin.CommitSHA, auth, imp.logger)
	if err != nil {
		return nil, fmt.Errorf("checking updates: %w", err)
	}

	// A legacy skill import with a trustworthy, unchanged snapshot must run once
	// through the supporting-file installer, even when upstream is unchanged.
	// Older origins without InstalledHash cannot distinguish local edits, so they
	// retain the warning rather than risk overwriting the installed document.
	needsSupportingInstall := false
	if isSkill && !origin.SupportingFilesInstalled && origin.InstalledHash != "" {
		currentHash, hashErr := ContentHashFile(filepath.Join(skillDir, "SKILL.md"))
		needsSupportingInstall = hashErr == nil && currentHash == origin.InstalledHash
	}
	// force re-installs from upstream even when the commit is unchanged, so a
	// caller can discard local edits and restore the tracked version (reset).
	if !changed && !force && !needsSupportingInstall {
		return &ImportResult{
			Warnings: []string{fmt.Sprintf("%s is already up to date", skillName)},
		}, nil
	}

	if dryRun {
		if needsSupportingInstall && !changed {
			return &ImportResult{
				Warnings: []string{fmt.Sprintf("%s needs a supporting-file reinstall", skillName)},
			}, nil
		}
		return &ImportResult{
			Warnings: []string{fmt.Sprintf("%s: update available (%s → %s)", skillName, ShortSHA(origin.CommitSHA), ShortSHA(newSHA))},
		}, nil
	}

	if changed {
		imp.logger.Info("update available", "skill", skillName, "current", ShortSHA(origin.CommitSHA), "latest", ShortSHA(newSHA))
	} else {
		imp.logger.Info("reinstalling legacy skill package", "skill", skillName, "commit", ShortSHA(origin.CommitSHA))
	}

	// Store old fingerprint for comparison
	oldFingerprint := origin.Fingerprint

	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return nil, err
	}
	result, err := imp.Import(ctx, ImportOptions{
		Repo:          origin.Repo,
		Ref:           origin.Ref,
		Path:          origin.Path,
		SourceName:    gitLockSourceName(lf, origin, skillName, isSkill),
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

	if origin.IsLocal() {
		return fmt.Errorf("local sources have no refs to pin")
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

// authFromOrigin builds an AuthConfig from a stored Origin.
func (imp *Importer) authFromOrigin(origin *Origin) (AuthConfig, error) {
	return ResolveStoredAuth(origin.StoredAuth(), imp.credentialResolver)
}

// ResolveStoredAuth rebuilds an AuthConfig from a stored record.
// A CredentialRef wins and is resolved to a token. Otherwise an ssh-key
// path is rebuilt and the passphrase is re-read from the environment.
// Anything else is ambient (the zero value). The passphrase is never
// taken from the record, because it is never stored.
func ResolveStoredAuth(stored StoredAuth, resolver CredentialResolver) (AuthConfig, error) {
	if stored.CredentialRef != "" {
		if resolver == nil {
			return AuthConfig{}, fmt.Errorf("%w: credential %q requires a resolver; vault not available", gitpkg.ErrAuthFailed, stored.CredentialRef)
		}
		token, err := resolver(stored.CredentialRef)
		if err != nil {
			return AuthConfig{}, fmt.Errorf("%w: resolving %q: %w", gitpkg.ErrAuthFailed, stored.CredentialRef, err)
		}
		if token == "" {
			return AuthConfig{}, fmt.Errorf("%w: %q resolved to empty value", gitpkg.ErrEmptyToken, stored.CredentialRef)
		}
		return AuthConfig{
			Method:        "token",
			Token:         token,
			CredentialRef: stored.CredentialRef,
		}, nil
	}
	if stored.Method == "ssh-key" && stored.SSHKeyPath != "" {
		return AuthConfig{
			Method:        "ssh-key",
			SSHUser:       stored.SSHUser,
			SSHKeyPath:    stored.SSHKeyPath,
			SSHPassphrase: os.Getenv("GRIDCTL_SSH_KEY_PASSPHRASE"),
		}, nil
	}
	return AuthConfig{}, nil
}
