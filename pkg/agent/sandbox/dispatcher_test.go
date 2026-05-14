package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestDispatcher_DispatchReadsSourceAndWrapsResult is the unit-test
// proof that the concrete Dispatcher reads the TS source from the
// supplied path, runs it under the sandbox with per-call Bindings, and
// wraps the resolved value in an mcp.ToolCallResult shaped the way an
// MCP tool reply does. This is the path the gateway hands to external
// MCP clients calling a TS skill by name.
func TestDispatcher_DispatchReadsSourceAndWrapsResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "echo.ts")
	src := `export default async function (i: any) { return { echoed: i }; }`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, func(_ context.Context, _ string) Bindings { return Bindings{} })
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := disp.Dispatch(context.Background(), "echo", path, map[string]any{"v": 7})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(res.Content))
	}
	text := res.Content[0].Text
	if !strings.Contains(text, `"echoed":`) || !strings.Contains(text, `"v":7`) {
		t.Errorf("content text = %q, want JSON containing echoed.v=7", text)
	}
}

// TestDispatcher_BindingsProviderReceivesContextAndName confirms the
// per-call BindingsProvider hook is invoked with the run's context and
// skill name on every Dispatch — that is the surface long-lived
// dispatchers use to scope ToolCaller/ChatModel/Approver to one call.
func TestDispatcher_BindingsProviderReceivesContextAndName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "noop.ts")
	if err := os.WriteFile(path, []byte(`export default async function () { return null; }`), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	type captured struct {
		ctx  context.Context
		name string
	}
	calls := []captured{}
	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, func(ctx context.Context, name string) Bindings {
		calls = append(calls, captured{ctx: ctx, name: name})
		return Bindings{}
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	type ctxKey string
	parentCtx := context.WithValue(context.Background(), ctxKey("trace"), "abc")
	if _, err := disp.Dispatch(parentCtx, "skill-name", path, nil); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("BindingsProvider invoked %d times, want 1", len(calls))
	}
	if calls[0].name != "skill-name" {
		t.Errorf("BindingsProvider got name=%q, want skill-name", calls[0].name)
	}
	if got := calls[0].ctx.Value(ctxKey("trace")); got != "abc" {
		t.Errorf("BindingsProvider context lost trace value: got %v", got)
	}
}

// TestDispatcher_DispatchReportsMissingSource confirms a missing source
// file surfaces as a structured error rather than a panic.
func TestDispatcher_DispatchReportsMissingSource(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, nil)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	_, err = disp.Dispatch(context.Background(), "ghost", filepath.Join(t.TempDir(), "missing.ts"), nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), `ts skill "ghost"`) {
		t.Errorf("err = %v, want skill-named error", err)
	}
}

// TestDispatcher_NewDispatcherRejectsNilSandbox locks in the
// constructor's contract: a nil sandbox is a programmer error caught
// at wire time, not at first call.
func TestDispatcher_NewDispatcherRejectsNilSandbox(t *testing.T) {
	t.Parallel()
	_, err := NewDispatcher(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil sandbox")
	}
	if !strings.Contains(err.Error(), "non-nil sandbox") {
		t.Errorf("err = %v, want explanation that sandbox is required", err)
	}
}

