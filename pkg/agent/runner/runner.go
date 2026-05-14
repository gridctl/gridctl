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

	if _, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{
		Skill:  opts.Skill,
		Flavor: opts.Flavor,
		Input:  opts.RawInput,
	}); err != nil {
		return "", nil, fmt.Errorf("runner: recording run_started: %w", err)
	}

	args := opts.Input
	if args == nil {
		args = map[string]any{}
	}

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
	if _, err := rec.Record(persist.EventRunCompleted, persist.RunCompletedPayload{
		Status: "ok",
		Output: outputFromResult(result),
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

	ev, err := rec.Record(persist.EventRunStarted, persist.RunStartedPayload{
		Skill:  opts.Skill,
		Flavor: opts.Flavor,
		Input:  opts.RawInput,
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
	// cancellation does not.
	go runAsync(context.WithoutCancel(ctx), rec, exec, opts.Skill, args)

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

	if _, err := rec.Record(persist.EventRunCompleted, persist.RunCompletedPayload{
		Status: "ok",
		Output: outputFromResult(result),
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
// result. The dispatcher wraps the typed return value as a single text
// content block; probe-parse as JSON and store the raw bytes when
// valid. Non-JSON text is JSON-string-wrapped so the ledger row
// remains a syntactically valid value. This diverges from the CLI
// (cmd/gridctl/run.go), which records `null` and emits a stderr
// warning — we preserve the literal text because the API surface
// returns the run_id immediately and has no stderr to write to;
// inspectors then see the actual return value rather than a silent
// data loss.
func outputFromResult(result *mcp.ToolCallResult) json.RawMessage {
	if result == nil || len(result.Content) == 0 {
		return json.RawMessage("null")
	}
	text := result.Content[0].Text
	if text == "" {
		return json.RawMessage("null")
	}
	var probe any
	if err := json.Unmarshal([]byte(text), &probe); err == nil {
		return json.RawMessage(text)
	}
	encoded, _ := json.Marshal(text)
	return json.RawMessage(encoded)
}

func extractText(result *mcp.ToolCallResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	return result.Content[0].Text
}
