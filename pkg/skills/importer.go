package skills

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	Auth       AuthConfig
}

// ImportResult contains the results of an import operation.
type ImportResult struct {
	Imported []ImportedSkill `json:"imported"`
	Skipped  []SkippedSkill  `json:"skipped"`
	Warnings []string        `json:"warnings"`
}

// ImportedSkill records a successfully imported skill.
type ImportedSkill struct {
	Name     string            `json:"name"`
	Path     string            `json:"path"`
	Origin   *Origin           `json:"origin,omitempty"`
	Findings []SecurityFinding `json:"findings,omitempty"`
}

// SkippedSkill records a skill that was skipped during import.
type SkippedSkill struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Importer orchestrates the skill import process.
type Importer struct {
	store              *registry.Store
	registryDir        string
	lockPath           string
	logger             *slog.Logger
	credentialResolver CredentialResolver
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

// Import clones a repo, discovers skills, validates, scans, and imports.
func (imp *Importer) Import(opts ImportOptions) (*ImportResult, error) {
	if opts.Path != "" {
		if err := SafeRepoPath(opts.Path); err != nil {
			return nil, err
		}
	}

	imp.logger.Info("importing skills", "repo", gitpkg.RedactURL(opts.Repo), "ref", opts.Ref)

	result, err := CloneAndDiscover(opts.Repo, opts.Ref, opts.Path, opts.Auth, imp.logger)
	if err != nil {
		return nil, err
	}

	if len(result.Skills) == 0 {
		return nil, fmt.Errorf("no SKILL.md files found in repository")
	}

	imp.logger.Info("discovered skills", "count", len(result.Skills))

	importResult := &ImportResult{}
	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return nil, fmt.Errorf("reading lock file: %w", err)
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

		// Security scan
		scanResult := ScanSkill(discovered.Skill)
		if !scanResult.Safe && !opts.Trust {
			importResult.Skipped = append(importResult.Skipped, SkippedSkill{
				Name:   skillName,
				Reason: fmt.Sprintf("security findings detected (use --trust to proceed):\n%s", FormatFindings(scanResult.Findings)),
			})
			continue
		}

		// Set state
		discovered.Skill.Name = skillName
		if opts.NoActivate {
			discovered.Skill.State = registry.StateDraft
		} else {
			discovered.Skill.State = registry.StateActive
		}

		// Save to registry
		if err := imp.store.SaveSkill(discovered.Skill); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to save %s: %v", skillName, err))
			continue
		}

		// Compute fingerprint
		fp := ComputeFingerprint(discovered.Skill)

		// Write origin sidecar. CredentialRef (if any) is persisted as an
		// opaque reference string — the raw token is never written to disk.
		origin := &Origin{
			Repo:          opts.Repo,
			Ref:           opts.Ref,
			Path:          discovered.Path,
			CommitSHA:     result.CommitSHA,
			ImportedAt:    time.Now().UTC(),
			ContentHash:   discovered.ContentHash,
			Fingerprint:   fp,
			CredentialRef: opts.Auth.CredentialRef,
		}

		skillDir := imp.skillDir(skillName)
		if err := WriteOrigin(skillDir, origin); err != nil {
			importResult.Warnings = append(importResult.Warnings, fmt.Sprintf("failed to write origin for %s: %v", skillName, err))
		}

		lockedSkills[skillName] = LockedSkill{
			Path:        discovered.Path,
			ContentHash: discovered.ContentHash,
			Fingerprint: fp,
		}

		imported := ImportedSkill{
			Name:   skillName,
			Path:   discovered.Path,
			Origin: origin,
		}
		if !scanResult.Safe {
			imported.Findings = scanResult.Findings
		}
		importResult.Imported = append(importResult.Imported, imported)

		imp.logger.Info("imported skill", "name", skillName)
	}

	// Update lock file
	if len(lockedSkills) > 0 {
		sourceName := repoToName(opts.Repo)
		lf.SetSource(sourceName, LockedSource{
			Repo:          opts.Repo,
			Ref:           opts.Ref,
			CommitSHA:     result.CommitSHA,
			FetchedAt:     time.Now().UTC(),
			ContentHash:   result.CommitSHA,
			Skills:        lockedSkills,
			CredentialRef: opts.Auth.CredentialRef,
		})

		if err := WriteLockFile(imp.lockPath, lf); err != nil {
			return importResult, fmt.Errorf("writing lock file: %w", err)
		}
	}

	return importResult, nil
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

	// Update lock file
	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return fmt.Errorf("reading lock file: %w", err)
	}
	lf.RemoveSkill(skillName)
	if err := WriteLockFile(imp.lockPath, lf); err != nil {
		return fmt.Errorf("writing lock file: %w", err)
	}

	return nil
}

// Update fetches latest for a skill and applies changes.
func (imp *Importer) Update(skillName string, dryRun, force bool) (*ImportResult, error) {
	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		return nil, fmt.Errorf("skill %q has no origin (not an imported skill): %w", skillName, err)
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

	if !changed {
		return &ImportResult{
			Warnings: []string{fmt.Sprintf("%s is already up to date", skillName)},
		}, nil
	}

	imp.logger.Info("update available", "skill", skillName, "current", origin.CommitSHA[:8], "latest", newSHA[:8])

	if dryRun {
		return &ImportResult{
			Warnings: []string{fmt.Sprintf("%s: update available (%s → %s)", skillName, origin.CommitSHA[:8], newSHA[:8])},
		}, nil
	}

	// Store old fingerprint for comparison
	oldFingerprint := origin.Fingerprint

	result, err := imp.Import(ImportOptions{
		Repo:  origin.Repo,
		Ref:   origin.Ref,
		Path:  origin.Path,
		Trust: true,
		Force: true,
		Auth:  auth,
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

	// Update lock file
	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return fmt.Errorf("reading lock file: %w", err)
	}

	srcName, src, found := lf.FindSkillSource(skillName)
	if found {
		src.Ref = ref
		lf.SetSource(srcName, *src)
		_ = WriteLockFile(imp.lockPath, lf)
	}

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
