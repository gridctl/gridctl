package metrics

import (
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pricing"
	"github.com/gridctl/gridctl/pkg/token"
)

// ModelResolver returns the configured model ID for a server, or "" when
// the server has no model attribution. Used by the Observer when a tool
// result does not carry a model in its CallUsage metadata. Resolvers must
// be safe for concurrent calls.
type ModelResolver func(serverName string) string

// Observer implements mcp.ToolCallObserver by counting tokens, pricing the
// call against the active pricing.Source, and recording both into an
// Accumulator.
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
	inputTokens := token.CountJSON(o.counter, arguments)

	outputTokens := 0
	var usageMeta *mcp.CallUsage
	if result != nil {
		for _, content := range result.Content {
			outputTokens += o.counter.Count(content.Text)
		}
		usageMeta = result.Usage
	}

	o.accumulator.RecordReplica(serverName, replicaID, inputTokens, outputTokens)

	model := o.resolveModel(serverName, usageMeta)
	if model == "" {
		return
	}
	usage := pricing.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	if usageMeta != nil {
		usage.CacheReadTokens = usageMeta.CacheReadTokens
		usage.CacheWriteTokens = usageMeta.CacheCreationTokens
	}
	cost, ok := pricing.CalculateBreakdown(model, usage)
	if !ok {
		return
	}
	o.accumulator.RecordCost(serverName, replicaID, CostBreakdown{
		Input:      cost.Input,
		Output:     cost.Output,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
	})
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
