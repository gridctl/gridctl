package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestSkillUpdate_LockedSourceWarnsAndContinues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	lockedDir := writeNamedSkillRepo(t, "locked-skill", "original locked")
	otherDir := writeNamedSkillRepo(t, "other-skill", "original other")

	store, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	imp := newImporter(store)
	ctx := context.Background()
	if _, err := imp.Import(ctx, skills.ImportOptions{Repo: lockedDir, Trust: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := imp.Import(ctx, skills.ImportOptions{Repo: otherDir, Trust: true}); err != nil {
		t.Fatal(err)
	}
	if err := skills.MutateLockFile(ctx, skills.LockFilePath(), func(lf *skills.LockFile) (bool, error) {
		key := skills.RepoToName(lockedDir)
		src, ok := lf.Sources[key]
		if !ok {
			t.Fatalf("source %s missing", key)
		}
		src.LockedBy = "netops-team"
		lf.SetSource(key, src)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	commitNamedSkill(t, lockedDir, "locked-skill", "updated locked")
	commitNamedSkill(t, otherDir, "other-skill", "updated other")

	prevTrust, prevForce, prevIgnore := skillUpdateTrust, skillUpdateForce, skillUpdateIgnoreLock
	skillUpdateTrust, skillUpdateForce, skillUpdateIgnoreLock = true, true, false
	t.Cleanup(func() {
		skillUpdateTrust, skillUpdateForce, skillUpdateIgnoreLock = prevTrust, prevForce, prevIgnore
	})

	err = runSkillUpdate(ctx, "locked-skill")
	if !errors.Is(err, skills.ErrSourceLocked) {
		t.Fatalf("named update err = %v", err)
	}
	if body := readInstalledSkill(t, "locked-skill"); strings.Contains(body, "updated locked") {
		t.Fatalf("named update changed the locked skill: %s", body)
	}

	if err := runSkillUpdate(ctx, ""); err != nil {
		t.Fatalf("bulk update aborted: %v", err)
	}
	if body := readInstalledSkill(t, "locked-skill"); !strings.Contains(body, "original locked") || strings.Contains(body, "updated locked") {
		t.Fatalf("bulk update changed the locked skill: %s", body)
	}
	if body := readInstalledSkill(t, "other-skill"); !strings.Contains(body, "updated other") {
		t.Fatalf("bulk update did not continue to the unlocked skill: %s", body)
	}
}

func writeNamedSkillRepo(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, dir, name, body)
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func commitNamedSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	writeSkillFile(t, dir, name, body)
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("update", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
}

func writeSkillFile(t *testing.T, dir, name, body string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: Test skill\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readInstalledSkill(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(registryDir(), "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
