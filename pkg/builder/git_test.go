package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initBareRepo creates a bare git repo with an initial commit and returns its path.
// The default branch is "master" (go-git default).
func initBareRepo(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()
	repo, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	testFile := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test repo"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
		},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}

	bareDir := t.TempDir()
	_, err = git.PlainClone(bareDir, true, &git.CloneOptions{URL: workDir})
	if err != nil {
		t.Fatalf("clone to bare: %v", err)
	}

	return bareDir
}

func TestCloneRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bareRepo := initBareRepo(t)
	destDir := filepath.Join(t.TempDir(), "clone")
	logger := newTestLogger()

	path, err := cloneRepo(bareRepo, "", destDir, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != destDir {
		t.Errorf("expected path %q, got %q", destDir, path)
	}

	readmePath := filepath.Join(destDir, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Errorf("expected README.md in clone: %v", err)
	}
}

func TestCloneRepo_WithRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bareRepo := initBareRepo(t)
	destDir := filepath.Join(t.TempDir(), "clone")
	logger := newTestLogger()

	// go-git PlainInit creates "master" as the default branch
	path, err := cloneRepo(bareRepo, "master", destDir, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != destDir {
		t.Errorf("expected path %q, got %q", destDir, path)
	}
}

func TestCloneRepo_InvalidURL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	destDir := filepath.Join(t.TempDir(), "clone")
	logger := newTestLogger()

	_, err := cloneRepo("/nonexistent/path", "", destDir, logger)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestUpdateRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bareRepo := initBareRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	logger := newTestLogger()

	_, err := cloneRepo(bareRepo, "", cloneDir, logger)
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	path, err := updateRepo(cloneDir, "", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != cloneDir {
		t.Errorf("expected path %q, got %q", cloneDir, path)
	}
}

func TestUpdateRepo_InvalidRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	invalidDir := t.TempDir()
	logger := newTestLogger()

	_, err := updateRepo(invalidDir, "", logger)
	if err == nil {
		t.Fatal("expected error for invalid repo")
	}
}

func TestCloneOrUpdate_Clone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bareRepo := initBareRepo(t)
	t.Setenv("HOME", t.TempDir())

	logger := newTestLogger()

	path, err := CloneOrUpdate(bareRepo, "", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	readmePath := filepath.Join(path, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Errorf("expected README.md in clone: %v", err)
	}
}

func TestCloneOrUpdate_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bareRepo := initBareRepo(t)
	t.Setenv("HOME", t.TempDir())

	logger := newTestLogger()

	// First call clones
	path1, err := CloneOrUpdate(bareRepo, "", logger)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call updates
	path2, err := CloneOrUpdate(bareRepo, "", logger)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if path1 != path2 {
		t.Errorf("expected same path, got %q and %q", path1, path2)
	}
}

func TestCheckoutRef_InvalidRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	bareRepo := initBareRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	logger := newTestLogger()

	_, err := cloneRepo(bareRepo, "", cloneDir, logger)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}

	repo, err := git.PlainOpen(cloneDir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	err = checkoutRef(repo, "nonexistent-branch-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent ref")
	}
}
