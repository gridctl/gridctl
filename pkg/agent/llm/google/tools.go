package google

import (
	"encoding/json"

	"github.com/gridctl/gridctl/pkg/agent"
)

// wireToolset is the Gemini tool catalogue envelope. Functions are
// nested one level deeper than other providers — Gemini groups them
// under "functionDeclarations".
type wireToolset struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

type functionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// translateTools converts gridctl ToolInfo entries into the Gemini
// catalogue. Gemini accepts JSON Schema directly under parameters,
// matching gridctl's ToolInfo.InputSchema verbatim.
func translateTools(in []agent.ToolInfo) []wireToolset {
	if len(in) == 0 {
		return nil
	}
	decls := make([]functionDeclaration, 0, len(in))
	for _, t := range in {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		decls = append(decls, functionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  schema,
		})
	}
	return []wireToolset{{FunctionDeclarations: decls}}
}
