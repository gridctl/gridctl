package config_test

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestNormalizedClientModelKeyParity guards the deliberate copy of the
// client-ID normalization logic in pkg/config/clientmodels.go against drift
// from its source of truth, mcp.NormalizeClientID. pkg/config cannot import
// pkg/mcp in production code (it would invert the foundational-package
// layering), but this external test package can import both. If this test
// fails, sync pkg/config/clientmodels.go with pkg/mcp/clientid.go.
func TestNormalizedClientModelKeyParity(t *testing.T) {
	samples := []string{
		// alias-table entries (the high-risk drift surface)
		"claude-ai", "claude desktop", "claude code", "claude-code",
		"cursor", "cursor-ide", "windsurf", "continue", "continue.dev",
		"cline", "zed", "goose",
		// case and whitespace variants
		"Claude Code", "  CURSOR  ", "Claude-AI",
		// slug-path inputs
		"gemini-cli", "Gemini CLI", "grok_build", "My Custom Agent v2",
		"continue.dev nightly", "weird//name__here", "-leading-trailing-",
		"", "   ", "日本語クライアント",
	}
	for _, raw := range samples {
		want := mcp.NormalizeClientID(raw)
		got := config.NormalizedClientModelKeyForTest(raw)
		if got != want {
			t.Errorf("normalization drift for %q: config=%q mcp=%q — sync pkg/config/clientmodels.go with pkg/mcp/clientid.go", raw, got, want)
		}
	}
}
