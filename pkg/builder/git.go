package builder

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// CloneOrUpdate clones a git repository or updates it if it already exists.
// Returns the path to the cloned repository.
func CloneOrUpdate(url, ref string, logger *slog.Logger) (string, error) {
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
		return updateRepo(repoPath, ref, logger)
	}

	// Clone the repository
	return cloneRepo(url, ref, repoPath, logger)
}

func cloneRepo(url, ref, destPath string, logger *slog.Logger) (string, error) {
	logger.Info("cloning repository", "url", url)

	cloneOpts := &git.CloneOptions{
		URL:      url,
		Progress: nil, // Could add os.Stdout for progress
	}

	// If ref is specified and looks like a branch, set it
	if ref != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		cloneOpts.SingleBranch = true
	}

	repo, err := git.PlainClone(destPath, false, cloneOpts)
	if err != nil {
		// If branch clone failed, try without SingleBranch
		if ref != "" {
			os.RemoveAll(destPath)
			cloneOpts.SingleBranch = false
			cloneOpts.ReferenceName = ""
			repo, err = git.PlainClone(destPath, false, cloneOpts)
			if err != nil {
				return "", fmt.Errorf("cloning repository: %w", err)
			}
			// Checkout the specific ref
			if err := checkoutRef(repo, ref); err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("cloning repository: %w", err)
		}
	}

	// Get current commit for logging
	head, err := repo.Head()
	if err == nil {
		logger.Info("cloned repository", "commit", head.Hash().String()[:8])
	}

	return destPath, nil
}

func updateRepo(repoPath, ref string, logger *slog.Logger) (string, error) {
	logger.Info("updating cached repository")

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		// If we can't open, remove and re-clone
		os.RemoveAll(repoPath)
		return "", fmt.Errorf("opening repository (will need to re-clone): %w", err)
	}

	// Get the worktree
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("getting worktree: %w", err)
	}

	// Fetch updates
	err = repo.Fetch(&git.FetchOptions{
		Force: true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		// Non-fatal, continue with what we have
		logger.Warn("fetch failed, using existing", "error", err)
	}

	// Checkout the ref
	if ref != "" {
		if err := checkoutRef(repo, ref); err != nil {
			return "", err
		}
	}

	// Pull latest
	err = wt.Pull(&git.PullOptions{
		Force: true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		// Non-fatal for detached HEAD
		if err != git.ErrNonFastForwardUpdate {
			logger.Warn("pull failed, using existing", "error", err)
		}
	}

	// Get current commit for logging
	head, err := repo.Head()
	if err == nil {
		logger.Info("repository at commit", "commit", head.Hash().String()[:8])
	}

	return repoPath, nil
}

func checkoutRef(repo *git.Repository, ref string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	// Try as branch first
	branchRef := plumbing.NewBranchReferenceName(ref)
	err = wt.Checkout(&git.CheckoutOptions{
		Branch: branchRef,
		Force:  true,
	})
	if err == nil {
		return nil
	}

	// Try as remote branch
	remoteBranchRef := plumbing.NewRemoteReferenceName("origin", ref)
	err = wt.Checkout(&git.CheckoutOptions{
		Branch: remoteBranchRef,
		Force:  true,
	})
	if err == nil {
		return nil
	}

	// Try as tag
	tagRef := plumbing.NewTagReferenceName(ref)
	err = wt.Checkout(&git.CheckoutOptions{
		Branch: tagRef,
		Force:  true,
	})
	if err == nil {
		return nil
	}

	// Try as commit hash
	hash := plumbing.NewHash(ref)
	if hash.IsZero() {
		return fmt.Errorf("invalid ref: %s", ref)
	}

	err = wt.Checkout(&git.CheckoutOptions{
		Hash:  hash,
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("checking out ref %s: %w", ref, err)
	}

	return nil
}
