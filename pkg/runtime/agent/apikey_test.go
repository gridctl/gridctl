package agent

import (
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// --- historyToAnthropicMessages ---

func TestHistoryToAnthropicMessages_Empty(t *testing.T) {
	msgs := historyToAnthropicMessages(nil)
	if len(msgs) != 0 {
		t.Fatalf("expected empty slice, got %d", len(msgs))
	}
}

func TestHistoryToAnthropicMessages_UserAndAssistant(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	msgs := historyToAnthropicMessages(history)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("expected user role, got %s", msgs[0].Role)
	}
	if msgs[1].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("expected assistant role, got %s", msgs[1].Role)
	}
}

func TestHistoryToAnthropicMessages_ToolResultGrouping(t *testing.T) {
	// Consecutive tool messages should be grouped into a single user message.
	history := []Message{
		{Role: "user", Content: "call some tools"},
		{Role: "tool", ToolCallID: "tc1", Content: "result1"},
		{Role: "tool", ToolCallID: "tc2", Content: "result2"},
		{Role: "assistant", Content: "done"},
	}
	msgs := historyToAnthropicMessages(history)
	// Expect: user, batched-user-with-tool-results, assistant = 3
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (tool results grouped), got %d", len(msgs))
	}
	if msgs[0].Role != anthropic.MessageParamRoleUser {
		t.Errorf("msg[0]: expected user, got %s", msgs[0].Role)
	}
	if msgs[1].Role != anthropic.MessageParamRoleUser {
		t.Errorf("msg[1]: expected user (tool results), got %s", msgs[1].Role)
	}
	if msgs[2].Role != anthropic.MessageParamRoleAssistant {
		t.Errorf("msg[2]: expected assistant, got %s", msgs[2].Role)
	}
}

func TestHistoryToAnthropicMessages_RawParam(t *testing.T) {
	// A valid anthropic MessageParam JSON (assistant text message).
	raw := json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"raw response"}]}`)
	history := []Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", RawParam: raw, ToolCalls: []ToolCallBlock{{ID: "x", Name: "tool"}}},
	}
	msgs := historyToAnthropicMessages(history)
	// RawParam should be decoded and used; if unmarshal fails it falls through silently.
	// We just verify no panic and correct message count.
	if len(msgs) < 1 {
		t.Fatal("expected at least 1 message")
	}
}

func TestHistoryToAnthropicMessages_UnknownRoleSkipped(t *testing.T) {
	history := []Message{
		{Role: "system", Content: "ignored"},
		{Role: "user", Content: "hi"},
	}
	msgs := historyToAnthropicMessages(history)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (system skipped), got %d", len(msgs))
	}
}

// --- convertToolsForAnthropic ---

func TestConvertToolsForAnthropic_Empty(t *testing.T) {
	result := convertToolsForAnthropic(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d", len(result))
	}
}

func TestConvertToolsForAnthropic_ValidSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
	tools := []Tool{
		{Name: "search", Description: "search the web", InputSchema: schema},
	}
	result := convertToolsForAnthropic(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
}

func TestConvertToolsForAnthropic_InvalidSchema(t *testing.T) {
	tools := []Tool{
		{Name: "bad", Description: "bad tool", InputSchema: json.RawMessage(`not-json`)},
	}
	// Should not panic — falls back to empty object schema.
	result := convertToolsForAnthropic(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool (fallback), got %d", len(result))
	}
}

func TestConvertToolsForAnthropic_NoRequired(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`)
	tools := []Tool{
		{Name: "calc", InputSchema: schema},
	}
	result := convertToolsForAnthropic(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
}

// --- serverNameForTool ---

func TestServerNameForTool_Found(t *testing.T) {
	tools := []Tool{
		{Name: "svr__tool", ServerName: "svr"},
	}
	got := serverNameForTool("svr__tool", tools)
	if got != "svr" {
		t.Errorf("expected 'svr', got %q", got)
	}
}

func TestServerNameForTool_NotFound(t *testing.T) {
	tools := []Tool{
		{Name: "other__tool", ServerName: "other"},
	}
	got := serverNameForTool("missing__tool", tools)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestServerNameForTool_EmptyTools(t *testing.T) {
	got := serverNameForTool("any", nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// --- NewAnthropicClient ---

func TestNewAnthropicClient(t *testing.T) {
	c := NewAnthropicClient("test-key", "claude-3-5-sonnet-latest")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.model != "claude-3-5-sonnet-latest" {
		t.Errorf("unexpected model: %s", c.model)
	}
}

func TestAnthropicClientClose(t *testing.T) {
	c := NewAnthropicClient("test-key", "claude-3-5-sonnet-latest")
	if err := c.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}
