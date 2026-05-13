package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/mcp"
)

type stubExecutor struct {
	result *mcp.ToolCallResult
	err    error
	done   chan struct{}
}

func newStubExecutor(result *mcp.ToolCallResult, err error) *stubExecutor {
	return &stubExecutor{result: result, err: err, done: make(chan struct{})}
}

func (e *stubExecutor) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.ToolCallResult, error) {
	defer close(e.done)
	return e.result, e.err
}

// waitForStatus polls the ledger until a run reaches a terminal status
// or the deadline elapses. The async goroutine in Start writes
// EventRunCompleted before closing the recorder, so the summary's
// status flips off "running" within milliseconds of dispatch return.
func waitForStatus(t *testing.T, store *persist.Store, runID string) persist.RunSummary {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		summary, err := store.Summary(runID)
		if err == nil && summary.Status != "running" {
			return summary
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach terminal status within deadline", runID)
	return persist.RunSummary{}
}

func TestStart_HappyPathWritesStartedAndCompleted(t *testing.T) {
	store := persist.NewStore(t.TempDir())
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(`{"ok":true}`)},
	}
	exec := newStubExecutor(result, nil)

	runID, startedAt, err := Start(context.Background(), store, exec, StartOptions{
		Skill:    "demo",
		Flavor:   "ts",
		Input:    map[string]any{"name": "world"},
		RawInput: json.RawMessage(`{"name":"world"}`),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runID == "" {
		t.Fatal("expected non-empty run id")
	}
	if startedAt.IsZero() {
		t.Fatal("expected non-zero started_at")
	}

	summary := waitForStatus(t, store, runID)
	if summary.Status != "ok" {
		t.Fatalf("expected status=ok, got %q (error=%q)", summary.Status, summary.Error)
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

	var completed persist.RunCompletedPayload
	if err := json.Unmarshal(events[1].Payload, &completed); err != nil {
		t.Fatalf("decode run_completed: %v", err)
	}
	if completed.Status != "ok" {
		t.Fatalf("expected status=ok, got %q", completed.Status)
	}
	if string(completed.Output) != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", completed.Output)
	}
}

func TestStart_ExecutorErrorRecordsErrorAndCompleted(t *testing.T) {
	store := persist.NewStore(t.TempDir())
	exec := newStubExecutor(nil, errors.New("boom"))

	runID, _, err := Start(context.Background(), store, exec, StartOptions{Skill: "demo", Flavor: "ts"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	summary := waitForStatus(t, store, runID)
	if summary.Status != "error" {
		t.Fatalf("expected status=error, got %q", summary.Status)
	}
	if summary.Error != "boom" {
		t.Fatalf("expected error=%q, got %q", "boom", summary.Error)
	}
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// run_started + error + run_completed
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[1].Type != persist.EventError {
		t.Fatalf("event 1: expected error, got %s", events[1].Type)
	}
}

func TestStart_IsErrorResultRecordsAsErrorStatus(t *testing.T) {
	store := persist.NewStore(t.TempDir())
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("auth failed")},
		IsError: true,
	}
	exec := newStubExecutor(result, nil)

	runID, _, err := Start(context.Background(), store, exec, StartOptions{Skill: "demo", Flavor: "ts"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	summary := waitForStatus(t, store, runID)
	if summary.Status != "error" {
		t.Fatalf("expected status=error, got %q", summary.Status)
	}
	if !strings.Contains(summary.Error, "auth failed") {
		t.Fatalf("expected error to include %q, got %q", "auth failed", summary.Error)
	}
}

func TestStart_RejectsMissingDependencies(t *testing.T) {
	store := persist.NewStore(t.TempDir())
	exec := newStubExecutor(nil, nil)

	if _, _, err := Start(context.Background(), nil, exec, StartOptions{Skill: "demo"}); err == nil {
		t.Fatal("expected error for nil store")
	}
	if _, _, err := Start(context.Background(), store, nil, StartOptions{Skill: "demo"}); err == nil {
		t.Fatal("expected error for nil executor")
	}
	if _, _, err := Start(context.Background(), store, exec, StartOptions{Skill: ""}); err == nil {
		t.Fatal("expected error for empty skill")
	}
}

func TestStart_RunStartedPayloadCarriesInput(t *testing.T) {
	store := persist.NewStore(t.TempDir())
	exec := newStubExecutor(&mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("null")}}, nil)

	raw := json.RawMessage(`{"q":"hi"}`)
	runID, _, err := Start(context.Background(), store, exec, StartOptions{
		Skill:    "demo",
		Flavor:   "ts",
		Input:    map[string]any{"q": "hi"},
		RawInput: raw,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// run_started is written synchronously; readable immediately.
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) == 0 || events[0].Type != persist.EventRunStarted {
		t.Fatalf("expected first event run_started, got %+v", events)
	}
	var started persist.RunStartedPayload
	if err := json.Unmarshal(events[0].Payload, &started); err != nil {
		t.Fatalf("decode run_started: %v", err)
	}
	if started.Skill != "demo" {
		t.Fatalf("expected skill=demo, got %q", started.Skill)
	}
	if started.Flavor != "ts" {
		t.Fatalf("expected flavor=ts, got %q", started.Flavor)
	}
	if string(started.Input) != string(raw) {
		t.Fatalf("expected raw input preserved, got %s", started.Input)
	}

	// Drain async work so the test doesn't race the goroutine.
	waitForStatus(t, store, runID)
}

func TestStart_NonJSONOutputIsWrappedAsString(t *testing.T) {
	store := persist.NewStore(t.TempDir())
	result := &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("hello world")},
	}
	exec := newStubExecutor(result, nil)

	runID, _, err := Start(context.Background(), store, exec, StartOptions{Skill: "demo", Flavor: "ts"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForStatus(t, store, runID)
	events, err := store.Read(runID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var completed persist.RunCompletedPayload
	if err := json.Unmarshal(events[len(events)-1].Payload, &completed); err != nil {
		t.Fatalf("decode run_completed: %v", err)
	}
	if string(completed.Output) != `"hello world"` {
		t.Fatalf("expected wrapped string output, got %s", completed.Output)
	}
}
