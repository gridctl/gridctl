package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/dev/scaffold"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// fakeToolCaller is a hand-rolled stub so the sandbox tests stay
// pure-Go (no network, no goroutines outside the sandbox itself).
// Constitution Article IV (no mocks in *integration* tests) is fine
// here — these are unit tests for the binding layer.
type fakeToolCaller struct {
	mu    sync.Mutex
	calls []fakeCall
	respond func(name string, args map[string]any) (*mcp.ToolCallResult, error)
}

type fakeCall struct {
	name string
	args map[string]any
}

func (f *fakeToolCaller) CallTool(_ context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{name: name, args: args})
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(name, args)
	}
	return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(`{"ok":true}`)}}, nil
}

type fakeChatModel struct {
	resp agent.ChatResponse
	err  error
	last agent.ChatRequest
	mu   sync.Mutex
}

func (f *fakeChatModel) Generate(_ context.Context, req agent.ChatRequest) (agent.ChatResponse, error) {
	f.mu.Lock()
	f.last = req
	f.mu.Unlock()
	return f.resp, f.err
}
func (f *fakeChatModel) Stream(_ context.Context, _ agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	return nil, errors.New("not used in sandbox tests")
}

type fakeApprover struct {
	decision ApprovalDecision
	err      error
	prompt   string
}

func (f *fakeApprover) Approve(_ context.Context, prompt string) (ApprovalDecision, error) {
	f.prompt = prompt
	return f.decision, f.err
}

func helloSkillSource(t *testing.T) string {
	t.Helper()
	return `
		export default async function (input: { name: string }): Promise<{ greeting: string }> {
			return { greeting: "hello " + input.name };
		}
	`
}

func TestExecute_RunsAsyncTSDefaultExport(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	res, err := sb.Execute(context.Background(), helloSkillSource(t),
		map[string]any{"name": "world"}, Bindings{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Value), &out); err != nil {
		t.Fatalf("decode value: %v (raw=%q)", err, res.Value)
	}
	if out["greeting"] != "hello world" {
		t.Errorf("greeting = %v, want hello world", out["greeting"])
	}
}

func TestExecute_CapturesConsoleOutput(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			console.log("first");
			console.warn("second", 42);
			return null;
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Console) != 2 || res.Console[0] != "first" || res.Console[1] != "second 42" {
		t.Errorf("console = %v", res.Console)
	}
}

func TestExecute_RejectsMissingDefaultExport(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	src := `function hello() { return 1 } export const named = hello;`
	_, err := sb.Execute(context.Background(), src, nil, Bindings{})
	if err == nil || !strings.Contains(err.Error(), "default") {
		t.Errorf("expected missing-default error, got %v", err)
	}
}

func TestExecute_PropagatesSyntaxError(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	_, err := sb.Execute(context.Background(), `not valid typescript {{`, nil, Bindings{})
	if err == nil || !strings.Contains(err.Error(), "ts transpile failed") {
		t.Errorf("expected transpile error, got %v", err)
	}
}

