package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initWorkRepo creates a non-bare repo with one commit on master and an
// annotated tag v1.0.0, and returns its path. Unlike initBareRepo it stays
// writable, so tests can push follow-up commits into it via commitFile.
func initWorkRepo(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Test repo"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	sig := &object.Signature{Name: "test", Email: "test@test.com"}
	commitHash, err := wt.Commit("initial commit", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	if _, err := repo.CreateTag("v1.0.0", commitHash, &gogit.CreateTagOptions{
		Tagger:  sig,
		Message: "v1.0.0",
	}); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	return workDir
}

// commitFile writes a file into the repo at repoPath and commits it,
// returning the new commit hash.
func commitFile(t *testing.T, repoPath, name, content, msg string) string {
	t.Helper()

	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatalf("git add: %v", err)
	}
	hash, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return hash.String()
}

// makeStaleClone clones srcPath and manufactures the post-fetch stale state
// the bug lives in: the remote-tracking branch points at a fresh commit while
// the local branch (and worktree) remain on the old one. The fresh commit is
// created inside the clone so its objects are present locally, keeping the
// test independent of fetch object-transfer behavior.
func makeStaleClone(t *testing.T, srcPath, destPath string) (repo *gogit.Repository, staleSHA, freshSHA string) {
	t.Helper()

	repo, err := Clone(context.Background(), destPath, CloneOptions{URL: srcPath}, testLogger())
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	staleSHA = head.Hash().String()

	freshSHA = commitFile(t, destPath, "README.md", "# Updated", "second commit")

	// origin/master advances to the fresh commit (what a fetch does)...
	if err := repo.Storer.SetReference(plumbing.NewHashReference(
		plumbing.NewRemoteReferenceName("origin", "master"), plumbing.NewHash(freshSHA))); err != nil {
		t.Fatalf("set origin/master: %v", err)
	}
	// ...while the local branch and worktree stay on the old one (what a
	// fetch does NOT touch).
	if err := repo.Storer.SetReference(plumbing.NewHashReference(
		plumbing.NewBranchReferenceName("master"), plumbing.NewHash(staleSHA))); err != nil {
		t.Fatalf("reset local master: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("master"), Force: true,
	}); err != nil {
		t.Fatalf("checkout stale master: %v", err)
	}
	return repo, staleSHA, freshSHA
}

// TestSyncWorktree_AdvancesStaleLocalBranch is the pkg/git regression test
// for the stale-clone bug: checking out by branch name lands on the stale
// local branch, while SyncWorktree must land on the remote-tracking tip.
func TestSyncWorktree_AdvancesStaleLocalBranch(t *testing.T) {
	srcPath := initWorkRepo(t)
	destPath := filepath.Join(t.TempDir(), "clone")
	repo, staleSHA, freshSHA := makeStaleClone(t, srcPath, destPath)

	gotSHA, err := SyncWorktree(context.Background(), repo, "master")
	if err != nil {
		t.Fatalf("sync worktree: %v", err)
	}
	if gotSHA != freshSHA {
		t.Errorf("synced to %s, want %s (stale was %s)", gotSHA, freshSHA, staleSHA)
	}

	data, err := os.ReadFile(filepath.Join(destPath, "README.md"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "Updated") {
		t.Errorf("worktree not advanced, README.md = %q", string(data))
	}
}

// TestSyncWorktree_DefaultBranchIdempotent covers the unpinned (ref == "")
// case, including a second sync from the detached-HEAD state the first one
// leaves behind.
func TestSyncWorktree_DefaultBranchIdempotent(t *testing.T) {
	srcPath := initWorkRepo(t)
	destPath := filepath.Join(t.TempDir(), "clone")
	repo, _, freshSHA := makeStaleClone(t, srcPath, destPath)

	first, err := SyncWorktree(context.Background(), repo, "")
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first != freshSHA {
		t.Errorf("first sync landed on %s, want %s", first, freshSHA)
	}

	// A branch sync re-attaches HEAD; a repeat sync must resolve the default
	// branch again and be a no-op.
	second, err := SyncWorktree(context.Background(), repo, "")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second != first {
		t.Errorf("second sync landed on %s, want %s", second, first)
	}

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Name().Short() != "master" {
		t.Errorf("HEAD = %s, want re-attached to master", head.Name())
	}
}

// TestDefaultBranch_DetachedHead confirms default-branch resolution survives
// a genuinely detached HEAD (the resting state after a tag or raw-hash sync)
// by falling back to remote-tracking branch enumeration.
func TestDefaultBranch_DetachedHead(t *testing.T) {
	srcPath := initWorkRepo(t)
	destPath := filepath.Join(t.TempDir(), "clone")

	repo, err := Clone(context.Background(), destPath, CloneOptions{URL: srcPath}, testLogger())
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Hash: head.Hash(), Force: true}); err != nil {
		t.Fatalf("detach: %v", err)
	}

	branch, err := DefaultBranch(repo)
	if err != nil {
		t.Fatalf("default branch: %v", err)
	}
	if branch != "master" {
		t.Errorf("default branch = %q, want master", branch)
	}
}

// TestResolveRemoteRef_AnnotatedTagPeels asserts an annotated tag resolves to
// its target commit, not the tag object, so the result compares cleanly
// against HEAD commits and checks out by hash.
func TestResolveRemoteRef_AnnotatedTagPeels(t *testing.T) {
	srcPath := initWorkRepo(t) // carries annotated tag v1.0.0 on the initial commit
	destPath := filepath.Join(t.TempDir(), "clone")

	repo, err := Clone(context.Background(), destPath, CloneOptions{URL: srcPath, AllTags: true}, testLogger())
	if err != nil {
		t.Fatalf("clone: %v", err)
	}

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	sha, err := ResolveRemoteRef(repo, "v1.0.0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sha != head.Hash().String() {
		t.Errorf("resolved %s, want peeled commit %s", sha, head.Hash().String())
	}

	if _, err := SyncWorktree(context.Background(), repo, "v1.0.0"); err != nil {
		t.Errorf("sync to annotated tag: %v", err)
	}
}

// TestResolveRemoteRef_RawSHA covers the hash fallback used when reconcile
// passes a resolved commit hash as the ref.
func TestResolveRemoteRef_RawSHA(t *testing.T) {
	srcPath := initWorkRepo(t)
	destPath := filepath.Join(t.TempDir(), "clone")

	repo, err := Clone(context.Background(), destPath, CloneOptions{URL: srcPath}, testLogger())
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	sha, err := ResolveRemoteRef(repo, head.Hash().String())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if sha != head.Hash().String() {
		t.Errorf("resolved %s, want %s", sha, head.Hash().String())
	}
}

func TestSyncWorktree_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SyncWorktree(ctx, nil, "main"); err != context.Canceled {
		t.Fatalf("SyncWorktree error = %v, want context canceled", err)
	}
}
