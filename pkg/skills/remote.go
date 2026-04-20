package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/gridctl/gridctl/pkg/builder"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/registry"
)

// CloneResult contains the result of a clone + discovery operation.
type CloneResult struct {
	RepoPath  string
	CommitSHA string
	Skills    []DiscoveredSkill
}

// DiscoveredSkill represents a SKILL.md found in a cloned repo.
type DiscoveredSkill struct {
	Name        string
	Path        string // Relative path from repo root to SKILL.md directory
	Skill       *registry.AgentSkill
	ContentHash string
}

// skillsAuth returns a transport.AuthMethod built from the GITHUB_TOKEN env
// var, or nil if the env var is not set. The token-backed flow will be
// replaced by a pluggable Auther interface in a follow-up change; keeping it
// as a private helper here preserves the current behavior.
func skillsAuth() transport.AuthMethod {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return &http.BasicAuth{Username: token, Password: ""}
	}
	return nil
}

// CloneAndDiscover clones a repo and discovers all SKILL.md files.
func CloneAndDiscover(repo, ref, subPath string, logger *slog.Logger) (*CloneResult, error) {
	repoPath, err := cloneShallow(repo, ref, logger)
	if err != nil {
		return nil, fmt.Errorf("cloning repository: %w", err)
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

	skills, err := discoverSkills(searchDir, repoPath)
	if err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}

	return &CloneResult{
		RepoPath:  repoPath,
		CommitSHA: commitSHA,
		Skills:    skills,
	}, nil
}

// FetchAndCompare fetches the latest from a remote and compares with current.
func FetchAndCompare(repo, ref string, currentSHA string, logger *slog.Logger) (string, bool, error) {
	repoPath, err := builder.URLToPath(repo)
	if err != nil {
		return "", false, fmt.Errorf("getting cache path: %w", err)
	}

	if _, err := os.Stat(repoPath); err != nil {
		// Repo not cached, needs full clone
		return "", true, nil
	}

	if err := gitpkg.Fetch(repoPath, gitpkg.FetchOptions{Auth: skillsAuth()}, logger); err != nil {
		logger.Warn("fetch failed", "error", err)
		return currentSHA, false, nil
	}

	r, err := gitpkg.Open(repoPath)
	if err != nil {
		return currentSHA, false, nil
	}

	if ref != "" {
		newSHA, err := gitpkg.ResolveRef(r, ref)
		if err != nil {
			return currentSHA, false, nil
		}
		return newSHA, newSHA != currentSHA, nil
	}

	head, err := r.Head()
	if err != nil {
		return currentSHA, false, nil
	}
	newSHA := head.Hash().String()
	return newSHA, newSHA != currentSHA, nil
}

// ListRemoteTags returns all tags from a cached repository.
func ListRemoteTags(repoPath string) ([]string, error) {
	return gitpkg.ListTags(repoPath)
}

func cloneShallow(url, ref string, logger *slog.Logger) (string, error) {
	if err := builder.EnsureReposCacheDir(); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	repoPath, err := builder.URLToPath(url)
	if err != nil {
		return "", fmt.Errorf("getting cache path: %w", err)
	}

	// If repo exists, fetch updates instead
	if _, err := os.Stat(repoPath); err == nil {
		return updateExisting(repoPath, ref, logger)
	}

	cloneRef := ref
	if IsSemVerConstraint(ref) {
		// Semver constraints require a full clone so tags are available.
		cloneRef = ""
	}
	r, err := gitpkg.Clone(repoPath, gitpkg.CloneOptions{
		URL:     url,
		Ref:     cloneRef,
		Depth:   1,
		AllTags: true,
		Auth:    skillsAuth(),
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
			if err := gitpkg.Checkout(r, resolvedTag); err != nil {
				return "", err
			}
		} else {
			if err := gitpkg.Checkout(r, ref); err != nil {
				return "", err
			}
		}
	}

	return repoPath, nil
}

func updateExisting(repoPath, ref string, logger *slog.Logger) (string, error) {
	logger.Info("updating cached repository")

	// Fail fast on a corrupt cache (mirrors previous behavior).
	r, err := gitpkg.Open(repoPath)
	if err != nil {
		_ = os.RemoveAll(repoPath)
		return "", fmt.Errorf("opening cached repo (removed): %w", err)
	}

	if err := gitpkg.Fetch(repoPath, gitpkg.FetchOptions{AllTags: true, Auth: skillsAuth()}, logger); err != nil {
		logger.Warn("fetch failed, using cached", "error", err)
	}

	if ref != "" {
		if IsSemVerConstraint(ref) {
			tags, err := gitpkg.ListTags(repoPath)
			if err != nil {
				logger.Warn("failed to list tags, using cached", "error", err)
				return repoPath, nil
			}
			resolvedTag, err := ResolveSemVerConstraint(ref, tags)
			if err != nil {
				logger.Warn("failed to resolve constraint, using cached", "constraint", ref, "error", err)
				return repoPath, nil
			}
			if err := gitpkg.Checkout(r, resolvedTag); err != nil {
				logger.Warn("failed to checkout tag, using cached", "tag", resolvedTag, "error", err)
				return repoPath, nil
			}
		} else {
			if err := gitpkg.Checkout(r, ref); err != nil {
				logger.Warn("failed to checkout ref, using cached", "ref", ref, "error", err)
			}
		}
	}

	return repoPath, nil
}

func discoverSkills(searchDir, repoRoot string) ([]DiscoveredSkill, error) {
	var skills []DiscoveredSkill

	err := filepath.WalkDir(searchDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		sk, err := registry.ParseSkillMD(data)
		if err != nil {
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

	return skills, err
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
