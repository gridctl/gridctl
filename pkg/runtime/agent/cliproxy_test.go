package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// --- helpers ---

func TestServerNameFromPrefix(t *testing.T) {
	tests := []struct {
		toolName string
		want     string
	}{
		{"filesystem__read_file", "filesystem"},
		{"my_server__do_thing", "my_server"},
		{"notool", ""},
		{"", ""},
		{"__leading", ""},
		{"trailing__", "trailing"},
	}
	for _, tt := range tests {
		got := serverNameFromPrefix(tt.toolName)
		if got != tt.want {
			t.Errorf("serverNameFromPrefix(%q) = %q; want %q", tt.toolName, got, tt.want)
		}
	}
}

func TestLastUserMsg(t *testing.T) {
	tests := []struct {
		name    string
		history []Message
		want    string
	}{
		{
			name:    "single user message",
			history: []Message{{Role: "user", Content: "hello"}},
			want:    "hello",
		},
		{
			name: "last user message",
			history: []Message{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "reply"},
				{Role: "user", Content: "second"},
			},
			want: "second",
		},
		{
			name:    "no user message",
			history: []Message{{Role: "assistant", Content: "hi"}},
			want:    "",
		},
		{
			name:    "empty history",
			history: nil,
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastUserMsg(tt.history)
			if got != tt.want {
				t.Errorf("lastUserMsg() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestMCPConfigJSON(t *testing.T) {
	got := MCPConfigJSON("http://localhost:8180/sse")
	if !strings.Contains(got, "gridctl") {
		t.Errorf("MCPConfigJSON missing server name: %s", got)
	}
	if !strings.Contains(got, "http://localhost:8180/sse") {
		t.Errorf("MCPConfigJSON missing URL: %s", got)
	}
	// Must be valid JSON
	var v any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Errorf("MCPConfigJSON not valid JSON: %v", err)
	}
}

func TestWriteTempMCPConfig(t *testing.T) {
	content := `{"mcpServers":{}}`
	path, err := writeTempMCPConfig(content)
	if err != nil {
		t.Fatalf("writeTempMCPConfig error: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read temp file: %v", err)
	}
	if string(data) != content {
		t.Errorf("temp file content = %q; want %q", string(data), content)
	}
}

// --- NewCLIProxyClient ---

func TestNewCLIProxyClient_NilMCPConfig(t *testing.T) {
	c := NewCLIProxyClient("/usr/bin/claude", "")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.cliPath != "/usr/bin/claude" {
		t.Errorf("cliPath = %q; want %q", c.cliPath, "/usr/bin/claude")
	}
	if c.mcpConfigPath != "" {
		t.Errorf("expected no mcpConfigPath, got %q", c.mcpConfigPath)
	}
}

func TestNewCLIProxyClient_WithMCPConfig(t *testing.T) {
	c := NewCLIProxyClient("/usr/bin/claude", `{"mcpServers":{}}`)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.mcpConfigPath == "" {
		t.Fatal("expected mcpConfigPath to be set")
	}
	defer os.Remove(c.mcpConfigPath)

	data, err := os.ReadFile(c.mcpConfigPath)
	if err != nil {
		t.Fatalf("could not read mcp config: %v", err)
	}
	if !strings.Contains(string(data), "mcpServers") {
		t.Errorf("mcp config file missing expected content: %s", string(data))
	}
}

// --- Close ---

func TestCLIProxyClient_CloseWithNoProcess(t *testing.T) {
	c := NewCLIProxyClient("/usr/bin/claude", "")
	// Close with no running process should not panic or error
	if err := c.Close(); err != nil {
		t.Errorf("Close() = %v; want nil", err)
	}
}

func TestCLIProxyClient_CloseRemovesMCPConfig(t *testing.T) {
	c := NewCLIProxyClient("/usr/bin/claude", `{"mcpServers":{}}`)
	if c.mcpConfigPath == "" {
		t.Fatal("expected mcpConfigPath to be set")
	}
	path := c.mcpConfigPath

	if err := c.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected temp file %q to be removed after Close", path)
	}
}

// --- Stream error path ---

func TestCLIProxyClient_Stream_CLINotFound(t *testing.T) {
	c := NewCLIProxyClient("/nonexistent/claude", "")
	events := make(chan LLMEvent, 16)
	history := []Message{{Role: "user", Content: "hello"}}

	_, _, err := c.Stream(context.Background(), "", history, nil, nil, events)
	if err == nil {
		t.Fatal("expected error when CLI binary not found")
	}
}

func TestCLIProxyClient_Stream_NoUserMessage(t *testing.T) {
	c := NewCLIProxyClient("/usr/bin/true", "")
	events := make(chan LLMEvent, 16)

	_, _, err := c.Stream(context.Background(), "", nil, nil, nil, events)
	if err == nil {
		t.Fatal("expected error with empty history")
	}
}

// --- parseStreamJSON ---

func makeStreamLine(t string, extra map[string]any) string {
	m := map[string]any{"type": t}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestParseStreamJSON_TextTokens(t *testing.T) {
	lines := []string{
		makeStreamLine("message_start", map[string]any{
			"message": map[string]any{"usage": map[string]any{"input_tokens": 10}},
		}),
		makeStreamLine("content_block_start", map[string]any{
			"index":         0,
			"content_block": map[string]any{"type": "text"},
		}),
		makeStreamLine("content_block_delta", map[string]any{
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "Hello"},
		}),
		makeStreamLine("content_block_delta", map[string]any{
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": " world"},
		}),
		makeStreamLine("content_block_stop", map[string]any{"index": 0}),
		makeStreamLine("message_delta", map[string]any{
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": 5},
		}),
		makeStreamLine("message_stop", nil),
	}
	input := strings.NewReader(strings.Join(lines, "\n") + "\n")

	var sb strings.Builder
	var inputTokens, outputTokens int
	events := make(chan LLMEvent, 32)

	if err := parseStreamJSON(context.Background(), input, &sb, &inputTokens, &outputTokens, events); err != nil {
		t.Fatalf("parseStreamJSON error: %v", err)
	}

	if sb.String() != "Hello world" {
		t.Errorf("text = %q; want %q", sb.String(), "Hello world")
	}
	if inputTokens != 10 {
		t.Errorf("inputTokens = %d; want 10", inputTokens)
	}
	if outputTokens != 5 {
		t.Errorf("outputTokens = %d; want 5", outputTokens)
	}

	close(events)
	var tokenTexts []string
	for ev := range events {
		if ev.Type == EventTypeToken {
			tokenTexts = append(tokenTexts, ev.Data.(TokenData).Text)
		}
	}
	if strings.Join(tokenTexts, "") != "Hello world" {
		t.Errorf("streamed tokens = %q; want %q", strings.Join(tokenTexts, ""), "Hello world")
	}
}

func TestParseStreamJSON_ToolUseBlock(t *testing.T) {
	lines := []string{
		makeStreamLine("content_block_start", map[string]any{
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "toolu_01",
				"name": "fs__read_file",
			},
		}),
		makeStreamLine("content_block_delta", map[string]any{
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"/tmp`},
		}),
		makeStreamLine("content_block_delta", map[string]any{
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `/foo"}`},
		}),
		makeStreamLine("content_block_stop", map[string]any{"index": 0}),
	}
	input := strings.NewReader(strings.Join(lines, "\n") + "\n")

	var sb strings.Builder
	var in, out int
	events := make(chan LLMEvent, 32)

	if err := parseStreamJSON(context.Background(), input, &sb, &in, &out, events); err != nil {
		t.Fatalf("parseStreamJSON error: %v", err)
	}

	close(events)
	var startEvs, endEvs []LLMEvent
	for ev := range events {
		switch ev.Type {
		case EventTypeToolCallStart:
			startEvs = append(startEvs, ev)
		case EventTypeToolCallEnd:
			endEvs = append(endEvs, ev)
		}
	}

	if len(startEvs) != 1 {
		t.Fatalf("expected 1 tool_call_start, got %d", len(startEvs))
	}
	if len(endEvs) != 1 {
		t.Fatalf("expected 1 tool_call_end, got %d", len(endEvs))
	}

	startData := startEvs[0].Data.(ToolCallStartData)
	if startData.ToolName != "fs__read_file" {
		t.Errorf("ToolName = %q; want %q", startData.ToolName, "fs__read_file")
	}
	if startData.ServerName != "fs" {
		t.Errorf("ServerName = %q; want %q", startData.ServerName, "fs")
	}

	endData := endEvs[0].Data.(ToolCallEndData)
	if endData.ToolName != "fs__read_file" {
		t.Errorf("end ToolName = %q; want %q", endData.ToolName, "fs__read_file")
	}
}

func TestParseStreamJSON_ContextCancelled(t *testing.T) {
	// Infinite reader — context cancellation should stop parsing
	r := strings.NewReader(strings.Repeat(makeStreamLine("message_stop", nil)+"\n", 100))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var sb strings.Builder
	var in, out int
	events := make(chan LLMEvent, 32)

	err := parseStreamJSON(ctx, r, &sb, &in, &out, events)
	if err == nil {
		t.Error("expected context error, got nil")
	}
}

func TestParseStreamJSON_SkipsInvalidJSON(t *testing.T) {
	lines := "not json at all\n" +
		makeStreamLine("content_block_delta", map[string]any{
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		}) + "\n"

	var sb strings.Builder
	var in, out int
	events := make(chan LLMEvent, 8)

	err := parseStreamJSON(context.Background(), strings.NewReader(lines), &sb, &in, &out, events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sb.String() != "ok" {
		t.Errorf("text = %q; want %q", sb.String(), "ok")
	}
}
