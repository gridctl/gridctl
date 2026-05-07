package metrics

import (
	"context"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pricing"
	"github.com/gridctl/gridctl/pkg/token"
)

// ModelResolver returns the configured model ID for a server, or "" when
// the server has no model attribution. Used by the Observer when a tool
// result does not carry a model in its CallUsage metadata. Resolvers must
// be safe for concurrent calls.
type ModelResolver func(serverName string) string

// Observer implements mcp.ToolCallObserver and mcp.ClientObserver by
// counting tokens, pricing the call against the active pricing.Source,
// and recording both into an Accumulator.
type Observer struct {
	counter       token.Counter
	accumulator   *Accumulator
	modelResolver ModelResolver
}

// NewObserver creates a ToolCallObserver that counts tokens and records
// metrics. The cost path is wired but inert until SetModelResolver
// installs a server -> model mapping or tool results carry CallUsage with
// a model field. Until then RecordCost is called only when both the call
// reports a model and that model is known to the active pricing.Source.
func NewObserver(counter token.Counter, accumulator *Accumulator) *Observer {
	return &Observer{
		counter:     counter,
		accumulator: accumulator,
	}
}

// SetModelResolver installs the server -> model resolver used as a fallback
// when a tool result does not carry a model in its CallUsage. Passing nil
// clears the resolver, after which only call-level model attribution is
// honored.
func (o *Observer) SetModelResolver(r ModelResolver) {
	o.modelResolver = r
}

// ObserveToolCall counts input/output tokens and records them, then prices
// the call against the active pricing.Source and records the per-component
// USD breakdown alongside the tokens.
//
// The cost path is best-effort: a call against an unknown model records
// tokens normally and skips RecordCost. Cache-read and cache-write tokens
// reported in result._meta (CallUsage) are priced via the provider's
// cache rates rather than rolled into the input rate.
func (o *Observer) ObserveToolCall(serverName string, replicaID int, arguments map[string]any, result *mcp.ToolCallResult) {
	o.observe(serverName, replicaID, "", "", arguments, result)
}

// ObserveToolCallWithClient is the ClientObserver entry point. It records
// the same tokens + cost as ObserveToolCall, additionally attributes
// them to the supplied client, and returns a summary the gateway uses to
// populate OTel GenAI semantic span attributes without re-counting tokens.
func (o *Observer) ObserveToolCallWithClient(_ context.Context, obs mcp.ToolCallObservation) mcp.ToolCallSummary {
	return o.observe(obs.ServerName, obs.ReplicaID, obs.ClientID, obs.ToolName, obs.Arguments, obs.Result)
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
	o.accumulator.RecordToolCall(serverName, toolName)

	summary := mcp.ToolCallSummary{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	if usageMeta != nil {
		summary.CacheReadTokens = usageMeta.CacheReadTokens
		summary.CacheCreationTokens = usageMeta.CacheCreationTokens
	}

	model := o.resolveModel(serverName, usageMeta)
	if model == "" {
		return summary
	}
	summary.Model = model

	usage := pricing.Usage{
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  summary.CacheReadTokens,
		CacheWriteTokens: summary.CacheCreationTokens,
	}
	cost, ok := pricing.CalculateBreakdown(model, usage)
	if !ok {
		return summary
	}
	o.accumulator.RecordCostWithClient(serverName, replicaID, clientID, CostBreakdown{
		Input:      cost.Input,
		Output:     cost.Output,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
	})
	summary.CostUSD = cost.Input + cost.Output + cost.CacheRead + cost.CacheWrite
	summary.HasCost = summary.CostUSD > 0
	return summary
}

// resolveModel picks the model ID for a call: the call-level model wins,
// then the server-level resolver fallback, then "" (skip pricing).
func (o *Observer) resolveModel(serverName string, usage *mcp.CallUsage) string {
	if usage != nil && usage.Model != "" {
		return usage.Model
	}
	if o.modelResolver != nil {
		return o.modelResolver(serverName)
	}
	return ""
}
