package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent"
)

func TestNew_RequiresAPIKey(t *testing.T) {
	t.Parallel()
	if _, err := New(""); err == nil {
		t.Errorf("New(\"\") returned nil error")
	}
}

func TestProvider_Generate_ParsesContentAndToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q, want Bearer sk-test", got)
		}
		body, _ := io.ReadAll(r.Body)
		var raw chatRequest
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if raw.Model != "gpt-test" {
			t.Errorf("Model = %q, want gpt-test", raw.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
  "id": "chatcmpl-x",
  "model": "gpt-test",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Sure.",
      "tool_calls": [{
        "id": "call_1",
        "type": "function",
        "function": {"name": "search", "arguments": "{\"q\":\"x\"}"}
      }]
    },
    "finish_reason": "tool_calls"
  }],
  "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15, "prompt_tokens_details": {"cached_tokens": 3}}
}`)
	}))
	defer server.Close()

	p, err := New("sk-test", WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.Generate(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "search"},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content != "Sure." {
		t.Errorf("Content = %q, want %q", resp.Content, "Sure.")
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" {
		t.Errorf("ToolCalls = %+v", resp.ToolCalls)
	}
	if resp.StopReason != agent.StopReasonToolUse {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, agent.StopReasonToolUse)
	}
	if resp.Usage.InputTokens != 7 { // 10 - 3 cache
		t.Errorf("InputTokens = %d, want 7", resp.Usage.InputTokens)
	}
	if resp.Usage.CacheReadTokens != 3 {
		t.Errorf("CacheReadTokens = %d, want 3", resp.Usage.CacheReadTokens)
	}
}

func TestProvider_Generate_ErrorBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key","type":"invalid_request_error"}}`)
	}))
	defer server.Close()
	p, _ := New("sk-test", WithBaseURL(server.URL))
	_, err := p.Generate(context.Background(), agent.ChatRequest{
		Model:    "gpt-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bad key") {
		t.Errorf("error did not surface message: %v", err)
	}
}

func TestProvider_Stream_ParsesSSE(t *testing.T) {
	t.Parallel()
	const body = `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"x","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"id":"x","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":null}]}

data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}

data: {"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"x\"}"}}]},"finish_reason":null}]}

data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"x","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}

data: [DONE]

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	p, _ := New("sk-test", WithBaseURL(server.URL))
	stream, err := p.Stream(context.Background(), agent.ChatRequest{
		Model:    "gpt-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	var (
		text       string
		args       string
		name       string
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
				name = ch.ToolCallDelta.Name
			}
			args += ch.ToolCallDelta.ArgsDelta
		}
		if ch.StopReason != "" {
			stopReason = ch.StopReason
		}
		if ch.Usage != nil {
			usage = *ch.Usage
		}
	}
	if text != "Hi there" {
		t.Errorf("text = %q, want %q", text, "Hi there")
	}
	if name != "search" {
		t.Errorf("tool name = %q, want search", name)
	}
	if args != `{"q":"x"}` {
		t.Errorf("tool args = %q, want {\"q\":\"x\"}", args)
	}
	if stopReason != agent.StopReasonToolUse {
		t.Errorf("stopReason = %q, want %q", stopReason, agent.StopReasonToolUse)
	}
	if usage.InputTokens != 7 || usage.OutputTokens != 4 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestTranslateMessages_ToolMessageRequiresID(t *testing.T) {
	t.Parallel()
	_, err := translateMessages("", []agent.Message{
		{Role: agent.RoleTool, Content: "x"},
	})
	if err == nil {
		t.Errorf("expected error for tool message with empty ToolCallID")
	}
}

func TestTranslateMessages_PlacesSystemFirst(t *testing.T) {
	t.Parallel()
	out, err := translateMessages("be helpful", []agent.Message{
		{Role: agent.RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("translateMessages: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[0].Role != "system" {
		t.Errorf("out[0].Role = %q, want system", out[0].Role)
	}
}

func TestTranslateStopReason(t *testing.T) {
	t.Parallel()
	cases := map[string]agent.StopReason{
		"stop":           agent.StopReasonEnd,
		"length":         agent.StopReasonMaxTokens,
		"tool_calls":     agent.StopReasonToolUse,
		"function_call":  agent.StopReasonToolUse,
		"content_filter": agent.StopReasonError,
		"":               agent.StopReasonEnd,
	}
	for in, want := range cases {
		if got := translateStopReason(in); got != want {
			t.Errorf("translateStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}
