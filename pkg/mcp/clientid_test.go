package mcp

import (
	"context"
	"testing"
)

func TestNormalizeClientID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"claude-ai alias", "claude-ai", "claude-desktop"},
		{"claude-ai mixed case", "Claude-AI", "claude-desktop"},
		{"claude desktop spaced", "Claude Desktop", "claude-desktop"},
		{"claude code spaced", "Claude Code", "claude-code"},
		{"claude-code hyphen", "claude-code", "claude-code"},
		{"cursor", "Cursor", "cursor"},
		{"cursor ide", "Cursor-IDE", "cursor"},
		{"windsurf", "windsurf", "windsurf"},
		{"continue.dev", "continue.dev", "continue"},
		{"continue plain", "continue", "continue"},
		{"cline", "Cline", "cline"},
		{"zed", "Zed", "zed"},
		{"goose", "goose", "goose"},

		// Unknown clients fall through with slugification.
		{"unknown camel", "MyAgent", "myagent"},
		{"unknown spaces", "My Custom Agent", "my-custom-agent"},
		{"unknown underscores", "my_custom_agent", "my-custom-agent"},
		{"unknown punctuation", "agent (v2)!", "agent-v2"},
		{"unknown leading/trailing", "  --weird@@  ", "weird"},
		{"alpha-numeric digit", "agent42", "agent42"},
		{"digits only", "12345", "12345"},
		{"version dot kept", "myagent-1.2", "myagent-1.2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeClientID(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeClientID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClientIDContext_RoundTrip(t *testing.T) {
	ctx := WithClientID(context.Background(), "claude-code")
	if got := ClientIDFromContext(ctx); got != "claude-code" {
		t.Errorf("ClientIDFromContext = %q, want %q", got, "claude-code")
	}
}

func TestClientIDContext_EmptyIsNoOp(t *testing.T) {
	parent := context.Background()
	ctx := WithClientID(parent, "")
	if ctx != parent {
		t.Error("WithClientID with empty id should return the parent context unchanged")
	}
	if got := ClientIDFromContext(ctx); got != "" {
		t.Errorf("ClientIDFromContext on empty context = %q, want empty", got)
	}
}

func TestClientIDFromContext_BackgroundReturnsEmpty(t *testing.T) {
	if got := ClientIDFromContext(context.Background()); got != "" {
		t.Errorf("ClientIDFromContext(Background) = %q, want empty", got)
	}
}
