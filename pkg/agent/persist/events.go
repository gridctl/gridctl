// Package persist is the JSONL run-state ledger for the gridctl agent
// runtime. Every run owns a single append-only file at
// ~/.gridctl/runs/<run_id>.jsonl; each line is a typed Event encoding
// one observable boundary in the run's lifecycle (node entry/exit, a
// tool or LLM invocation, a structured-output validation, an approval
// gate exchange, or a terminal error).
//
// The format is the source of truth for time-travel resume: replaying
// events in order reconstructs the run's state, which the runtime then
// hands to the eino adapter's checkpoint/resume surface (see
// pkg/agent/internal/eino) to continue execution from a chosen step.
//
// Persistence is intentionally additive — events are append-only so a
// crash mid-write can never corrupt earlier history; consumers ignore
// trailing partial lines on read. The JSONL shape also gives operators
// a tail-and-grep affordance without bringing up the daemon.
package persist

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventType discriminates Event records on disk. The vocabulary is
// closed: callers MUST use one of the constants below so the reader
// can route each event to a typed payload without falling back to a
// generic map.
type EventType string

const (
	// EventRunStarted is emitted exactly once per run, before any
	// node activity. It anchors the run's metadata (skill, input
	// digest, parent) so a reader can render the run header without
	// scanning the whole file.
	EventRunStarted EventType = "run_started"

	// EventRunCompleted is the terminal event for a run that ran to
	// completion. Carries the final status (ok / error / cancelled)
	// and the OTel root span ID so traces can be cross-referenced.
	EventRunCompleted EventType = "run_completed"

	// EventNodeEnter is emitted when execution enters a graph node.
	// NodeID identifies the call site; NodeName is the author-facing
	// label.
	EventNodeEnter EventType = "node_enter"

	// EventNodeExit is emitted when a node returns. Carries the
	// duration in microseconds and the success flag; a node that
	// errored emits both EventNodeExit (with success=false) and
	// EventError (with the propagated message).
	EventNodeExit EventType = "node_exit"

	// EventToolCall captures a tool invocation request. The arguments
	// are stored verbatim (json.RawMessage) so the model's exact
	// emission survives a round-trip without provider-specific
	// reformatting.
	EventToolCall EventType = "tool_call"

	// EventToolResult captures the runtime's reply to a tool call.
	// IsError mirrors the gateway's error flag so a reader can render
	// failures distinctly without parsing the output.
	EventToolResult EventType = "tool_result"

	// EventLLMCall captures a Provider.Generate or Provider.Stream
	// invocation. Records the model, prompt-token count, and any
	// stop sequences so cost can be reconstructed offline.
	EventLLMCall EventType = "llm_call"

	// EventLLMChunk captures a single streaming chunk. Chunks are
	// optional in the persisted ledger — recorders may skip them for
	// large streams and emit only the final response — but when
	// present they are ordered per-call.
	EventLLMChunk EventType = "llm_chunk"

	// EventStructuredOutput captures a validated structured-output
	// result. Carries the JSON schema fingerprint and the validated
	// payload; the schema delta UI in the IDE diff-renders against
	// this event.
	EventStructuredOutput EventType = "structured_output"

	// EventApprovalRequest is emitted when the runtime hits an
	// approval gate. The accompanying ApprovalID is the handle a CLI
	// or web-UI consumer uses to respond.
	EventApprovalRequest EventType = "approval_request"

	// EventApprovalResponse is the matching reply: approve or reject,
	// with optional reason text. The runtime resumes (or aborts) the
	// run after writing this event.
	EventApprovalResponse EventType = "approval_response"

	// EventError is emitted whenever the runtime catches an error it
	// cannot handle locally. May appear mid-run (e.g. a transient
	// provider failure that the orchestrator retries) or as the
	// terminal event for a failed run.
	EventError EventType = "error"
)

