package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(dir)
	server := New(store)
	return server, dir
}

func writeTestSkill(t *testing.T, dir, name, description, body string, state ItemState) {
	t.Helper()
	skillDir := filepath.Join(dir, "skills", name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\nstate: %s\n---\n\n%s",
		name, description, state, body)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- Initialization ---

func TestServer_Initialize(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "audit-repo", "Audit a repository", "# Audit\n\nRun the audit steps.", StateActive)
	writeTestSkill(t, dir, "draft-skill", "A draft skill", "Draft body.", StateDraft)

	if srv.IsInitialized() {
		t.Error("expected not initialized before Initialize()")
	}

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}

	if !srv.IsInitialized() {
		t.Error("expected initialized after Initialize()")
	}
}

func TestServer_Initialize_EmptyStore(t *testing.T) {
	srv, _ := setupTestServer(t)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() with empty store error: %v", err)
	}

	if !srv.IsInitialized() {
		t.Error("expected initialized after Initialize() with empty store")
	}
}

// --- PromptProvider ---

func TestServer_ListPromptData_ReturnsActiveSkills(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "active-skill", "Active", "Active body.", StateActive)
	writeTestSkill(t, dir, "draft-skill", "Draft", "Draft body.", StateDraft)
	writeTestSkill(t, dir, "disabled-skill", "Disabled", "Disabled body.", StateDisabled)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	prompts := srv.ListPromptData()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt (active only), got %d", len(prompts))
	}
	if prompts[0].Name != "active-skill" {
		t.Errorf("expected name 'active-skill', got %q", prompts[0].Name)
	}
}

func TestServer_ListPromptData_EmptyRegistry(t *testing.T) {
	srv, _ := setupTestServer(t)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	prompts := srv.ListPromptData()
	if len(prompts) != 0 {
		t.Errorf("expected 0 prompts for empty registry, got %d", len(prompts))
	}
}

func TestServer_ListPromptData_SkillContent(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "code-review", "Review code for issues", "# Code Review\n\nCheck for bugs and style issues.", StateActive)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	prompts := srv.ListPromptData()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	p := prompts[0]
	if p.Name != "code-review" {
		t.Errorf("Name = %q, want %q", p.Name, "code-review")
	}
	if p.Description != "Review code for issues" {
		t.Errorf("Description = %q, want %q", p.Description, "Review code for issues")
	}
	if p.Content != "# Code Review\n\nCheck for bugs and style issues." {
		t.Errorf("Content = %q, want skill body", p.Content)
	}
}

func TestServer_ListPromptData_HasContextArgument(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "test-skill", "Test", "Body.", StateActive)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	prompts := srv.ListPromptData()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(prompts))
	}

	args := prompts[0].Arguments
	if len(args) != 1 {
		t.Fatalf("expected 1 argument, got %d", len(args))
	}
	if args[0].Name != "context" {
		t.Errorf("argument name = %q, want %q", args[0].Name, "context")
	}
	if args[0].Required {
		t.Error("expected context argument to be optional")
	}
}

func TestServer_GetPromptData_ActiveSkill(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "active-skill", "Active skill", "# Instructions\n\nDo the thing.", StateActive)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	p, err := srv.GetPromptData("active-skill")
	if err != nil {
		t.Fatalf("GetPromptData() error: %v", err)
	}
	if p.Name != "active-skill" {
		t.Errorf("Name = %q, want %q", p.Name, "active-skill")
	}
	if p.Description != "Active skill" {
		t.Errorf("Description = %q, want %q", p.Description, "Active skill")
	}
	if p.Content != "# Instructions\n\nDo the thing." {
		t.Errorf("Content = %q, want skill body", p.Content)
	}
	if len(p.Arguments) != 1 || p.Arguments[0].Name != "context" {
		t.Errorf("expected context argument, got %v", p.Arguments)
	}
}

func TestServer_GetPromptData_InactiveSkill(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "draft-skill", "A draft", "Draft body.", StateDraft)
	writeTestSkill(t, dir, "disabled-skill", "Disabled", "Disabled body.", StateDisabled)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, err := srv.GetPromptData("draft-skill")
	if err == nil {
		t.Error("expected error for draft skill")
	}

	_, err = srv.GetPromptData("disabled-skill")
	if err == nil {
		t.Error("expected error for disabled skill")
	}
}

func TestServer_GetPromptData_NotFound(t *testing.T) {
	srv, _ := setupTestServer(t)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, err := srv.GetPromptData("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

// --- AgentClient ---

func TestServer_Tools_ReturnsEmpty(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "active-skill", "Active", "Body.", StateActive)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	tools := srv.Tools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools (skills are not tools), got %d", len(tools))
	}
}

func TestServer_CallTool_ReturnsError(t *testing.T) {
	srv, _ := setupTestServer(t)

	result, err := srv.CallTool(context.Background(), "anything", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result from CallTool")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in error result")
	}
	if result.Content[0].Text == "" {
		t.Error("expected explanation text in error result")
	}
}

func TestServer_Name(t *testing.T) {
	srv, _ := setupTestServer(t)
	if got := srv.Name(); got != "registry" {
		t.Errorf("Name() = %q, want %q", got, "registry")
	}
}

func TestServer_ServerInfo(t *testing.T) {
	srv, _ := setupTestServer(t)

	info := srv.ServerInfo()
	if info.Name != "registry" {
		t.Errorf("Name = %q, want %q", info.Name, "registry")
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.0.0")
	}
}

// --- Integration ---

func TestServer_HasContent(t *testing.T) {
	srv, dir := setupTestServer(t)

	if srv.HasContent() {
		t.Error("expected no content initially")
	}

	writeTestSkill(t, dir, "test", "Test skill", "Body.", StateDraft)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !srv.HasContent() {
		t.Error("expected content after loading skill")
	}
}

func TestServer_RefreshTools_ReloadsStore(t *testing.T) {
	srv, dir := setupTestServer(t)

	writeTestSkill(t, dir, "skill1", "First skill", "Body.", StateActive)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(srv.ListPromptData()) != 1 {
		t.Fatalf("expected 1 prompt after init, got %d", len(srv.ListPromptData()))
	}

	// Add a new active skill to disk
	writeTestSkill(t, dir, "skill2", "Second skill", "Body 2.", StateActive)

	if err := srv.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools() error: %v", err)
	}

	if len(srv.ListPromptData()) != 2 {
		t.Errorf("expected 2 prompts after refresh, got %d", len(srv.ListPromptData()))
	}
}

func TestServer_Store(t *testing.T) {
	store := NewStore(t.TempDir())
	srv := New(store)

	if srv.Store() != store {
		t.Error("Store() should return the underlying store")
	}
}

func TestServer_ImplementsInterfaces(t *testing.T) {
	var _ mcp.AgentClient = (*Server)(nil)
	var _ mcp.PromptProvider = (*Server)(nil)
}
