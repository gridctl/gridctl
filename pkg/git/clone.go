// Package git contains shared git helpers used by both the skills importer
// (pkg/skills) and the MCP server source builder (pkg/builder).
//
// The helpers are thin wrappers over go-git that factor out duplicated
// clone/fetch/checkout logic. They do not know anything about gridctl's
// cache layout or authentication strategy: callers compute destination
// paths and pass in a transport.AuthMethod (or nil).
package git

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
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
	// AllBranches fetches every remote branch with an explicit refspec,
	// overriding the refspec stored at clone time. A single-branch clone's
	// remote config only covers its own branch, so without this a later
	// fetch cannot see branches (or a default branch) added upstream.
	AllBranches bool
	Auth        transport.AuthMethod
}

// Clone performs a plain git clone into destPath. When Ref is set it first
// attempts a single-branch clone of that ref (as a branch); if that fails it
// removes destPath and retries with a full clone. Callers are responsible for
// a subsequent Checkout when they need to land on a non-branch ref (tag,
// commit, remote branch) after the full-clone fallback.
func Clone(ctx context.Context, destPath string, opts CloneOptions, logger *slog.Logger) (*gogit.Repository, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger.Info("cloning repository", "url", RedactURL(opts.URL))

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

	repo, err := gogit.PlainCloneContext(ctx, destPath, false, cloneOpts)
	if err != nil && opts.Ref != "" {
		// Ref may not be a branch; retry with a full clone.
		_ = os.RemoveAll(destPath)
		cloneOpts.SingleBranch = false
		cloneOpts.ReferenceName = ""
		repo, err = gogit.PlainCloneContext(ctx, destPath, false, cloneOpts)
	}
	return repo, err
}

// Fetch updates the cached repository at repoPath from its remote.
// Returns nil if the remote had no new refs.
func Fetch(ctx context.Context, repoPath string, opts FetchOptions, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if opts.AllBranches {
		fetchOpts.RefSpecs = []config.RefSpec{"+refs/heads/*:refs/remotes/origin/*"}
	}
	if err := r.FetchContext(ctx, fetchOpts); err != nil && err != gogit.NoErrAlreadyUpToDate {
		return err
	}
	return nil
}

// Checkout lands the worktree on ref, trying in order: tag, local branch,
// remote branch (origin), commit hash. Force is used so uncommitted changes
// in the worktree (unlikely in a cache) are discarded.
func Checkout(ctx context.Context, repo *gogit.Repository, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewTagReferenceName(ref),
		Force:  true,
	}); err == nil {
		return ctx.Err()
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(ref),
		Force:  true,
	}); err == nil {
		return ctx.Err()
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewRemoteReferenceName("origin", ref),
		Force:  true,
	}); err == nil {
		return ctx.Err()
	}

	hash := plumbing.NewHash(ref)
	if !hash.IsZero() {
		if err := wt.Checkout(&gogit.CheckoutOptions{
			Hash:  hash,
			Force: true,
		}); err == nil {
			return ctx.Err()
		}
	}

	return fmt.Errorf("unable to checkout ref %q", ref)
}

