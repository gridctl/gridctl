// Package runner orchestrates skill-run execution against the daemon's
// wired runtime, persisting the typed event ledger as it goes.
//
// The runner sits between the API layer (POST /api/agent/runs) and the
// registry-server dispatch path. It opens a JSONL ledger via
// persist.Store, writes EventRunStarted synchronously, dispatches the
// skill asynchronously, and records EventRunCompleted (and an
// EventError on failure) when the dispatcher returns. The synchronous
// start lets the API return {run_id, started_at} before the run
// completes; SSE subscribers see the head of the ledger without racing
// the first event.
//
// The runner is intentionally decoupled from pkg/registry: it accepts
// an Executor interface that any caller can satisfy. *registry.Server
// satisfies it via its existing CallTool method.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// Executor invokes a registered skill with the daemon's fully-wired
// bindings (tool/llm/approval). *registry.Server satisfies this via
// its CallTool method.
type Executor interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error)
}

// runIDKey is the context-value handle for the active run's ID. A
// caller that sees a non-empty value knows it is running inside a run
// — the parent of any nested skill call dispatched off this context.
// The key is a private struct (not a string) so unrelated packages
// can't collide with it and so leak-detection tools surface the type
// in trace dumps.
type runIDKey struct{}

// recorderKey is the context-value handle for the active run's
// recorder. Carrying it lets sandbox bindings emit per-call events
// (tool_call, llm_call, approval_*) into the same ledger the runner
// opened — no second OpenWriter, no risk of two writers racing on the
// same JSONL file.
type recorderKey struct{}

// RunIDFromContext returns the run ID stashed on ctx by a parent
// runner.Run / runner.Start, plus a flag noting whether the value was
// present. The flag distinguishes "no parent run" from "parent run
// with empty id" (which should never happen but is structurally
// possible).
func RunIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(runIDKey{}).(string)
	return v, ok && v != ""
}

// RecorderFromContext returns the active run's recorder, when one is
// in scope. Bindings that emit per-call events read it through this
// accessor rather than caching the recorder at install time, because
// the install* helpers run before the recorder is wired in.
func RecorderFromContext(ctx context.Context) (*persist.Recorder, bool) {
	if ctx == nil {
		return nil, false
	}
	rec, ok := ctx.Value(recorderKey{}).(*persist.Recorder)
	return rec, ok && rec != nil
}

// contextWithRun returns a child context carrying the run ID and
// recorder under the package's private keys. Used by Run and Start to
// hand the dispatcher a context that downstream bindings can read.
func contextWithRun(ctx context.Context, runID string, rec *persist.Recorder) context.Context {
	if runID != "" {
		ctx = context.WithValue(ctx, runIDKey{}, runID)
	}
	if rec != nil {
		ctx = context.WithValue(ctx, recorderKey{}, rec)
	}
	return ctx
}

// StartOptions configures a single Start call.
type StartOptions struct {
	// Skill is the registered skill name to invoke.
	Skill string

	// Flavor is the skill's handler-language flavor ("ts" today).
	// Recorded in the ledger so the inspector can render it.
	Flavor string

	// Input is the parsed JSON input handed to the executor.
	Input map[string]any

	// RawInput is the original JSON bytes for the input, preserved
	// verbatim in EventRunStarted so resume can re-issue the run
	// without re-encoding through Go's map iteration order.
	RawInput json.RawMessage
}

// Run opens a new run ledger, writes EventRunStarted synchronously,
// dispatches the skill synchronously via exec, records the terminal
// event, and closes the recorder before returning. Unlike Start it
// blocks until dispatch completes and returns both the run ID and the
// tool-call result so callers (e.g. the MCP transport, which must put
// the result on the wire) can surface them together.
//
// ctx is propagated as-is to the dispatcher — cancellation does flow
// through, so an interrupted MCP request records an error event and
// returns ctx.Err() to the caller.
func Run(ctx context.Context, store *persist.Store, exec Executor, opts StartOptions) (string, *mcp.ToolCallResult, error) {
	if store == nil {
		return "", nil, errors.New("runner: store is required")
	}
	if exec == nil {
		return "", nil, errors.New("runner: executor is required")
	}
	if opts.Skill == "" {
		return "", nil, errors.New("runner: skill is required")
	}

	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		return "", nil, fmt.Errorf("runner: opening run ledger: %w", err)
	}
	defer func() {
		if cerr := rec.Close(); cerr != nil {
			slog.Warn("runner: closing recorder", "run_id", runID, "err", cerr)
		}
	}()

	// If ctx already carries a run ID we are dispatching a nested
	// skill call (e.g. handoff() from inside a parent run); preserve
	// the linkage on the child's RunStarted payload so the IDE can
	// reconstruct the tree.
	parentRunID, _ := RunIDFromContext(ctx)
	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{
		Skill:       opts.Skill,
		Flavor:      opts.Flavor,
		Input:       opts.RawInput,
		ParentRunID: parentRunID,
	}); err != nil {
		return "", nil, fmt.Errorf("runner: recording run_started: %w", err)
	}

	args := opts.Input
	if args == nil {
		args = map[string]any{}
	}

	// Hand the dispatcher a ctx tagged with this run's ID and
	// recorder so sandbox bindings can attach per-call telemetry and
	// nested skill calls can inherit the parent run id.
	ctx = contextWithRun(ctx, runID, rec)
	result, callErr := exec.CallTool(ctx, opts.Skill, args)
	if callErr != nil {
		recordFailure(rec, callErr.Error())
		return runID, nil, callErr
	}
	if result != nil && result.IsError {
		msg := extractText(result)
		if msg == "" {
			msg = "skill returned error result"
		}
		recordFailure(rec, msg)
		return runID, result, nil
	}
	output, truncated := outputFromResult(result)
	if _, err := rec.Record(persist.EventRunCompleted, persist.RunCompletedPayload{
		Status:    "ok",
		Output:    output,
		Truncated: truncated,
	}); err != nil {
		slog.Warn("runner: recording run_completed", "run_id", runID, "err", err)
	}
	return runID, result, nil
}

