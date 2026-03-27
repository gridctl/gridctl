package agent

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/genai"
)

// --- NewGeminiClient ---

func TestNewGeminiClient_WithFakeKey(t *testing.T) {
	// Client construction should succeed without network access.
	c, err := NewGeminiClient(context.Background(), "fake-api-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.model != "gemini-2.0-flash" {
		t.Errorf("unexpected model: %s", c.model)
	}
}

func TestNewGeminiClient_EmptyModel(t *testing.T) {
	c, err := NewGeminiClient(context.Background(), "fake-key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.model != "" {
		t.Errorf("expected empty model, got %q", c.model)
	}
}

func TestGeminiClientClose(t *testing.T) {
	c, err := NewGeminiClient(context.Background(), "fake-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("setup error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

// --- convertToolsForGemini ---

func TestConvertToolsForGemini_Empty(t *testing.T) {
	result := convertToolsForGemini(nil)
	if result != nil {
		t.Fatalf("expected nil for empty tools, got %v", result)
	}
}

func TestConvertToolsForGemini_EmptySlice(t *testing.T) {
	result := convertToolsForGemini([]Tool{})
	if result != nil {
		t.Fatalf("expected nil for empty slice, got %v", result)
	}
}

func TestConvertToolsForGemini_SingleTool(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
	tools := []Tool{
		{Name: "search", Description: "search the web", InputSchema: schema},
	}
	result := convertToolsForGemini(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 Tool wrapper, got %d", len(result))
	}
	if len(result[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(result[0].FunctionDeclarations))
	}
	decl := result[0].FunctionDeclarations[0]
	if decl.Name != "search" {
		t.Errorf("unexpected name: %s", decl.Name)
	}
	if decl.Description != "search the web" {
		t.Errorf("unexpected description: %s", decl.Description)
	}
	if decl.ParametersJsonSchema == nil {
		t.Error("expected non-nil ParametersJsonSchema")
	}
}

func TestConvertToolsForGemini_MultipleTools(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	tools := []Tool{
		{Name: "tool_a", InputSchema: schema},
		{Name: "tool_b", InputSchema: schema},
		{Name: "tool_c", InputSchema: schema},
	}
	result := convertToolsForGemini(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 Tool wrapper, got %d", len(result))
	}
	if len(result[0].FunctionDeclarations) != 3 {
		t.Errorf("expected 3 declarations, got %d", len(result[0].FunctionDeclarations))
	}
}

func TestConvertToolsForGemini_InvalidSchema(t *testing.T) {
	tools := []Tool{
		{Name: "broken", InputSchema: json.RawMessage(`not-json!!!`)},
	}
	// Should not panic — falls back to {"type":"object"}.
	result := convertToolsForGemini(tools)
	if len(result) != 1 {
		t.Fatalf("expected 1 Tool wrapper (fallback), got %d", len(result))
	}
	if len(result[0].FunctionDeclarations) != 1 {
		t.Errorf("expected 1 declaration (fallback), got %d", len(result[0].FunctionDeclarations))
	}
}

func TestConvertToolsForGemini_NoDescription(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	tools := []Tool{
		{Name: "nodesc", InputSchema: schema},
	}
	result := convertToolsForGemini(tools)
	if result[0].FunctionDeclarations[0].Description != "" {
		t.Errorf("expected empty description, got %q", result[0].FunctionDeclarations[0].Description)
	}
}

// --- historyToGeminiContents ---

func TestHistoryToGeminiContents_Empty(t *testing.T) {
	result := historyToGeminiContents(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestHistoryToGeminiContents_UserAndAssistant(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	result := historyToGeminiContents(history)
	if len(result) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(result))
	}
	if result[0].Role != genai.RoleUser {
		t.Errorf("expected user role, got %q", result[0].Role)
	}
	if result[0].Parts[0].Text != "hello" {
		t.Errorf("unexpected text: %s", result[0].Parts[0].Text)
	}
	if result[1].Role != genai.RoleModel {
		t.Errorf("expected model role, got %q", result[1].Role)
	}
	if result[1].Parts[0].Text != "hi there" {
		t.Errorf("unexpected text: %s", result[1].Parts[0].Text)
	}
}

func TestHistoryToGeminiContents_ToolCallsGrouped(t *testing.T) {
	// Consecutive tool messages should be grouped into a single user content.
	history := []Message{
		{Role: "user", Content: "call two tools"},
		{Role: "tool", ToolCallID: "tc1", Content: "result1"},
		{Role: "tool", ToolCallID: "tc2", Content: "result2"},
		{Role: "assistant", Content: "done"},
	}
	result := historyToGeminiContents(history)
	// user + grouped-tool-user + assistant = 3
	if len(result) != 3 {
		t.Fatalf("expected 3 contents (tools grouped), got %d", len(result))
	}
	if result[0].Role != genai.RoleUser {
		t.Errorf("expected user role at [0], got %q", result[0].Role)
	}
	// Grouped tool results: role=user, 2 FunctionResponse parts
	if result[1].Role != genai.RoleUser {
		t.Errorf("expected user role for tool results at [1], got %q", result[1].Role)
	}
	if len(result[1].Parts) != 2 {
		t.Errorf("expected 2 FunctionResponse parts, got %d", len(result[1].Parts))
	}
	if result[1].Parts[0].FunctionResponse == nil {
		t.Error("expected FunctionResponse in first part")
	}
	if result[2].Role != genai.RoleModel {
		t.Errorf("expected model role at [2], got %q", result[2].Role)
	}
}

func TestHistoryToGeminiContents_AssistantWithToolCalls(t *testing.T) {
	history := []Message{
		{
			Role:    "assistant",
			Content: "let me search",
			ToolCalls: []ToolCallBlock{
				{ID: "tc1", Name: "search", Arguments: `{"q":"test"}`},
			},
		},
	}
	result := historyToGeminiContents(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result))
	}
	if result[0].Role != genai.RoleModel {
		t.Errorf("expected model role, got %q", result[0].Role)
	}
	// Should have text part + FunctionCall part
	if len(result[0].Parts) != 2 {
		t.Errorf("expected 2 parts (text + function call), got %d", len(result[0].Parts))
	}
	if result[0].Parts[0].Text != "let me search" {
		t.Errorf("unexpected text: %s", result[0].Parts[0].Text)
	}
	if result[0].Parts[1].FunctionCall == nil {
		t.Error("expected FunctionCall in second part")
	}
	if result[0].Parts[1].FunctionCall.Name != "search" {
		t.Errorf("unexpected function name: %s", result[0].Parts[1].FunctionCall.Name)
	}
}