// DefaultBranch returns the short name of the remote's default branch. It
// consults, in order: the origin/HEAD symbolic ref (when the clone recorded
// one), the local HEAD's branch when a matching remote-tracking ref exists,
// and finally the remote-tracking branches themselves (a sole branch wins;
// otherwise "main" then "master"). It works from a detached HEAD, which is
// the resting state of a cache worktree after SyncWorktree.
func DefaultBranch(repo *gogit.Repository) (string, error) {
	if ref, err := repo.Reference("refs/remotes/origin/HEAD", false); err == nil && ref.Type() == plumbing.SymbolicReference {
		return strings.TrimPrefix(ref.Target().Short(), "origin/"), nil
	}

	if head, err := repo.Reference(plumbing.HEAD, false); err == nil && head.Type() == plumbing.SymbolicReference {
		name := head.Target().Short()
		if _, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", name), true); err == nil {
			return name, nil
		}
	}

	refs, err := repo.References()
	if err != nil {
		return "", fmt.Errorf("listing references: %w", err)
	}
	var branches []string
	_ = refs.ForEach(func(r *plumbing.Reference) error {
		name := string(r.Name())
		if strings.HasPrefix(name, "refs/remotes/origin/") && name != "refs/remotes/origin/HEAD" {
			branches = append(branches, strings.TrimPrefix(name, "refs/remotes/origin/"))
		}
		return nil
	})
	if len(branches) == 1 {
		return branches[0], nil
	}
	for _, candidate := range []string{"main", "master"} {
		for _, b := range branches {
			if b == candidate {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("unable to determine default branch")
}

// ResolveRemoteRef resolves ref to a commit hash, preferring freshly fetched
// remote-tracking state over possibly-stale local refs: tag, remote branch
// (origin), local branch, raw commit hash. An empty ref resolves the remote
// default branch. Annotated tags are peeled to their target commit so the
// result is always checkout-able and comparable against HEAD commits.
func ResolveRemoteRef(repo *gogit.Repository, ref string) (string, error) {
	sha, _, err := resolveRemote(repo, ref)
	return sha, err
}

// resolveRemote is ResolveRemoteRef plus the branch name the resolution went
// through ("" for tags and raw hashes), so SyncWorktree can re-attach HEAD.
func resolveRemote(repo *gogit.Repository, ref string) (sha, branch string, err error) {
	if ref == "" {
		b, err := DefaultBranch(repo)
		if err != nil {
			return "", "", err
		}
		r, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", b), true)
		if err != nil {
			return "", "", fmt.Errorf("resolving origin/%s: %w", b, err)
		}
		return r.Hash().String(), b, nil
	}
	if t, err := repo.Tag(ref); err == nil {
		return peelToCommit(repo, t.Hash()).String(), "", nil
	}
	if r, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", ref), true); err == nil {
		return r.Hash().String(), ref, nil
	}
	if b, err := repo.Reference(plumbing.NewBranchReferenceName(ref), true); err == nil {
		return b.Hash().String(), ref, nil
	}
	if h := plumbing.NewHash(ref); !h.IsZero() && len(ref) == 40 {
		if _, err := repo.CommitObject(h); err == nil {
			return h.String(), "", nil
		}
	}
	return "", "", fmt.Errorf("unable to resolve ref %q", ref)
}

// peelToCommit dereferences an annotated tag object to its target commit;
// lightweight tag and commit hashes pass through unchanged.
func peelToCommit(repo *gogit.Repository, h plumbing.Hash) plumbing.Hash {
	if tag, err := repo.TagObject(h); err == nil {
		return tag.Target
	}
	return h
}

// SyncWorktree lands the worktree on the commit ResolveRemoteRef finds for
// ref and returns its hash. Unlike Checkout, it never resolves through a
// stale local branch: after a Fetch, this is what actually surfaces new
// upstream commits in the worktree. Branch targets fast-forward the local
// branch and keep HEAD attached (the equivalent of `git checkout -B <branch>
// <sha>`) so default-branch resolution keeps working on later syncs; tag and
// raw-hash targets leave HEAD detached. Repeated syncs are idempotent.
func SyncWorktree(ctx context.Context, repo *gogit.Repository, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sha, branch, err := resolveRemote(repo, ref)
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("getting worktree: %w", err)
	}

	if branch != "" {
		branchRef := plumbing.NewBranchReferenceName(branch)
		if err := repo.Storer.SetReference(plumbing.NewHashReference(branchRef, plumbing.NewHash(sha))); err != nil {
			return "", fmt.Errorf("updating branch %s: %w", branch, err)
		}
		if err := wt.Checkout(&gogit.CheckoutOptions{Branch: branchRef, Force: true}); err != nil {
			return "", fmt.Errorf("checking out %s at %s: %w", branch, sha, err)
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return sha, nil
	}

	if err := wt.Checkout(&gogit.CheckoutOptions{
		Hash:  plumbing.NewHash(sha),
		Force: true,
	}); err != nil {
		return "", fmt.Errorf("checking out %s: %w", sha, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return sha, nil
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
