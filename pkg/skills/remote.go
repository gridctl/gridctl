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
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/gridctl/gridctl/pkg/builder"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/registry"
)

// repoLocks serializes fetch/checkout mutations per cached clone. The
// daemon's background update checker, the web UI's sync fan-out, and CLI
// commands can all touch the same cache directory, and go-git makes no
// concurrency guarantees for a shared on-disk repository.
var repoLocks sync.Map // map[string]*sync.Mutex

// lockRepoPath locks the mutex for repoPath and returns its unlock func.
func lockRepoPath(repoPath string) func() {
	m, _ := repoLocks.LoadOrStore(repoPath, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// CloneResult contains the result of a clone + discovery operation.
type CloneResult struct {
	RepoPath  string
	CommitSHA string
	Skills    []DiscoveredSkill
	Malformed []MalformedSkill
	// Agents are agent definitions discovered under the agents/*.md
	// convention; MalformedAgents records files in agents/ directories
	// that are not parseable agent definitions.
	Agents          []DiscoveredAgent
	MalformedAgents []MalformedAgent
}

// MalformedSkill records a SKILL.md that could not be read or parsed (or a
// directory that could not be walked), so callers can surface the failure
// instead of silently dropping it.
type MalformedSkill struct {
	Path string `json:"path"` // Relative path from repo root
	Err  string `json:"error"`
}

// MalformedAgent aliases MalformedSkill so agent call sites read as what
// they are; the shape and JSON encoding are identical.
type MalformedAgent = MalformedSkill

// DiscoveredSkill represents a SKILL.md found in a cloned repo.
type DiscoveredSkill struct {
	Name        string
	Path        string // Relative path from repo root to SKILL.md directory
	Skill       *registry.AgentSkill
	ContentHash string
}

// DiscoveredAgent represents an agent definition found in a cloned repo
// under an agents/ directory.
type DiscoveredAgent struct {
	Name        string
	Path        string // Relative path from repo root to the .md file
	Definition  *AgentDefinition
	ContentHash string
}

// authMethodFor maps an AuthConfig + URL into the concrete
// transport.AuthMethod that go-git uses. Errors surface as-is from the
// underlying Auther (e.g. ErrEmptyToken, ErrProtocolMismatch).
func authMethodFor(cfg AuthConfig, url string) (transport.AuthMethod, error) {
	auther, err := resolveAuther(cfg, url)
	if err != nil {
		return nil, err
	}
	return auther.AuthFor(url)
}

// CloneAndDiscover clones a repo and discovers all SKILL.md files plus
// any agents/*.md definitions it ships.
func CloneAndDiscover(repo, ref, subPath string, auth AuthConfig, logger *slog.Logger) (*CloneResult, error) {
	repoPath, err := cloneShallow(repo, ref, auth, logger)
	if err != nil {
		return nil, fmt.Errorf("cloning repository: %w", gitpkg.RedactError(err))
	}

	commitSHA, err := gitpkg.HeadCommit(repoPath)
	if err != nil {
		return nil, fmt.Errorf("getting HEAD commit: %w", err)
	}

	searchDir := repoPath
	if subPath != "" {
		searchDir = filepath.Join(repoPath, subPath)
		if _, err := os.Stat(searchDir); err != nil {
			return nil, fmt.Errorf("path %q not found in repository: %w", subPath, err)
		}
	}

	skills, malformed, err := discoverSkills(searchDir, repoPath)
	if err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}
	for _, m := range malformed {
		logger.Warn("skipping malformed SKILL.md", "path", m.Path, "error", m.Err)
	}

	agents, malformedAgents, err := discoverAgents(searchDir, repoPath)
	if err != nil {
		return nil, fmt.Errorf("discovering agents: %w", err)
	}
	for _, m := range malformedAgents {
		logger.Warn("skipping malformed agent definition", "path", m.Path, "error", m.Err)
	}

	return &CloneResult{
		RepoPath:        repoPath,
		CommitSHA:       commitSHA,
		Skills:          skills,
		Malformed:       malformed,
		Agents:          agents,
		MalformedAgents: malformedAgents,
	}, nil
}

// FetchAndCompare fetches the latest from a remote and compares with current.
func FetchAndCompare(repo, ref, currentSHA string, auth AuthConfig, logger *slog.Logger) (string, bool, error) {
	repoPath, err := builder.URLToPath(repo)
	if err != nil {
		return "", false, fmt.Errorf("getting cache path: %w", err)
	}

	if _, err := os.Stat(repoPath); err != nil {
		// Repo not cached, needs full clone
		return "", true, nil
	}

	unlock := lockRepoPath(repoPath)
	defer unlock()

	authMethod, err := authMethodFor(auth, repo)
	if err != nil {
		return currentSHA, false, gitpkg.RedactError(err)
	}

	if err := gitpkg.Fetch(context.Background(), repoPath, gitpkg.FetchOptions{AllTags: true, AllBranches: true, Auth: authMethod}, logger); err != nil {
		logger.Warn("fetch failed", "error", gitpkg.RedactError(err))
		return currentSHA, false, nil
	}

	r, err := gitpkg.Open(repoPath)
	if err != nil {
		return currentSHA, false, nil
	}

	// Resolve against remote-tracking state, never the local HEAD: a fetch
	// updates only refs/remotes/origin/*, so local refs (and the worktree)
	// still describe the previous sync and would mask upstream changes.
	target, err := semverTarget(repoPath, ref)
	if err != nil {
		return currentSHA, false, nil
	}
	newSHA, err := gitpkg.ResolveRemoteRef(r, target)
	if err != nil {
		return currentSHA, false, nil
	}
	return newSHA, newSHA != currentSHA, nil
}

// semverTarget resolves a semver-constraint ref to its best matching tag in
// the cached repo; any other ref passes through unchanged.
func semverTarget(repoPath, ref string) (string, error) {
	if !IsSemVerConstraint(ref) {
		return ref, nil
	}
	tags, err := gitpkg.ListTags(repoPath)
	if err != nil {
		return "", fmt.Errorf("listing tags: %w", err)
	}
	return ResolveSemVerConstraint(ref, tags)
}

// ListRemoteTags returns all tags from a cached repository.
func ListRemoteTags(repoPath string) ([]string, error) {
	return gitpkg.ListTags(repoPath)
}

func cloneShallow(url, ref string, auth AuthConfig, logger *slog.Logger) (string, error) {
	if err := builder.EnsureReposCacheDir(); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	repoPath, err := builder.URLToPath(url)
	if err != nil {
		return "", fmt.Errorf("getting cache path: %w", err)
	}

	unlock := lockRepoPath(repoPath)
	defer unlock()

	// If repo exists, fetch updates instead
	if _, err := os.Stat(repoPath); err == nil {
		return updateExisting(repoPath, url, ref, auth, logger)
	}

	return cloneFresh(repoPath, url, ref, auth, logger)
}

// cloneFresh clones url into repoPath and lands the worktree on ref.
// Callers must hold the repoPath lock.
func cloneFresh(repoPath, url, ref string, auth AuthConfig, logger *slog.Logger) (string, error) {
	authMethod, err := authMethodFor(auth, url)
	if err != nil {
		return "", err
	}

	cloneRef := ref
	if IsSemVerConstraint(ref) {
		// Semver constraints require a full clone so tags are available.
		cloneRef = ""
	}
	r, err := gitpkg.Clone(context.Background(), repoPath, gitpkg.CloneOptions{
		URL:     url,
		Ref:     cloneRef,
		Depth:   1,
		AllTags: true,
		Auth:    authMethod,
	}, logger)
	if err != nil {
		return "", fmt.Errorf("cloning: %w", err)
	}

	if ref != "" {
		if IsSemVerConstraint(ref) {
			tags, err := gitpkg.ListTags(repoPath)
			if err != nil {
				return "", err
			}
			resolvedTag, err := ResolveSemVerConstraint(ref, tags)
			if err != nil {
				return "", err
			}
			if err := gitpkg.Checkout(context.Background(), r, resolvedTag); err != nil {
				return "", err
			}
		} else {
			if err := gitpkg.Checkout(context.Background(), r, ref); err != nil {
				return "", err
			}
		}
	}

	return repoPath, nil
}

func updateExisting(repoPath, url, ref string, auth AuthConfig, logger *slog.Logger) (string, error) {
	logger.Info("updating cached repository")

	// Fail fast on a corrupt cache (mirrors previous behavior).
	r, err := gitpkg.Open(repoPath)
	if err != nil {
		_ = os.RemoveAll(repoPath)
		return "", fmt.Errorf("opening cached repo (removed): %w", err)
	}

	authMethod, err := authMethodFor(auth, url)
	if err != nil {
		return "", err
	}

	if err := gitpkg.Fetch(context.Background(), repoPath, gitpkg.FetchOptions{AllTags: true, AllBranches: true, Auth: authMethod}, logger); err != nil {
		// Offline or unreachable remote: serve the cached content rather
		// than failing. The worktree cannot have anything newer to land.
		logger.Warn("fetch failed, using cached", "error", gitpkg.RedactError(err))
		return repoPath, nil
	}

	// Land the worktree on what the fetch brought in. A fetch updates only
	// remote-tracking refs; without this step the worktree (and therefore
	// discovery) stays frozen at the first-import commit forever, for every
	// ref shape including the unpinned default branch.
	target, err := semverTarget(repoPath, ref)
	if err != nil {
		logger.Warn("failed to resolve constraint, using cached", "constraint", ref, "error", err)
		return repoPath, nil
	}
	if _, err := gitpkg.SyncWorktree(context.Background(), r, target); err != nil {
		// go-git can advance remote-tracking refs without transferring the
		// backing objects (observed with local-path remotes), leaving the
		// resolved commit un-checkoutable. The fetch above succeeded, so the
		// remote is reachable: a fresh clone is the reliable recovery and
		// costs one shallow clone only when upstream actually changed.
		logger.Warn("worktree sync failed, re-cloning", "ref", target, "error", gitpkg.RedactError(err))
		if rmErr := os.RemoveAll(repoPath); rmErr != nil {
			return "", fmt.Errorf("removing stale cache for re-clone: %w", rmErr)
		}
		return cloneFresh(repoPath, url, ref, auth, logger)
	}

	return repoPath, nil
}

func discoverSkills(searchDir, repoRoot string) ([]DiscoveredSkill, []MalformedSkill, error) {
	var skills []DiscoveredSkill
	var malformed []MalformedSkill

	recordMalformed := func(path string, cause error) {
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		malformed = append(malformed, MalformedSkill{Path: relPath, Err: cause.Error()})
	}

	err := filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory hides any SKILL.md beneath it;
			// record the failure so it isn't silently skipped.
			recordMalformed(path, err)
			return nil
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			recordMalformed(path, err)
			return nil
		}

		sk, err := registry.ParseSkillMD(data)
		if err != nil {
			recordMalformed(path, err)
			return nil
		}

		skillDir := filepath.Dir(path)
		relPath, _ := filepath.Rel(repoRoot, skillDir)
		dirName := filepath.Base(skillDir)

		if sk.Name == "" {
			sk.Name = dirName
		}

		skills = append(skills, DiscoveredSkill{
			Name:        sk.Name,
			Path:        relPath,
			Skill:       sk,
			ContentHash: contentHash(data),
		})

		return nil
	})

	return skills, malformed, err
}

