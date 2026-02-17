package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

// writeSkillMD writes a SKILL.md file in the directory-based skill layout.
func writeSkillMD(t *testing.T, baseDir, skillName, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, "skills", skillName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStore_Load_EmptyDir(t *testing.T) {
	s := newTestStore(t)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on empty dir: %v", err)
	}
	if s.HasContent() {
		t.Error("expected no content in empty store")
	}
}

func TestStore_Load_NonexistentDir(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "does-not-exist"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on nonexistent dir: %v", err)
	}
	if s.HasContent() {
		t.Error("expected no content")
	}
}

func TestStore_Load(t *testing.T) {
	dir := t.TempDir()

	writeSkillMD(t, dir, "deploy", `---
name: deploy
description: Deploy workflow
state: active
---

# Deploy

Run the deployment steps.
`)

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.HasContent() {
		t.Error("expected content after load")
	}

	sk, err := s.GetSkill("deploy")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if sk.Description != "Deploy workflow" {
		t.Errorf("unexpected description: %s", sk.Description)
	}
	if sk.State != StateActive {
		t.Errorf("expected active state, got %s", sk.State)
	}
}

func TestStore_Load_SkillNameFromDirectory(t *testing.T) {
	dir := t.TempDir()

	// Write a SKILL.md with no name in frontmatter
	writeSkillMD(t, dir, "my-tool", `---
description: Tool without name in frontmatter
state: active
---

Body content.
`)

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	sk, err := s.GetSkill("my-tool")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if sk.Name != "my-tool" {
		t.Errorf("expected name from directory, got %q", sk.Name)
	}
}

func TestStore_Load_SkipsInvalidSkills(t *testing.T) {
	dir := t.TempDir()

	// Invalid skill: name with uppercase (will fail validation)
	writeSkillMD(t, dir, "BadSkill", `---
name: BadSkill
description: Invalid name
state: active
---

Body.
`)

	// Valid skill
	writeSkillMD(t, dir, "good-skill", `---
name: good-skill
description: A good skill
state: draft
---

Good body.
`)

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	skills := s.ListSkills()
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "good-skill" {
		t.Errorf("expected good-skill, got %s", skills[0].Name)
	}
}

func TestStore_SaveSkill(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{
		Name:        "deploy",
		Description: "Deploy workflow",
		State:       StateActive,
		Body:        "# Deploy\n\nRun the steps.\n",
	}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	got, err := s.GetSkill("deploy")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if got.Description != "Deploy workflow" {
		t.Errorf("unexpected description: %s", got.Description)
	}

	// Verify persistence by reloading
	s2 := NewStore(s.baseDir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got2, err := s2.GetSkill("deploy")
	if err != nil {
		t.Fatalf("GetSkill after reload: %v", err)
	}
	if got2.Description != "Deploy workflow" {
		t.Errorf("unexpected description after reload: %s", got2.Description)
	}
}

func TestStore_SaveSkill_CreatesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "registry")
	s := NewStore(dir)

	sk := &AgentSkill{
		Name:        "test",
		Description: "A test skill",
		State:       StateDraft,
	}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	skillDir := filepath.Join(dir, "skills", "test")
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		t.Error("skill directory was not created")
	}
}

func TestStore_SaveSkill_ValidationError(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "", Description: "missing name"}
	if err := s.SaveSkill(sk); err == nil {
		t.Error("expected validation error for empty name")
	}
}

func TestStore_SaveSkill_Update(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "test", Description: "v1", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill v1: %v", err)
	}

	sk.Description = "v2"
	sk.State = StateActive
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill v2: %v", err)
	}

	got, _ := s.GetSkill("test")
	if got.Description != "v2" {
		t.Errorf("expected v2, got %s", got.Description)
	}
	if got.State != StateActive {
		t.Errorf("expected active, got %s", got.State)
	}
}

func TestStore_DeleteSkill(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "to-delete", Description: "bye", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSkill("to-delete"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}

	if _, err := s.GetSkill("to-delete"); err == nil {
		t.Error("expected not-found error after delete")
	}

	// Verify directory is gone
	dir := filepath.Join(s.baseDir, "skills", "to-delete")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("expected skill directory to be deleted")
	}
}

func TestStore_DeleteSkill_Nonexistent(t *testing.T) {
	s := newTestStore(t)

	if err := s.DeleteSkill("ghost"); err != nil {
		t.Fatalf("DeleteSkill(ghost): %v", err)
	}
}

func TestStore_GetSkill_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetSkill("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_HasContent(t *testing.T) {
	s := newTestStore(t)

	if s.HasContent() {
		t.Error("expected no content initially")
	}

	sk := &AgentSkill{Name: "test", Description: "content", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	if !s.HasContent() {
		t.Error("expected content after save")
	}
}

func TestStore_ActiveSkills(t *testing.T) {
	s := newTestStore(t)

	skills := []*AgentSkill{
		{Name: "draft-skill", Description: "Draft", State: StateDraft},
		{Name: "active-skill", Description: "Active", State: StateActive},
		{Name: "disabled-skill", Description: "Disabled", State: StateDisabled},
	}
	for _, sk := range skills {
		if err := s.SaveSkill(sk); err != nil {
			t.Fatalf("SaveSkill(%s): %v", sk.Name, err)
		}
	}

	active := s.ActiveSkills()
	if len(active) != 1 {
		t.Fatalf("expected 1 active skill, got %d", len(active))
	}
	if active[0].Name != "active-skill" {
		t.Errorf("expected active-skill, got %s", active[0].Name)
	}
}

func TestStore_ListSkills(t *testing.T) {
	s := newTestStore(t)

	if got := s.ListSkills(); len(got) != 0 {
		t.Errorf("expected 0 skills, got %d", len(got))
	}

	for _, name := range []string{"alpha", "beta", "gamma"} {
		sk := &AgentSkill{Name: name, Description: "Skill " + name, State: StateDraft}
		if err := s.SaveSkill(sk); err != nil {
			t.Fatal(err)
		}
	}

	if got := s.ListSkills(); len(got) != 3 {
		t.Errorf("expected 3 skills, got %d", len(got))
	}
}

func TestStore_Status(t *testing.T) {
	s := newTestStore(t)

	st := s.Status()
	if st.TotalSkills != 0 || st.ActiveSkills != 0 {
		t.Errorf("expected all zeros, got %+v", st)
	}

	skills := []*AgentSkill{
		{Name: "s1", Description: "Skill 1", State: StateActive},
		{Name: "s2", Description: "Skill 2", State: StateDisabled},
		{Name: "s3", Description: "Skill 3", State: StateActive},
	}
	for _, sk := range skills {
		if err := s.SaveSkill(sk); err != nil {
			t.Fatal(err)
		}
	}

	st = s.Status()
	if st.TotalSkills != 3 {
		t.Errorf("expected 3 total skills, got %d", st.TotalSkills)
	}
	if st.ActiveSkills != 2 {
		t.Errorf("expected 2 active skills, got %d", st.ActiveSkills)
	}
}

func TestStore_Load_FileCount(t *testing.T) {
	dir := t.TempDir()

	// Create skill with supporting files
	skillDir := filepath.Join(dir, "skills", "with-files")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	skillMD := `---
name: with-files
description: Skill with supporting files
state: active
---

# With Files
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/bash\necho hi"), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	sk, err := s.GetSkill("with-files")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	// scripts/ directory counts as 1 supporting file entry
	if sk.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", sk.FileCount)
	}
}
