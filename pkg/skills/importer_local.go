package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gridctl/gridctl/pkg/registry"
)

func (imp *Importer) updateLocal(ctx context.Context, name, dir string, origin *Origin, isSkill, dryRun, force, trust bool) (*ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return nil, err
	}
	owners := skillOwners(lf, name)
	if !isSkill {
		owners = agentOwners(lf, name)
	}
	if len(owners) == 0 {
		return nil, fmt.Errorf("skill %q has no lock source", name)
	}
	if len(owners) != 1 {
		return nil, fmt.Errorf("%w for %q: %v", ErrAmbiguousOwner, name, owners)
	}
	src, ok := lf.Sources[owners[0]]
	if !ok || !src.IsLocal() {
		return nil, fmt.Errorf("lock source %q is not a local source", owners[0])
	}
	if src.Repo != origin.Repo {
		return nil, fmt.Errorf("lock source %q records %s, origin records %s", owners[0], src.Repo, origin.Repo)
	}
	entryPath := filepath.Join(origin.Repo, filepath.FromSlash(origin.Path))
	if isSkill {
		entryPath = filepath.Join(entryPath, "SKILL.md")
	}
	if _, err := os.Stat(origin.Repo); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source path %s no longer exists; remove the source or re-add it", origin.Repo)
		}
		return nil, err
	}
	if _, err := os.Stat(entryPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source path %s no longer exists; remove the source or re-add it", entryPath)
		}
		return nil, err
	}
	inside, err := InsideGridctlHome(origin.Repo)
	if err != nil {
		return nil, err
	}
	if inside {
		return nil, fmt.Errorf("source %s now resolves into the gridctl home (projected by gridctl); nothing to update", origin.Repo)
	}
	disc, err := DiscoverLocal(ctx, origin.Repo, origin.Path, imp.logger)
	if err != nil {
		return nil, err
	}
	changed, err := localEntryChanged(ctx, src, name, isSkill)
	if err != nil {
		return nil, err
	}
	if !changed && !force {
		return &ImportResult{Warnings: []string{fmt.Sprintf("%s is already up to date", name)}}, nil
	}
	if dryRun {
		return &ImportResult{Warnings: []string{fmt.Sprintf("%s: update available (local content changed)", name)}}, nil
	}
	opts := ImportOptions{
		Repo:          origin.Repo,
		Kind:          SourceKindLocal,
		SourceName:    owners[0],
		Path:          origin.Path,
		Trust:         trust,
		Force:         true,
		PreserveState: true,
		Discovered:    disc.Result,
	}
	if isSkill {
		opts.ResourceKind = ResourceKindSkill
		opts.Selected = []string{name}
	} else {
		opts.ResourceKind = ResourceKindAgent
		opts.SelectedAgents = []string{name}
	}
	return imp.Import(ctx, opts)
}

func (imp *Importer) diffLocal(ctx context.Context, skillName string, origin *Origin, localPath string) (*DiffResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	localBytes, err := os.ReadFile(localPath) // #nosec G304 -- registry skill path
	if err != nil {
		return nil, fmt.Errorf("reading local SKILL.md: %w", err)
	}
	upstreamPath := filepath.Join(origin.Repo, filepath.FromSlash(origin.Path), "SKILL.md")
	data, err := os.ReadFile(upstreamPath) // #nosec G304 -- recorded local source path
	if err != nil {
		return nil, fmt.Errorf("reading source SKILL.md: %w", err)
	}
	upstreamSkill, err := registry.ParseSkillMD(data)
	if err != nil {
		return nil, fmt.Errorf("upstream SKILL.md failed to parse: %w", err)
	}
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

func localEntryChanged(ctx context.Context, src LockedSource, name string, isSkill bool) (bool, error) {
	if isSkill {
		sk, ok := src.Skills[name]
		if !ok {
			return true, nil
		}
		dir := filepath.Join(src.Repo, filepath.FromSlash(sk.Path))
		hash, err := SkillTreeHash(ctx, dir)
		if err != nil {
			return true, nil
		}
		return hash != sk.TreeHash, nil
	}
	ag, ok := src.Agents[name]
	if !ok {
		return true, nil
	}
	path := filepath.Join(src.Repo, filepath.FromSlash(ag.Path))
	data, err := os.ReadFile(path) // #nosec G304 -- path is under the recorded local root
	if err != nil {
		return true, nil
	}
	return contentHash(data) != ag.ContentHash, nil
}
