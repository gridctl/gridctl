package sandbox

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/gridctl/gridctl/pkg/agent/persist"
)

// RunSession is the per-invocation telemetry handle the sandbox
// bindings emit through. It carries the parent run's recorder (opened
// by runner.Run) plus a monotonic node counter so each top-level
// binding call (tool, llm, handoff, approval) can be addressed as a
// stable "node" in the run's event timeline.
//
// A nil *RunSession is the no-recording mode every accessor recognises
// — the in-process compose tests and the registry's NewInvoker harness
// run without a recorder wired, and the bindings stay quiet rather
// than panicking. Production dispatches (gateway → runner → sandbox)
// always carry a session.
type RunSession struct {
	// Recorder is the parent run's ledger writer. Bindings call
	// Record* helpers below; those helpers route every write through
	// this recorder's existing mutex so events from concurrent
	// goroutines don't interleave bytes mid-line.
	Recorder *persist.Recorder

	// nodeSeq is the monotonic counter behind NextNodeID. Atomic so
	// concurrent goroutine deliveries (parallel(), background tool
	// calls) get distinct ids without a mutex.
	nodeSeq atomic.Uint64
}

// NewRunSession constructs a session bound to the given recorder. A
// nil recorder yields a session whose Record* methods are no-ops —
// callers don't need to special-case test or no-persistence paths.
func NewRunSession(rec *persist.Recorder) *RunSession {
	return &RunSession{Recorder: rec}
}

// NextNodeID returns a "<kind>:<name>#<n>" id for the next top-level
// binding call. The id pairs the NodeEnter and NodeExit events the
// caller emits around its work and gives the inspector a stable key
// for per-call decoration even when the same tool is called twice.
func (s *RunSession) NextNodeID(kind, name string) string {
	if s == nil {
		return ""
	}
	n := s.nodeSeq.Add(1)
	if name == "" {
		return fmt.Sprintf("%s#%d", kind, n)
	}
	return fmt.Sprintf("%s:%s#%d", kind, name, n)
}

// RecordNodeEnter writes an EventNodeEnter for the given binding
// invocation. NodeName carries a human-friendly label ("tool", "llm",
// etc.); the id is whatever NextNodeID produced. A nil session is a
// silent no-op so test wiring without a recorder still passes.
func (s *RunSession) RecordNodeEnter(nodeID, nodeName string) {
	if s == nil || s.Recorder == nil || nodeID == "" {
		return
	}
	if _, err := s.Recorder.Record(persist.EventNodeEnter, persist.NodeEnterPayload{
		NodeID:   nodeID,
		NodeName: nodeName,
	}); err != nil {
		slog.Warn("sandbox: recording node_enter", "run_id", s.runID(), "err", err)
	}
}

// RecordNodeExit writes the matching EventNodeExit. DurationMicros is
// the wall-clock time the binding held — typically computed as
// time.Since(start).Microseconds() at the boundary. Success=false
// signals a failure; callers typically also emit EventError for the
// propagated message.
func (s *RunSession) RecordNodeExit(nodeID string, durationMicros int64, success bool) {
	if s == nil || s.Recorder == nil || nodeID == "" {
		return
	}
	if _, err := s.Recorder.Record(persist.EventNodeExit, persist.NodeExitPayload{
		NodeID:         nodeID,
		DurationMicros: durationMicros,
		Success:        success,
	}); err != nil {
		slog.Warn("sandbox: recording node_exit", "run_id", s.runID(), "err", err)
	}
}

// RecordToolCall captures the tool() binding's pre-dispatch event.
// Arguments are size-capped via persist.CapRawJSON so a tool fed a
// multi-megabyte blob can't bloat the ledger; the cap flag is mirrored
// into the payload so consumers know the value is a prefix, not the
// full picture.
func (s *RunSession) RecordToolCall(nodeID, callID, name string, args map[string]any) {
	if s == nil || s.Recorder == nil {
		return
	}
	var raw json.RawMessage
	if args != nil {
		if encoded, err := json.Marshal(args); err == nil {
			raw = encoded
		}
	}
	capped, truncated := persist.CapRawJSON(raw)
	if _, err := s.Recorder.Record(persist.EventToolCall, persist.ToolCallPayload{
		CallID:    callID,
		NodeID:    nodeID,
		Name:      name,
		Arguments: capped,
		Truncated: truncated,
	}); err != nil {
		slog.Warn("sandbox: recording tool_call", "run_id", s.runID(), "err", err)
	}
}

