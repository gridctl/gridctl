// Package agent is the gridctl agent runtime. It hosts typed graph
// composition (via the internal/eino adapter), an LLM provider
// abstraction, the typed Skill SDK, the multi-agent orchestrator, and
// JSONL run persistence. The runtime sits on top of the existing MCP
// gateway: tool calls flow through pkg/mcp.Gateway so existing tracing,
// pricing, replica routing, vault auth, and tool whitelisting apply
// unchanged.
//
// This file ships the public type surface that the rest of pkg/agent
// (and downstream callers) build against. The wrappers around
// cloudwego/eino live in pkg/agent/internal/eino — that boundary is
// where reversibility lives, and is enforced in CI by
// scripts/check-eino-boundary.sh.
package agent

import (
	"context"
	"encoding/json"

	einoadapter "github.com/gridctl/gridctl/pkg/agent/internal/eino"
)

// Graph is a typed graph composition that compiles into a Runnable.
// The underlying composition library is hidden behind the adapter
// boundary; callers interact only with this gridctl-shaped surface.
type Graph[I, O any] = einoadapter.Graph[I, O]

// Runnable is a compiled, executable graph. It exposes Invoke for
// synchronous execution and Stream for chunked streaming output.
type Runnable[I, O any] = einoadapter.Runnable[I, O]

// StreamReader emits typed chunks from a streaming Runnable. Recv
// returns io.EOF on stream completion; callers are responsible for
// Close.
type StreamReader[T any] = einoadapter.StreamReader[T]

// NewGraph creates an empty typed graph keyed by the input and output
// types. Wire it from START to END via AddEdge before Compile.
func NewGraph[I, O any]() *Graph[I, O] {
	return einoadapter.NewGraph[I, O]()
}

// StreamReaderFromSlice wraps a slice as a StreamReader. Phase B
// provider adapters use it to bridge non-streaming responses into the
// streaming interface; tests use it for fixtures.
func StreamReaderFromSlice[T any](items []T) *StreamReader[T] {
	return einoadapter.StreamReaderFromSlice(items)
}

// START is the implicit graph entry vertex. Use it as the source
// argument to AddEdge to receive the graph's input.
const START = einoadapter.START

// END is the implicit graph exit vertex. Use it as the destination
// argument to AddEdge to surface a node's output as the graph's
// output.
const END = einoadapter.END

// ToolInfo is the gridctl-shaped tool descriptor used across the agent
// runtime. It is intentionally derivable from pkg/mcp.Tool: a
// registered typed skill becomes a tool in the same envelope the
// gateway already routes for any other MCP tool, and an upstream
// client that points at a gridctl gateway sees the same shape whether
// the tool is implemented as a typed Go skill, a TS skill, or a
// downstream MCP server.
//
// Defined here, not in pkg/mcp, to keep Phase A from touching pkg/mcp.
// Phase C reconciles the two when the registry walker grows to
// recognise typed-skill metadata; for now the structural overlap is
// intentional.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Role is a chat-message role used by ChatRequest.Messages. Providers
// translate roles to their wire representations. The vocabulary is
// intentionally narrow; provider adapters reject unknown roles rather
// than silently coercing them.
type Role string

const (
	// RoleSystem is a system instruction. Anthropic surfaces this
	// through the top-level `system` field rather than a message;
	// providers translate accordingly.
	RoleSystem Role = "system"

	// RoleUser is a user-authored message.
	RoleUser Role = "user"

	// RoleAssistant is a model-authored message.
	RoleAssistant Role = "assistant"

	// RoleTool is a tool-result message attached to a prior assistant
	// tool_call. The ToolCallID field references the call.
	RoleTool Role = "tool"
)

// Message is a single chat-message exchanged with a Provider. A message
// either carries human/assistant text content, tool-call requests
// (assistant), or a tool-call result (tool). Cross-provider translation
// is the responsibility of each provider package.
type Message struct {
	// Role is "system", "user", "assistant", or "tool".
	Role Role `json:"role"`

	// Content is the textual body of the message. Empty when an
	// assistant message contains only ToolCalls.
	Content string `json:"content,omitempty"`

	// ToolCalls are the tool invocations an assistant message asks
	// the runtime to perform. Populated only for Role == RoleAssistant.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolCallID is the ID of the tool call this message answers.
	// Populated only for Role == RoleTool. Must reference a prior
	// assistant ToolCall.ID in the same conversation.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Name is the tool name for tool-result messages. Some providers
	// (notably Anthropic) require the tool name on the result block;
	// others (OpenAI, Gemini) ignore it. Required when Role == RoleTool.
	Name string `json:"name,omitempty"`
}

