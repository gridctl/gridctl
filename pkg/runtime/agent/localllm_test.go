package agent

import (
	"encoding/json"
	"testing"
)

// --- historyToOpenAIMessages ---

func TestHistoryToOpenAIMessages_Empty(t *testing.T) {
	msgs := historyToOpenAIMessages("", nil)
	if len(msgs) != 0 {
		t.Fatalf("expected empty slice, got %d", len(msgs))
	}
}

func TestHistoryToOpenAIMessages_WithSystemPrompt(t *testing.T) {
	msgs := historyToOpenAIMessages("you are helpful", nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (system), got %d", len(msgs))
	}
}

func TestHistoryToOpenAIMessages_UserAndAssistant(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	msgs := historyToOpenAIMessages("", history)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestHistoryToOpenAIMessages_AssistantWithToolCalls(t *testing.T) {
	history := []Message{
		{
			Role:    "assistant",
			Content: "let me check",
			ToolCalls: []ToolCallBlock{
				{ID: "tc1", Name: "search", Arguments: `{"q":"test"}`},
			},
		},
	}
	msgs := historyToOpenAIMessages("", history)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// The message should be an assistant message with tool calls.
	if msgs[0].OfAssistant == nil {
		t.Fatal("expected assistant message with tool calls")
	}
	if len(msgs[0].OfAssistant.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(msgs[0].OfAssistant.ToolCalls))
	}
	if msgs[0].OfAssistant.ToolCalls[0].ID != "tc1" {
		t.Errorf("unexpected tool call ID: %s", msgs[0].OfAssistant.ToolCalls[0].ID)
	}
}

func TestHistoryToOpenAIMessages_ToolResult(t *testing.T) {
	history := []Message{
		{Role: "tool", ToolCallID: "tc1", Content: "tool output"},
	}
	msgs := historyToOpenAIMessages("", history)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].OfTool == nil {
		t.Fatal("expected tool message")
	}
	if msgs[0].OfTool.ToolCallID != "tc1" {
		t.Errorf("unexpected tool call ID: %s", msgs[0].OfTool.ToolCallID)
	}
}

func TestHistoryToOpenAIMessages_AssistantNoContent(t *testing.T) {
	// Assistant with tool calls but no text content.
	history := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCallBlock{
				{ID: "tc2", Name: "calc", Arguments: "{}"},
			},
		},
	}
	msgs := historyToOpenAIMessages("", history)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].OfAssistant == nil {
		t.Fatal("expected assistant message")
	}
}

func TestHistoryToOpenAIMessages_SystemPlusHistory(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
	}
	msgs := historyToOpenAIMessages("sys", history)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (system + 2), got %d", len(msgs))
	}
}

// --- convertToolsForOpenAI ---

func TestConvertToolsForOpenAI_Empty(t *testing.T) {
	result := convertToolsForOpenAI(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestConvertToolsForOpenAI_ValidSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)
	tools := []Tool{
		{Name: "my_tool", Description: "does stuff", InputSchema: schema},
	}
	result := convertToolsForOpenAI(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	if result[0].Function.Name != "my_tool" {
		t.Errorf("unexpected tool name: %s", result[0].Function.Name)
	}
}

func TestConvertToolsForOpenAI_InvalidSchema(t *testing.T) {
	tools := []Tool{
		{Name: "broken", InputSchema: json.RawMessage(`!!!`)},
	}
	// Should fall back to {"type":"object"} without panicking.
	result := convertToolsForOpenAI(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool (fallback), got %d", len(result))
	}
}

// --- buildAssistantMessageWithToolCalls ---

func TestBuildAssistantMessageWithToolCalls_NoContent(t *testing.T) {
	pending := map[int]*localToolCall{
		0: {id: "id1", name: "tool1"},
	}
	pending[0].args.WriteString(`{"key":"val"}`)
	msg := buildAssistantMessageWithToolCalls("", pending)
	if msg.OfAssistant == nil {
		t.Fatal("expected assistant message")
	}
	if len(msg.OfAssistant.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(msg.OfAssistant.ToolCalls))
	}
}

func TestBuildAssistantMessageWithToolCalls_WithContent(t *testing.T) {
	pending := map[int]*localToolCall{
		0: {id: "id2", name: "search"},
	}
	pending[0].args.WriteString(`{}`)
	msg := buildAssistantMessageWithToolCalls("thinking...", pending)
	if msg.OfAssistant == nil {
		t.Fatal("expected assistant message")
	}
	if msg.OfAssistant.ToolCalls[0].ID != "id2" {
		t.Errorf("unexpected ID: %s", msg.OfAssistant.ToolCalls[0].ID)
	}
}

func TestBuildAssistantMessageWithToolCalls_MultipleTools(t *testing.T) {
	pending := map[int]*localToolCall{
		0: {id: "a", name: "tool_a"},
		1: {id: "b", name: "tool_b"},
	}
	msg := buildAssistantMessageWithToolCalls("", pending)
	if msg.OfAssistant == nil {
		t.Fatal("expected assistant message")
	}
	if len(msg.OfAssistant.ToolCalls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(msg.OfAssistant.ToolCalls))
	}
}

func TestBuildAssistantMessageWithToolCalls_SparseMap(t *testing.T) {
	// Map with index 0 missing: len=2, loop checks i=0 (not found → continue) and i=1 (found).
	// This exercises the !ok continue branch.
	pending := map[int]*localToolCall{
		1: {id: "b", name: "tool_b"},
		2: {id: "c", name: "tool_c"},
	}
	msg := buildAssistantMessageWithToolCalls("", pending)
	if msg.OfAssistant == nil {
		t.Fatal("expected assistant message")
	}
	// i=0 skipped (not in map), i=1 found → 1 tool call.
	if len(msg.OfAssistant.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call (index 0 skipped), got %d", len(msg.OfAssistant.ToolCalls))
	}
}

// --- NewLocalLLMClient / NewLocalLLMClientWithKey ---

func TestNewLocalLLMClient_DefaultBaseURL(t *testing.T) {
	c := NewLocalLLMClient("", "llama3")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "http://localhost:11434/v1" {
		t.Errorf("expected default baseURL, got %q", c.baseURL)
	}
	if c.model != "llama3" {
		t.Errorf("unexpected model: %s", c.model)
	}
}

func TestNewLocalLLMClient_CustomBaseURL(t *testing.T) {
	c := NewLocalLLMClient("http://example.com/v1", "phi3")
	if c.baseURL != "http://example.com/v1" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewLocalLLMClientWithKey(t *testing.T) {
	c := NewLocalLLMClientWithKey("https://api.openai.com/v1", "gpt-4o", "sk-test")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "https://api.openai.com/v1" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
	if c.model != "gpt-4o" {
		t.Errorf("unexpected model: %s", c.model)
	}
}

func TestLocalLLMClientClose(t *testing.T) {
	c := NewLocalLLMClient("", "llama3")
	if err := c.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}
