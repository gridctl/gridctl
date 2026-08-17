//go:build integration

package integration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/wiring"
)

const privatePackManifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: private-pack
version: 1.0.0
description: Private pack fixture
author:
  name: Integration
skills: [private-test]
agents: []
wiring: false
`

// initPrivateBarePackRepo is the pack twin of initPrivateBareRepo: the same
// single-skill repository plus a gridctl-pack.yaml at the root, which is the
// only thing that makes it a pack.
func initPrivateBarePackRepo(t *testing.T) (bareParent, bareName string) {
	t.Helper()

	workDir := t.TempDir()
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	skillDir := filepath.Join(workDir, "skills", privateRepoSkillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillMD := []byte("---\nname: private-test\ndescription: integration fixture\n---\n\nBody.\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), skillMD, 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "gridctl-pack.yaml"), []byte(privatePackManifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	for _, p := range []string{filepath.Join("skills", privateRepoSkillName, "SKILL.md"), "gridctl-pack.yaml"} {
		if _, err := wt.Add(p); err != nil {
			t.Fatalf("git add %s: %v", p, err)
		}
	}
	sig := &object.Signature{Name: "it", Email: "it@test"}
	if _, err := wt.Commit("initial commit", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	bareParent = t.TempDir()
	bareName = "pack.git"
	barePath := filepath.Join(bareParent, bareName)
	if _, err := gogit.PlainClone(barePath, true, &gogit.CloneOptions{URL: workDir}); err != nil {
		t.Fatalf("clone to bare: %v", err)
	}
	return bareParent, bareName
}

// packEnv builds the pack engine and an importer rooted at the current HOME,
// with a credential resolver that serves one reference. Rebuilding it is how
// a second CLI invocation is simulated.
func packEnv(t *testing.T, ref, token string) (*packops.Managers, *skills.Importer) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	registryDir := filepath.Join(home, ".gridctl", "skills")
	store := registry.NewStore(registryDir)
	if err := store.Load(); err != nil {
		t.Fatalf("registry load: %v", err)
	}
	imp := skills.NewImporter(store, registryDir, skills.LockFilePath(), slog.Default())
	imp.SetCredentialResolver(func(got string) (string, error) {
		if got != ref {
			return "", errors.New("unexpected credential reference: " + got)
		}
		return token, nil
	})

	sm, err := skillsync.NewManager(store)
	if err != nil {
		t.Fatalf("skillsync: %v", err)
	}
	am, err := agentsync.NewManager(registryDir)
	if err != nil {
		t.Fatalf("agentsync: %v", err)
	}
	wm, err := wiring.NewManager()
	if err != nil {
		t.Fatalf("wiring: %v", err)
	}
	cm, err := contexts.NewManager()
	if err != nil {
		t.Fatalf("contexts: %v", err)
	}
	return &packops.Managers{Skills: sm, Agents: am, Wiring: wm, Contexts: cm, Home: home}, imp
}

func TestPacks_PrivateHTTPS_NoAuth_ReturnsAuthRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBarePackRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)
	repoURL := srv.URL + "/" + bareName

	_, err := packops.Preview(context.Background(), packops.PreviewOptions{Repo: repoURL})
	if err == nil {
		t.Fatal("expected preview of a private pack with no credentials to fail")
	}
	if classified := gitpkg.ClassifyError(err); !errors.Is(classified, gitpkg.ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired, got %v", classified)
	}
}

func TestPacks_PrivateHTTPS_PreviewWithToken_Succeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBarePackRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)
	repoURL := srv.URL + "/" + bareName

	res, err := packops.Preview(context.Background(), packops.PreviewOptions{
		Repo: repoURL,
		Auth: skills.AuthConfig{Method: "token", Token: privateRepoValidToken},
	})
	if err != nil {
		t.Fatalf("preview with token: %v", err)
	}
	if res.Pack != "private-pack" {
		t.Errorf("Pack = %q, want private-pack", res.Pack)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != privateRepoSkillName {
		t.Errorf("expected the fixture skill resolved, got %+v", res.Skills)
	}
}

// The lifecycle test: import a private pack by reference, then update it with
// no credentials re-supplied. This is the gap that made pack auth "work" at
// import and fail at the first update.
func TestPacks_PrivateHTTPS_ImportThenUpdateReResolves(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBarePackRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)
	repoURL := srv.URL + "/" + bareName

	const ref = "${var:GIT_TOKEN}"
	mgrs, imp := packEnv(t, ref, privateRepoValidToken)

	res, err := mgrs.Add(context.Background(), imp, packops.AddOptions{
		Repo: repoURL,
		Auth: skills.AuthConfig{Method: "token", Token: privateRepoValidToken, CredentialRef: ref},
	})
	if err != nil {
		t.Fatalf("pack add with credentials: %v", err)
	}
	if res.Doc.Pack != "private-pack" {
		t.Fatalf("imported pack = %q, want private-pack", res.Doc.Pack)
	}

	// Only the reference may have been recorded.
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	src, ok := lf.Sources[skills.RepoToName(repoURL)]
	if !ok {
		t.Fatalf("no lockfile source for %q", repoURL)
	}
	if src.CredentialRef != ref {
		t.Fatalf("lockfile CredentialRef = %q, want %q", src.CredentialRef, ref)
	}
	if strings.Contains(src.CredentialRef, privateRepoValidToken) {
		t.Fatal("lockfile recorded the token instead of the reference")
	}

	// A fresh process: no AuthConfig anywhere, only the recorded reference and
	// a resolver. Update must re-resolve and reach the authed remote.
	_, imp2 := packEnv(t, ref, privateRepoValidToken)
	if _, err := imp2.Update(privateRepoSkillName, false, true, true); err != nil {
		t.Fatalf("update re-resolving the stored reference: %v", err)
	}
}

// Without the resolver the same update must fail loudly rather than silently
// falling back to an unauthenticated fetch.
func TestPacks_PrivateHTTPS_UpdateWithoutResolverFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initPrivateBarePackRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)
	repoURL := srv.URL + "/" + bareName

	const ref = "${var:GIT_TOKEN}"
	mgrs, imp := packEnv(t, ref, privateRepoValidToken)
	if _, err := mgrs.Add(context.Background(), imp, packops.AddOptions{
		Repo: repoURL,
		Auth: skills.AuthConfig{Method: "token", Token: privateRepoValidToken, CredentialRef: ref},
	}); err != nil {
		t.Fatalf("pack add: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	registryDir := filepath.Join(home, ".gridctl", "skills")
	store := registry.NewStore(registryDir)
	if err := store.Load(); err != nil {
		t.Fatalf("registry load: %v", err)
	}
	// No SetCredentialResolver: the reference cannot be resolved.
	bare := skills.NewImporter(store, registryDir, skills.LockFilePath(), slog.Default())

	_, err = bare.Update(privateRepoSkillName, false, true, true)
	if err == nil {
		t.Fatal("expected update to fail with no resolver for the stored reference")
	}
	if !strings.Contains(err.Error(), "resolver") && !errors.Is(err, gitpkg.ErrAuthFailed) {
		t.Errorf("expected a resolver/auth failure, got %v", err)
	}
}

// The motivating bug: an SSH pack URL with no reachable agent must fail with
// the friendly classified error, never the raw xanzy string, and must offer the
// HTTPS equivalent.
func TestPacks_SSHWithoutAgent_FriendlyError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)
	t.Setenv("SSH_AUTH_SOCK", "")

	const repoURL = "git@github.com:acme/private-pack.git"
	_, err := packops.Preview(context.Background(), packops.PreviewOptions{Repo: repoURL})
	if err == nil {
		t.Fatal("expected preview to fail with no ssh-agent")
	}
	if !errors.Is(gitpkg.ClassifyError(err), gitpkg.ErrSSHAgentMissing) {
		t.Fatalf("expected ErrSSHAgentMissing, got %v", err)
	}
	if strings.Contains(err.Error(), "not-specified") {
		t.Errorf("raw xanzy/ssh-agent string reached the caller: %v", err)
	}
	if !strings.Contains(err.Error(), "gridctl process") {
		t.Errorf("error should name the process lacking the agent, got %v", err)
	}
	https, ok := gitpkg.HTTPSEquivalent(repoURL)
	if !ok || https != "https://github.com/acme/private-pack" {
		t.Errorf("HTTPSEquivalent(%q) = %q, %v", repoURL, https, ok)
	}
}
