package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// --- Loading Tests ---

func TestStore_Load_EmptyDirectory(t *testing.T) {
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

func TestStore_Load_ValidSkills(t *testing.T) {
	dir := t.TempDir()

	writeSkillMD(t, dir, "deploy", `---
name: deploy
description: Deploy workflow
state: active
---

# Deploy

Run the deployment steps.
`)

	writeSkillMD(t, dir, "lint", `---
name: lint
description: Run linters
state: draft
---

# Lint

Check the code.
`)

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.HasContent() {
		t.Error("expected content after load")
	}

	skills := s.ListSkills()
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
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

func TestStore_Load_MalformedSkillMD(t *testing.T) {
	dir := t.TempDir()

	// Invalid YAML frontmatter
	writeSkillMD(t, dir, "broken", `---
name: [invalid yaml
---

Body.
`)

	// Valid skill alongside the broken one
	writeSkillMD(t, dir, "good", `---
name: good
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
		t.Fatalf("expected 1 skill (broken skipped), got %d", len(skills))
	}
	if skills[0].Name != "good" {
		t.Errorf("expected 'good', got %q", skills[0].Name)
	}
}

func TestStore_Load_MissingSkillMD(t *testing.T) {
	dir := t.TempDir()

	// Directory without SKILL.md
	if err := os.MkdirAll(filepath.Join(dir, "skills", "empty-dir"), 0755); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if s.HasContent() {
		t.Error("expected no content from directory without SKILL.md")
	}
}

func TestStore_Load_NameMismatch(t *testing.T) {
	dir := t.TempDir()

	// Directory name is "deploy" but frontmatter says "build"
	writeSkillMD(t, dir, "deploy", `---
name: build
description: Should use directory name
state: active
---

Body.
`)

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Should use directory name, not frontmatter name
	_, err := s.GetSkill("deploy")
	if err != nil {
		t.Fatalf("expected skill under directory name 'deploy': %v", err)
	}

	_, err = s.GetSkill("build")
	if !errors.Is(err, ErrNotFound) {
		t.Error("expected frontmatter name 'build' to not be used")
	}
}

func TestStore_Load_SkillNameFromDirectory(t *testing.T) {
	dir := t.TempDir()

	// SKILL.md with no name in frontmatter
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

func TestStore_Load_LegacyYAMLWarning(t *testing.T) {
	dir := t.TempDir()

	// Create legacy prompts YAML file
	legacyDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old-prompt.yaml"), []byte("name: old"), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	// Should not error, just log a warning
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
}

func TestStore_Load_CountsSupportingFiles(t *testing.T) {
	dir := t.TempDir()

	skillDir := filepath.Join(dir, "skills", "with-files")
	for _, sub := range []string{"scripts", "references", "assets"} {
		if err := os.MkdirAll(filepath.Join(skillDir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: with-files
description: Skill with supporting files
state: active
---

# With Files
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "lint.sh"), []byte("#!/bin/bash\necho lint"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "test.sh"), []byte("#!/bin/bash\necho test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "GUIDE.md"), []byte("# Guide"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "assets", "template.csv"), []byte("a,b,c"), 0644); err != nil {
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
	// 2 scripts + 1 reference + 1 asset = 4
	if sk.FileCount != 4 {
		t.Errorf("FileCount = %d, want 4", sk.FileCount)
	}
}

func TestStore_Load_SkipsInvalidSkills(t *testing.T) {
	dir := t.TempDir()

	// Invalid skill: name with uppercase (fails validation)
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

// --- CRUD Tests ---

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

func TestStore_SaveSkill_CreatesDirectory(t *testing.T) {
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
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); os.IsNotExist(err) {
		t.Error("SKILL.md was not created")
	}
}

func TestStore_SaveSkill_AtomicWrite(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{
		Name:        "atomic",
		Description: "Test atomic write",
		State:       StateDraft,
	}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	// Verify no .tmp file left behind
	tmpPath := filepath.Join(s.baseDir, "skills", "atomic", "SKILL.md.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("expected no .tmp file after successful write")
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

func TestStore_DeleteSkill_NotFound(t *testing.T) {
	s := newTestStore(t)

	if err := s.DeleteSkill("ghost"); err != nil {
		t.Fatalf("DeleteSkill(ghost): %v", err)
	}
}

func TestStore_ListSkills_CopyOnRead(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "original", Description: "Original", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	skills := s.ListSkills()
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	// Mutate the returned copy
	skills[0].Description = "Mutated"

	// Verify the store's internal copy is unchanged
	got, _ := s.GetSkill("original")
	if got.Description != "Original" {
		t.Errorf("expected 'Original', got %q — copy-on-read violated", got.Description)
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

// --- RenameSkill Tests ---

func TestStore_RenameSkill(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "old-name", Description: "Rename me", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameSkill("old-name", "new-name"); err != nil {
		t.Fatalf("RenameSkill: %v", err)
	}

	// Old name should not exist
	if _, err := s.GetSkill("old-name"); !errors.Is(err, ErrNotFound) {
		t.Error("expected old name to be gone")
	}

	// New name should exist with correct data
	got, err := s.GetSkill("new-name")
	if err != nil {
		t.Fatalf("GetSkill new-name: %v", err)
	}
	if got.Name != "new-name" {
		t.Errorf("expected name 'new-name', got %q", got.Name)
	}
	if got.Description != "Rename me" {
		t.Errorf("expected description 'Rename me', got %q", got.Description)
	}

	// Verify directory was renamed
	oldDir := filepath.Join(s.baseDir, "skills", "old-name")
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("old directory should not exist")
	}
	newDir := filepath.Join(s.baseDir, "skills", "new-name")
	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("new directory should exist: %v", err)
	}

	// Verify SKILL.md has updated name
	s2 := NewStore(s.baseDir)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	got2, err := s2.GetSkill("new-name")
	if err != nil {
		t.Fatalf("GetSkill after reload: %v", err)
	}
	if got2.Name != "new-name" {
		t.Errorf("frontmatter name not updated, got %q", got2.Name)
	}
}

func TestStore_RenameSkill_InvalidNewName(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "original", Description: "Skill", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameSkill("original", "INVALID"); err == nil {
		t.Error("expected error for invalid new name")
	}
}

func TestStore_RenameSkill_ConflictingName(t *testing.T) {
	s := newTestStore(t)

	for _, name := range []string{"skill-a", "skill-b"} {
		sk := &AgentSkill{Name: name, Description: "Skill", State: StateDraft}
		if err := s.SaveSkill(sk); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RenameSkill("skill-a", "skill-b"); err == nil {
		t.Error("expected error for conflicting name")
	}
}

func TestStore_RenameSkill_NotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.RenameSkill("ghost", "new-name")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- File Management Tests ---

func TestStore_ListFiles(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "with-files", Description: "Has files", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	// Create supporting files
	skillDir := filepath.Join(s.baseDir, "skills", "with-files")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.sh"), []byte("#!/bin/bash"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := s.ListFiles("with-files")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	// Should include scripts/ dir and scripts/run.sh, but not SKILL.md
	if len(files) != 2 {
		t.Fatalf("expected 2 entries (dir + file), got %d: %+v", len(files), files)
	}

	hasDir := false
	hasFile := false
	for _, f := range files {
		if f.Path == "scripts" && f.IsDir {
			hasDir = true
		}
		if f.Path == filepath.Join("scripts", "run.sh") && !f.IsDir {
			hasFile = true
		}
	}
	if !hasDir {
		t.Error("expected scripts/ directory in listing")
	}
	if !hasFile {
		t.Error("expected scripts/run.sh in listing")
	}
}

func TestStore_ListFiles_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.ListFiles("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_ReadFile(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "readable", Description: "Has files", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	// Create a file to read
	skillDir := filepath.Join(s.baseDir, "skills", "readable")
	refsDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Style Guide\n\nUse consistent formatting.\n")
	if err := os.WriteFile(filepath.Join(refsDir, "STYLE.md"), content, 0644); err != nil {
		t.Fatal(err)
	}

	data, err := s.ReadFile("readable", filepath.Join("references", "STYLE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q", string(data))
	}
}

func TestStore_ReadFile_NotFound(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "test", Description: "Test", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	_, err := s.ReadFile("test", "scripts/nonexistent.sh")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_WriteFile(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "writable", Description: "Can write", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	content := []byte("#!/bin/bash\necho hello\n")
	if err := s.WriteFile("writable", filepath.Join("scripts", "hello.sh"), content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verify on disk
	path := filepath.Join(s.baseDir, "skills", "writable", "scripts", "hello.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q", string(data))
	}

	// Verify file count updated
	got, _ := s.GetSkill("writable")
	if got.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", got.FileCount)
	}
}

func TestStore_DeleteFile(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "deletable", Description: "Can delete", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	// Create and then delete a file
	content := []byte("temp content")
	if err := s.WriteFile("deletable", filepath.Join("scripts", "temp.sh"), content); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteFile("deletable", filepath.Join("scripts", "temp.sh")); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	// Verify file is gone
	path := filepath.Join(s.baseDir, "skills", "deletable", "scripts", "temp.sh")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}

	// Verify file count updated
	got, _ := s.GetSkill("deletable")
	if got.FileCount != 0 {
		t.Errorf("FileCount = %d, want 0", got.FileCount)
	}
}

func TestStore_DeleteFile_NotFound(t *testing.T) {
	s := newTestStore(t)

	sk := &AgentSkill{Name: "test", Description: "Test", State: StateDraft}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	err := s.DeleteFile("test", "scripts/ghost.sh")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- Path Traversal Prevention Tests ---

func TestStore_SafeFilePath_TraversalPrevention(t *testing.T) {
	s := newTestStore(t)

	tests := []struct {
		name     string
		skill    string
		path     string
		wantErr  bool
	}{
		{
			name:    "parent directory traversal",
			skill:   "test",
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "nested traversal",
			skill:   "test",
			path:    "scripts/../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path",
			skill:   "test",
			path:    "/etc/passwd",
			wantErr: true,
		},
		{
			name:    "dot-dot prefix",
			skill:   "test",
			path:    "..secret",
			wantErr: true, // rejected by prefix check (defense in depth)
		},
		{
			name:    "skill name with path separator",
			skill:   "../evil",
			path:    "file.txt",
			wantErr: true,
		},
		{
			name:    "valid path",
			skill:   "test",
			path:    "scripts/lint.sh",
			wantErr: false,
		},
		{
			name:    "valid nested path",
			skill:   "test",
			path:    "references/deep/GUIDE.md",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.safeFilePath(tt.skill, tt.path)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for skill=%q path=%q", tt.skill, tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for skill=%q path=%q: %v", tt.skill, tt.path, err)
			}
		})
	}
}

// --- Concurrency Tests ---

func TestStore_ConcurrentAccess(t *testing.T) {
	s := newTestStore(t)

	// Seed with initial skills
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("skill-%d", i)
		sk := &AgentSkill{Name: name, Description: "Concurrent " + name, State: StateActive}
		if err := s.SaveSkill(sk); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = s.ListSkills()
				_, _ = s.GetSkill("skill-0")
				_ = s.ActiveSkills()
				_ = s.Status()
				_ = s.HasContent()
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%d", id)
			sk := &AgentSkill{Name: name, Description: "Written concurrently", State: StateDraft}
			if err := s.SaveSkill(sk); err != nil {
				errCh <- err
				return
			}
			sk.State = StateActive
			if err := s.SaveSkill(sk); err != nil {
				errCh <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}

	// All skills should be present
	skills := s.ListSkills()
	if len(skills) != 10 { // 5 initial + 5 concurrent
		t.Errorf("expected 10 skills, got %d", len(skills))
	}
}

// --- countSupportingFiles Tests ---

func TestCountSupportingFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	if count := countSupportingFiles(dir); count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountSupportingFiles_Nonexistent(t *testing.T) {
	if count := countSupportingFiles("/nonexistent/path"); count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountSupportingFiles_IgnoresSubdirectories(t *testing.T) {
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "a.sh"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	// nested/ directory should not be counted (only files)
	if count := countSupportingFiles(dir); count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}
