package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gridctl/gridctl/pkg/logging"
)

func testLogger() *slog.Logger {
	return logging.NewDiscardLogger()
}

// initBareRepo creates a bare repo with one commit on master and one annotated
// tag "v1.0.0", and returns its path.
func initBareRepo(t *testing.T) string {
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

	bareDir := t.TempDir()
	if _, err := gogit.PlainClone(bareDir, true, &gogit.CloneOptions{URL: workDir, Tags: gogit.AllTags}); err != nil {
		t.Fatalf("clone to bare: %v", err)
	}

	return bareDir
}

func TestClone_NoRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")

	_, err := Clone(dest, CloneOptions{URL: bare}, testLogger())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Errorf("expected README.md in clone: %v", err)
	}
}

func TestClone_WithBranchRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")

	_, err := Clone(dest, CloneOptions{URL: bare, Ref: "master"}, testLogger())
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
}

func TestClone_WithTagRefFallsBack(t *testing.T) {
	// A tag ref is not a branch, so single-branch clone should fail and
	// fall back to a full clone. The caller is responsible for a
	// subsequent Checkout, which is verified in TestCheckout_Tag.
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")

	_, err := Clone(dest, CloneOptions{URL: bare, Ref: "v1.0.0", AllTags: true}, testLogger())
	if err != nil {
		t.Fatalf("Clone with tag ref: %v", err)
	}
}

func TestClone_InvalidURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	dest := filepath.Join(t.TempDir(), "clone")
	_, err := Clone(dest, CloneOptions{URL: "/nonexistent/path"}, testLogger())
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestFetch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if err := Fetch(dest, FetchOptions{AllTags: true}, testLogger()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetch_InvalidRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	err := Fetch(t.TempDir(), FetchOptions{}, testLogger())
	if err == nil {
		t.Fatal("expected error fetching from a non-repo directory")
	}
}

func TestCheckout_Tag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare, AllTags: true}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	repo, err := gogit.PlainOpen(dest)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Checkout(repo, "v1.0.0"); err != nil {
		t.Fatalf("Checkout tag: %v", err)
	}
}

func TestCheckout_InvalidRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	repo, err := gogit.PlainOpen(dest)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Checkout(repo, "does-not-exist-xyz"); err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}

func TestResolveRef_Tag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare, AllTags: true}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	repo, err := gogit.PlainOpen(dest)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sha, err := ResolveRef(repo, "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if sha == "" {
		t.Fatal("empty sha")
	}
}

func TestResolveRef_Unknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	repo, err := gogit.PlainOpen(dest)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := ResolveRef(repo, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown ref")
	}
}

func TestHeadCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	sha, err := HeadCommit(dest)
	if err != nil {
		t.Fatalf("HeadCommit: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char sha, got %q", sha)
	}
}

func TestHeadCommit_InvalidRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	if _, err := HeadCommit(t.TempDir()); err == nil {
		t.Fatal("expected error for non-repo directory")
	}
}

func TestListTags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := Clone(dest, CloneOptions{URL: bare, AllTags: true}, testLogger()); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	tags, err := ListTags(dest)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	found := false
	for _, tag := range tags {
		if tag == "v1.0.0" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find v1.0.0 in tags, got %v", tags)
	}
}

func TestListTags_InvalidRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	if _, err := ListTags(t.TempDir()); err == nil {
		t.Fatal("expected error for non-repo directory")
	}
}

