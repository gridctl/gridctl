package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient implements LLMClient using the Anthropic Messages API.
type AnthropicClient struct {
	client anthropic.Client
	model  string
}

// NewAnthropicClient creates a new Anthropic API client for the given model.
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

// Stream runs the full agentic loop against the Anthropic Messages API.
// It streams tokens as they arrive, executes tool calls via caller, and loops
// until the model returns stop_reason "end_turn" (or another terminal reason).
func (c *AnthropicClient) Stream(
	ctx context.Context,
	systemPrompt string,
	history []Message,
	tools []Tool,
	caller ToolCaller,
	events chan<- LLMEvent,
) (string, error) {
	anthropicTools := convertToolsForAnthropic(tools)
	messages := historyToAnthropicMessages(history)

	var totalInputTokens, totalOutputTokens int64
	var finalResponse strings.Builder

	for {
		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: 8192,
			Messages:  messages,
		}
		if systemPrompt != "" {
			params.System = []anthropic.TextBlockParam{{Text: systemPrompt}}
		}
		if len(anthropicTools) > 0 {
			params.Tools = anthropicTools
		}

		stream := c.client.Messages.NewStreaming(ctx, params)
		var acc anthropic.Message

		for stream.Next() {
			event := stream.Current()
			if err := acc.Accumulate(event); err != nil {
				continue // non-fatal accumulation errors
			}
			switch e := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				switch d := e.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					if d.Text != "" {
						finalResponse.WriteString(d.Text)
						select {
						case events <- LLMEvent{Type: EventTypeToken, Data: TokenData{Text: d.Text}}:
						case <-ctx.Done():
							return finalResponse.String(), ctx.Err()
						}
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			if ctx.Err() != nil {
				return finalResponse.String(), nil // context cancelled — clean exit
			}
			select {
			case events <- LLMEvent{Type: EventTypeError, Data: ErrorData{Message: err.Error()}}:
			default:
			}
			return finalResponse.String(), fmt.Errorf("anthropic stream: %w", err)
		}

		totalInputTokens += int64(acc.Usage.InputTokens)
		totalOutputTokens += int64(acc.Usage.OutputTokens)

		if acc.StopReason != anthropic.StopReasonToolUse {
			// Finished — no more tool calls
			break
		}

		// Collect tool use blocks and execute them
		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range acc.Content {
			toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}

			// Parse arguments from the accumulated input
			var args map[string]any
			if len(toolUse.Input) > 0 {
				_ = json.Unmarshal(toolUse.Input, &args)
			}

			serverName := serverNameForTool(toolUse.Name, tools)
			select {
			case events <- LLMEvent{Type: EventTypeToolCallStart, Data: ToolCallStartData{
				ToolName:   toolUse.Name,
				ServerName: serverName,
				Input:      args,
			}}:
			case <-ctx.Done():
				return finalResponse.String(), ctx.Err()
			}

			start := time.Now()
			result, callErr := caller.CallTool(ctx, toolUse.Name, args)
			durationMs := time.Since(start).Milliseconds()

			output := result.Content
			isError := callErr != nil || result.IsError
			if callErr != nil {
				output = callErr.Error()
			}

			select {
			case events <- LLMEvent{Type: EventTypeToolCallEnd, Data: ToolCallEndData{
				ToolName:   toolUse.Name,
				Output:     output,
				DurationMs: durationMs,
			}}:
			case <-ctx.Done():
				return finalResponse.String(), ctx.Err()
			}

			toolResults = append(toolResults, anthropic.NewToolResultBlock(toolUse.ID, output, isError))
		}

		// Add assistant message + tool results and continue the loop
		messages = append(messages, acc.ToParam())
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

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

	return finalResponse.String(), nil
}

// Close releases any held resources (the HTTP client is managed externally).
func (c *AnthropicClient) Close() error { return nil }

// convertToolsForAnthropic converts agent.Tool slice to Anthropic ToolUnionParam slice.
func convertToolsForAnthropic(tools []Tool) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		var schema map[string]any
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			schema = map[string]any{"type": "object"}
		}

		props := schema["properties"]
		var required []string
		if reqRaw, ok := schema["required"].([]any); ok {
			for _, r := range reqRaw {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}

		toolParam := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: props,
				Required:   required,
			},
		}
		result = append(result, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return result
}

// historyToAnthropicMessages converts conversation history to Anthropic MessageParam slice.
func historyToAnthropicMessages(history []Message) []anthropic.MessageParam {
	msgs := make([]anthropic.MessageParam, 0, len(history))
	for _, m := range history {
		switch m.Role {
		case "user":
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	return msgs
}

// serverNameForTool finds the ServerName for a tool by its prefixed name.
func serverNameForTool(toolName string, tools []Tool) string {
	for _, t := range tools {
		if t.Name == toolName {
			return t.ServerName
		}
	}
	return ""
}
