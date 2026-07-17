package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gridctl/gridctl/pkg/registry"
)

// ShortSHA returns the first 8 characters of a commit SHA, or the whole string
// when it is shorter (including the empty string from an uncached fetch). It
// keeps SHA formatting panic-free for logs, backup names, and messages.
func ShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// DiffResult holds a skill's current on-disk SKILL.md alongside the content an
// update would install, for on-demand comparison. Producing it changes no
// registry state, SHAs, or InstalledHashes.
type DiffResult struct {
	Skill    string `json:"skill"`
	Local    string `json:"local"`    // current on-disk full SKILL.md text
	Upstream string `json:"upstream"` // content an update would install
	Drifted  bool   `json:"drifted"`  // on-disk file diverges from InstalledHash
}

// Diff fetches the latest upstream SKILL.md for an imported skill and returns
// both the current on-disk content and the content an update would install,
// without writing anything to the registry or changing any SHAs/InstalledHash.
// It is on-demand only — the caller pays for one git fetch.
func (imp *Importer) Diff(ctx context.Context, skillName string) (*DiffResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		return nil, fmt.Errorf("skill %q has no origin (not an imported skill): %w", skillName, err)
	}

	auth, err := imp.authFromOrigin(origin)
	if err != nil {
		return nil, err
	}

	localPath := filepath.Join(skillDir, "SKILL.md")
	localBytes, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("reading local SKILL.md: %w", err)
	}

	// Resolve the latest upstream commit and discover at that exact SHA. A
	// cached clone's local branch does not fast-forward on fetch, so checking
	// out by ref name would surface stale content; checking out the resolved
	// commit hash lands the worktree on true upstream. When the repo is not yet
	// cached, FetchAndCompare returns no SHA and a fresh clone at the ref already
	// yields the latest.
	ref := origin.Ref
	if newSHA, _, ferr := FetchAndCompare(origin.Repo, origin.Ref, origin.CommitSHA, auth, imp.logger); ferr == nil && newSHA != "" {
		ref = newSHA
	}

	result, err := CloneAndDiscover(origin.Repo, ref, origin.Path, auth, imp.logger)
	if err != nil {
		return nil, fmt.Errorf("fetching upstream: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// origin.Path points to the skill's own directory, so there is normally
	// exactly one discovered skill; match by name, then fall back to the sole
	// result.
	var discovered *DiscoveredSkill
	for i := range result.Skills {
		if result.Skills[i].Name == skillName {
			discovered = &result.Skills[i]
			break
		}
	}
	if discovered == nil && len(result.Skills) == 1 {
		discovered = &result.Skills[0]
	}
	if discovered == nil {
		if len(result.Malformed) > 0 {
			m := result.Malformed[0]
			return nil, fmt.Errorf("upstream SKILL.md failed to parse: %s: %s", m.Path, m.Err)
		}
		return nil, fmt.Errorf("skill %q not found at upstream path %q", skillName, origin.Path)
	}

	// Render the content an update would install: the same skill with its name
	// preserved and its state carried over (PreserveState semantics, matching
	// Import) so the comparison reflects the actual installed bytes rather than
	// a raw upstream file.
	upstreamSkill := discovered.Skill
	upstreamSkill.Name = skillName
	if existing, err := imp.store.GetSkill(skillName); err == nil && existing.State != "" {
		upstreamSkill.State = existing.State
	}
	upstreamBytes, err := registry.RenderSkillMD(upstreamSkill)
	if err != nil {
		return nil, fmt.Errorf("rendering upstream SKILL.md: %w", err)
	}

	currentHash, _ := ContentHashFile(localPath)
	drifted := origin.InstalledHash != "" && currentHash != origin.InstalledHash

	return &DiffResult{
		Skill:    skillName,
		Local:    string(localBytes),
		Upstream: string(upstreamBytes),
		Drifted:  drifted,
	}, nil
}

// AdvanceTracking records that a skill has been reconciled against upstream
// commit newSHA without changing its on-disk content. It advances only the
// version-tracking metadata — the skill's origin CommitSHA and the lock-file
// source's CommitSHA/ContentHash/FetchedAt — leaving the SKILL.md file and its
// InstalledHash untouched.
//
// Used when a sync skips a locally-edited (drifted) skill: the reviewed
// upstream version is recorded so it no longer surfaces as an available
// update, while the user's local edits (and the drift signal that DetectDrift
// derives from InstalledHash) are preserved.
func (imp *Importer) AdvanceTracking(ctx context.Context, skillName, newSHA string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if newSHA == "" {
		return fmt.Errorf("newSHA is required")
	}

	skillDir := imp.skillDir(skillName)
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		return fmt.Errorf("skill %q has no origin: %w", skillName, err)
	}
	origin.CommitSHA = newSHA
	if err := WriteOrigin(skillDir, origin); err != nil {
		return fmt.Errorf("writing origin: %w", err)
	}

	imp.lockfileMu.Lock()
	defer imp.lockfileMu.Unlock()

	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return fmt.Errorf("reading lock file: %w", err)
	}
	if srcName, src, found := lf.FindSkillSource(skillName); found {
		src.CommitSHA = newSHA
		src.ContentHash = newSHA
		src.FetchedAt = time.Now().UTC()
		lf.SetSource(srcName, *src)
		if err := WriteLockFile(imp.lockPath, lf); err != nil {
			return fmt.Errorf("writing lock file: %w", err)
		}
	}
	return nil
}

// BackupSkillFile copies a skill's current SKILL.md to SKILL.md.pre-<shortSHA>
// next to it before an overwrite, so a forced update of a locally-edited skill
// stays recoverable. It returns the backup file name (relative to the skill
// directory). A missing SKILL.md is a no-op that returns an empty name.
func (imp *Importer) BackupSkillFile(ctx context.Context, skillName, shortSHA string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	skillDir := imp.skillDir(skillName)
	srcPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading SKILL.md: %w", err)
	}

	suffix := shortSHA
	if suffix == "" {
		suffix = "local"
	}
	backupName := "SKILL.md.pre-" + suffix
	if err := os.WriteFile(filepath.Join(skillDir, backupName), data, 0644); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}
	return backupName, nil
}

// Detach makes an imported skill local-only by removing its origin sidecar and
// its lock-file entry. The SKILL.md and the skill itself remain; it simply no
// longer tracks an upstream source and will not be touched by sync.
func (imp *Importer) Detach(ctx context.Context, skillName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	skillDir := imp.skillDir(skillName)
	if err := DeleteOrigin(skillDir); err != nil {
		return fmt.Errorf("removing origin: %w", err)
	}

	imp.lockfileMu.Lock()
	defer imp.lockfileMu.Unlock()

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
