package api

import (
	"context"
	"os"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// stubSkillRegistry satisfies registry.SkillRegistry for the adapter
// tests without bringing the agent sandbox into the unit-test surface.
// It always returns the configured result, so registry.Server.CallTool
// short-circuits before reaching the TS dispatcher branch.
type stubSkillRegistry struct {
	result *mcp.ToolCallResult
	err    error
}

func (s *stubSkillRegistry) Tools() []mcp.Tool { return nil }

func (s *stubSkillRegistry) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
	return s.result, s.err
}

// fakeAgentClient stands in for a proxied upstream MCP server.
type fakeAgentClient struct {
	name       string
	callResult *mcp.ToolCallResult
	callErr    error
}

func (f *fakeAgentClient) Name() string                                { return f.name }
func (f *fakeAgentClient) Initialize(_ context.Context) error          { return nil }
func (f *fakeAgentClient) RefreshTools(_ context.Context) error        { return nil }
func (f *fakeAgentClient) Tools() []mcp.Tool                           { return nil }
func (f *fakeAgentClient) IsInitialized() bool                         { return true }
func (f *fakeAgentClient) ServerInfo() mcp.ServerInfo                  { return mcp.ServerInfo{Name: f.name, Version: "1.0"} }
func (f *fakeAgentClient) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
	return f.callResult, f.callErr
}

func TestRunPersisterAdapter_PersistsTypedSkill(t *testing.T) {
	srv, store, regServer := newAgentRunLaunchTestServer(t)
	regServer.SetSkillRegistry(&stubSkillRegistry{
		result: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(`{"ok":true}`)}},
	})
	seedTypedSkill(t, regServer, "demo-ts", "ts")

	adapter := newRunPersisterAdapter(srv)
	runID, result, err := adapter.PersistAndCall(context.Background(), regServer, "demo-ts", map[string]any{"q": "hi"})
	if err != nil {
		t.Fatalf("PersistAndCall: %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty run id for typed-skill dispatch")
	}
	if result == nil || len(result.Content) == 0 || result.Content[0].Text != `{"ok":true}` {
		t.Fatalf("expected result forwarded unchanged, got %+v", result)
	}

	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (run_started, run_completed), got %d", len(events))
	}
	if events[0].Type != persist.EventRunStarted {
		t.Fatalf("event 0: expected run_started, got %s", events[0].Type)
	}
	if events[1].Type != persist.EventRunCompleted {
		t.Fatalf("event 1: expected run_completed, got %s", events[1].Type)
	}

	summary, err := store.Summary(runID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Status != "ok" {
		t.Fatalf("expected status=ok, got %q", summary.Status)
	}
	if summary.Skill != "demo-ts" {
		t.Fatalf("expected skill=demo-ts, got %q", summary.Skill)
	}
	if summary.Flavor != "ts" {
		t.Fatalf("expected flavor=ts, got %q", summary.Flavor)
	}
}

func TestRunPersisterAdapter_FallsThrough_NonRegistryClient(t *testing.T) {
	srv, store, _ := newAgentRunLaunchTestServer(t)

	upstream := &fakeAgentClient{
		name:       "upstream",
		callResult: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("ok")}},
	}
	adapter := newRunPersisterAdapter(srv)
	runID, result, err := adapter.PersistAndCall(context.Background(), upstream, "echo", map[string]any{})
	if err != nil {
		t.Fatalf("PersistAndCall: %v", err)
	}
	if runID != "" {
		t.Fatalf("expected empty run id for proxied upstream call, got %q", runID)
	}
	if result == nil || len(result.Content) == 0 || result.Content[0].Text != "ok" {
		t.Fatalf("expected upstream result forwarded unchanged, got %+v", result)
	}
	assertLedgerEmpty(t, store)
}

func TestRunPersisterAdapter_FallsThrough_NonTypedSkill(t *testing.T) {
	srv, store, regServer := newAgentRunLaunchTestServer(t)
	regServer.SetSkillRegistry(&stubSkillRegistry{
		result: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("ok")}},
	})
	// Seed a Go-plugin (non-TS) skill — must not land in the ledger.
	seedTypedSkill(t, regServer, "demo-go", "go")

	adapter := newRunPersisterAdapter(srv)
	runID, result, err := adapter.PersistAndCall(context.Background(), regServer, "demo-go", map[string]any{})
	if err != nil {
		t.Fatalf("PersistAndCall: %v", err)
	}
	if runID != "" {
		t.Fatalf("expected empty run id for Go-plugin skill, got %q", runID)
	}
	if result == nil {
		t.Fatal("expected result forwarded for Go-plugin skill")
	}
	assertLedgerEmpty(t, store)
}

func TestRunPersisterAdapter_FallsThrough_StoreUnset(t *testing.T) {
	// A bare Server with neither run store nor registry server wired
	// must still pass tool calls through — the adapter's guard rails
	// preserve pre-fix behaviour for partial daemon configurations.
	srv := &Server{}
	upstream := &fakeAgentClient{
		name:       "upstream",
		callResult: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("ok")}},
	}
	adapter := newRunPersisterAdapter(srv)
	runID, result, err := adapter.PersistAndCall(context.Background(), upstream, "echo", map[string]any{})
	if err != nil {
		t.Fatalf("PersistAndCall: %v", err)
	}
	if runID != "" {
		t.Fatalf("expected empty run id when store is unset, got %q", runID)
	}
	if result == nil {
		t.Fatal("expected result forwarded when store is unset")
	}
}

// assertLedgerEmpty fails the test when the store's directory contains
// any JSONL files. The directory itself may not exist yet — that is
// equivalent to empty for this assertion's purpose.
func assertLedgerEmpty(t *testing.T, store *persist.Store) {
	t.Helper()
	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected empty ledger, got %d entries: %v", len(entries), names)
	}
}
