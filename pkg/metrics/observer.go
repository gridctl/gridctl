package metrics

import (
	"context"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/token"
)

// Observer implements mcp.ToolCallObserver and mcp.ClientObserver by
// counting tokens and recording them into an Accumulator.
type Observer struct {
	counter     token.Counter
	accumulator *Accumulator
}

// NewObserver creates a ToolCallObserver that counts tokens and records
// metrics.
func NewObserver(counter token.Counter, accumulator *Accumulator) *Observer {
	return &Observer{
		counter:     counter,
		accumulator: accumulator,
	}
}

// ObserveToolCall counts input/output tokens and records them.
func (o *Observer) ObserveToolCall(serverName string, replicaID int, arguments map[string]any, result *mcp.ToolCallResult) {
	o.observe(serverName, replicaID, "", "", arguments, result)
}

// ObserveToolCallWithClient is the ClientObserver entry point. It records
// the same tokens as ObserveToolCall, additionally attributes them to the
// supplied client, and returns a summary the gateway uses to populate OTel
// GenAI semantic span attributes without re-counting tokens.
func (o *Observer) ObserveToolCallWithClient(_ context.Context, obs mcp.ToolCallObservation) mcp.ToolCallSummary {
	return o.observe(obs.ServerName, obs.ReplicaID, obs.ClientID, obs.ToolName, obs.Arguments, obs.Result)
}

// ObservePromptGet records that a registry skill was served via prompts/get,
// incrementing its cumulative count and last-used timestamp in the parallel
// prompt-usage namespace. The token path does not apply: prompts are
// static content, not tool calls.
func (o *Observer) ObservePromptGet(obs mcp.PromptGetObservation) {
	o.accumulator.RecordPromptGet(obs.PromptName)
}

// observe is the shared core of the legacy and client-aware observer entry
// points. It returns the values needed to set OTel GenAI span attributes
// for callers that pass the call through ObserveToolCallWithClient; the
// legacy ObserveToolCall path discards the return value.
func (o *Observer) observe(serverName string, replicaID int, clientID, toolName string, arguments map[string]any, result *mcp.ToolCallResult) mcp.ToolCallSummary {
	inputTokens := token.CountJSON(o.counter, arguments)

	outputTokens := 0
	var usageMeta *mcp.CallUsage
	if result != nil {
		for _, content := range result.Content {
			outputTokens += o.counter.Count(content.Text)
		}
		usageMeta = result.Usage
	}

	o.accumulator.RecordReplicaWithClient(serverName, replicaID, clientID, inputTokens, outputTokens)
	// Per-tool attribution no-ops on the legacy path (empty toolName), so a
	// legacy observer never creates a phantom "" tool entry.
	o.accumulator.RecordToolCallUsage(serverName, toolName, inputTokens, outputTokens)

	summary := mcp.ToolCallSummary{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	if usageMeta != nil {
		summary.CacheReadTokens = usageMeta.CacheReadTokens
		summary.CacheCreationTokens = usageMeta.CacheCreationTokens
	}
	return summary
}