func TestExecute_HonorsTimeout(t *testing.T) {
	t.Parallel()
	sb := New(150 * time.Millisecond)
	src := `
		export default async function () {
			while (true) {} // hot loop, no event loop yield
		}
	`
	_, err := sb.Execute(context.Background(), src, nil, Bindings{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestToolBinding_DispatchesAndDecodesJSON(t *testing.T) {
	t.Parallel()
	caller := &fakeToolCaller{
		respond: func(name string, args map[string]any) (*mcp.ToolCallResult, error) {
			if name != "github__get-issue" {
				return nil, fmt.Errorf("unexpected name %q", name)
			}
			payload, _ := json.Marshal(map[string]any{"number": int(args["number"].(float64))})
			return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(string(payload))}}, nil
		},
	}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const issue = await tool("github__get-issue", { number: 42 });
			return { number: issue.number };
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{
		ToolCaller:   caller,
		AllowedTools: []mcp.Tool{{Name: "github__get-issue"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, `"number":42`) {
		t.Errorf("value = %s, want number=42", res.Value)
	}
	if len(caller.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(caller.calls))
	}
}

func TestToolBinding_AcceptsUnprefixedNameWhenUnambiguous(t *testing.T) {
	t.Parallel()
	caller := &fakeToolCaller{}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const r = await tool("get-issue", { id: 1 });
			return r;
		}
	`
	_, err := sb.Execute(context.Background(), src, nil, Bindings{
		ToolCaller:   caller,
		AllowedTools: []mcp.Tool{{Name: "github__get-issue"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(caller.calls) != 1 || caller.calls[0].name != "github__get-issue" {
		t.Errorf("calls = %+v, want one call to prefixed name", caller.calls)
	}
}

func TestToolBinding_RejectsAmbiguousUnprefixedName(t *testing.T) {
	t.Parallel()
	caller := &fakeToolCaller{}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			try {
				await tool("get", { x: 1 });
				return { ok: true };
			} catch (e) {
				return { ok: false, msg: String(e) };
			}
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{
		ToolCaller:   caller,
		AllowedTools: []mcp.Tool{{Name: "a__get"}, {Name: "b__get"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "ambiguous") {
		t.Errorf("expected ambiguous in result, got %s", res.Value)
	}
}

func TestToolBinding_RejectsToolOutsideAllowList(t *testing.T) {
	t.Parallel()
	caller := &fakeToolCaller{}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			try {
				await tool("server__notallowed", {});
				return "no error";
			} catch (e) {
				return String(e);
			}
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{
		ToolCaller:   caller,
		AllowedTools: []mcp.Tool{{Name: "server__allowed"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "not in allowed tool list") {
		t.Errorf("expected ACL rejection, got %s", res.Value)
	}
	if len(caller.calls) != 0 {
		t.Errorf("ACL violation should not reach the caller; got %d calls", len(caller.calls))
	}
}

func TestLLMBinding_PassesRequestAndReturnsResponse(t *testing.T) {
	t.Parallel()
	model := &fakeChatModel{
		resp: agent.ChatResponse{
			Model:      "claude-opus-4-7",
			Content:    "hi there",
			StopReason: agent.StopReasonEnd,
			Usage:      agent.Usage{InputTokens: 5, OutputTokens: 3},
		},
	}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const r = await llm({
				model: "claude-opus-4-7",
				messages: [{ role: "user", content: "hi" }]
			});
			return { content: r.content };
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{ChatModel: model})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "hi there") {
		t.Errorf("value = %s, want content from model", res.Value)
	}
	if model.last.Model != "claude-opus-4-7" || len(model.last.Messages) != 1 {
		t.Errorf("model received %+v", model.last)
	}
}

func TestLLMBinding_PropagatesGenerateError(t *testing.T) {
	t.Parallel()
	model := &fakeChatModel{err: errors.New("boom")}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			try {
				await llm({ model: "x", messages: [{ role: "user", content: "y" }] });
				return "no error";
			} catch (e) {
				return String(e);
			}
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{ChatModel: model})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "boom") {
		t.Errorf("expected boom in result, got %s", res.Value)
	}
}

func TestParallelBinding_RunsAllItemsAndPreservesOrder(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const items = ["a", "b", "c", "d", "e"];
			return await parallel(items, async (item, idx) => {
				await new Promise(r => setTimeout(r, 5));
				return idx + ":" + item;
			});
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{MaxParallel: 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out []string
	if err := json.Unmarshal([]byte(res.Value), &out); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, res.Value)
	}
	want := []string{"0:a", "1:b", "2:c", "3:d", "4:e"}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %q, want %q (got %v)", i, out[i], want[i], out)
			break
		}
	}
}

func TestParallelBinding_PropagatesRejection(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			try {
				await parallel([1, 2, 3], async (n) => {
					if (n === 2) throw new Error("nope");
					return n;
				});
				return "no error";
			} catch (e) {
				return String(e);
			}
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "nope") {
		t.Errorf("expected nope in result, got %s", res.Value)
	}
}

func TestHandoffBinding_RoutesThroughSkillCaller(t *testing.T) {
	t.Parallel()
	caller := &fakeToolCaller{
		respond: func(_ string, args map[string]any) (*mcp.ToolCallResult, error) {
			text, _ := json.Marshal(map[string]any{"echo": args})
			return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(string(text))}}, nil
		},
	}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const r = await handoff("greet", { who: "registry" });
			return r;
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{SkillCaller: caller})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "registry") {
		t.Errorf("value = %s, want echo of input", res.Value)
	}
	if len(caller.calls) != 1 || caller.calls[0].name != "greet" {
		t.Errorf("calls = %+v", caller.calls)
	}
}

func TestApprovalBinding_AutoApprovesWhenStubbed(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const r = await approval("ship it?");
			return r;
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, `"approved":true`) {
		t.Errorf("expected auto-approve, got %s", res.Value)
	}
}

func TestApprovalBinding_HonorsCustomApprover(t *testing.T) {
	t.Parallel()
	app := &fakeApprover{decision: ApprovalDecision{Approved: false, Reason: "nope"}}
	sb := New(2 * time.Second)
	src := `
		export default async function () {
			const r = await approval("ship?");
			return r;
		}
	`
	res, err := sb.Execute(context.Background(), src, nil, Bindings{Approver: app})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, `"approved":false`) || !strings.Contains(res.Value, `"reason":"nope"`) {
		t.Errorf("expected custom decision, got %s", res.Value)
	}
	if app.prompt != "ship?" {
		t.Errorf("approver got prompt %q, want ship?", app.prompt)
	}
}

// TestExecute_ScaffoldOutputRunsThroughRequireShim is the regression
// test for the gap that shipped in the Code-First Agent Runtime slice:
// `gridctl agent init` scaffolds a skill.ts that imports from
// "@gridctl/agent", esbuild lowers that to require("@gridctl/agent"),
// and the sandbox previously had no require global. The test runs the
// literal scaffold body so any future scaffold change re-exercises the
// runtime contract automatically.
func TestExecute_ScaffoldOutputRunsThroughRequireShim(t *testing.T) {
	t.Parallel()
	src := scaffold.HelloSkillTS("hello-ts")
	caller := &fakeToolCaller{
		respond: func(_ string, _ map[string]any) (*mcp.ToolCallResult, error) {
			return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(`"casual"`)}}, nil
		},
	}
	model := &fakeChatModel{
		resp: agent.ChatResponse{
			Model:   "claude-sonnet-4-6",
			Content: "Hi, world!",
		},
	}
	sb := New(2 * time.Second)
	res, err := sb.Execute(context.Background(),
		src,
		map[string]any{"name": "world"},
		Bindings{
			ToolCaller:   caller,
			AllowedTools: []mcp.Tool{{Name: "gridctl__greeting_style"}},
			ChatModel:    model,
		},
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, "Hi, world!") {
		t.Errorf("value = %s, want greeting from chat model", res.Value)
	}
	if len(caller.calls) != 1 || caller.calls[0].name != "gridctl__greeting_style" {
		t.Errorf("expected one tool call to gridctl__greeting_style, got %+v", caller.calls)
	}
}

// TestExecute_RequireShim_GridctlAgent verifies that an explicit import
// of the agent SDK transpiles into a working require("@gridctl/agent")
// call and the resulting module object exposes the binding globals.
// This is the smaller, focused mirror of the scaffold-output test —
// useful when debugging because it isolates require() resolution from
// the broader scaffold path.
func TestExecute_RequireShim_GridctlAgent(t *testing.T) {
	t.Parallel()
	src := `
		import { tool } from "@gridctl/agent";
		export default async function (i: { x: number }) {
			return await tool("svc__op", { x: i.x });
		}
	`
	caller := &fakeToolCaller{
		respond: func(_ string, args map[string]any) (*mcp.ToolCallResult, error) {
			payload, _ := json.Marshal(map[string]any{"ok": true, "x": args["x"]})
			return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent(string(payload))}}, nil
		},
	}
	sb := New(2 * time.Second)
	res, err := sb.Execute(context.Background(), src, map[string]any{"x": 1}, Bindings{
		ToolCaller:   caller,
		AllowedTools: []mcp.Tool{{Name: "svc__op"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Value, `"ok":true`) {
		t.Errorf("value = %s, want ok:true", res.Value)
	}
}

// TestExecute_RequireShim_RejectsUnknownModule guards against the
// require shim accidentally returning empty modules for arbitrary names.
// Unknown imports must surface as a clear runtime error so authors
// notice unsupported imports immediately rather than getting
// undefined-method failures deep inside their handler.
func TestExecute_RequireShim_RejectsUnknownModule(t *testing.T) {
	t.Parallel()
	src := `import * as fs from "fs"; export default async function () { return fs; };`
	sb := New(2 * time.Second)
	_, err := sb.Execute(context.Background(), src, nil, Bindings{})
	if err == nil {
		t.Fatal("expected unknown-module error, got nil")
	}
	if !strings.Contains(err.Error(), `unknown module "fs"`) {
		t.Errorf("err = %v, want unknown-module error", err)
	}
}

func TestNewInvoker_ReadsSourceAndWrapsResult(t *testing.T) {
	t.Parallel()
	src := `
		export default async function (input: { name: string }) {
			return { greeting: "hi " + input.name };
		}
	`
	sb := New(2 * time.Second)
	loader := func(name string) (string, error) {
		if name != "hello" {
			return "", fmt.Errorf("unknown skill %q", name)
		}
		return src, nil
	}
	invoker := sb.NewInvoker("hello", loader, nil)

	res, err := invoker(context.Background(), map[string]any{"name": "yes"})
	if err != nil {
		t.Fatalf("invoker: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, "hi yes") {
		t.Errorf("content = %s", res.Content[0].Text)
	}
}
