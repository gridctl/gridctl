package google

import (
	"encoding/json"
	"fmt"

	"github.com/gridctl/gridctl/pkg/agent"
)

// translateMessages converts gridctl-shaped messages into Gemini's
// Content array. Notable rules:
//   - System content lives in systemInstruction, NOT in Contents.
//     Callers must pass system text via ChatRequest.System.
//   - The assistant role on the wire is "model".
//   - Tool results are user-role messages with functionResponse parts.
func translateMessages(in []agent.Message) ([]wireContent, error) {
	out := make([]wireContent, 0, len(in))

	for i, m := range in {
		switch m.Role {
		case agent.RoleSystem:
			return nil, fmt.Errorf("google: system messages must be passed via ChatRequest.System (index %d)", i)

		case agent.RoleUser:
			if m.Content == "" {
				return nil, fmt.Errorf("google: user message at index %d has empty content", i)
			}
			out = append(out, wireContent{
				Role:  "user",
				Parts: []wirePart{{Text: m.Content}},
			})

		case agent.RoleAssistant:
			parts := make([]wirePart, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				parts = append(parts, wirePart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				parts = append(parts, wirePart{
					FunctionCall: &functionCallPart{
						Name: tc.Name,
						Args: args,
					},
				})
			}
			if len(parts) == 0 {
				return nil, fmt.Errorf("google: assistant message at index %d has neither content nor tool_calls", i)
			}
			out = append(out, wireContent{
				Role:  "model",
				Parts: parts,
			})

		case agent.RoleTool:
			if m.Name == "" {
				return nil, fmt.Errorf("google: tool message at index %d has empty Name (Gemini requires the tool name on the response)", i)
			}
			// Wrap the textual content in {"output": "..."} so the
			// model sees a JSON-shaped response. Callers that need
			// structured responses should serialize ahead of time.
			body, _ := json.Marshal(map[string]string{"output": m.Content})
			out = append(out, wireContent{
				Role: "user",
				Parts: []wirePart{{
					FunctionResponse: &functionResponsePart{
						Name:     m.Name,
						Response: body,
					},
				}},
			})

		default:
			return nil, fmt.Errorf("google: unknown role %q at message index %d", m.Role, i)
		}
	}
	return out, nil
}
