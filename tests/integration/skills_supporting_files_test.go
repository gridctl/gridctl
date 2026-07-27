//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// initSupportingFilesBareRepo builds a bare repo whose single skill ships the
// shape real packages use: an executable script, a nested script, a reference
// document, and a top-level license.
func initSupportingFilesBareRepo(t *testing.T) (bareParent, bareName string) {
	t.Helper()

	workDir := t.TempDir()
	repo, err := gogit.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	skillRoot := filepath.Join(workDir, "office")
	files := map[string]struct {
		body string
		mode os.FileMode
	}{
		"SKILL.md":                 {"---\nname: office\ndescription: integration fixture with scripts\n---\n\nRun `python scripts/office/unpack.py`.\n", 0644},
		"scripts/unpack.sh":        {"#!/bin/sh\necho unpack\n", 0755},
		"scripts/office/unpack.py": {"print('unpack')\n", 0644},
		"references/format.md":     {"# Format\n", 0644},
		"LICENSE.txt":              {"license text\n", 0644},
		"notes.md":                 {"not a supporting file\n", 0644},
	}
	for rel, f := range files {
		full := filepath.Join(skillRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(f.body), f.mode); err != nil {
			t.Fatalf("write %s: %v", rel, err)
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
	bareName = "office.git"
	barePath := filepath.Join(bareParent, bareName)
	if _, err := gogit.PlainClone(barePath, true, &gogit.CloneOptions{URL: workDir}); err != nil {
		t.Fatalf("clone to bare: %v", err)
	}
	return bareParent, bareName
}

// TestSkills_Import_InstallsSupportingFilesOverHTTP runs a full import against
// a real git server and asserts the registry directory holds the whole skill
// package, not just SKILL.md. This is the end-to-end guard for the defect
// where scripts/ was silently dropped and skills looked healthy while being
// unrunnable.
func TestSkills_Import_InstallsSupportingFilesOverHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	isolateGridctlHome(t)

	bareParent, bareName := initSupportingFilesBareRepo(t)
	srv := startAuthedGitHTTPServer(t, bareParent, privateRepoValidToken)
	repoURL := srv.URL + "/" + bareName

	regDir := t.TempDir()
	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatalf("loading registry: %v", err)
	}
	imp := skills.NewImporter(store, regDir, filepath.Join(regDir, "skills.lock.yaml"), logging.NewDiscardLogger())

	result, err := imp.Import(skills.ImportOptions{
		Repo:  repoURL,
		Trust: true,
		Auth:  skills.AuthConfig{Method: "token", Token: privateRepoValidToken},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("expected 1 imported skill, got %d (skipped: %+v)", len(result.Imported), result.Skipped)
	}
	if got := result.Imported[0].FilesCopied; got != 4 {
		t.Errorf("expected 4 supporting files copied, got %d", got)
	}

	skillDir := filepath.Join(regDir, "skills", "office")
	for _, rel := range []string{
		"SKILL.md",
		"scripts/unpack.sh",
		"scripts/office/unpack.py",
		"references/format.md",
		"LICENSE.txt",
	} {
		if _, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to be installed: %v", rel, err)
		}
	}

	// The executable bit is what makes the skill's own instructions runnable.
	info, err := os.Stat(filepath.Join(skillDir, "scripts", "unpack.sh"))
	if err != nil {
		t.Fatalf("stat unpack.sh: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("expected mode 0755 on unpack.sh, got %o", perm)
	}

	// Content outside the allowlist stays out, and .git never lands.
	if _, err := os.Stat(filepath.Join(skillDir, "notes.md")); !os.IsNotExist(err) {
		t.Error("expected notes.md to be excluded from the install")
	}
	if _, err := os.Stat(filepath.Join(skillDir, ".git")); !os.IsNotExist(err) {
		t.Error(".git must never be installed into the registry")
	}

	sk, err := store.GetSkill("office")
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if sk.FileCount != 3 {
		t.Errorf("expected FileCount 3 (nested files counted, LICENSE excluded), got %d", sk.FileCount)
	}

	files, err := store.ListFiles("office")
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(files) == 0 {
		t.Error("expected ListFiles to report the installed supporting files")
	}
}
