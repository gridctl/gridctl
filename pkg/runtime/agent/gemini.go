package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

// GeminiClient implements LLMClient using the Google Gemini API.
type GeminiClient struct {
	client *genai.Client
	model  string
}

// NewGeminiClient creates a new Gemini API client for the given model.
func NewGeminiClient(ctx context.Context, apiKey, model string) (*GeminiClient, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("creating Gemini client: %w", err)
	}
	return &GeminiClient{client: client, model: model}, nil
}

// Stream runs the full agentic loop against the Gemini API.
// It streams tokens as they arrive, executes tool calls via caller, and loops
// until the model returns a non-function-call response.
func (c *GeminiClient) Stream(
	ctx context.Context,
	systemPrompt string,
	history []Message,
	tools []Tool,
	caller ToolCaller,
	events chan<- LLMEvent,
) (string, []Message, error) {
	contents := historyToGeminiContents(history)
	geminiTools := convertToolsForGemini(tools)

	cfg := &genai.GenerateContentConfig{}
	if systemPrompt != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: systemPrompt}},
		}
	}
	if len(geminiTools) > 0 {
		cfg.Tools = geminiTools
	}

	var totalInputTokens, totalOutputTokens int32
	var toolTurns []Message

	for {
		var roundText strings.Builder
		var accumulatedParts []*genai.Part
		var lastUsage *genai.GenerateContentResponseUsageMetadata

		for resp, err := range c.client.Models.GenerateContentStream(ctx, c.model, contents, cfg) {
			if err != nil {
				if ctx.Err() != nil {
					return roundText.String(), toolTurns, nil
				}
				select {
				case events <- LLMEvent{Type: EventTypeError, Data: ErrorData{Message: err.Error()}}:
				default:
				}
				return roundText.String(), toolTurns, fmt.Errorf("gemini stream: %w", err)
			}

			if resp.UsageMetadata != nil {
				lastUsage = resp.UsageMetadata
			}

			if len(resp.Candidates) == 0 {
				continue
			}
			cand := resp.Candidates[0]
			if cand.Content == nil {
				continue
			}

			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					roundText.WriteString(part.Text)
					select {
					case events <- LLMEvent{Type: EventTypeToken, Data: TokenData{Text: part.Text}}:
					case <-ctx.Done():
						return roundText.String(), toolTurns, ctx.Err()
					}
				}
				if part.FunctionCall != nil {
					accumulatedParts = append(accumulatedParts, part)
				}
			}
		}

		// Collect usage from the final streaming chunk
		if lastUsage != nil {
			totalInputTokens += lastUsage.PromptTokenCount
			totalOutputTokens += lastUsage.CandidatesTokenCount
		}

		if len(accumulatedParts) == 0 {
			// No function calls — final round
			select {
			case events <- LLMEvent{Type: EventTypeMetrics, Data: MetricsData{
				TokensIn:  int(totalInputTokens),
				TokensOut: int(totalOutputTokens),
			}}:
			default:
			}
			select {
			case events <- LLMEvent{Type: EventTypeDone}:
			default:
			}
			return roundText.String(), toolTurns, nil
		}

		// Persist the assistant tool-use turn
		toolCallBlocks := make([]ToolCallBlock, 0, len(accumulatedParts))
		for _, part := range accumulatedParts {
			fc := part.FunctionCall
			argsJSON, _ := json.Marshal(fc.Args)
			toolCallBlocks = append(toolCallBlocks, ToolCallBlock{
				ID:        fc.ID,
				Name:      fc.Name,
				Arguments: string(argsJSON),
			})
		}
		toolTurns = append(toolTurns, Message{
			Role:      "assistant",
			Content:   roundText.String(),
			ToolCalls: toolCallBlocks,
		})

		// Append model content with function call parts to the in-flight contents
		modelContent := &genai.Content{
			Role:  genai.RoleModel,
			Parts: append([]*genai.Part{{Text: roundText.String()}}, accumulatedParts...),
		}
		if roundText.Len() == 0 {
			modelContent.Parts = accumulatedParts
		}
		contents = append(contents, modelContent)

		// Execute all tool calls and collect results
		responseParts := make([]*genai.Part, 0, len(accumulatedParts))
		for _, part := range accumulatedParts {
			fc := part.FunctionCall

			serverName := serverNameForTool(fc.Name, tools)
			select {
			case events <- LLMEvent{Type: EventTypeToolCallStart, Data: ToolCallStartData{
				ToolName:   fc.Name,
				ServerName: serverName,
				Input:      fc.Args,
			}}:
			case <-ctx.Done():
				return roundText.String(), toolTurns, ctx.Err()
			}

			start := time.Now()
			result, callErr := caller.CallTool(ctx, fc.Name, fc.Args)
			durationMs := time.Since(start).Milliseconds()

			output := result.Content
			if callErr != nil {
				output = callErr.Error()
			}

			select {
			case events <- LLMEvent{Type: EventTypeToolCallEnd, Data: ToolCallEndData{
				ToolName:   fc.Name,
				Output:     output,
				DurationMs: durationMs,
			}}:
			case <-ctx.Done():
				return roundText.String(), toolTurns, ctx.Err()
			}

			responseParts = append(responseParts, genai.NewPartFromFunctionResponse(fc.Name, map[string]any{"output": output}))
			toolTurns = append(toolTurns, Message{
				Role:       "tool",
				ToolCallID: fc.ID,
				Content:    output,
			})
		}

		// Add all tool results as a single user content and loop
		contents = append(contents, &genai.Content{
			Role:  genai.RoleUser,
			Parts: responseParts,
		})
	}
}