func TestHistoryToGeminiContents_AssistantWithToolCallsNoText(t *testing.T) {
	// Tool call without text content — parts should contain only FunctionCall.
	history := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCallBlock{
				{ID: "tc1", Name: "calc", Arguments: `{"x":1}`},
			},
		},
	}
	result := historyToGeminiContents(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result))
	}
	// No text — only function call part
	if len(result[0].Parts) != 1 {
		t.Errorf("expected 1 part (function call only), got %d", len(result[0].Parts))
	}
	if result[0].Parts[0].FunctionCall == nil {
		t.Error("expected FunctionCall part")
	}
}

func TestHistoryToGeminiContents_AssistantWithInvalidToolArgs(t *testing.T) {
	// Invalid JSON in tool arguments — should not panic, falls back to nil args.
	history := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCallBlock{
				{ID: "tc1", Name: "tool", Arguments: `not-json`},
			},
		},
	}
	result := historyToGeminiContents(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result))
	}
	// Should still produce a FunctionCall part, just with nil args.
	if result[0].Parts[0].FunctionCall == nil {
		t.Error("expected FunctionCall part even with invalid args")
	}
}

func TestHistoryToGeminiContents_SingleToolResult(t *testing.T) {
	history := []Message{
		{Role: "tool", ToolCallID: "t1", Content: "result"},
	}
	result := historyToGeminiContents(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result))
	}
	if result[0].Role != genai.RoleUser {
		t.Errorf("expected user role for tool result, got %q", result[0].Role)
	}
	if len(result[0].Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(result[0].Parts))
	}
	if result[0].Parts[0].FunctionResponse == nil {
		t.Error("expected FunctionResponse part")
	}
}

func TestHistoryToGeminiContents_UnknownRoleSkipped(t *testing.T) {
	history := []Message{
		{Role: "system", Content: "ignored"},
		{Role: "user", Content: "real"},
	}
	result := historyToGeminiContents(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 content (system skipped), got %d", len(result))
	}
	if result[0].Parts[0].Text != "real" {
		t.Errorf("unexpected text: %s", result[0].Parts[0].Text)
	}
}

func TestHistoryToGeminiContents_FullConversation(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "what is 2+2?"},
		{
			Role: "assistant",
			ToolCalls: []ToolCallBlock{
				{ID: "c1", Name: "calc", Arguments: `{"a":2,"b":2}`},
			},
		},
		{Role: "tool", ToolCallID: "c1", Content: "4"},
		{Role: "assistant", Content: "The answer is 4."},
	}
	result := historyToGeminiContents(history)
	// user + assistant(function call) + user(function response) + assistant(text) = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 contents, got %d", len(result))
	}
	if result[0].Role != genai.RoleUser {
		t.Errorf("[0] expected user, got %q", result[0].Role)
	}
	if result[1].Role != genai.RoleModel {
		t.Errorf("[1] expected model, got %q", result[1].Role)
	}
	if result[2].Role != genai.RoleUser {
		t.Errorf("[2] expected user (tool result), got %q", result[2].Role)
	}
	if result[3].Role != genai.RoleModel {
		t.Errorf("[3] expected model, got %q", result[3].Role)
	}
}
