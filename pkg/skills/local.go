package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/state"
)

// ErrInsideGridctlHome is returned when a local source resolves inside the
// gridctl home. The home holds the registry, pack checkouts, the clone
// cache, and projection backups.
var ErrInsideGridctlHome = fmt.Errorf("refusing to import from inside the gridctl home")

// LocalDiscovery is the result of walking a local directory for skills and
// agents. Result.RepoPath is the resolved root. CommitSHA is empty.
type LocalDiscovery struct {
	Result   *CloneResult
	Warnings []string
}

// DiscoverLocal walks a resolved directory root for SKILL.md trees and
// agents/*.md files. subpath, when set, narrows the walk to that relative
// entry; the recorded paths stay relative to root. The walkers do not follow
// symlinks. Managed copies (a skill directory carrying .origin.json) are
// removed with a warning. Skill names are rewritten to the directory name.
func DiscoverLocal(ctx context.Context, root, subpath string, logger *slog.Logger) (*LocalDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving local root: %w", err)
	}
	if inside, ierr := InsideGridctlHome(root); ierr != nil {
		return nil, ierr
	} else if inside {
		return nil, fmt.Errorf("%w: %s", ErrInsideGridctlHome, root)
	}
	searchDir, err := localSearchDir(root, subpath)
	if err != nil {
		return nil, err
	}

	linkWarnings, err := skippedLinkWarnings(ctx, root, searchDir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	skills, malformed, err := discoverSkills(searchDir, root)
	if err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}
	agents, malformedAgents, err := discoverAgents(searchDir, root)
	if err != nil {
		return nil, fmt.Errorf("discovering agents: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var warnings []string
	warnings = append(warnings, linkWarnings...)
	skills, nameWarnings := applyDirectoryNames(root, skills)
	warnings = append(warnings, nameWarnings...)
	skills, filtered := filterManagedSkills(root, skills)
	for _, name := range filtered {
		warnings = append(warnings, fmt.Sprintf("%s: skipping managed copy (.origin.json present; remove the sidecar to import this copy deliberately)", name))
	}
	for _, w := range warnings {
		logger.Warn(w)
	}
	if len(skills) == 0 && len(agents) == 0 {
		if len(filtered) > 0 {
			return nil, fmt.Errorf("no importable skills or agents found in %s (managed copies skipped)", root)
		}
		if len(malformed) > 0 {
			return nil, fmt.Errorf("no importable skills found: %s", summarizeMalformed(malformed))
		}
		if len(malformedAgents) > 0 {
			return nil, fmt.Errorf("no importable agents found: %s", summarizeMalformed(malformedAgents))
		}
		return nil, fmt.Errorf("no importable skills or agents found in %s", root)
	}

	return &LocalDiscovery{
		Result: &CloneResult{
			RepoPath:        root,
			Skills:          skills,
			Malformed:       malformed,
			Agents:          agents,
			MalformedAgents: malformedAgents,
			FilteredSkills:  filtered,
		},
		Warnings: warnings,
	}, nil
}

// localSearchDir resolves an optional relative subpath and refuses escapes,
// including a symlink that leaves the root or enters the gridctl home.
func localSearchDir(root, subpath string) (string, error) {
	if subpath == "" || subpath == "." {
		return root, nil
	}
	if err := SafeRepoPath(subpath); err != nil {
		return "", err
	}
	joined := filepath.Join(root, filepath.FromSlash(subpath))
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", subpath, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if !withinDir(root, resolved) {
		return "", fmt.Errorf("path %q escapes the source root", subpath)
	}
	if inside, ierr := InsideGridctlHome(resolved); ierr != nil {
		return "", ierr
	} else if inside {
		return "", fmt.Errorf("%w: %s", ErrInsideGridctlHome, resolved)
	}
	return joined, nil
}

