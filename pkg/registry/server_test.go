package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// writeSkillYAML writes a skill YAML file to the given directory.
func writeSkillYAML(t *testing.T, dir, filename, content string) {
	t.Helper()
	skillDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writePromptYAML writes a prompt YAML file to the given directory.
func writePromptYAML(t *testing.T, dir, filename, content string) {
	t.Helper()
	promptDir := filepath.Join(dir, "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(dir)
	srv := New(store, nil)
	return srv, dir
}

func TestNew(t *testing.T) {
	store := NewStore(t.TempDir())
	srv := New(store, nil)

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.store != store {
		t.Error("store not set")
	}
	if srv.toolCaller != nil {
		t.Error("expected nil toolCaller")
	}
}

func TestServer_Name(t *testing.T) {
	srv, _ := newTestServer(t)
	if got := srv.Name(); got != "registry" {
		t.Errorf("Name() = %q, want %q", got, "registry")
	}
}

func TestServer_Initialize(t *testing.T) {
	dir := t.TempDir()

	writeSkillYAML(t, dir, "audit.yaml", `name: audit-repo
description: Audit a repository
steps:
  - tool: git__log
  - tool: lint__check
input:
  - name: repo
    description: Repository URL
    required: true
state: active
`)
	writeSkillYAML(t, dir, "draft-skill.yaml", `name: draft-skill
description: A draft skill
steps:
  - tool: noop
state: draft
`)
	writePromptYAML(t, dir, "greeting.yaml", `name: greeting
description: A greeting prompt
content: "Hello {{name}}"
state: active
`)

	store := NewStore(dir)
	srv := New(store, nil)

	if srv.IsInitialized() {
		t.Error("expected not initialized before Initialize()")
	}

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}

	if !srv.IsInitialized() {
		t.Error("expected initialized after Initialize()")
	}

	// Should have 1 tool (only active skills)
	tools := srv.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "audit-repo" {
		t.Errorf("tool name = %q, want %q", tools[0].Name, "audit-repo")
	}
	if tools[0].Description != "Audit a repository" {
		t.Errorf("tool description = %q, want %q", tools[0].Description, "Audit a repository")
	}

	// Verify input schema
	var schema mcp.InputSchemaObject
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("schema type = %q, want %q", schema.Type, "object")
	}
	repoProp, ok := schema.Properties["repo"]
	if !ok {
		t.Fatal("expected 'repo' property in schema")
	}
	if repoProp.Type != "string" {
		t.Errorf("repo type = %q, want %q", repoProp.Type, "string")
	}
	if repoProp.Description != "Repository URL" {
		t.Errorf("repo description = %q, want %q", repoProp.Description, "Repository URL")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "repo" {
		t.Errorf("required = %v, want [repo]", schema.Required)
	}
}

func TestServer_Tools(t *testing.T) {
	dir := t.TempDir()

	writeSkillYAML(t, dir, "active.yaml", `name: active-skill
description: Active
steps:
  - tool: exec
state: active
`)
	writeSkillYAML(t, dir, "draft.yaml", `name: draft-skill
description: Draft
steps:
  - tool: exec
state: draft
`)
	writeSkillYAML(t, dir, "disabled.yaml", `name: disabled-skill
description: Disabled
steps:
  - tool: exec
state: disabled
`)

	store := NewStore(dir)
	srv := New(store, nil)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	tools := srv.Tools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (active only), got %d", len(tools))
	}
	if len(tools) > 0 && tools[0].Name != "active-skill" {
		t.Errorf("expected active-skill, got %s", tools[0].Name)
	}
}

func TestServer_RefreshTools(t *testing.T) {
	dir := t.TempDir()

	writeSkillYAML(t, dir, "skill1.yaml", `name: skill1
description: First skill
steps:
  - tool: exec
state: active
`)

	store := NewStore(dir)
	srv := New(store, nil)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(srv.Tools()) != 1 {
		t.Fatalf("expected 1 tool after init, got %d", len(srv.Tools()))
	}

	// Add a new active skill to disk
	writeSkillYAML(t, dir, "skill2.yaml", `name: skill2
description: Second skill
steps:
  - tool: deploy
state: active
`)

	if err := srv.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools() error: %v", err)
	}

	if len(srv.Tools()) != 2 {
		t.Errorf("expected 2 tools after refresh, got %d", len(srv.Tools()))
	}
}

