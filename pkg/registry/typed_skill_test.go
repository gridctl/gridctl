package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/skill"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// stubRegistry stands in for *skill.Registry without dragging the
// pkg/agent dependency into the registry tests. The contract is the
// SkillRegistry interface in server.go — implement Tools() / CallTool().
type stubRegistry struct {
	tools map[string]mcp.Tool
	calls []string
	resp  *mcp.ToolCallResult
	err   error
}

func (r *stubRegistry) Tools() []mcp.Tool {
	out := make([]mcp.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *stubRegistry) CallTool(_ context.Context, name string, _ map[string]any) (*mcp.ToolCallResult, error) {
	r.calls = append(r.calls, name)
	if _, ok := r.tools[name]; !ok {
		return nil, fmt.Errorf("%w: %q", skill.ErrSkillNotRegistered, name)
	}
	return r.resp, r.err
}

type stubTSDispatcher struct {
	calls []dispatchCall
	resp  *mcp.ToolCallResult
	err   error
}

type dispatchCall struct {
	Name       string
	SourcePath string
	Arguments  map[string]any
}

func (d *stubTSDispatcher) Dispatch(_ context.Context, name, sourcePath string, args map[string]any) (*mcp.ToolCallResult, error) {
	d.calls = append(d.calls, dispatchCall{Name: name, SourcePath: sourcePath, Arguments: args})
	return d.resp, d.err
}

// writeTSSkill writes both SKILL.md and a sibling skill.ts so the
// walker recognises the typed handler.
func writeTSSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, "skills", name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: ts skill\nstate: active\n---\n\nbody"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.ts"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeGoSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, "skills", name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: go skill\nstate: active\n---\n\nbody"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStore_DetectsTSHandlerSibling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	writeTSSkill(t, dir, "hello", "export default async function() { return {}; }")

	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	sk, err := store.GetSkill("hello")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if sk.HandlerLanguage != "ts" {
		t.Errorf("HandlerLanguage = %q, want ts", sk.HandlerLanguage)
	}
	if sk.HandlerPath != "skill.ts" {
		t.Errorf("HandlerPath = %q, want skill.ts", sk.HandlerPath)
	}

	abs, ok := store.HandlerPath("hello")
	if !ok {
		t.Fatal("HandlerPath returned not ok")
	}
	if !strings.HasSuffix(abs, "skills/hello/skill.ts") {
		t.Errorf("HandlerPath abs = %q", abs)
	}
}

func TestStore_DetectsGoHandlerSibling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	writeGoSkill(t, dir, "hello-go", "package main")

	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	sk, err := store.GetSkill("hello-go")
	if err != nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if sk.HandlerLanguage != "go" || sk.HandlerPath != "skill.go" {
		t.Errorf("Handler = %q, %q", sk.HandlerLanguage, sk.HandlerPath)
	}
}

func TestStore_PrefersGoHandlerWhenBothPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)

	skillDir := filepath.Join(dir, "skills", "both")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: both\ndescription: x\nstate: active\n---\n\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.go"), []byte("package x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.ts"), []byte("export default async function(){}"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	sk, _ := store.GetSkill("both")
	if sk.HandlerLanguage != "go" {
		t.Errorf("HandlerLanguage = %q, want go", sk.HandlerLanguage)
	}
}

func TestStore_HandlerPath_ReturnsFalseForMarkdownOnlySkill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	writeTestSkill(t, dir, "doc", "doc only", "body", StateActive)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.HandlerPath("doc"); ok {
		t.Error("HandlerPath returned true for markdown-only skill")
	}
	if _, ok := store.HandlerPath("nonexistent"); ok {
		t.Error("HandlerPath returned true for nonexistent skill")
	}
}

// --- Server.Tools / Server.CallTool ---

