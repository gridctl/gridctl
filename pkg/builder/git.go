package builder

import (
	"fmt"
	"log/slog"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
)

// CloneOrUpdate clones a git repository or updates it if it already exists.
// Returns the path to the cloned repository. A nil auth means unauthenticated.
func CloneOrUpdate(url, ref string, auth transport.AuthMethod, logger *slog.Logger) (string, error) {
	if err := EnsureReposCacheDir(); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	repoPath, err := URLToPath(url)
	if err != nil {
		return "", fmt.Errorf("getting cache path: %w", err)
	}

	// Check if repo already exists
	if _, err := os.Stat(repoPath); err == nil {
		// Repo exists, try to update
		return updateRepo(repoPath, ref, auth, logger)
	}

	// Clone the repository
	return cloneRepo(url, ref, repoPath, auth, logger)
}

func cloneRepo(url, ref, destPath string, auth transport.AuthMethod, logger *slog.Logger) (string, error) {
	repo, err := gitpkg.Clone(destPath, gitpkg.CloneOptions{
		URL:  url,
		Ref:  ref,
		Auth: auth,
	}, logger)
	if err != nil {
		return "", fmt.Errorf("cloning repository: %w", err)
	}

	// Land on ref explicitly so the single-branch fallback path ends in the
	// right worktree state. On the happy path this is a no-op.
	if ref != "" {
		if err := gitpkg.Checkout(repo, ref); err != nil {
			return "", err
		}
	}

	if head, err := repo.Head(); err == nil {
		logger.Info("cloned repository", "commit", head.Hash().String()[:8])
	}

	return destPath, nil
}

func updateRepo(repoPath, ref string, auth transport.AuthMethod, logger *slog.Logger) (string, error) {
	logger.Info("updating cached repository")

	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		_ = os.RemoveAll(repoPath)
		return "", fmt.Errorf("opening repository (will need to re-clone): %w", err)
	}

	if err := gitpkg.Fetch(repoPath, gitpkg.FetchOptions{Auth: auth}, logger); err != nil {
		logger.Warn("fetch failed, using existing", "error", err)
	}

	if ref != "" {
		if err := gitpkg.Checkout(repo, ref); err != nil {
			return "", err
		}
	}

	// Pull latest (best-effort; a detached HEAD or non-fast-forward is non-fatal)
	if wt, wtErr := repo.Worktree(); wtErr == nil {
		err := wt.Pull(&gogit.PullOptions{Force: true})
		if err != nil && err != gogit.NoErrAlreadyUpToDate && err != gogit.ErrNonFastForwardUpdate {
			logger.Warn("pull failed, using existing", "error", err)
		}
	}

	if head, err := repo.Head(); err == nil {
		logger.Info("repository at commit", "commit", head.Hash().String()[:8])
	}

	return repoPath, nil
}