func skippedLinkWarnings(ctx context.Context, root, searchDir string) ([]string, error) {
	var warnings []string
	err := filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		warnings = append(warnings, fmt.Sprintf("skipping symlink %s", filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

func applyDirectoryNames(root string, skills []DiscoveredSkill) ([]DiscoveredSkill, []string) {
	var warnings []string
	for i := range skills {
		dirName := filepath.Base(skills[i].Path)
		if skills[i].Path == "" || skills[i].Path == "." {
			dirName = filepath.Base(root)
		}
		if skills[i].Name != dirName {
			warnings = append(warnings, fmt.Sprintf("name mismatch: frontmatter %q, directory %q; installed as %q", skills[i].Name, dirName, dirName))
			skills[i].Name = dirName
			if skills[i].Skill != nil {
				skills[i].Skill.Name = dirName
			}
		}
	}
	return skills, warnings
}

func filterManagedSkills(root string, skills []DiscoveredSkill) ([]DiscoveredSkill, []string) {
	kept := make([]DiscoveredSkill, 0, len(skills))
	var filtered []string
	for _, sk := range skills {
		dir := filepath.Join(root, filepath.FromSlash(sk.Path))
		if HasOrigin(dir) {
			filtered = append(filtered, sk.Path)
			continue
		}
		kept = append(kept, sk)
	}
	return kept, filtered
}

// InsideGridctlHome reports whether path resolves to state.BaseDir or a
// path beneath it. A missing gridctl home is not an error.
func InsideGridctlHome(path string) (bool, error) {
	base, err := state.BaseDir()
	if err != nil {
		return false, err
	}
	resolved, err := resolveExisting(path)
	if err != nil {
		return false, err
	}
	baseResolved, err := resolveExisting(base)
	if err != nil {
		return false, err
	}
	return withinDir(baseResolved, resolved), nil
}

func resolveExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	return filepath.Abs(resolved)
}

// ResolveLocalRoot returns the absolute, symlink-resolved directory.
func ResolveLocalRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", resolved)
	}
	return resolved, nil
}

