package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestRunSkillAdd_GitSourceName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	prevName, prevTrust, prevRef := skillAddSourceName, skillAddTrust, skillAddRef
	t.Cleanup(func() {
		skillAddSourceName, skillAddTrust, skillAddRef = prevName, prevTrust, prevRef
	})
	skillAddTrust = true
	skillAddRef = ""
	skillAddSourceName = ""

	local := filepath.Join(home, "local", "skills", "widget")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "SKILL.md"), []byte("---\nname: widget\ndescription: Widget skill\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runSkillAdd(context.Background(), filepath.Dir(local)); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(home, "git", "skills")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRepo, err := git.PlainInit(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "SKILL.md"), []byte("---\nname: git-skill\ndescription: Git skill\n---\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := gitRepo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("SKILL.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}

	skillAddSourceName = ""
	if err := runSkillAdd(context.Background(), repo); err == nil {
		t.Fatal("expected source-name collision")
	}
	skillAddSourceName = "repo-skills"
	skillAddRef = "master"
	if err := runSkillAdd(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lf.Sources["repo-skills"]; !ok {
		t.Fatalf("sources = %#v", lf.Sources)
	}
	if _, ok := lf.Sources["skills"]; !ok {
		t.Fatalf("local source missing: %#v", lf.Sources)
	}
}