// TestDispatcher_EmitsTelemetryAroundBindings drives a TS skill that
// calls tool(), llm(), and handoff() through the dispatcher with a
// RunSession wired in, and asserts the JSONL ledger captures the
// expected per-binding event sequence: node_enter / tool_call /
// tool_result / node_exit pairs around tool(), node_enter / llm_call
// / node_exit around llm(), and node_enter / tool_call / tool_result
// / node_exit around handoff() (the dispatcher path doesn't open a
// child recorder — that wrapping lives in pkg/controller — so handoff
// surfaces as a single skill node here). Closes the test gap PR #628
// deferred.
func TestDispatcher_EmitsTelemetryAroundBindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "exercise.ts")
	src := `
		export default async function (input: any) {
			const toolRes = await tool('srv__echo', { from: 'tool' });
			const llmRes = await llm({ model: 'claude-opus-4-7', messages: [{ role: 'user', content: 'hi' }] });
			const handoffRes = await handoff('leaf-skill', { from: 'handoff' });
			return { toolRes, llmRes, handoffRes };
		}
	`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	store := persist.NewStore(t.TempDir())
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = rec.Close() })
	session := NewRunSession(rec)

	bindings := Bindings{
		ToolCaller: stubToolCaller{
			result: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(`{"ok":"tool"}`)}},
		},
		AllowedTools: []mcp.Tool{{Name: "srv__echo"}},
		ChatModel: stubChatModel{resp: agent.ChatResponse{
			Model:   "claude-opus-4-7",
			Content: "hello",
			Usage: agent.Usage{
				InputTokens:  17,
				OutputTokens: 42,
			},
		}},
		SkillCaller: stubSkillCaller{
			result: &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(`{"ok":"handoff"}`)}},
		},
		Session: session,
	}

	sb := New(5 * time.Second)
	disp, err := NewDispatcher(sb, func(_ context.Context, _ string) Bindings { return bindings })
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if _, err := disp.Dispatch(context.Background(), "exercise", path, map[string]any{}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Group events by type; the wall-clock order of LLM / tool /
	// handoff is governed by the skill's await sequence, but each
	// pair (enter/exit, call/result) must appear and pair correctly.
	byType := map[persist.EventType]int{}
	for _, ev := range events {
		byType[ev.Type]++
	}
	if byType[persist.EventNodeEnter] != 3 {
		t.Errorf("node_enter count = %d, want 3 (tool, llm, handoff)", byType[persist.EventNodeEnter])
	}
	if byType[persist.EventNodeExit] != 3 {
		t.Errorf("node_exit count = %d, want 3", byType[persist.EventNodeExit])
	}
	if byType[persist.EventToolCall] != 2 {
		t.Errorf("tool_call count = %d, want 2 (tool + handoff)", byType[persist.EventToolCall])
	}
	if byType[persist.EventToolResult] != 2 {
		t.Errorf("tool_result count = %d, want 2", byType[persist.EventToolResult])
	}
	if byType[persist.EventLLMCall] != 1 {
		t.Errorf("llm_call count = %d, want 1", byType[persist.EventLLMCall])
	}

	// Locate the llm_call event and confirm tokens propagated.
	for _, ev := range events {
		if ev.Type != persist.EventLLMCall {
			continue
		}
		var p persist.LLMCallPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("decode llm_call: %v", err)
		}
		if p.Model != "claude-opus-4-7" {
			t.Errorf("llm_call model = %q, want claude-opus-4-7", p.Model)
		}
		if p.PromptTokens != 17 || p.OutputTokens != 42 {
			t.Errorf("llm_call tokens = (%d, %d), want (17, 42)", p.PromptTokens, p.OutputTokens)
		}
	}

	// tool_call NodeID must pair with a matching tool_result NodeID.
	callsByID := map[string]string{}
	for _, ev := range events {
		if ev.Type == persist.EventToolCall {
			var p persist.ToolCallPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("decode tool_call: %v", err)
			}
			callsByID[p.CallID] = p.NodeID
		}
	}
	for _, ev := range events {
		if ev.Type != persist.EventToolResult {
			continue
		}
		var p persist.ToolResultPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("decode tool_result: %v", err)
		}
		if want, ok := callsByID[p.CallID]; !ok {
			t.Errorf("tool_result references unknown call_id %q", p.CallID)
		} else if p.NodeID != want {
			t.Errorf("tool_result node_id = %q, want %q (matching tool_call)", p.NodeID, want)
		}
	}
}

// TestDispatcher_EmitsApprovalEventsWhenStubAutoApproves confirms the
// auto-approve stub (no Approver wired) still emits the paired
// approval_request / approval_response events the inspector pairs
// when rendering a suspended-then-resumed gate.
func TestDispatcher_EmitsApprovalEventsWhenStubAutoApproves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "approve.ts")
	src := `
		export default async function () {
			return await approval('ship it?');
		}
	`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	store := persist.NewStore(t.TempDir())
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = rec.Close() })
	session := NewRunSession(rec)

	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, func(_ context.Context, _ string) Bindings {
		return Bindings{Session: session}
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if _, err := disp.Dispatch(context.Background(), "approve", path, nil); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var sawRequest, sawResponse bool
	var requestID, responseID string
	for _, ev := range events {
		switch ev.Type {
		case persist.EventApprovalRequest:
			sawRequest = true
			var p persist.ApprovalRequestPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("decode approval_request: %v", err)
			}
			requestID = p.ApprovalID
			if p.Prompt != "ship it?" {
				t.Errorf("prompt = %q, want %q", p.Prompt, "ship it?")
			}
		case persist.EventApprovalResponse:
			sawResponse = true
			var p persist.ApprovalResponsePayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("decode approval_response: %v", err)
			}
			responseID = p.ApprovalID
			if !p.Approved || p.Source != "auto" {
				t.Errorf("response = %+v, want approved=true source=auto", p)
			}
		}
	}
	if !sawRequest || !sawResponse {
		t.Fatalf("missing approval pair: req=%v resp=%v", sawRequest, sawResponse)
	}
	if requestID != responseID {
		t.Errorf("approval IDs do not pair: request=%q response=%q", requestID, responseID)
	}
}

