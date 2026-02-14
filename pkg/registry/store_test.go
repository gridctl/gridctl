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

	// Write prompt YAML
	promptDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}
	promptYAML := `name: greeting
description: A greeting prompt
content: "Hello {{name}}"
state: active
`
	if err := os.WriteFile(filepath.Join(promptDir, "greeting.yaml"), []byte(promptYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Write skill YAML
	skillDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillYAML := `name: deploy
description: Deploy workflow
steps:
  - tool: docker__build
  - tool: docker__push
state: active
`
	if err := os.WriteFile(filepath.Join(skillDir, "deploy.yaml"), []byte(skillYAML), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !s.HasContent() {
		t.Error("expected content after load")
	}

	p, err := s.GetPrompt("greeting")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if p.Content != "Hello {{name}}" {
		t.Errorf("unexpected content: %s", p.Content)
	}
	if p.State != StateActive {
		t.Errorf("expected active state, got %s", p.State)
	}

	sk, err := s.GetSkill("deploy")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if len(sk.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(sk.Steps))
	}
}

func TestStore_Load_SkipsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	promptDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write an invalid YAML file (missing required content)
	invalidYAML := `name: bad-prompt
state: active
`
	if err := os.WriteFile(filepath.Join(promptDir, "bad.yaml"), []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a valid prompt alongside
	validYAML := `name: good-prompt
content: "hello"
state: draft
`
	if err := os.WriteFile(filepath.Join(promptDir, "good.yaml"), []byte(validYAML), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Only the valid prompt should be loaded
	prompts := s.ListPrompts()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "good-prompt" {
		t.Errorf("expected good-prompt, got %s", prompts[0].Name)
	}
}

func TestStore_SavePrompt(t *testing.T) {
	s := newTestStore(t)

	p := &Prompt{
		Name:    "test-prompt",
		Content: "Hello world",
		State:   StateActive,
	}
	if err := s.SavePrompt(p); err != nil {
		t.Fatalf("SavePrompt: %v", err)
	}

	// Verify in memory
	got, err := s.GetPrompt("test-prompt")
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if got.Content != "Hello world" {
		t.Errorf("unexpected content: %s", got.Content)
	}

	// Verify on disk by reloading
	s2 := NewStore(s.baseDir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got2, err := s2.GetPrompt("test-prompt")
	if err != nil {
		t.Fatalf("GetPrompt after reload: %v", err)
	}
	if got2.Content != "Hello world" {
		t.Errorf("unexpected content after reload: %s", got2.Content)
	}
}

func TestStore_SavePrompt_CreatesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "registry")
	s := NewStore(dir)

	p := &Prompt{
		Name:    "test",
		Content: "content",
		State:   StateDraft,
	}
	if err := s.SavePrompt(p); err != nil {
		t.Fatalf("SavePrompt: %v", err)
	}

	// Verify the directory structure was created
	promptDir := filepath.Join(dir, "prompts")
	if _, err := os.Stat(promptDir); os.IsNotExist(err) {
		t.Error("prompts directory was not created")
	}
}

func TestStore_SavePrompt_ValidationError(t *testing.T) {
	s := newTestStore(t)

	p := &Prompt{Name: "", Content: "content"}
	if err := s.SavePrompt(p); err == nil {
		t.Error("expected validation error for empty name")
	}
}

func TestStore_SavePrompt_Update(t *testing.T) {
	s := newTestStore(t)

	p := &Prompt{Name: "test", Content: "v1", State: StateDraft}
	if err := s.SavePrompt(p); err != nil {
		t.Fatalf("SavePrompt v1: %v", err)
	}

	p.Content = "v2"
	p.State = StateActive
	if err := s.SavePrompt(p); err != nil {
		t.Fatalf("SavePrompt v2: %v", err)
	}

	got, _ := s.GetPrompt("test")
	if got.Content != "v2" {
		t.Errorf("expected v2, got %s", got.Content)
	}
	if got.State != StateActive {
		t.Errorf("expected active, got %s", got.State)
	}
}

func TestStore_DeletePrompt(t *testing.T) {
	s := newTestStore(t)

	p := &Prompt{Name: "to-delete", Content: "bye", State: StateDraft}
	if err := s.SavePrompt(p); err != nil {
		t.Fatalf("SavePrompt: %v", err)
	}

	if err := s.DeletePrompt("to-delete"); err != nil {
		t.Fatalf("DeletePrompt: %v", err)
	}

	if _, err := s.GetPrompt("to-delete"); err == nil {
		t.Error("expected not-found error after delete")
	}

	// Verify file is gone
	path := s.promptPath("to-delete")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestStore_DeletePrompt_Nonexistent(t *testing.T) {
	s := newTestStore(t)

	// Deleting a nonexistent prompt should not error
	if err := s.DeletePrompt("ghost"); err != nil {
		t.Fatalf("DeletePrompt(ghost): %v", err)
	}
}

func TestStore_GetPrompt_NotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetPrompt("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent prompt")
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

	p := &Prompt{Name: "test", Content: "content", State: StateDraft}
	if err := s.SavePrompt(p); err != nil {
		t.Fatal(err)
	}

	if !s.HasContent() {
		t.Error("expected content after save")
	}
}

func TestStore_ActivePrompts(t *testing.T) {
	s := newTestStore(t)

	prompts := []*Prompt{
		{Name: "draft-prompt", Content: "draft", State: StateDraft},
		{Name: "active-prompt", Content: "active", State: StateActive},
		{Name: "disabled-prompt", Content: "disabled", State: StateDisabled},
		{Name: "another-active", Content: "active2", State: StateActive},
	}
	for _, p := range prompts {
		if err := s.SavePrompt(p); err != nil {
			t.Fatalf("SavePrompt(%s): %v", p.Name, err)
		}
	}

	active := s.ActivePrompts()
	if len(active) != 2 {
		t.Fatalf("expected 2 active prompts, got %d", len(active))
	}

	names := map[string]bool{}
	for _, p := range active {
		names[p.Name] = true
	}
	if !names["active-prompt"] || !names["another-active"] {
		t.Errorf("unexpected active prompts: %v", names)
	}
}

func TestStore_SaveSkill(t *testing.T) {
	s := newTestStore(t)

	sk := &Skill{
		Name:  "deploy",
		Steps: []Step{{Tool: "docker__build"}, {Tool: "docker__push"}},
		State: StateActive,
	}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	got, err := s.GetSkill("deploy")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if len(got.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(got.Steps))
	}

	// Verify persistence
	s2 := NewStore(s.baseDir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got2, err := s2.GetSkill("deploy")
	if err != nil {
		t.Fatalf("GetSkill after reload: %v", err)
	}
	if got2.Steps[0].Tool != "docker__build" {
		t.Errorf("unexpected first step tool: %s", got2.Steps[0].Tool)
	}
}

func TestStore_SaveSkill_CreatesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "registry")
	s := NewStore(dir)

	sk := &Skill{
		Name:  "test",
		Steps: []Step{{Tool: "exec"}},
		State: StateDraft,
	}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}

	skillDir := filepath.Join(dir, "skills")
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		t.Error("skills directory was not created")
	}
}

