package google

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

func TestProvider_Generate_ParsesContentAndFunctionCall(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "key=test-key") {
			t.Errorf("URL did not include key: %s", r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		var raw generateRequest
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(raw.Contents) == 0 {
			t.Errorf("request contents empty")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
  "candidates": [{
    "content": {
      "role": "model",
      "parts": [
        {"text": "I will search."},
        {"functionCall": {"name": "search", "args": {"q": "x"}}}
      ]
    },
    "finishReason": "STOP",
    "index": 0
  }],
  "usageMetadata": {"promptTokenCount": 12, "candidatesTokenCount": 8, "totalTokenCount": 20, "cachedContentTokenCount": 4},
  "modelVersion": "gemini-test"
}`)
	}))
	defer server.Close()

	p, err := New("test-key", WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.Generate(context.Background(), agent.ChatRequest{
		Model: "gemini-test",
		Messages: []agent.Message{
			{Role: agent.RoleUser, Content: "find x"},
		},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Content != "I will search." {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls = %+v", resp.ToolCalls)
	}
	if resp.StopReason != agent.StopReasonToolUse {
		t.Errorf("StopReason = %q, want %q (tool_use heuristic)", resp.StopReason, agent.StopReasonToolUse)
	}
	if resp.Usage.InputTokens != 8 { // 12 - 4 cached
		t.Errorf("InputTokens = %d, want 8", resp.Usage.InputTokens)
	}
	if resp.Usage.CacheReadTokens != 4 {
		t.Errorf("CacheReadTokens = %d, want 4", resp.Usage.CacheReadTokens)
	}
}

func TestProvider_Generate_ErrorBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":400,"message":"bad model","status":"INVALID_ARGUMENT"}}`)
	}))
	defer server.Close()
	p, _ := New("test-key", WithBaseURL(server.URL))
	_, err := p.Generate(context.Background(), agent.ChatRequest{
		Model:    "gemini-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Errorf("error did not surface message: %v", err)
	}
}

func TestProvider_Stream_ParsesSSE(t *testing.T) {
	t.Parallel()
	const body = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "}]},"index":0}],"modelVersion":"gemini-test"}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":"world"}]},"index":0}],"modelVersion":"gemini-test"}

data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search","args":{"q":"x"}}}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":7,"totalTokenCount":12},"modelVersion":"gemini-test"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	p, _ := New("test-key", WithBaseURL(server.URL))
	stream, err := p.Stream(context.Background(), agent.ChatRequest{
		Model:    "gemini-test",
		Messages: []agent.Message{{Role: agent.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	var (
		text       string
		toolName   string
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
		if ch.ToolCallDelta != nil && ch.ToolCallDelta.Name != "" {
			toolName = ch.ToolCallDelta.Name
		}
		if ch.StopReason != "" {
			stopReason = ch.StopReason
		}
		if ch.Usage != nil {
			usage = *ch.Usage
		}
	}
	if text != "Hello world" {
		t.Errorf("text = %q", text)
	}
	if toolName != "search" {
		t.Errorf("tool name = %q", toolName)
	}
	if stopReason != agent.StopReasonToolUse {
		t.Errorf("stopReason = %q, want %q", stopReason, agent.StopReasonToolUse)
	}
	if usage.InputTokens != 5 || usage.OutputTokens != 7 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestTranslateMessages_RejectsSystemRole(t *testing.T) {
	t.Parallel()
	_, err := translateMessages([]agent.Message{{Role: agent.RoleSystem, Content: "hi"}})
	if err == nil {
		t.Errorf("expected system-role rejection")
	}
}

func TestTranslateMessages_ToolMessageRequiresName(t *testing.T) {
	t.Parallel()
	_, err := translateMessages([]agent.Message{
		{Role: agent.RoleTool, ToolCallID: "x", Content: "y"},
	})
	if err == nil {
		t.Errorf("expected name-required error")
	}
}

func TestTranslateStopReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		hasCalls bool
		want     agent.StopReason
	}{
		{"STOP", false, agent.StopReasonEnd},
		{"STOP", true, agent.StopReasonToolUse},
		{"MAX_TOKENS", false, agent.StopReasonMaxTokens},
		{"SAFETY", false, agent.StopReasonError},
		{"", false, agent.StopReasonEnd},
		{"weird", false, agent.StopReasonEnd},
	}
	for _, c := range cases {
		if got := translateStopReason(c.in, c.hasCalls); got != c.want {
			t.Errorf("translateStopReason(%q, %v) = %q, want %q", c.in, c.hasCalls, got, c.want)
		}
	}
}