// discoverAgents walks searchDir for the agents/*.md convention: any
// file directly inside a directory named "agents" (at the repo root or
// any subdirectory root, the Claude Code plugin layout). Non-markdown
// files and files failing the agent frontmatter parse are recorded as
// malformed rather than silently skipped. A SKILL.md inside an agents/
// directory is left to skill discovery.
func discoverAgents(searchDir, repoRoot string) ([]DiscoveredAgent, []MalformedAgent, error) {
	var agents []DiscoveredAgent
	var malformed []MalformedAgent

	recordMalformed := func(path string, cause error) {
		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			relPath = path
		}
		malformed = append(malformed, MalformedAgent{Path: relPath, Err: cause.Error()})
	}

	err := filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is already recorded by the skill
			// walk, which visits the same tree.
			return nil
		}
		if d.IsDir() || filepath.Base(filepath.Dir(path)) != "agents" {
			return nil
		}
		if d.Name() == "SKILL.md" {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			recordMalformed(path, fmt.Errorf("not a markdown file (agents/ entries must be *.md)"))
			return nil
		}

		data, err := os.ReadFile(path) // #nosec G304 -- walking the cloned repo
		if err != nil {
			recordMalformed(path, err)
			return nil
		}
		def, err := ParseAgentMD(data)
		if err != nil {
			recordMalformed(path, err)
			return nil
		}
		if def.Name == "" {
			def.Name = strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		}

		relPath, _ := filepath.Rel(repoRoot, path)
		agents = append(agents, DiscoveredAgent{
			Name:        def.Name,
			Path:        relPath,
			Definition:  def,
			ContentHash: contentHash(data),
		})
		return nil
	})

	return agents, malformed, err
}

func contentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ContentHashFile computes a SHA-256 hash of a file.
func ContentHashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return contentHash(data), nil
}

// SafeRepoPath validates a path component to prevent directory traversal.
func SafeRepoPath(path string) error {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("invalid path: %q", path)
	}
	return nil
}