func TestStore_DeleteSkill(t *testing.T) {
	s := newTestStore(t)

	sk := &Skill{
		Name:  "to-delete",
		Steps: []Step{{Tool: "test"}},
		State: StateDraft,
	}
	if err := s.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteSkill("to-delete"); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}

	if _, err := s.GetSkill("to-delete"); err == nil {
		t.Error("expected not-found error after delete")
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

func TestStore_ActiveSkills(t *testing.T) {
	s := newTestStore(t)

	skills := []*Skill{
		{Name: "draft-skill", Steps: []Step{{Tool: "a"}}, State: StateDraft},
		{Name: "active-skill", Steps: []Step{{Tool: "b"}}, State: StateActive},
		{Name: "disabled-skill", Steps: []Step{{Tool: "c"}}, State: StateDisabled},
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

func TestStore_Status(t *testing.T) {
	s := newTestStore(t)

	// Empty store
	st := s.Status()
	if st.TotalPrompts != 0 || st.ActivePrompts != 0 || st.TotalSkills != 0 || st.ActiveSkills != 0 {
		t.Errorf("expected all zeros, got %+v", st)
	}

	// Add some items
	prompts := []*Prompt{
		{Name: "p1", Content: "c1", State: StateActive},
		{Name: "p2", Content: "c2", State: StateDraft},
		{Name: "p3", Content: "c3", State: StateActive},
	}
	for _, p := range prompts {
		if err := s.SavePrompt(p); err != nil {
			t.Fatal(err)
		}
	}

	skills := []*Skill{
		{Name: "s1", Steps: []Step{{Tool: "t1"}}, State: StateActive},
		{Name: "s2", Steps: []Step{{Tool: "t2"}}, State: StateDisabled},
	}
	for _, sk := range skills {
		if err := s.SaveSkill(sk); err != nil {
			t.Fatal(err)
		}
	}

	st = s.Status()
	if st.TotalPrompts != 3 {
		t.Errorf("expected 3 total prompts, got %d", st.TotalPrompts)
	}
	if st.ActivePrompts != 2 {
		t.Errorf("expected 2 active prompts, got %d", st.ActivePrompts)
	}
	if st.TotalSkills != 2 {
		t.Errorf("expected 2 total skills, got %d", st.TotalSkills)
	}
	if st.ActiveSkills != 1 {
		t.Errorf("expected 1 active skill, got %d", st.ActiveSkills)
	}
}

func TestStore_ListPrompts(t *testing.T) {
	s := newTestStore(t)

	if got := s.ListPrompts(); len(got) != 0 {
		t.Errorf("expected 0 prompts, got %d", len(got))
	}

	for _, name := range []string{"alpha", "beta"} {
		p := &Prompt{Name: name, Content: "c", State: StateDraft}
		if err := s.SavePrompt(p); err != nil {
			t.Fatal(err)
		}
	}

	if got := s.ListPrompts(); len(got) != 2 {
		t.Errorf("expected 2 prompts, got %d", len(got))
	}
}

func TestStore_ListSkills(t *testing.T) {
	s := newTestStore(t)

	if got := s.ListSkills(); len(got) != 0 {
		t.Errorf("expected 0 skills, got %d", len(got))
	}

	for _, name := range []string{"x", "y", "z"} {
		sk := &Skill{Name: name, Steps: []Step{{Tool: "t"}}, State: StateDraft}
		if err := s.SaveSkill(sk); err != nil {
			t.Fatal(err)
		}
	}

	if got := s.ListSkills(); len(got) != 3 {
		t.Errorf("expected 3 skills, got %d", len(got))
	}
}