// ToolCall is a model-issued request to invoke a tool. The runtime
// resolves Arguments against the tool's input schema, then invokes
// the tool through the gateway via a ToolCaller. Each provider package
// translates its native tool-use representation to/from this shape so
// the compose graph never sees provider-specific tool formats.
type ToolCall struct {
	// ID is the provider-issued tool-call identifier. Required so
	// tool-result messages can reference the originating call.
	ID string `json:"id"`

	// Name is the tool name (no server prefix; the gateway prefixes
	// when needed).
	Name string `json:"name"`

	// Arguments is the JSON-encoded argument object as the model
	// emitted it. Callers MUST validate against the tool's input
	// schema before invoking; raw JSON is preserved verbatim so
	// provider quirks (key ordering, escaping) are visible.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the runtime's reply to a ToolCall after the gateway
// has invoked the tool. Providers translate this into their tool-result
// wire shape (Anthropic `tool_result` content block, OpenAI tool role
// message, Gemini function-response part).
type ToolResult struct {
	// ToolCallID references the assistant message's ToolCall.ID.
	ToolCallID string `json:"tool_call_id"`

	// Name is the tool name. Some providers require it on the result.
	Name string `json:"name,omitempty"`

	// Output is the textual content surfaced to the model. The runtime
	// renders gateway ToolCallResult.Content into a single string here;
	// structured-content support lands in a follow-up.
	Output string `json:"output"`

	// IsError reports whether the tool invocation produced an error.
	// Providers map this to their error-flag conventions (Anthropic
	// `is_error: true`, OpenAI tool error message, Gemini error part).
	IsError bool `json:"is_error,omitempty"`
}

// Usage is the per-call token accounting reported by a Provider.
// Cache fields default to zero when the provider does not surface
// cache usage. Gateway-level cost recording prices the four components
// independently — see pkg/pricing.
type Usage struct {
	// InputTokens is the count of prompt tokens billed at the input
	// rate. Excludes cache-read tokens, which are priced separately.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the count of generated tokens billed at the
	// output rate.
	OutputTokens int `json:"output_tokens"`

	// CacheReadTokens is the count of input tokens served from a
	// prompt cache. Anthropic and OpenAI both report this; Gemini
	// surfaces it through cached_content_token_count.
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`

	// CacheWriteTokens is the count of input tokens written to a
	// prompt cache (Anthropic-only on the providers we cover).
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// StopReason describes why a model stopped generating. Each provider
// maps its native stop-reason vocabulary to one of these values; the
// compose graph and UI both depend on the gridctl-shaped form.
type StopReason string

const (
	// StopReasonEnd is a natural end of generation.
	StopReasonEnd StopReason = "end"

	// StopReasonMaxTokens is the model hitting MaxTokens before
	// finishing.
	StopReasonMaxTokens StopReason = "max_tokens"

	// StopReasonToolUse is the model emitting a tool_use stop, which
	// the runtime must satisfy with tool results before resuming.
	StopReasonToolUse StopReason = "tool_use"

	// StopReasonStopSequence is the model hitting a configured stop
	// sequence.
	StopReasonStopSequence StopReason = "stop_sequence"

	// StopReasonError is a provider-side error that aborted
	// generation. The accompanying response carries the error text.
	StopReasonError StopReason = "error"
)

// ChatRequest is the gridctl-shaped LLM request envelope. Providers
// translate it to their wire formats. Required fields: Model and
// Messages. System is the conversation-level system prompt; providers
// that take a top-level `system` field (Anthropic) populate that field,
// providers that take a system-role message (OpenAI, Gemini) prepend a
// message instead.
type ChatRequest struct {
	// Model is the canonical model ID (e.g. "claude-opus-4-7",
	// "gpt-4o", "gemini-2.0-flash"). Provider packages reject IDs
	// outside their family.
	Model string `json:"model"`

	// Messages are the conversation history in order. The last
	// message is the active turn.
	Messages []Message `json:"messages"`

	// System is an optional conversation-level system prompt.
	// Providers that take a top-level `system` field populate that
	// field; providers without one prepend a system-role message.
	System string `json:"system,omitempty"`

	// Tools is the catalog the model may invoke. Empty disables tool
	// use. Each provider translates ToolInfo to its own catalog
	// shape.
	Tools []ToolInfo `json:"tools,omitempty"`

	// Temperature is the sampling temperature. Zero means "use the
	// provider default"; explicit zero is not addressable through
	// this field. Negative values are rejected at the provider layer.
	Temperature float64 `json:"temperature,omitempty"`

	// MaxTokens is the maximum number of tokens the model may emit.
	// Zero means "use the provider default" (Anthropic requires a
	// value; the provider supplies one).
	MaxTokens int `json:"max_tokens,omitempty"`

	// StopSequences are textual sequences that, when generated, end
	// the model's turn. Providers translate verbatim.
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// ChatResponse is the gridctl-shaped LLM response envelope returned by
// Provider.Generate. Streamed responses deliver the same data through
// ChatChunk; see Provider.Stream.
type ChatResponse struct {
	// Model is the canonical model ID the provider actually served
	// the call with. May differ from ChatRequest.Model when the
	// provider auto-substitutes (e.g. "claude-3-5-sonnet-latest" →
	// dated revision).
	Model string `json:"model"`

	// Content is the textual body of the assistant turn. Empty when
	// the turn consisted only of tool-use blocks.
	Content string `json:"content,omitempty"`

	// ToolCalls are the tool invocations the model wants the runtime
	// to perform. The runtime is responsible for executing them and
	// feeding the results back through subsequent ChatRequests.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// StopReason is the gridctl-shaped reason generation ended.
	StopReason StopReason `json:"stop_reason"`

	// Usage is the per-call token accounting.
	Usage Usage `json:"usage"`
}

// ChatChunk is a single delta in a streaming Provider response. Providers
// emit chunks in the order they arrive on the wire; the runtime is
// responsible for accumulating Content across token chunks and
// stitching ToolCallDelta entries into complete ToolCalls before
// surfacing them.
type ChatChunk struct {
	// Delta is appended text since the last chunk. Empty for chunks
	// that only carry tool-call deltas, usage, or stop information.
	Delta string `json:"delta,omitempty"`

	// ToolCallDelta carries an incremental update to a tool call.
	// Empty when the chunk only carries text or end-of-stream
	// metadata.
	ToolCallDelta *ToolCallDelta `json:"tool_call_delta,omitempty"`

	// Usage is populated only on the final chunk of the stream when
	// the provider reports usage at the end (Anthropic and OpenAI
	// both do; Gemini reports per-chunk usage that the runtime
	// accumulates).
	Usage *Usage `json:"usage,omitempty"`

	// StopReason is populated on the final chunk when generation
	// ended cleanly. Empty on intermediate chunks.
	StopReason StopReason `json:"stop_reason,omitempty"`
}

// ToolCallDelta is an incremental update to a single ToolCall during
// streaming. Providers emit Index to identify which call the delta
// belongs to (the same call may receive multiple deltas across many
// chunks). On the first delta for a given Index, ID and Name are
// populated; subsequent deltas carry only ArgsDelta (a partial JSON
// fragment for Arguments).
type ToolCallDelta struct {
	// Index identifies which tool call this delta belongs to. Tool
	// calls are emitted in declaration order (0, 1, 2, ...).
	Index int `json:"index"`

	// ID is the tool-call identifier. Populated on the first delta
	// for an index.
	ID string `json:"id,omitempty"`

	// Name is the tool name. Populated on the first delta for an
	// index.
	Name string `json:"name,omitempty"`

	// ArgsDelta is a partial JSON fragment for the call's Arguments.
	// Concatenating all ArgsDelta values for a given Index yields the
	// final Arguments JSON.
	ArgsDelta string `json:"args_delta,omitempty"`
}

// ChatModel is the gridctl-shaped LLM provider surface. Each Phase B
// provider package implements this interface; the agent runtime only
// ever depends on it. Generate is the synchronous shape; Stream
// returns a typed reader whose Close MUST be invoked by the caller.
type ChatModel interface {
	Generate(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (*StreamReader[ChatChunk], error)
}