func TestServer_Tools_IncludesProgrammaticSkills(t *testing.T) {
	t.Parallel()
	srv, _ := setupTestServer(t)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	srv.SetSkillRegistry(&stubRegistry{
		tools: map[string]mcp.Tool{
			"go-skill": {Name: "go-skill", Description: "from registry", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})

	tools := srv.Tools()
	if len(tools) != 1 || tools[0].Name != "go-skill" {
		t.Errorf("Tools = %+v, want one go-skill entry", tools)
	}
}

func TestServer_Tools_IncludesTSHandlerSkillsFromStore(t *testing.T) {
	t.Parallel()
	srv, dir := setupTestServer(t)
	writeTSSkill(t, dir, "hello", "export default async function() { return {}; }")
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	srv.SetTSDispatcher(&stubTSDispatcher{})

	tools := srv.Tools()
	if len(tools) != 1 || tools[0].Name != "hello" {
		t.Errorf("Tools = %+v", tools)
	}
	if !strings.Contains(string(tools[0].InputSchema), "object") {
		t.Errorf("InputSchema = %s", tools[0].InputSchema)
	}
}

func TestServer_Tools_DeduplicatesAcrossSources(t *testing.T) {
	t.Parallel()
	srv, dir := setupTestServer(t)
	writeTSSkill(t, dir, "shared", "export default async function() { return {}; }")
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	srv.SetSkillRegistry(&stubRegistry{
		tools: map[string]mcp.Tool{
			"shared": {Name: "shared", Description: "registry wins"},
		},
	})
	srv.SetTSDispatcher(&stubTSDispatcher{})

	tools := srv.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %+v", tools)
	}
	if tools[0].Description != "registry wins" {
		t.Errorf("dedupe favored TS instead of programmatic registry: %+v", tools[0])
	}
}

func TestServer_Tools_SortsByName(t *testing.T) {
	t.Parallel()
	srv, dir := setupTestServer(t)
	writeTSSkill(t, dir, "zebra", "export default async function() { return {}; }")
	writeTSSkill(t, dir, "alpha", "export default async function() { return {}; }")
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.SetTSDispatcher(&stubTSDispatcher{})

	tools := srv.Tools()
	if len(tools) != 2 || tools[0].Name != "alpha" || tools[1].Name != "zebra" {
		t.Errorf("Tools order = %+v", tools)
	}
}

func TestServer_Tools_ExcludesGoHandlerSkillsBeforeBuild(t *testing.T) {
	t.Parallel()
	srv, dir := setupTestServer(t)
	writeGoSkill(t, dir, "go-skill", "package x")
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.SetTSDispatcher(&stubTSDispatcher{})

	tools := srv.Tools()
	if len(tools) != 0 {
		t.Errorf("Go-handler skills should not surface until registered: got %+v", tools)
	}
}

func TestServer_CallTool_DispatchesProgrammaticRegistryFirst(t *testing.T) {
	t.Parallel()
	srv, _ := setupTestServer(t)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	reg := &stubRegistry{
		tools: map[string]mcp.Tool{"hello": {Name: "hello"}},
		resp:  &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("from-registry")}},
	}
	srv.SetSkillRegistry(reg)

	res, err := srv.CallTool(context.Background(), "hello", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.Content[0].Text != "from-registry" {
		t.Errorf("content = %s", res.Content[0].Text)
	}
	if len(reg.calls) != 1 || reg.calls[0] != "hello" {
		t.Errorf("registry calls = %v", reg.calls)
	}
}

func TestServer_CallTool_FallsThroughToTSDispatcher(t *testing.T) {
	t.Parallel()
	srv, dir := setupTestServer(t)
	writeTSSkill(t, dir, "hello", "export default async function() { return {}; }")
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	disp := &stubTSDispatcher{
		resp: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("from-ts")}},
	}
	srv.SetTSDispatcher(disp)
	srv.SetSkillRegistry(&stubRegistry{tools: map[string]mcp.Tool{}})

	res, err := srv.CallTool(context.Background(), "hello", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.Content[0].Text != "from-ts" {
		t.Errorf("content = %s", res.Content[0].Text)
	}
	if len(disp.calls) != 1 {
		t.Fatalf("dispatcher calls = %d", len(disp.calls))
	}
	if disp.calls[0].Name != "hello" || !strings.HasSuffix(disp.calls[0].SourcePath, "skill.ts") {
		t.Errorf("dispatcher call = %+v", disp.calls[0])
	}
	if disp.calls[0].Arguments["k"] != "v" {
		t.Errorf("dispatcher args = %+v", disp.calls[0].Arguments)
	}
}

