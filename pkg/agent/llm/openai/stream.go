package openai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent"
)

// streamChunk is the on-wire shape of a single SSE delta chunk.
type streamChunk struct {
	ID      string         `json:"id"`
	Choices []streamChoice `json:"choices"`
	Usage   *responseUsage `json:"usage"`
}

type streamChoice struct {
	Index        int            `json:"index"`
	Delta        streamDelta    `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type streamDelta struct {
	Role      string             `json:"role,omitempty"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []streamToolCallDelta `json:"tool_calls,omitempty"`
}

type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// readStream drains the OpenAI SSE stream into a slice of agent.ChatChunk.
// Tool-call deltas arrive in pieces (id+name on first delta for an
// index, args fragments thereafter). The function emits each piece as a
// chunk and lets the runtime accumulate.
func readStream(body io.Reader) ([]agent.ChatChunk, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var chunks []agent.ChatChunk

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
			return nil, fmt.Errorf("openai: decode stream chunk: %w", err)
		}

		// Final usage chunk (stream_options.include_usage=true) carries
		// no choices; emit a chunk with only Usage populated.
		if len(chunk.Choices) == 0 && chunk.Usage != nil {
			usage := translateUsage(chunk.Usage)
			chunks = append(chunks, agent.ChatChunk{Usage: &usage})
			continue
		}

		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				chunks = append(chunks, agent.ChatChunk{Delta: c.Delta.Content})
			}
			for _, tc := range c.Delta.ToolCalls {
				delta := agent.ToolCallDelta{
					Index:     tc.Index,
					ID:        tc.ID,
					Name:      tc.Function.Name,
					ArgsDelta: tc.Function.Arguments,
				}
				if delta.ID == "" && delta.Name == "" && delta.ArgsDelta == "" {
					continue
				}
				chunks = append(chunks, agent.ChatChunk{ToolCallDelta: &delta})
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				ck := agent.ChatChunk{StopReason: translateStopReason(*c.FinishReason)}
				if chunk.Usage != nil {
					usage := translateUsage(chunk.Usage)
					ck.Usage = &usage
				}
				chunks = append(chunks, ck)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("openai: read stream: %w", err)
	}
	return chunks, nil
}

func translateUsage(u *responseUsage) agent.Usage {
	out := agent.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil {
		out.CacheReadTokens = u.PromptTokensDetails.CachedTokens
		if out.CacheReadTokens > 0 && out.InputTokens >= out.CacheReadTokens {
			out.InputTokens -= out.CacheReadTokens
		}
	}
	return out
}
