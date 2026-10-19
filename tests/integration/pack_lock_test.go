//go:build integration && !windows

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http/cgi" //nolint:gosec // G504: test-only git http-backend
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/skills"
	"gopkg.in/yaml.v3"
)

func TestPackLock_HonoredAcrossHomes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	buildRace(ctx, t, filepath.Join(root, "cmd/gridctl"), bin, false)

	sourceURL, bare, locked := serveSkillRepo(t)
	packDir := filepath.Join(dir, "pack")
	writeLockedPackRepo(t, packDir, sourceURL)
	homeLock := filepath.Join(dir, "author")
	if out, err := runGridctl(ctx, t, bin, homeLock, "pack", "lock", packDir); err != nil {
		t.Fatalf("pack lock: %v\n%s", err, out)
	}
	commitInPack(t, packDir, "gridctl-pack.lock.yaml", "lock")
	if got := packLockCommit(t, packDir); got != locked {
		t.Fatalf("lock commit = %s, want %s", got, locked)
	}

	home1 := filepath.Join(dir, "home1")
	out, err := runGridctl(ctx, t, bin, home1, "pack", "add", packDir, "--trust")
	if err != nil {
		t.Fatalf("home1 add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "lock: honored") {
		t.Fatalf("home1 stdout =\n%s", out)
	}
	if got := memberCommit(t, home1); got != locked {
		t.Fatalf("home1 commit = %s, want %s", got, locked)
	}

	newer := pushSkillCommit(t, bare)
	if newer == locked {
		t.Fatal("pushed commit did not move the branch")
	}

	home2 := filepath.Join(dir, "home2")
	out, err = runGridctl(ctx, t, bin, home2, "pack", "add", packDir, "--trust")
	if err != nil {
		t.Fatalf("home2 add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "lock: honored") {
		t.Fatalf("home2 stdout =\n%s", out)
	}
	if got := memberCommit(t, home2); got != locked {
		t.Fatalf("home2 commit = %s, want locked %s", got, locked)
	}
	if head := cacheHead(t, home2, sourceURL); head != locked {
		t.Fatalf("home2 checkout HEAD = %s, want %s", head, locked)
	}

	home3 := filepath.Join(dir, "home3")
	out, err = runGridctl(ctx, t, bin, home3, "pack", "add", packDir, "--trust", "--ignore-lock")
	if err != nil {
		t.Fatalf("ignore-lock add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "lock: ignored") {
		t.Fatalf("ignore-lock stdout =\n%s", out)
	}
	if got := memberCommit(t, home3); got != newer {
		t.Fatalf("ignore-lock commit = %s, want %s", got, newer)
	}

	forceReplaceMaster(t, bare)
	home4 := filepath.Join(dir, "home4")
	out, err = runGridctl(ctx, t, bin, home4, "pack", "add", packDir, "--trust")
	if err == nil || exitCode(err) != 1 || !strings.Contains(out, "locked commit "+locked[:8]+" is not reachable at "+sourceURL) {
		t.Fatalf("unreachable err=%v code=%d\n%s", err, exitCode(err), out)
	}

	checkDir := filepath.Join(dir, "check")
	writeLockedPackRepo(t, checkDir, sourceURL)
	out, err = runGridctl(ctx, t, bin, homeLock, "pack", "lock", "--check", checkDir)
	if exitCode(err) != 1 || !strings.Contains(out, "lock file is missing") {
		t.Fatalf("missing lock err=%v\n%s", err, out)
	}
	if out, err = runGridctl(ctx, t, bin, homeLock, "pack", "lock", checkDir); err != nil {
		t.Fatalf("check pack lock: %v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, homeLock, "pack", "lock", "--check", checkDir)
	if err != nil || !strings.Contains(out, "is up to date") {
		t.Fatalf("up to date err=%v\n%s", err, out)
	}
	manifestPath := filepath.Join(checkDir, "gridctl-pack.yaml")
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(body, []byte("# comment only\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runGridctl(ctx, t, bin, homeLock, "pack", "lock", "--check", checkDir)
	if err != nil || !strings.Contains(out, "is up to date") {
		t.Fatalf("comment check err=%v\n%s", err, out)
	}
	if err := os.WriteFile(manifestPath, append(body, []byte("  beta:\n    repo: https://example.com/beta.git\n    ref: main\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runGridctl(ctx, t, bin, homeLock, "pack", "lock", "--check", checkDir)
	if exitCode(err) != 1 || !strings.Contains(out, "source beta added") {
		t.Fatalf("source added err=%v\n%s", err, out)
	}
}

func TestPackLock_NewerVersionExit2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	buildRace(ctx, t, filepath.Join(root, "cmd/gridctl"), bin, false)
	packDir := filepath.Join(dir, "pack")
	writeLockedPackRepo(t, packDir, "https://example.com/upstream.git")
	lock := "version: 2\npack: locked-pack\nmanifestDigest: sha256:ab\nsources:\n  upstream:\n    repo: https://example.com/upstream.git\n    ref: master\n    commit: 0123456789abcdef0123456789abcdef01234567\n"
	if err := os.WriteFile(filepath.Join(packDir, "gridctl-pack.lock.yaml"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	commitInPack(t, packDir, "gridctl-pack.lock.yaml", "newer lock")
	home := filepath.Join(dir, "home")
	want := "written by a newer gridctl version"
	out, err := runGridctl(ctx, t, bin, home, "pack", "add", packDir)
	if exitCode(err) != 2 || !strings.Contains(out, want) {
		t.Fatalf("add err=%v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "lock", packDir)
	if exitCode(err) != 2 || !strings.Contains(out, want) {
		t.Fatalf("lock err=%v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "lock", "--check", packDir)
	if exitCode(err) != 2 || !strings.Contains(out, want) {
		t.Fatalf("check err=%v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".gridctl", "skills.lock.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("skills.lock.yaml written: %v", statErr)
	}
}

func serveSkillRepo(t *testing.T) (url, bare, sha string) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git binary required")
	}
	work := t.TempDir()
	repo, err := git.PlainInit(work, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(work, "skills", "locked-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\nname: locked-skill\ndescription: locked\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("skills/locked-skill/SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	sha = head.Hash().String()
	parent := t.TempDir()
	bare = filepath.Join(parent, "repo.git")
	if _, err := git.PlainClone(bare, true, &git.CloneOptions{URL: work}); err != nil {
		t.Fatal(err)
	}
	handler := &cgi.Handler{
		Path: gitBin,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + parent, "GIT_HTTP_EXPORT_ALL=1"},
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git", bare, sha
}

func writeLockedPackRepo(t *testing.T, dir, sourceURL string) {
	t.Helper()
	manifest := "apiVersion: gridctl.dev/v1\nkind: Pack\nname: locked-pack\nskills:\n  - { name: locked-skill, source: upstream }\nsources:\n  upstream:\n    repo: " + sourceURL + "\n    ref: master\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gridctl-pack.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("gridctl-pack.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("pack", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
}

func commitInPack(t *testing.T, dir, rel, msg string) {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(rel); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
}

func packLockCommit(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "gridctl-pack.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var lf struct {
		Sources map[string]struct {
			Commit string `yaml:"commit"`
		} `yaml:"sources"`
	}
	if err := yaml.Unmarshal(data, &lf); err != nil {
		t.Fatal(err)
	}
	return lf.Sources["upstream"].Commit
}

func memberCommit(t *testing.T, home string) string {
	t.Helper()
	lf, err := skills.ReadLockFile(filepath.Join(home, ".gridctl", "skills.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src, ok := lf.Sources["locked-pack/upstream"]
	if !ok {
		t.Fatalf("member source missing: %v", lf.Sources)
	}
	return src.CommitSHA
}

func cacheHead(t *testing.T, home, repoURL string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(repoURL))
	dir := filepath.Join(home, ".gridctl", "cache", "repos", hex.EncodeToString(sum[:8]))
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func pushSkillCommit(t *testing.T, bare string) string {
	t.Helper()
	work := t.TempDir()
	repo, err := git.PlainClone(work, false, &git.CloneOptions{URL: bare})
	if err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(work, "skills", "locked-skill", "SKILL.md")
	if err := os.WriteFile(skill, []byte("---\nname: locked-skill\ndescription: locked\n---\n\nnewer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("skills/locked-skill/SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("newer", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{}); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	return head.Hash().String()
}

func forceReplaceMaster(t *testing.T, bare string) {
	t.Helper()
	work := t.TempDir()
	runGit(t, "", "init", "-b", "master", work)
	skill := filepath.Join(work, "skills", "locked-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\nname: locked-skill\ndescription: locked\n---\n\nreplaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.email=test@test.com", "-c", "user.name=test", "commit", "-m", "replacement")
	runGit(t, work, "remote", "add", "origin", bare)
	runGit(t, work, "push", "--force", "origin", "HEAD:refs/heads/master")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
