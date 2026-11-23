//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// initTwoSkillBareRepo builds a bare repo holding two plain skills.
func initTwoSkillBareRepo(t *testing.T) (bareParent, bareName string) {
	t.Helper()

	workDir := t.TempDir()
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	for _, name := range []string{"keep-active", "keep-draft"} {
		dir := filepath.Join(workDir, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		body := "---\nname: " + name + "\ndescription: activation fixture\n---\n\nBody.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	sig := &object.Signature{Name: "it", Email: "it@test"}
	if _, err := wt.Commit("initial commit", &gogit.CommitOptions{Author: sig}); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	bareParent = t.TempDir()
	bareName = "pair.git"
	if _, err := gogit.PlainClone(filepath.Join(bareParent, bareName), true, &gogit.CloneOptions{URL: workDir}); err != nil {
		t.Fatalf("clone to bare: %v", err)
	}
	return bareParent, bareName
}

// TestSkills_Import_NoActivateSkillsOverHTTP imports two skills from a real git
// server with one named in NoActivateSkills, then reloads the registry from
// disk to confirm the unticked skill persisted as draft.
func TestSkills_Import_NoActivateSkillsOverHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initTwoSkillBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)

	regDir := t.TempDir()
	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("loading registry: %v", err)
	}
	imp := skills.NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), logging.NewDiscardLogger())

	result, err := imp.Import(context.Background(), skills.ImportOptions{
		Repo:             srv.URL + "/" + bareName,
		Selected:         []string{"keep-active", "keep-draft"},
		NoActivateSkills: []string{"keep-draft"},
		Auth:             skills.AuthConfig{Method: "token", Token: privateRepoValidToken},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(result.Imported) != 2 {
		t.Fatalf("expected 2 imported skills, got %d (skipped: %+v)", len(result.Imported), result.Skipped)
	}

	reloaded := registry.NewStore(regDir)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reloading registry: %v", err)
	}
	for name, want := range map[string]registry.ItemState{
		"keep-active": registry.StateActive,
		"keep-draft":  registry.StateDraft,
	} {
		sk, err := reloaded.GetSkill(name)
		if err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if sk.State != want {
			t.Errorf("%s state = %q, want %q", name, sk.State, want)
		}
	}
}
