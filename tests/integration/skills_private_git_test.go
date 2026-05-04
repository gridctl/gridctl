//go:build integration

package integration

import (
	"errors"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: CVE-2016-5386 (Httpoxy) fixed in Go 1.6.3; used only against httptest.Server in this integration test
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/skills"
)

const (
	privateRepoValidToken = "correct-horse-battery-staple"
	privateRepoSkillName  = "private-test"
)

// initPrivateBareRepo creates a bare repo containing a single SKILL.md file
// and returns the on-disk bare path plus the name of the repository on disk
// (e.g. "repo.git"), which is exposed verbatim in the server URL path.
func initPrivateBareRepo(t *testing.T) (bareParent, bareName string) {
	t.Helper()

	workDir := t.TempDir()
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	skillDir := filepath.Join(workDir, privateRepoSkillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillMD := []byte("---\nname: private-test\ndescription: integration fixture\n---\n\nBody.\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), skillMD, 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add(privateRepoSkillName + "/SKILL.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	sig := &object.Signature{Name: "it", Email: "it@test"}
	if _, err := wt.Commit("initial commit", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	bareParent = t.TempDir()
	bareName = "repo.git"
	barePath := filepath.Join(bareParent, bareName)
	if _, err := gogit.PlainClone(barePath, true, &gogit.CloneOptions{URL: workDir}); err != nil {
		t.Fatalf("clone to bare: %v", err)
	}
	return bareParent, bareName
}

// startAuthedGitHTTPServer wraps a bare repository with httptest + the system
// `git http-backend` CGI, gated by HTTP basic auth. The test is skipped when
// the `git` binary is not available.
//
// Auth behavior (intentional):
//
//	no Authorization header    → 401 (→ ErrAuthRequired)
//	wrong Authorization header → 403 (→ ErrAuthFailed)
//	correct Authorization      → served by git-http-backend
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
		_, pass, ok := r.BasicAuth()
		switch {
		case !ok:
			w.Header().Set("WWW-Authenticate", `Basic realm="gridctl-it"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case pass != validToken:
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		cgiHandler.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// isolateGridctlHome points $HOME at a temp dir so clone caches under
// ~/.gridctl/cache/repos don't leak between tests or into the user's home.
func isolateGridctlHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestSkills_PrivateHTTPS_NoAuth_ReturnsAuthRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	repoURL := srv.URL + "/" + bareName

	_, err := skills.CloneAndDiscover(repoURL, "", "", skills.AuthConfig{}, logging.NewDiscardLogger())
	if err == nil {
		t.Fatal("expected error cloning private repo without credentials")
	}
	classified := gitpkg.ClassifyError(err)
	if !errors.Is(classified, gitpkg.ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired after classification, got %v", classified)
	}
}

func TestSkills_PrivateHTTPS_WrongToken_ReturnsAuthFailed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	repoURL := srv.URL + "/" + bareName
	cfg := skills.AuthConfig{Method: "token", Token: "not-the-right-token"}

	_, err := skills.CloneAndDiscover(repoURL, "", "", cfg, logging.NewDiscardLogger())
	if err == nil {
		t.Fatal("expected error cloning with a wrong token")
	}
	classified := gitpkg.ClassifyError(err)
	if !errors.Is(classified, gitpkg.ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed after classification, got %v", classified)
	}
}

func TestSkills_PrivateHTTPS_ValidToken_Succeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	repoURL := srv.URL + "/" + bareName
	cfg := skills.AuthConfig{Method: "token", Token: privateRepoValidToken}

	result, err := skills.CloneAndDiscover(repoURL, "", "", cfg, logging.NewDiscardLogger())
	if err != nil {
		t.Fatalf("clone with valid token: %v", err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != privateRepoSkillName {
		t.Errorf("expected 1 discovered skill %q, got %+v", privateRepoSkillName, result.Skills)
	}
	if result.CommitSHA == "" {
		t.Error("expected CommitSHA to be populated after successful clone")
	}
}

func TestSkills_PrivateHTTPS_ErrorRedactsToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	// A GitHub-classic-PAT-shaped string intentionally embedded in the URL
	// userinfo. RedactString must scrub both the PAT pattern and the
	// userinfo component before the error surfaces.
	fakePAT := "ghp_" + strings.Repeat("a", 40)
	repoURL := strings.Replace(srv.URL, "http://", "http://"+fakePAT+"@", 1) + "/" + bareName

	_, err := skills.CloneAndDiscover(repoURL, "", "", skills.AuthConfig{}, logging.NewDiscardLogger())
	if err == nil {
		t.Fatal("expected error cloning with bogus embedded token")
	}
	if strings.Contains(err.Error(), fakePAT) {
		t.Errorf("error message leaked embedded PAT: %q", err.Error())
	}
	if strings.Contains(err.Error(), "ghp_") {
		t.Errorf("error message still contains ghp_ prefix: %q", err.Error())
	}
}

func TestSkills_PrivateSSH_Optional(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// SSH end-to-end requires a reachable SSH remote + a populated ssh-agent.
	// Opt in via GRIDCTL_IT_SSH_URL (e.g. "git@github.com:acme/private.git");
	// otherwise skip cleanly so this suite stays hermetic in CI.
	repoURL := os.Getenv("GRIDCTL_IT_SSH_URL")
	if repoURL == "" {
		t.Skip("GRIDCTL_IT_SSH_URL not set; skipping SSH end-to-end test")
	}
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set; skipping SSH end-to-end test")
	}
	isolateGridctlHome(t)

	cfg := skills.AuthConfig{Method: "ssh-agent"}
	if _, err := skills.CloneAndDiscover(repoURL, "", "", cfg, logging.NewDiscardLogger()); err != nil {
		t.Fatalf("ssh-agent clone: %v", err)
	}
}