// Start opens a new run ledger, writes EventRunStarted synchronously,
// and dispatches the skill asynchronously via exec. Returns the run ID
// and the started_at timestamp from the recorded event. The async
// goroutine writes EventRunCompleted (and an EventError on failure)
// before closing the recorder.
//
// The goroutine inherits ctx's values (trace span context, request
// IDs) but not its cancellation — the dispatch outlives the HTTP
// request that started it.
func Start(ctx context.Context, store *persist.Store, exec Executor, opts StartOptions) (string, time.Time, error) {
	if store == nil {
		return "", time.Time{}, errors.New("runner: store is required")
	}
	if exec == nil {
		return "", time.Time{}, errors.New("runner: executor is required")
	}
	if opts.Skill == "" {
		return "", time.Time{}, errors.New("runner: skill is required")
	}

	runID := persist.NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("runner: opening run ledger: %w", err)
	}

	parentRunID, _ := RunIDFromContext(ctx)
	ev, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{
		Skill:       opts.Skill,
		Flavor:      opts.Flavor,
		Input:       opts.RawInput,
		ParentRunID: parentRunID,
	})
	if err != nil {
		_ = rec.Close()
		return "", time.Time{}, fmt.Errorf("runner: recording run_started: %w", err)
	}

	args := opts.Input
	if args == nil {
		args = map[string]any{}
	}

	// Detach from the request context so the dispatch outlives the
	// HTTP request. Values (trace context, request IDs) propagate;
	// cancellation does not. Tag the detached ctx with this run's ID
	// and recorder so the dispatcher and downstream bindings can
	// attach per-call telemetry and inherit the parent run id.
	dispatchCtx := contextWithRun(context.WithoutCancel(ctx), runID, rec)
	go runAsync(dispatchCtx, rec, exec, opts.Skill, args)

	return runID, ev.Time, nil
}

func runAsync(ctx context.Context, rec *persist.Recorder, exec Executor, skillName string, args map[string]any) {
	defer func() {
		if err := rec.Close(); err != nil {
			slog.Warn("runner: closing recorder", "run_id", rec.RunID(), "err", err)
		}
	}()

	result, callErr := exec.CallTool(ctx, skillName, args)
	if callErr != nil {
		recordFailure(rec, callErr.Error())
		return
	}

	if result != nil && result.IsError {
		msg := extractText(result)
		if msg == "" {
			msg = "skill returned error result"
		}
		recordFailure(rec, msg)
		return
	}

	output, truncated := outputFromResult(result)
	if _, err := rec.Record(persist.EventRunCompleted, persist.RunCompletedPayload{
		Status:    "ok",
		Output:    output,
		Truncated: truncated,
	}); err != nil {
		slog.Warn("runner: recording run_completed", "run_id", rec.RunID(), "err", err)
	}
}

// recordFailure writes the EventError + EventRunCompleted{status:error}
// pair the async path emits on dispatch failure. Ledger writes are
// logged at warn level on error so a corrupted ledger is not silent.
func recordFailure(rec *persist.Recorder, msg string) {
	if _, err := rec.Record(persist.EventError, persist.ErrorPayload{Message: msg}); err != nil {
		slog.Warn("runner: recording error event", "run_id", rec.RunID(), "err", err)
	}
	if _, err := rec.Record(persist.EventRunCompleted, persist.RunCompletedPayload{
		Status: "error",
		Error:  msg,
	}); err != nil {
		slog.Warn("runner: recording run_completed", "run_id", rec.RunID(), "err", err)
	}
}

// outputFromResult extracts the skill's output payload from a tool-call
// result, capped at persist.MaxPayloadBytes. The dispatcher wraps the
// typed return value as a single text content block; probe-parse as
// JSON and store the raw bytes when valid. Non-JSON text is
// JSON-string-wrapped so the ledger row remains a syntactically valid
// value. Returns the (possibly truncated) JSON value and a flag noting
// whether truncation occurred so the caller can populate the
// RunCompletedPayload.Truncated field. This diverges from the CLI
// (cmd/gridctl/run.go), which records `null` and emits a stderr
// warning — we preserve the literal text because the API surface
// returns the run_id immediately and has no stderr to write to;
// inspectors then see the actual return value rather than a silent
// data loss.
func outputFromResult(result *mcp.ToolCallResult) (json.RawMessage, bool) {
	if result == nil || len(result.Content) == 0 {
		return json.RawMessage("null"), false
	}
	text := result.Content[0].Text
	if text == "" {
		return json.RawMessage("null"), false
	}
	var raw json.RawMessage
	var probe any
	if err := json.Unmarshal([]byte(text), &probe); err == nil {
		raw = json.RawMessage(text)
	} else {
		encoded, _ := json.Marshal(text)
		raw = json.RawMessage(encoded)
	}
	capped, truncated := persist.CapRawJSON(raw)
	return capped, truncated
}

func extractText(result *mcp.ToolCallResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	return result.Content[0].Text
}
