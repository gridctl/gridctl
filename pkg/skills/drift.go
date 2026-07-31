package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/gridctl/gridctl/pkg/registry"
)

// DetectDrift returns the names of imported skills in sourceName whose on-disk
// SKILL.md has been edited since the last import or sync. Drift is detected by
// comparing the current file hash against the InstalledHash snapshot written
// when the skill was last installed.
//
// Skills imported before InstalledHash was tracked (empty value) are treated
// as not drifted — DetectDrift fails open rather than reporting noise.
// Skills with no Origin (purely local) are not considered.
//
// Pass an empty sourceName to scan every imported skill in the registry.
func DetectDrift(ctx context.Context, store *registry.Store, lockPath, sourceName string) ([]string, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}

	skillNames, err := skillNamesForSource(store, lockPath, sourceName)
	if err != nil {
		return nil, err
	}

	registryDir := store.Dir()
	var drifted []string

	for _, name := range skillNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sk, err := store.GetSkill(name)
		if err != nil {
			continue
		}
		dirName := sk.Dir
		if dirName == "" {
			dirName = sk.Name
		}
		skillDir := filepath.Join(registryDir, "skills", dirName)

		origin, err := ReadOrigin(skillDir)
		if err != nil || origin.InstalledHash == "" {
			// Local skill, or imported before InstalledHash existed — fail open.
			continue
		}

		currentHash, err := ContentHashFile(filepath.Join(skillDir, "SKILL.md"))
		if err != nil {
			continue
		}

		if currentHash != origin.InstalledHash {
			drifted = append(drifted, name)
		}
	}

	sort.Strings(drifted)
	return drifted, nil
}

// DetectAgentDrift returns the names of imported agents whose on-disk
// AGENT.md has been edited since the last import. Same fail-open policy
// as DetectDrift: a missing origin or InstalledHash is not drift.
func DetectAgentDrift(ctx context.Context, registryDir string) ([]string, error) {
	agents, err := ListAgents(registryDir)
	if err != nil {
		return nil, err
	}
	var drifted []string
	for _, a := range agents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		origin, err := ReadOrigin(a.Dir)
		if err != nil || origin.InstalledHash == "" {
			continue
		}
		currentHash, err := ContentHashFile(filepath.Join(a.Dir, "AGENT.md"))
		if err != nil {
			continue
		}
		if currentHash != origin.InstalledHash {
			drifted = append(drifted, a.Name)
		}
	}
	sort.Strings(drifted)
	return drifted, nil
}

// skillNamesForSource returns the skill names to scan. When sourceName is
// empty, every skill in the store is returned; otherwise only the skills
// recorded under that source in the lock file.
func skillNamesForSource(store *registry.Store, lockPath, sourceName string) ([]string, error) {
	if sourceName == "" {
		all := store.ListSkills()
		names := make([]string, 0, len(all))
		for _, sk := range all {
			names = append(names, sk.Name)
		}
		return names, nil
	}

	lf, err := ReadLockFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("reading lock file: %w", err)
	}
	src, ok := lf.Sources[sourceName]
	if !ok {
		return nil, fmt.Errorf("source %q not found in lock file", sourceName)
	}
	names := make([]string, 0, len(src.Skills))
	for name := range src.Skills {
		names = append(names, name)
	}
	return names, nil
}