// Close releases the Gemini client's HTTP resources.
func (c *GeminiClient) Close() error { return nil }

// convertToolsForGemini converts agent.Tool slice to Gemini Tool slice.
// ParametersJsonSchema accepts any JSON-decoded value, so we pass the
// unmarshalled schema map directly to avoid re-encoding overhead.
func convertToolsForGemini(tools []Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		var schemaMap any
		if err := json.Unmarshal(t.InputSchema, &schemaMap); err != nil {
			schemaMap = map[string]any{"type": "object"}
		}
		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 t.Name,
			Description:          t.Description,
			ParametersJsonSchema: schemaMap,
		})
	}
	return []*genai.Tool{{FunctionDeclarations: decls}}
}

// historyToGeminiContents converts conversation history to Gemini Content slice.
// Consecutive "tool" role messages are grouped into a single user content with
// multiple FunctionResponse parts, matching Gemini's expected conversation format.
func historyToGeminiContents(history []Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(history))
	i := 0
	for i < len(history) {
		m := history[i]

		switch {
		case m.Role == "tool":
			// Group consecutive tool-result messages into one user content
			var parts []*genai.Part
			for i < len(history) && history[i].Role == "tool" {
				tr := history[i]
				parts = append(parts, genai.NewPartFromFunctionResponse(
					tr.ToolCallID, // use ID as name fallback if Name is empty
					map[string]any{"output": tr.Content},
				))
				i++
			}
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: parts})
			}

		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			// Reconstruct model content with FunctionCall parts
			parts := make([]*genai.Part, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				parts = append(parts, &genai.Part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				if tc.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Arguments), &args)
				}
				parts = append(parts, genai.NewPartFromFunctionCall(tc.Name, args))
			}
			contents = append(contents, &genai.Content{Role: genai.RoleModel, Parts: parts})
			i++

		case m.Role == "user":
			contents = append(contents, &genai.Content{
				Role:  genai.RoleUser,
				Parts: []*genai.Part{{Text: m.Content}},
			})
			i++

		case m.Role == "assistant":
			contents = append(contents, &genai.Content{
				Role:  genai.RoleModel,
				Parts: []*genai.Part{{Text: m.Content}},
			})
			i++

		default:
			i++
		}
	}
	return contents
}
