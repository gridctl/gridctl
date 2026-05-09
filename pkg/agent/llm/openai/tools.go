package openai

import (
	"encoding/json"

	"github.com/gridctl/gridctl/pkg/agent"
)

// wireTool is the OpenAI on-wire tool descriptor. Tools are wrapped in
// a {"type":"function","function":{...}} envelope; gridctl emits only
// "function" tools — Phase B does not surface OpenAI's "code
// interpreter" or "file_search" built-ins through the runtime.
type wireTool struct {
	Type     string         `json:"type"`
	Function toolDescriptor `json:"function"`
}

type toolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// translateTools converts gridctl ToolInfo entries into the OpenAI
// tool catalog shape. A missing InputSchema is filled with a
// permissive empty-object schema, matching the no-arg conventions used
// by other providers.
func translateTools(in []agent.ToolInfo) []wireTool {
	out := make([]wireTool, 0, len(in))
	for _, t := range in {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, wireTool{
			Type: "function",
			Function: toolDescriptor{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	return out
}