// SkillTreeHash returns a sha256:-prefixed hash of SKILL.md and allowlisted
// supporting files, sorted by relative path. Modification times are not
// hashed. .origin.json, backups, and install temporary files are outside
// the allowlist and are excluded.
func SkillTreeHash(ctx context.Context, skillDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	skillBytes, err := os.ReadFile(skillPath) // #nosec G304 -- skill dir chosen by the caller
	if err != nil {
		return "", fmt.Errorf("reading SKILL.md: %w", err)
	}
	supporting, _, err := collectSupportingFiles(skillDir)
	if err != nil {
		return "", err
	}
	type fileHash struct {
		rel  string
		data []byte
	}
	files := []fileHash{{rel: "SKILL.md", data: skillBytes}}
	for _, f := range supporting {
		files = append(files, fileHash{rel: f.rel, data: f.content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	h := sha256.New()
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		writeLenDelimited(h, f.rel)
		writeLenDelimitedBytes(h, f.data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// CombineTrackedSourceHash hashes tracked skill tree hashes and agent
// content hashes. Records are domain-separated and length-delimited so a
// skill and an agent cannot alias. Order is kind, then name.
func CombineTrackedSourceHash(skills map[string]LockedSkill, agents map[string]LockedAgent) string {
	type rec struct {
		kind string
		name string
		hash string
	}
	var recs []rec
	for name, sk := range skills {
		recs = append(recs, rec{kind: "skill", name: name, hash: sk.TreeHash})
	}
	for name, ag := range agents {
		recs = append(recs, rec{kind: "agent", name: name, hash: ag.ContentHash})
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].kind != recs[j].kind {
			return recs[i].kind < recs[j].kind
		}
		return recs[i].name < recs[j].name
	})
	h := sha256.New()
	for _, r := range recs {
		writeLenDelimited(h, r.kind)
		writeLenDelimited(h, r.name)
		writeLenDelimited(h, r.hash)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func writeLenDelimited(h interface{ Write([]byte) (int, error) }, s string) {
	writeLenDelimitedBytes(h, []byte(s))
}

func writeLenDelimitedBytes(h interface{ Write([]byte) (int, error) }, b []byte) {
	_, _ = h.Write([]byte(strconv.Itoa(len(b))))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(b)
	_, _ = h.Write([]byte{0})
}

// LocalSourceHasUpdate recomputes hashes for tracked entries only. A missing
// entry counts as changed. Unrelated siblings are ignored. A missing root
// or a root that now resolves inside the gridctl home is an error.
func LocalSourceHasUpdate(ctx context.Context, src LockedSource) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !src.IsLocal() {
		return false, fmt.Errorf("source is not local")
	}
	info, err := os.Stat(src.Repo)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("source path %s no longer exists; remove the source or re-add it", src.Repo)
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("source path %s no longer exists; remove the source or re-add it", src.Repo)
	}
	inside, err := InsideGridctlHome(src.Repo)
	if err != nil {
		return false, err
	}
	if inside {
		return false, fmt.Errorf("source %s now resolves into the gridctl home (projected by gridctl); nothing to update", src.Repo)
	}
	names := make([]string, 0, len(src.Skills))
	for name := range src.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		sk := src.Skills[name]
		dir := filepath.Join(src.Repo, filepath.FromSlash(sk.Path))
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			return true, nil
		}
		hash, err := SkillTreeHash(ctx, dir)
		if err != nil {
			return true, nil
		}
		if hash != sk.TreeHash {
			return true, nil
		}
	}
	agentNames := make([]string, 0, len(src.Agents))
	for name := range src.Agents {
		agentNames = append(agentNames, name)
	}
	sort.Strings(agentNames)
	for _, name := range agentNames {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		ag := src.Agents[name]
		path := filepath.Join(src.Repo, filepath.FromSlash(ag.Path))
		data, err := os.ReadFile(path) // #nosec G304 -- path is under the recorded local root
		if err != nil {
			return true, nil
		}
		if contentHash(data) != ag.ContentHash {
			return true, nil
		}
	}
	return false, nil
}

func prepareLocalImport(ctx context.Context, opts *ImportOptions, logger *slog.Logger) (*CloneResult, []string, error) {
	if opts.Ref != "" {
		return nil, nil, fmt.Errorf("local sources have no refs")
	}
	if localAuthSpecified(opts.Auth) {
		return nil, nil, fmt.Errorf("local sources do not use git authentication")
	}
	root, err := ResolveLocalRoot(opts.Repo)
	if err != nil {
		return nil, nil, err
	}
	inside, err := InsideGridctlHome(root)
	if err != nil {
		return nil, nil, err
	}
	if inside {
		return nil, nil, fmt.Errorf("%w: %s", ErrInsideGridctlHome, root)
	}
	opts.Repo = root
	if opts.Discovered != nil {
		if err := validatePreDiscovered(root, opts.Discovered); err != nil {
			return nil, nil, err
		}
		return opts.Discovered, nil, nil
	}
	disc, err := DiscoverLocal(ctx, root, opts.Path, logger)
	if err != nil {
		return nil, nil, err
	}
	return disc.Result, disc.Warnings, nil
}

func validatePreDiscovered(root string, disc *CloneResult) error {
	if disc == nil {
		return fmt.Errorf("pre-discovered local content is missing")
	}
	resolved, err := filepath.EvalSymlinks(disc.RepoPath)
	if err != nil {
		return fmt.Errorf("resolving pre-discovered root: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if resolved != root {
		return fmt.Errorf("pre-discovered content is not rooted at %s", root)
	}
	if disc.CommitSHA != "" {
		return fmt.Errorf("local discovery must not carry a commit SHA")
	}
	return nil
}

func localAuthSpecified(cfg AuthConfig) bool {
	return (cfg.Method != "" && cfg.Method != "none") || cfg.Token != "" || cfg.CredentialRef != "" || cfg.SSHKeyPath != "" || cfg.SSHPassphrase != ""
}

// expandHome resolves a ~-template against home. Duplicated from skillsync
// so this package does not import it.
func expandHome(home, template string) string {
	if strings.HasPrefix(template, "~") {
		return filepath.Join(home, strings.TrimPrefix(template, "~"))
	}
	return template
}
