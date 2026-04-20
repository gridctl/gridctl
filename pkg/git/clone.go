// Package git contains shared git helpers used by both the skills importer
// (pkg/skills) and the MCP server source builder (pkg/builder).
//
// The helpers are thin wrappers over go-git that factor out duplicated
// clone/fetch/checkout logic. They do not know anything about gridctl's
// cache layout or authentication strategy: callers compute destination
// paths and pass in a transport.AuthMethod (or nil).
package git

import (
	"fmt"
	"log/slog"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// CloneOptions configures a Clone call.
type CloneOptions struct {
	URL     string
	Ref     string               // if set and branch-style, attempted as single-branch clone first
	Depth   int                  // 0 = full history
	AllTags bool                 // fetch all tags
	Auth    transport.AuthMethod // nil = unauthenticated
}

// FetchOptions configures a Fetch call.
type FetchOptions struct {
	AllTags bool
	Auth    transport.AuthMethod
}

// Clone performs a plain git clone into destPath. When Ref is set it first
// attempts a single-branch clone of that ref (as a branch); if that fails it
// removes destPath and retries with a full clone. Callers are responsible for
// a subsequent Checkout when they need to land on a non-branch ref (tag,
// commit, remote branch) after the full-clone fallback.
func Clone(destPath string, opts CloneOptions, logger *slog.Logger) (*gogit.Repository, error) {
	logger.Info("cloning repository", "url", opts.URL)

	cloneOpts := &gogit.CloneOptions{
		URL:   opts.URL,
		Depth: opts.Depth,
		Auth:  opts.Auth,
	}
	if opts.AllTags {
		cloneOpts.Tags = gogit.AllTags
	}
	if opts.Ref != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(opts.Ref)
		cloneOpts.SingleBranch = true
	}

	repo, err := gogit.PlainClone(destPath, false, cloneOpts)
	if err != nil && opts.Ref != "" {
		// Ref may not be a branch; retry with a full clone.
		_ = os.RemoveAll(destPath)
		cloneOpts.SingleBranch = false
		cloneOpts.ReferenceName = ""
		repo, err = gogit.PlainClone(destPath, false, cloneOpts)
	}
	return repo, err
}

// Fetch updates the cached repository at repoPath from its remote.
// Returns nil if the remote had no new refs.
func Fetch(repoPath string, opts FetchOptions, logger *slog.Logger) error {
	r, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("opening repository: %w", err)
	}
	fetchOpts := &gogit.FetchOptions{
		Force: true,
		Auth:  opts.Auth,
	}
	if opts.AllTags {
		fetchOpts.Tags = gogit.AllTags
	}
	if err := r.Fetch(fetchOpts); err != nil && err != gogit.NoErrAlreadyUpToDate {
		return err
	}
	return nil
}

// Checkout lands the worktree on ref, trying in order: tag, local branch,
// remote branch (origin), commit hash. Force is used so uncommitted changes
// in the worktree (unlikely in a cache) are discarded.
func Checkout(repo *gogit.Repository, ref string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewTagReferenceName(ref),
		Force:  true,
	}); err == nil {
		return nil
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(ref),
		Force:  true,
	}); err == nil {
		return nil
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewRemoteReferenceName("origin", ref),
		Force:  true,
	}); err == nil {
		return nil
	}

	hash := plumbing.NewHash(ref)
	if !hash.IsZero() {
		if err := wt.Checkout(&gogit.CheckoutOptions{
			Hash:  hash,
			Force: true,
		}); err == nil {
			return nil
		}
	}

	return fmt.Errorf("unable to checkout ref %q", ref)
}

// ResolveRef returns the commit hash for ref by consulting, in order:
// tag, remote branch (origin), local branch.
func ResolveRef(repo *gogit.Repository, ref string) (string, error) {
	if t, err := repo.Tag(ref); err == nil {
		return t.Hash().String(), nil
	}
	if r, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", ref), true); err == nil {
		return r.Hash().String(), nil
	}
	if b, err := repo.Reference(plumbing.NewBranchReferenceName(ref), true); err == nil {
		return b.Hash().String(), nil
	}
	return "", fmt.Errorf("unable to resolve ref %q", ref)
}

// Open is a thin wrapper around gogit.PlainOpen. It lets callers avoid
// importing go-git directly for routine repository access.
func Open(repoPath string) (*gogit.Repository, error) {
	return gogit.PlainOpen(repoPath)
}

// HeadCommit returns the HEAD commit hash for the repository at repoPath.
func HeadCommit(repoPath string) (string, error) {
	r, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return "", err
	}
	h, err := r.Head()
	if err != nil {
		return "", err
	}
	return h.Hash().String(), nil
}

// ListTags returns every tag name from the repository at repoPath.
func ListTags(repoPath string) ([]string, error) {
	r, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("opening repository: %w", err)
	}
	it, err := r.Tags()
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	var tags []string
	if err := it.ForEach(func(ref *plumbing.Reference) error {
		tags = append(tags, ref.Name().Short())
		return nil
	}); err != nil {
		return nil, fmt.Errorf("iterating tags: %w", err)
	}
	return tags, nil
}
