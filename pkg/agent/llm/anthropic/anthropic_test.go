package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent"
)

func TestNew_RequiresAPIKey(t *testing.T) {
	t.Parallel()

	if _, err := New(""); err == nil {
		t.Errorf("New(\"\") = nil error, want error")
	}
	if _, err := New("sk-test"); err != nil {
		t.Errorf("New(non-empty) returned error: %v", err)
	}
}

func TestProvider_Generate_ParsesTextAndToolUse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("x-api-key header = %q, want sk-test", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != DefaultAPIVersion {
			t.Errorf("anthropic-version header = %q, want %s", r.Header.Get("anthropic-version"), DefaultAPIVersion)
		}

		body, _ := io.ReadAll(r.Body)
		var raw messageRequest
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if raw.Model != "claude-test" {
			t.Errorf("Model = %q, want claude-test", raw.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
  "id": "msg_1",
  "type": "message",
  "role": "assistant",
  "model": "claude-test",
  "stop_reason": "tool_use",
  "content": [
    {"type": "text", "text": "Let me search."},
    {"type": "tool_use", "id": "toolu_1", "name": "search", "input": {"q": "x"}}
  ],
  "usage": {"input_tokens": 12, "output_tokens": 8, "cache_read_input_tokens": 3}
}`)
	}))
	defer server.Close()

	p, err := New("sk-test", WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.Generate(context.Background(), agent.ChatRequest{
		Model: "claude-test",
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "find x"},
		},
		Tools: []agent.ToolInfo{
			{Name: "search", Description: "Search", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content != "Let me search." {
		t.Errorf("Content = %q, want %q", resp.Content, "Let me search.")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "toolu_1" || resp.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCall[0] = %+v, want id=toolu_1 name=search", resp.ToolCalls[0])
	}
	if string(resp.ToolCalls[0].Arguments) != `{"q": "x"}` && string(resp.ToolCalls[0].Arguments) != `{"q":"x"}` {
		t.Errorf("ToolCall[0].Arguments = %q", string(resp.ToolCalls[0].Arguments))
	}
	if resp.StopReason != agent.StopReasonToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, agent.StopReasonToolUse)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 8 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if resp.Usage.CacheReadTokens != 3 {
		t.Errorf("CacheReadTokens = %d, want 3", resp.Usage.CacheReadTokens)
	}
}

func TestProvider_Generate_RejectsEmptyMessages(t *testing.T) {
	t.Parallel()

	p, err := New("sk-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Generate(context.Background(), agent.ChatRequest{Model: "x"}); err == nil {
		t.Errorf("Generate with empty messages returned nil error")
	}
	if _, err := p.Generate(context.Background(), agent.ChatRequest{
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	}); err == nil {
		t.Errorf("Generate with empty model returned nil error")
	}
}

func TestProvider_Generate_HTTPErrorIsParsed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`)
	}))
	defer server.Close()

	p, err := New("sk-test", WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Generate(context.Background(), agent.ChatRequest{
		Model:    "bad",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Errorf("error did not surface provider message: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid_request_error") {
		t.Errorf("error did not surface provider type: %v", err)
	}
}

func TestProvider_Stream_ParsesSSE(t *testing.T) {
	t.Parallel()

	const body = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"search","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}

event: message_stop
data: {"type":"message_stop"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	p, err := New("sk-test", WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stream, err := p.Stream(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	var (
		text       string
		argsByIdx  = map[int]string{}
		nameByIdx  = map[int]string{}
		stopReason agent.StopReason
		usage      agent.Usage
	)
	for {
		ch, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		text += ch.Delta
		if ch.ToolCallDelta != nil {
			if ch.ToolCallDelta.Name != "" {
				nameByIdx[ch.ToolCallDelta.Index] = ch.ToolCallDelta.Name
			}
			argsByIdx[ch.ToolCallDelta.Index] += ch.ToolCallDelta.ArgsDelta
		}
		if ch.StopReason != "" {
			stopReason = ch.StopReason
		}
		if ch.Usage != nil {
			usage = *ch.Usage
		}
	}

	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
	if nameByIdx[1] != "search" {
		t.Errorf("tool name at idx 1 = %q, want search", nameByIdx[1])
	}
	if argsByIdx[1] != `{"q":"x"}` {
		t.Errorf("tool args at idx 1 = %q, want {\"q\":\"x\"}", argsByIdx[1])
	}
	if stopReason != agent.StopReasonToolUse {
		t.Errorf("stopReason = %q, want %q", stopReason, agent.StopReasonToolUse)
	}
	if usage.OutputTokens != 4 {
		t.Errorf("usage.OutputTokens = %d, want 4", usage.OutputTokens)
	}
}

func TestTranslateMessages_RejectsSystemRole(t *testing.T) {
	t.Parallel()
	_, err := translateMessages([]agent.Message{
		{Role: agent.RoleSystem, Content: "hi"},
	})
	if err == nil {
		t.Errorf("translateMessages should reject system role")
	}
}

func TestTranslateMessages_PacksAdjacentToolResults(t *testing.T) {
	t.Parallel()
	out, err := translateMessages([]agent.Message{
		{Role: agent.RoleUser, Content: "go"},
		{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{
			{ID: "a", Name: "x", Arguments: json.RawMessage(`{}`)},
			{ID: "b", Name: "y", Arguments: json.RawMessage(`{}`)},
		}},
		{Role: agent.RoleTool, ToolCallID: "a", Content: "ra"},
		{Role: agent.RoleTool, ToolCallID: "b", Content: "rb"},
		{Role: agent.RoleUser, Content: "next"},
	})
	if err != nil {
		t.Fatalf("translateMessages: %v", err)
	}
	if len(out) != 4 {
		t.Fatalf("len(out) = %d, want 4", len(out))
	}
	if out[2].Role != "user" {
		t.Errorf("out[2].Role = %q, want user", out[2].Role)
	}
	if len(out[2].Content) != 2 {
		t.Errorf("merged tool_result blocks = %d, want 2", len(out[2].Content))
	}
}

func TestTranslateTools_FillsMissingSchema(t *testing.T) {
	t.Parallel()
	out := translateTools([]agent.ToolInfo{
		{Name: "noop"},
	})
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if string(out[0].InputSchema) == "" {
		t.Errorf("InputSchema not filled in for empty schema")
	}
}

func TestTranslateStopReason(t *testing.T) {
	t.Parallel()
	cases := map[string]agent.StopReason{
		"end_turn":      agent.StopReasonEnd,
		"max_tokens":    agent.StopReasonMaxTokens,
		"tool_use":      agent.StopReasonToolUse,
		"stop_sequence": agent.StopReasonStopSequence,
		"":              agent.StopReasonEnd,
		"weird":         agent.StopReasonEnd,
	}
	for in, want := range cases {
		if got := translateStopReason(in); got != want {
			t.Errorf("translateStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestProvider_Generate_RealAnthropic is the end-to-end smoke test.
// Skipped by default; runs when ANTHROPIC_API_KEY is set in the
// environment. The body is intentionally tiny — a single hello prompt
// against the cheapest current Claude model. If the test fails on a
// real key, the failure is in the wire translation, not the model.
func TestProvider_Generate_RealAnthropic(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping end-to-end smoke test")
	}

	p, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := p.Generate(context.Background(), agent.ChatRequest{
		Model:     "claude-3-5-haiku-latest",
		MaxTokens: 32,
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "Reply with just the word OK."},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content == "" {
		t.Errorf("Content is empty; full response: %+v", resp)
	}
	if resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens == 0 {
		t.Errorf("Usage not populated: %+v", resp.Usage)
	}
	if resp.Model == "" {
		t.Errorf("Model not populated")
	}
}
