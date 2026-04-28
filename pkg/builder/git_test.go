package builder

import (
	"errors"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: CVE-2016-5386 (Httpoxy) fixed in Go 1.6.3; used only against httptest.Server in this test
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	gohttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/object"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
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

	path, err := cloneRepo(bareRepo, "", destDir, nil, logger)
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
	path, err := cloneRepo(bareRepo, "master", destDir, nil, logger)
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

	_, err := cloneRepo("/nonexistent/path", "", destDir, nil, logger)
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

	_, err := cloneRepo(bareRepo, "", cloneDir, nil, logger)
	if err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	path, err := updateRepo(cloneDir, "", nil, logger)
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

	_, err := updateRepo(invalidDir, "", nil, logger)
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

	path, err := CloneOrUpdate(bareRepo, "", nil, logger)
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
	path1, err := CloneOrUpdate(bareRepo, "", nil, logger)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call updates
	path2, err := CloneOrUpdate(bareRepo, "", nil, logger)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if path1 != path2 {
		t.Errorf("expected same path, got %q and %q", path1, path2)
	}
}

// startAuthedGitHTTPServer wraps a bare repository with httptest + git http-backend,
// gated by HTTP basic auth. Returns the server; t.Cleanup handles teardown. Skips
// when the `git` binary is unavailable.
func startAuthedGitHTTPServer(t *testing.T, bareParent, validToken string) *httptest.Server {
	t.Helper()

	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found; skipping http-backend integration")
	}

	cgiHandler := &cgi.Handler{
		Path: gitBin,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + bareParent,
			"GIT_HTTP_EXPORT_ALL=1",
		},
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, ok := r.BasicAuth()
		switch {
		case !ok:
			w.Header().Set("WWW-Authenticate", `Basic realm="gridctl-builder-it"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case user != validToken:
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		cgiHandler.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// TestCloneOrUpdate_ForwardsAuth locks in the contract that an auth method
// supplied to CloneOrUpdate flows through to the underlying git transport. A
// regression here would re-introduce the "auth block silently dropped" bug
// even if the config struct still exposed the field.
func TestCloneOrUpdate_ForwardsAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git test in short mode")
	}

	const validToken = "correct-horse"

	// Build a bare repo and serve it behind HTTP basic auth.
	bareDir := initBareRepo(t)
	bareParent := filepath.Dir(bareDir)
	bareName := filepath.Base(bareDir)
	srv := startAuthedGitHTTPServer(t, bareParent, validToken)
	t.Setenv("HOME", t.TempDir())

	repoURL := srv.URL + "/" + bareName
	logger := newTestLogger()

	// Without auth: clone must fail with the classified ErrAuthRequired so
	// callers can distinguish auth from network errors.
	_, err := CloneOrUpdate(repoURL, "", nil, logger)
	if err == nil {
		t.Fatal("expected error cloning without auth")
	}
	if !errors.Is(gitpkg.ClassifyError(err), gitpkg.ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired, got %v", err)
	}

	// With valid auth: clone must succeed, proving the auth method is
	// forwarded into the git transport.
	t.Setenv("HOME", t.TempDir())
	auth := &gohttp.BasicAuth{Username: validToken, Password: ""}
	if _, err := CloneOrUpdate(repoURL, "", auth, logger); err != nil {
		t.Fatalf("clone with valid auth: %v", err)
	}
}

// Checkout logic moved to pkg/git; see pkg/git/clone_test.go:TestCheckout_InvalidRef.