func TestServer_HasContent(t *testing.T) {
	srv, dir := newTestServer(t)

	if srv.HasContent() {
		t.Error("expected no content initially")
	}

	writePromptYAML(t, dir, "test.yaml", `name: test
description: Test prompt
content: "hello"
state: draft
`)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !srv.HasContent() {
		t.Error("expected content after loading prompt")
	}
}

func TestServer_Prompts(t *testing.T) {
	dir := t.TempDir()

	writePromptYAML(t, dir, "active.yaml", `name: active-prompt
description: Active prompt
content: "hello"
state: active
`)
	writePromptYAML(t, dir, "draft.yaml", `name: draft-prompt
description: Draft prompt
content: "bye"
state: draft
`)

	store := NewStore(dir)
	srv := New(store, nil)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	prompts := srv.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("expected 1 active prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "active-prompt" {
		t.Errorf("expected active-prompt, got %s", prompts[0].Name)
	}
}

func TestServer_GetPrompt(t *testing.T) {
	dir := t.TempDir()

	writePromptYAML(t, dir, "greeting.yaml", `name: greeting
description: A greeting
content: "Hello {{name}}"
state: active
`)

	store := NewStore(dir)
	srv := New(store, nil)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	p, err := srv.GetPrompt("greeting")
	if err != nil {
		t.Fatalf("GetPrompt() error: %v", err)
	}
	if p.Content != "Hello {{name}}" {
		t.Errorf("content = %q, want %q", p.Content, "Hello {{name}}")
	}

	_, err = srv.GetPrompt("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent prompt")
	}
}

func TestServer_IsInitialized(t *testing.T) {
	srv, _ := newTestServer(t)

	if srv.IsInitialized() {
		t.Error("expected false before Initialize()")
	}

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !srv.IsInitialized() {
		t.Error("expected true after Initialize()")
	}
}

func TestServer_ServerInfo(t *testing.T) {
	srv, _ := newTestServer(t)

	info := srv.ServerInfo()
	if info.Name != "registry" {
		t.Errorf("Name = %q, want %q", info.Name, "registry")
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.0.0")
	}
}

func TestServer_CallTool_NoToolCaller(t *testing.T) {
	srv, _ := newTestServer(t)

	result, err := srv.CallTool(context.Background(), "any-skill", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("expected error result when toolCaller is nil")
	}
}

func TestServer_Store(t *testing.T) {
	store := NewStore(t.TempDir())
	srv := New(store, nil)

	if srv.Store() != store {
		t.Error("Store() should return the underlying store")
	}
}

func TestServer_ImplementsAgentClient(t *testing.T) {
	var _ mcp.AgentClient = (*Server)(nil) // compile-time check
}

func TestServer_SkillToTool_NoInput(t *testing.T) {
	sk := &Skill{
		Name:        "simple",
		Description: "A simple skill",
		Steps:       []Step{{Tool: "exec"}},
		State:       StateActive,
	}

	tool := skillToTool(sk)
	if tool.Name != "simple" {
		t.Errorf("name = %q, want %q", tool.Name, "simple")
	}

	var schema mcp.InputSchemaObject
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("type = %q, want %q", schema.Type, "object")
	}
	if len(schema.Properties) != 0 {
		t.Errorf("expected 0 properties, got %d", len(schema.Properties))
	}
	if len(schema.Required) != 0 {
		t.Errorf("expected 0 required, got %d", len(schema.Required))
	}
}

func TestServer_SkillToTool_WithInput(t *testing.T) {
	sk := &Skill{
		Name:        "deploy",
		Description: "Deploy workflow",
		Steps:       []Step{{Tool: "docker__build"}},
		Input: []Argument{
			{Name: "target", Description: "Deploy target", Required: true},
			{Name: "dry-run", Description: "Skip actual deploy", Required: false},
		},
		State: StateActive,
	}

	tool := skillToTool(sk)

	var schema mcp.InputSchemaObject
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(schema.Properties) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(schema.Properties))
	}

	targetProp := schema.Properties["target"]
	if targetProp.Type != "string" {
		t.Errorf("target type = %q, want %q", targetProp.Type, "string")
	}
	if targetProp.Description != "Deploy target" {
		t.Errorf("target description = %q, want %q", targetProp.Description, "Deploy target")
	}

	dryRunProp := schema.Properties["dry-run"]
	if dryRunProp.Description != "Skip actual deploy" {
		t.Errorf("dry-run description = %q, want %q", dryRunProp.Description, "Skip actual deploy")
	}

	if len(schema.Required) != 1 || schema.Required[0] != "target" {
		t.Errorf("required = %v, want [target]", schema.Required)
	}
}
