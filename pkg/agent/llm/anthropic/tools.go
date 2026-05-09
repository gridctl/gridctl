package anthropic

import (
	"encoding/json"

	"github.com/gridctl/gridctl/pkg/agent"
)

// wireTool is the Anthropic on-wire tool descriptor. Anthropic accepts
// the JSON Schema verbatim under the input_schema key — gridctl
// passes ToolInfo.InputSchema through unchanged.
type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// translateTools converts gridctl ToolInfo entries into the Anthropic
// tool catalog shape. Tools without an explicit InputSchema receive a
// permissive empty-object schema so the model can still emit calls
// with no arguments.
func translateTools(in []agent.ToolInfo) []wireTool {
	out := make([]wireTool, 0, len(in))
	for _, t := range in {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, wireTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out
}