// TestDispatcher_ApprovalErrorStillEmitsResponsePair guards the
// pairing invariant the inspector relies on: every EventApprovalRequest
// must be answered by exactly one EventApprovalResponse with the
// matching ApprovalID, even when the Approver itself errored out.
// Without this pair, an inspector renders the gate as
// permanently-suspended — a worse UX failure than the underlying
// error message.
func TestDispatcher_ApprovalErrorStillEmitsResponsePair(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "approve_err.ts")
	src := `
		export default async function () {
			try { await approval('ship?'); return { ok: true }; }
			catch (e) { return { ok: false, error: String(e) }; }
		}
	`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	store := persist.NewStore(t.TempDir())
	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = rec.Close() })

	bindings := Bindings{
		Session:  NewRunSession(rec),
		Approver: stubFailingApprover{},
	}
	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, func(_ context.Context, _ string) Bindings { return bindings })
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if _, err := disp.Dispatch(context.Background(), "approve_err", path, nil); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var requestID, responseID string
	var sawResponse bool
	for _, ev := range events {
		switch ev.Type {
		case persist.EventApprovalRequest:
			var p persist.ApprovalRequestPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("decode approval_request: %v", err)
			}
			requestID = p.ApprovalID
		case persist.EventApprovalResponse:
			sawResponse = true
			var p persist.ApprovalResponsePayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				t.Fatalf("decode approval_response: %v", err)
			}
			responseID = p.ApprovalID
			if p.Approved {
				t.Errorf("expected approved=false on error path, got true")
			}
			if p.Source != "error" {
				t.Errorf("expected source=error, got %q", p.Source)
			}
		}
	}
	if !sawResponse {
		t.Fatal("approver-error path emitted no EventApprovalResponse — pair would be orphaned")
	}
	if requestID == "" || requestID != responseID {
		t.Errorf("approval IDs do not pair: request=%q response=%q", requestID, responseID)
	}
}

type stubFailingApprover struct{}

func (stubFailingApprover) Approve(_ context.Context, _ string) (ApprovalDecision, error) {
	return ApprovalDecision{}, errAuthorityUnavailable
}

var errAuthorityUnavailable = stubError("authority unavailable")

type stubError string

func (e stubError) Error() string { return string(e) }

// stubToolCaller satisfies agent.ToolCaller for tests; returns the
// pre-baked result regardless of name/args.
type stubToolCaller struct {
	result *mcp.ToolCallResult
}

func (s stubToolCaller) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
	return s.result, nil
}

// stubChatModel satisfies agent.ChatModel; the Stream path is unused
// here so it returns a not-implemented error.
type stubChatModel struct {
	resp agent.ChatResponse
}

func (s stubChatModel) Generate(_ context.Context, _ agent.ChatRequest) (agent.ChatResponse, error) {
	return s.resp, nil
}
func (s stubChatModel) Stream(_ context.Context, _ agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	return nil, nil
}

// stubSkillCaller satisfies sandbox.SkillCaller.
type stubSkillCaller struct {
	result *mcp.ToolCallResult
}

func (s stubSkillCaller) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
	return s.result, nil
}

// TestDispatcher_SatisfiesRegistryTSDispatcherShape is a structural
// check: the concrete Dispatcher implements the dispatch contract the
// registry server consumes (Dispatch(ctx, name, path, args) → result).
// The registry interface lives in pkg/registry; we mirror its shape
// here so a future signature change in either package fails this test
// rather than a downstream integration.
func TestDispatcher_SatisfiesRegistryTSDispatcherShape(t *testing.T) {
	t.Parallel()
	type tsDispatcher interface {
		Dispatch(ctx context.Context, name, sourcePath string, arguments map[string]any) (*mcp.ToolCallResult, error)
	}
	sb := New(time.Second)
	disp, err := NewDispatcher(sb, nil)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	var _ tsDispatcher = disp
}
