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

// ChatRequest is the gridctl-shaped LLM request envelope. Phase B
// fills in the messages, model selection, tool catalogue, and decoding
// parameters that providers (anthropic, openai, google, gateway-
// passthrough) translate to their wire formats. The empty struct
// declared here lets the rest of pkg/agent refer to the request shape
// without committing to fields that the providers will refine.
type ChatRequest struct{}

// ChatResponse is the gridctl-shaped LLM response envelope. Phase B
// fills in content blocks, tool-use blocks, stop reasons, and usage
// accounting.
type ChatResponse struct{}

// ChatChunk is a single delta in a streaming LLM response. Phase B
// fills in content deltas, tool-use deltas, and per-chunk usage
// accounting. The streaming interface returns *StreamReader[ChatChunk].
type ChatChunk struct{}

// ChatModel is the gridctl-shaped LLM provider surface. Each Phase B
// provider package implements this interface; the agent runtime only
// ever depends on it. Generate is the synchronous shape; Stream
// returns a typed reader whose Close MUST be invoked by the caller.
type ChatModel interface {
	Generate(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (*StreamReader[ChatChunk], error)
}
