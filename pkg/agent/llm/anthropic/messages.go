package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/gridctl/gridctl/pkg/agent"
)

// translateMessages converts gridctl-shaped messages into the on-wire
// Anthropic message envelope. Anthropic groups text and tool_use as
// content blocks within a single role-tagged message; tool results are
// user-role messages with tool_result content blocks.
//
// Key conversion rules:
//   - RoleSystem messages are rejected — system content belongs in the
//     top-level `system` field of messageRequest. Callers should
//     populate ChatRequest.System instead.
//   - Empty assistant messages with no tool calls are rejected as
//     malformed input rather than silently dropped.
//   - Adjacent tool-result messages are merged into a single user
//     message with multiple tool_result blocks (the Anthropic wire
//     format expects this packing for multi-tool turns).
func translateMessages(in []agent.Message) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(in))
	var pendingToolResults []contentBlock

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		out = append(out, wireMessage{
			Role:    "user",
			Content: pendingToolResults,
		})
		pendingToolResults = nil
	}

	for i, m := range in {
		switch m.Role {
		case agent.RoleSystem:
			return nil, fmt.Errorf("anthropic: system messages must be passed via ChatRequest.System, not Messages (index %d)", i)

		case agent.RoleUser:
			flushToolResults()
			if m.Content == "" {
				return nil, fmt.Errorf("anthropic: user message at index %d has empty content", i)
			}
			out = append(out, wireMessage{
				Role:    "user",
				Content: []contentBlock{{Type: "text", Text: m.Content}},
			})

		case agent.RoleAssistant:
			flushToolResults()
			blocks := make([]contentBlock, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := tc.Arguments
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			if len(blocks) == 0 {
				return nil, fmt.Errorf("anthropic: assistant message at index %d has neither content nor tool_calls", i)
			}
			out = append(out, wireMessage{
				Role:    "assistant",
				Content: blocks,
			})

		case agent.RoleTool:
			if m.ToolCallID == "" {
				return nil, fmt.Errorf("anthropic: tool message at index %d has empty ToolCallID", i)
			}
			pendingToolResults = append(pendingToolResults, contentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			})

		default:
			return nil, fmt.Errorf("anthropic: unknown role %q at message index %d", m.Role, i)
		}
	}

	flushToolResults()
	return out, nil
}