func TestServer_CallTool_PropagatesRegistryErrors(t *testing.T) {
	t.Parallel()
	srv, _ := setupTestServer(t)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	reg := &stubRegistry{
		tools: map[string]mcp.Tool{"failing": {Name: "failing"}},
		err:   errors.New("boom"),
	}
	srv.SetSkillRegistry(reg)

	_, err := srv.CallTool(context.Background(), "failing", nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestServer_CallTool_UnknownToolReturnsError(t *testing.T) {
	t.Parallel()
	srv, _ := setupTestServer(t)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.SetSkillRegistry(&stubRegistry{tools: map[string]mcp.Tool{}})
	srv.SetTSDispatcher(&stubTSDispatcher{})

	_, err := srv.CallTool(context.Background(), "missing", nil)
	if err == nil || !strings.Contains(err.Error(), "not a registered tool") {
		t.Errorf("err = %v, want unregistered-tool error", err)
	}
}

// TestServer_RecursiveCompositionSmoke is the in-process stand-in for
// the gridctl_remote MCP server test. It proves the constraint:
// pointing one gridctl instance at another over MCP exposes the
// first's typed skill as a callable tool through the standard
// AgentClient surface — no bespoke "remote skill" code path.
//
// The test wires two registries: instance A registers a typed skill
// "greet" through the typed-skill registry; instance B's caller
// invokes A.Server.CallTool("greet") through the AgentClient
// interface. The path crossed is identical to the one a remote MCP
// client would take when the instance B's `gridctl_remote` server
// type connects to A over MCP — both go through Server.CallTool.
func TestServer_RecursiveCompositionSmoke(t *testing.T) {
	t.Parallel()
	srv, _ := setupTestServer(t)
	if err := srv.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Instance A: register a typed skill via the typed-skill registry.
	greetTool := mcp.Tool{
		Name:        "greet",
		Description: "greets a name",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`),
	}
	reg := &stubRegistry{
		tools: map[string]mcp.Tool{"greet": greetTool},
		resp: &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(`{"greeting":"hello"}`)},
		},
	}
	srv.SetSkillRegistry(reg)

	// AgentClient surface check: Server is what an upstream MCP client
	// (or a second gridctl) interacts with. Tools() must surface the
	// typed skill identically to any downstream MCP server's tools.
	var asAgent mcp.AgentClient = srv
	tools := asAgent.Tools()
	if len(tools) != 1 || tools[0].Name != "greet" {
		t.Fatalf("AgentClient.Tools() = %+v, want one greet tool", tools)
	}
	if string(tools[0].InputSchema) != string(greetTool.InputSchema) {
		t.Errorf("AgentClient.Tools()[0].InputSchema = %s, want %s",
			tools[0].InputSchema, greetTool.InputSchema)
	}

	// CallTool from the AgentClient surface — this is the bytes-on-the
	// -wire path a remote gridctl instance traverses. The skill executes
	// through the same registry the local `gridctl run greet` would.
	res, err := asAgent.CallTool(context.Background(), "greet", map[string]any{"name": "remote"})
	if err != nil {
		t.Fatalf("AgentClient.CallTool: %v", err)
	}
	if res.Content[0].Text != `{"greeting":"hello"}` {
		t.Errorf("content = %s", res.Content[0].Text)
	}
	if len(reg.calls) != 1 || reg.calls[0] != "greet" {
		t.Errorf("registry calls = %v, want one greet call", reg.calls)
	}
}