// RecordToolResult captures the tool() binding's post-dispatch event.
// Output is the raw text the gateway returned (the dispatcher already
// collapses multi-block content to a single string at the boundary);
// strings over persist.MaxPayloadBytes are trimmed.
func (s *RunSession) RecordToolResult(nodeID, callID, output string, isError bool) {
	if s == nil || s.Recorder == nil {
		return
	}
	capped, truncated := persist.CapString(output)
	if _, err := s.Recorder.Record(persist.EventToolResult, persist.ToolResultPayload{
		CallID:    callID,
		NodeID:    nodeID,
		Output:    capped,
		Truncated: truncated,
		IsError:   isError,
	}); err != nil {
		slog.Warn("sandbox: recording tool_result", "run_id", s.runID(), "err", err)
	}
}

// RecordLLMCall captures the llm() binding's post-dispatch event.
// Token counts and cost arrive only after the provider replies, so
// the event is emitted once on completion — not split across a
// request/response pair like tool calls. EventLLMChunk for streaming
// fragments is deliberately out of scope for this slice (the ledger
// would balloon on long generations) and lands in a follow-on.
func (s *RunSession) RecordLLMCall(model, provider string, promptTokens, outputTokens, cacheReadTokens, cacheWriteTokens int, costUSD float64) {
	if s == nil || s.Recorder == nil {
		return
	}
	if _, err := s.Recorder.Record(persist.EventLLMCall, persist.LLMCallPayload{
		Model:            model,
		Provider:         provider,
		PromptTokens:     promptTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		CostUSD:          costUSD,
	}); err != nil {
		slog.Warn("sandbox: recording llm_call", "run_id", s.runID(), "err", err)
	}
}

// RecordApprovalRequest captures the approval() binding's gate-open
// event. The matching EventApprovalResponse is written from the same
// binding once the Approver replies; the pair lets the inspector show
// the suspension and the resume cleanly.
func (s *RunSession) RecordApprovalRequest(approvalID, prompt string) {
	if s == nil || s.Recorder == nil {
		return
	}
	if _, err := s.Recorder.Record(persist.EventApprovalRequest, persist.ApprovalRequestPayload{
		ApprovalID: approvalID,
		Prompt:     prompt,
	}); err != nil {
		slog.Warn("sandbox: recording approval_request", "run_id", s.runID(), "err", err)
	}
}

// RecordApprovalResponse captures the Approver's reply. Source is
// "auto" for the stub auto-approver, otherwise whatever the gate
// surface reports ("cli", "web", "mcp").
func (s *RunSession) RecordApprovalResponse(approvalID string, approved bool, reason, source string) {
	if s == nil || s.Recorder == nil {
		return
	}
	if _, err := s.Recorder.Record(persist.EventApprovalResponse, persist.ApprovalResponsePayload{
		ApprovalID: approvalID,
		Approved:   approved,
		Reason:     reason,
		Source:     source,
	}); err != nil {
		slog.Warn("sandbox: recording approval_response", "run_id", s.runID(), "err", err)
	}
}

// RecordError writes a mid-run error (a binding goroutine surfacing a
// failure that does not abort the run). The terminal error path is
// the runner's recordFailure helper — bindings only write here.
func (s *RunSession) RecordError(nodeID, message string) {
	if s == nil || s.Recorder == nil {
		return
	}
	if _, err := s.Recorder.Record(persist.EventError, persist.ErrorPayload{
		Message: message,
		NodeID:  nodeID,
	}); err != nil {
		slog.Warn("sandbox: recording error", "run_id", s.runID(), "err", err)
	}
}

// runID is a logging helper — returns "" when the session has no
// recorder so the log line still emits without an extra guard.
func (s *RunSession) runID() string {
	if s == nil || s.Recorder == nil {
		return ""
	}
	return s.Recorder.RunID()
}