// Event is a single line in a run's JSONL ledger. Payload is shaped
// per Type — readers switch on Type and unmarshal Payload into the
// matching struct. The raw JSON is preserved verbatim so a future
// reader running against an older file doesn't drop fields it doesn't
// recognise.
type Event struct {
	// RunID is the run this event belongs to. Repeated on every line
	// so a reader can sanity-check the file's contents.
	RunID string `json:"run_id"`

	// Seq is the event's position in the run. Monotonic from 1; gaps
	// indicate a corrupted ledger.
	Seq uint64 `json:"seq"`

	// Time is the wall-clock timestamp the event was recorded. UTC
	// always; the recorder normalises before writing.
	Time time.Time `json:"time"`

	// Type discriminates Payload. Always one of the EventType
	// constants above.
	Type EventType `json:"type"`

	// Payload is the type-specific body. Stored as RawMessage on
	// read to avoid forcing the reader into a sealed-union shape.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RunStartedPayload is the body of an EventRunStarted event.
type RunStartedPayload struct {
	// Skill is the unprefixed skill name the run was launched
	// against. Empty when the run is an ad-hoc graph composition with
	// no registered skill.
	Skill string `json:"skill,omitempty"`

	// InputDigest is the SHA-256 of the canonical-JSON-encoded
	// input. Recorded so the inspect view can collapse identical
	// inputs across re-runs without surfacing the full payload.
	InputDigest string `json:"input_digest,omitempty"`

	// Input is the raw input as the caller supplied it. Stored
	// verbatim so resume can re-issue the run without recomputing.
	Input json.RawMessage `json:"input,omitempty"`

	// ParentRunID links a child run (e.g. a Handoff) to its parent.
	// Empty for a top-level run.
	ParentRunID string `json:"parent_run_id,omitempty"`

	// TraceID is the OTel root span ID for the run, hex-encoded.
	// Empty when tracing is not configured.
	TraceID string `json:"trace_id,omitempty"`

	// Flavor describes whether the run originated from a typed Go
	// skill ("go") or a TypeScript skill in the sandbox ("ts").
	// Markdown-only prompt skills surface as "prompt".
	Flavor string `json:"flavor,omitempty"`
}

// RunCompletedPayload is the body of an EventRunCompleted event.
type RunCompletedPayload struct {
	// Status is one of "ok", "error", "cancelled", "suspended" — the
	// last meaning the run is awaiting an approval response.
	Status string `json:"status"`

	// Output is the run's final output payload, when Status is
	// "ok". JSON-encoded; readers decode against the skill's output
	// schema. Truncated to MaxPayloadBytes (and wrapped as a JSON
	// string) when Truncated is set.
	Output json.RawMessage `json:"output,omitempty"`

	// Truncated is true when Output was capped at MaxPayloadBytes.
	Truncated bool `json:"truncated,omitempty"`

	// Error is the terminal error message when Status is "error" or
	// "cancelled". Empty otherwise.
	Error string `json:"error,omitempty"`
}

// NodeEnterPayload is the body of an EventNodeEnter event.
type NodeEnterPayload struct {
	// NodeID is a stable identifier for the node within the run
	// graph. Used by `runs resume --from-step <node_id>`.
	NodeID string `json:"node_id"`

	// NodeName is the author-facing label. May differ from NodeID
	// (e.g. autogenerated IDs for anonymous lambda nodes).
	NodeName string `json:"node_name,omitempty"`

	// SpanID is the OTel span the node executes under. Empty when
	// tracing is not configured.
	SpanID string `json:"span_id,omitempty"`
}

// NodeExitPayload is the body of an EventNodeExit event.
type NodeExitPayload struct {
	// NodeID matches the EventNodeEnter that opened the node.
	NodeID string `json:"node_id"`

	// DurationMicros is the wall-clock duration the node held
	// execution, in microseconds. The microsecond resolution keeps
	// the JSONL parseable as integers across languages while still
	// resolving fast Go skills.
	DurationMicros int64 `json:"duration_micros"`

	// Success reports whether the node returned without error.
	Success bool `json:"success"`
}

// ToolCallPayload is the body of an EventToolCall event.
type ToolCallPayload struct {
	// CallID is the provider-issued tool-call identifier; matches
	// agent.ToolCall.ID. Used to pair tool_call with tool_result.
	CallID string `json:"call_id"`

	// NodeID, when set, links the tool call to the wrapping
	// node_enter/node_exit pair so the inspector can collapse
	// per-tool detail under its node row.
	NodeID string `json:"node_id,omitempty"`

	// Name is the prefixed tool name (server__tool) the gateway
	// dispatches against. Recording the prefixed form keeps the
	// ledger unambiguous when the same tool name is exposed by
	// multiple servers.
	Name string `json:"name"`

	// Arguments is the JSON-encoded argument object as the model
	// emitted it. Verbatim — provider key ordering and escaping is
	// preserved, unless the encoded form exceeded MaxPayloadBytes,
	// in which case it is replaced with the first MaxPayloadBytes
	// as a JSON-quoted string and Truncated is set. Consumers
	// treating a truncated payload as opaque get a syntactically
	// valid value either way.
	Arguments json.RawMessage `json:"arguments,omitempty"`

	// Truncated is true when Arguments was capped at MaxPayloadBytes.
	// Inspectors render the prefix and a "truncated" marker.
	Truncated bool `json:"truncated,omitempty"`
}

// ToolResultPayload is the body of an EventToolResult event.
type ToolResultPayload struct {
	// CallID matches the EventToolCall it answers.
	CallID string `json:"call_id"`

	// NodeID, when set, mirrors the matching EventToolCall.NodeID so
	// the inspector can pair the two without an arguments lookup.
	NodeID string `json:"node_id,omitempty"`

	// Output is the textual content the runtime surfaced to the
	// model. Multi-block content collapses to a single string here;
	// structured-content support follows when the gateway exposes it
	// uniformly. Truncated to MaxPayloadBytes when Truncated is set.
	Output string `json:"output,omitempty"`

	// Truncated is true when Output was capped at MaxPayloadBytes.
	Truncated bool `json:"truncated,omitempty"`

	// IsError mirrors the gateway's error flag.
	IsError bool `json:"is_error,omitempty"`
}

// LLMCallPayload is the body of an EventLLMCall event.
type LLMCallPayload struct {
	// Model is the canonical model ID the runtime asked for.
	Model string `json:"model"`

	// Provider is the gridctl provider package responsible for the
	// call ("anthropic", "openai", "google", "gateway"). Populated
	// by the adapter, not the model.
	Provider string `json:"provider,omitempty"`

	// PromptTokens is the input-token count reported by the
	// provider.
	PromptTokens int `json:"prompt_tokens,omitempty"`

	// OutputTokens is the output-token count reported by the
	// provider on completion. Zero for events recorded before the
	// final chunk.
	OutputTokens int `json:"output_tokens,omitempty"`

	// CacheReadTokens captures cache-hit savings; cache-write tokens
	// land alongside (Anthropic-only on the providers we cover).
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`

	// CostUSD is the dollar cost computed by pkg/pricing, in
	// fractional US dollars. Stored alongside the call so the
	// inspect view doesn't need the pricing table to render the
	// totals.
	CostUSD float64 `json:"cost_usd,omitempty"`
}

// LLMChunkPayload is the body of an EventLLMChunk event.
type LLMChunkPayload struct {
	// Index is the chunk's order within the streamed response.
	// Monotonic from 0.
	Index int `json:"index"`

	// Delta is the text appended in this chunk.
	Delta string `json:"delta,omitempty"`
}

// StructuredOutputPayload is the body of an EventStructuredOutput
// event.
type StructuredOutputPayload struct {
	// SchemaDigest is the SHA-256 of the canonical JSON Schema the
	// payload was validated against. Used by the IDE's schema-delta
	// view to highlight cross-run drift.
	SchemaDigest string `json:"schema_digest,omitempty"`

	// Output is the validated payload, JSON-encoded.
	Output json.RawMessage `json:"output"`
}

// ApprovalRequestPayload is the body of an EventApprovalRequest
// event. The runtime suspends after writing this event and waits for
// a matching EventApprovalResponse.
type ApprovalRequestPayload struct {
	// ApprovalID is the handle the CLI / web-UI / MCP consumer uses
	// to respond. Format is run-id-scoped: ULID-shaped, deterministic
	// across replay so a resumed run sees the same handle.
	ApprovalID string `json:"approval_id"`

	// Prompt is the human-readable description of what's being
	// approved. Authors emit this from the skill via the approval()
	// binding (TS) or the Approver interface (Go).
	Prompt string `json:"prompt"`

	// TimeoutSeconds is the server-side timeout after which the
	// runtime auto-rejects the request. Zero means "no timeout";
	// callers should default to 86_400 (24h) per the design.
	TimeoutSeconds int64 `json:"timeout_seconds,omitempty"`

	// WarnAtSeconds is the elapsed time after which the runtime
	// emits a "still pending" warning to slog. Defaults to 80% of
	// TimeoutSeconds when zero on read.
	WarnAtSeconds int64 `json:"warn_at_seconds,omitempty"`
}

// ApprovalResponsePayload is the body of an EventApprovalResponse
// event.
type ApprovalResponsePayload struct {
	// ApprovalID matches the EventApprovalRequest it answers.
	ApprovalID string `json:"approval_id"`

	// Approved reports the human's decision.
	Approved bool `json:"approved"`

	// Reason is the optional free-text justification.
	Reason string `json:"reason,omitempty"`

	// Source records who supplied the response: "cli", "web",
	// "mcp", or "timeout" for an auto-rejection.
	Source string `json:"source,omitempty"`
}

// ErrorPayload is the body of an EventError event.
type ErrorPayload struct {
	// Message is the propagated error text.
	Message string `json:"message"`

	// NodeID names the node that produced the error, when known.
	// Empty for runtime-level errors that don't belong to a single
	// node.
	NodeID string `json:"node_id,omitempty"`
}

// MaxPayloadBytes caps the verbatim slice of `Arguments` / `Output` an
// event carries. The cap protects the ledger from authors feeding a
// multi-megabyte file read or LLM response straight into a tool call —
// the JSONL file would balloon, the SSE stream would stall, and the
// IDE would gag on a single 50 MB line. 64 KB is large enough to keep
// the common path lossless (typical tool args and prose outputs) and
// small enough that a truncated payload still renders quickly.
const MaxPayloadBytes = 64 * 1024

// CapRawJSON returns a value safe to store as a payload field of type
// json.RawMessage, along with a flag indicating whether the original
// exceeded the cap. Over-cap values are replaced with a JSON-encoded
// string of the first MaxPayloadBytes — that keeps the field
// syntactically valid JSON (so readers don't choke) while flagging
// loss. Callers should also set their payload's Truncated field when
// the returned flag is true.
func CapRawJSON(raw []byte) (json.RawMessage, bool) {
	if len(raw) <= MaxPayloadBytes {
		return raw, false
	}
	quoted, err := json.Marshal(string(raw[:MaxPayloadBytes]))
	if err != nil {
		// json.Marshal of a string never fails in practice; fall back
		// to a minimal valid placeholder rather than poison the ledger.
		return json.RawMessage(`""`), true
	}
	return quoted, true
}

// CapString returns the first MaxPayloadBytes of s and a flag noting
// whether the original was longer. Used for plain-text payload fields
// (e.g. ToolResultPayload.Output) where the cap is purely a length
// trim — no JSON-quoting is needed.
func CapString(s string) (string, bool) {
	if len(s) <= MaxPayloadBytes {
		return s, false
	}
	return s[:MaxPayloadBytes], true
}

// MarshalEvent renders a typed payload into an Event ready for write.
// The payload is JSON-encoded once here so the recorder doesn't need
// type-switching at the file-I/O layer.
func MarshalEvent(runID string, seq uint64, eventType EventType, payload any) (Event, error) {
	out := Event{
		RunID: runID,
		Seq:   seq,
		Time:  time.Now().UTC(),
		Type:  eventType,
	}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Event{}, fmt.Errorf("encoding %s payload: %w", eventType, err)
		}
		out.Payload = raw
	}
	return out, nil
}
