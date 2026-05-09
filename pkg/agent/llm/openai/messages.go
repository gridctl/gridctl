package openai

import (
	"fmt"

	"github.com/gridctl/gridctl/pkg/agent"
)

// translateMessages converts gridctl-shaped messages into the OpenAI
// Chat Completions wire format. OpenAI carries system content as a
// system-role message rather than a top-level field; when the
// ChatRequest has both ChatRequest.System and a system-role message
// in Messages, the top-level System wins (the prompt's
// system+messages contract is "system precedes everything").
//
// OpenAI rules respected:
//   - Assistant messages with tool calls may have content="" (the API
//     accepts empty content alongside tool_calls).
//   - Tool messages must carry tool_call_id and content; name is
//     optional and ignored by OpenAI.
func translateMessages(system string, in []agent.Message) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(in)+1)
	if system != "" {
		out = append(out, wireMessage{Role: "system", Content: system})
	}

	for i, m := range in {
		switch m.Role {
		case agent.RoleSystem:
			if system != "" {
				return nil, fmt.Errorf("openai: ChatRequest.System and system-role message both set (index %d)", i)
			}
			out = append(out, wireMessage{Role: "system", Content: m.Content})

		case agent.RoleUser:
			if m.Content == "" {
				return nil, fmt.Errorf("openai: user message at index %d has empty content", i)
			}
			out = append(out, wireMessage{Role: "user", Content: m.Content})

		case agent.RoleAssistant:
			msg := wireMessage{Role: "assistant", Content: m.Content}
			for _, tc := range m.ToolCalls {
				args := string(tc.Arguments)
				if args == "" {
					args = "{}"
				}
				msg.ToolCalls = append(msg.ToolCalls, wireToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: wireFunction{
						Name:      tc.Name,
						Arguments: args,
					},
				})
			}
			if msg.Content == "" && len(msg.ToolCalls) == 0 {
				return nil, fmt.Errorf("openai: assistant message at index %d has neither content nor tool_calls", i)
			}
			out = append(out, msg)

		case agent.RoleTool:
			if m.ToolCallID == "" {
				return nil, fmt.Errorf("openai: tool message at index %d has empty ToolCallID", i)
			}
			out = append(out, wireMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
				Name:       m.Name,
			})

		default:
			return nil, fmt.Errorf("openai: unknown role %q at message index %d", m.Role, i)
		}
	}
	return out, nil
}
