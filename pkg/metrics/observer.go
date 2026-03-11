package metrics

import (
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/token"
)

// Observer implements mcp.ToolCallObserver by counting tokens and recording
// them into an Accumulator.
type Observer struct {
	counter     token.Counter
	accumulator *Accumulator
}

// NewObserver creates a ToolCallObserver that counts tokens and records metrics.
func NewObserver(counter token.Counter, accumulator *Accumulator) *Observer {
	return &Observer{
		counter:     counter,
		accumulator: accumulator,
	}
}

// ObserveToolCall counts input/output tokens and records them.
func (o *Observer) ObserveToolCall(serverName string, arguments map[string]any, result *mcp.ToolCallResult) {
	inputTokens := token.CountJSON(o.counter, arguments)

	outputTokens := 0
	if result != nil {
		for _, content := range result.Content {
			outputTokens += o.counter.Count(content.Text)
		}
	}

	o.accumulator.Record(serverName, inputTokens, outputTokens)
}
