package google

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent"
)

// readStream drains the Gemini SSE stream into a slice of agent.ChatChunk.
//
// Gemini's streaming format mirrors the non-stream response shape but
// emits one full JSON object per SSE frame (each frame contains a
// candidates array with a partial Content). The final frame carries
// finishReason and usageMetadata.
func readStream(body io.Reader, requestedModel string) ([]agent.ChatChunk, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var (
		chunks         []agent.ChatChunk
		toolCallIndex  = 0
		seenFinishStop bool
	)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if raw == "" || raw == "[DONE]" {
			continue
		}

		var resp generateResponse
		if err := json.Unmarshal([]byte(raw), &resp); err != nil {
			return nil, fmt.Errorf("google: decode stream chunk: %w", err)
		}

		if len(resp.Candidates) == 0 {
			continue
		}
		c := resp.Candidates[0]
		hasFunctionCalls := false
		for _, part := range c.Content.Parts {
			if part.Text != "" {
				chunks = append(chunks, agent.ChatChunk{Delta: part.Text})
			}
			if part.FunctionCall != nil {
				hasFunctionCalls = true
				args := part.FunctionCall.Args
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				chunks = append(chunks, agent.ChatChunk{
					ToolCallDelta: &agent.ToolCallDelta{
						Index:     toolCallIndex,
						ID:        fmt.Sprintf("%s-%d", part.FunctionCall.Name, toolCallIndex),
						Name:      part.FunctionCall.Name,
						ArgsDelta: string(args),
					},
				})
				toolCallIndex++
			}
		}
		if c.FinishReason != "" && !seenFinishStop {
			seenFinishStop = true
			usage := translateUsage(resp.UsageMetadata)
			chunks = append(chunks, agent.ChatChunk{
				StopReason: translateStopReason(c.FinishReason, hasFunctionCalls || toolCallIndex > 0),
				Usage:      &usage,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("google: read stream: %w", err)
	}
	return chunks, nil
}
