package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// writeTestSkill writes a SKILL.md file in the directory-based layout for tests.
func writeTestSkill(t *testing.T, baseDir, skillName, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, "skills", skillName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
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

	writeTestSkill(t, dir, "audit-repo", `---
name: audit-repo
description: Audit a repository
state: active
---

# Audit

Run the audit steps.
`)
	writeTestSkill(t, dir, "draft-skill", `---
name: draft-skill
description: A draft skill
state: draft
---

Draft body.
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
}

func TestServer_Tools(t *testing.T) {
	dir := t.TempDir()

	writeTestSkill(t, dir, "active-skill", `---
name: active-skill
description: Active
state: active
---

Active body.
`)
	writeTestSkill(t, dir, "draft-skill", `---
name: draft-skill
description: Draft
state: draft
---

Draft body.
`)
	writeTestSkill(t, dir, "disabled-skill", `---
name: disabled-skill
description: Disabled
state: disabled
---

Disabled body.
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

	writeTestSkill(t, dir, "skill1", `---
name: skill1
description: First skill
state: active
---

Body.
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
	writeTestSkill(t, dir, "skill2", `---
name: skill2
description: Second skill
state: active
---

Body 2.
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

	writeTestSkill(t, dir, "test", `---
name: test
description: Test skill
state: draft
---

Body.
`)

	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !srv.HasContent() {
		t.Error("expected content after loading skill")
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

func TestServer_CallTool_SkillNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = srv.store.Load()

	result, err := srv.CallTool(context.Background(), "nonexistent", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestServer_CallTool_InactiveSkill(t *testing.T) {
	dir := t.TempDir()

	writeTestSkill(t, dir, "draft-skill", `---
name: draft-skill
description: A draft skill
state: draft
---

Body.
`)

	store := NewStore(dir)
	_ = store.Load()
	srv := New(store, nil)

	result, err := srv.CallTool(context.Background(), "draft-skill", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestServer_CallTool_Success(t *testing.T) {
	dir := t.TempDir()

	writeTestSkill(t, dir, "active-skill", `---
name: active-skill
description: Active skill
state: active
---

# Instructions

Do the thing.
`)

	store := NewStore(dir)
	_ = store.Load()
	srv := New(store, nil)

	result, err := srv.CallTool(context.Background(), "active-skill", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success result")
	}
	// Should return the body content
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
	if result.Content[0].Type != "text" {
		t.Errorf("content type = %q, want %q", result.Content[0].Type, "text")
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

func TestServer_SkillToTool(t *testing.T) {
	sk := &AgentSkill{
		Name:        "simple",
		Description: "A simple skill",
		State:       StateActive,
	}

	tool := skillToTool(sk)
	if tool.Name != "simple" {
		t.Errorf("name = %q, want %q", tool.Name, "simple")
	}
	if tool.Description != "A simple skill" {
		t.Errorf("description = %q, want %q", tool.Description, "A simple skill")
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
}
