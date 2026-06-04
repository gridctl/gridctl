package config

import "strings"

// clientNameAliases mirrors the alias table in pkg/mcp/clientid.go so the
// validation layer can warn about client_models keys that would never match
// the wire identity (e.g. "claude-ai" normalizes to "claude-desktop").
//
// pkg/config deliberately does not import pkg/mcp: config is a foundational
// package and pulling in the MCP stack for one function would freeze the
// dependency direction. The copy is small and drift-guarded by
// TestNormalizedClientModelKeyParity (clientmodels_parity_test.go), an
// external test that imports both packages and asserts they agree.
var clientNameAliases = map[string]string{
	"claude-ai":      "claude-desktop",
	"claude desktop": "claude-desktop",
	"claude code":    "claude-code",
	"claude-code":    "claude-code",
	"cursor":         "cursor",
	"cursor-ide":     "cursor",
	"windsurf":       "windsurf",
	"continue":       "continue",
	"continue.dev":   "continue",
	"cline":          "cline",
	"zed":            "zed",
	"goose":          "goose",
}

// normalizedClientModelKey returns the canonical client ID for a raw
// client_models key, mirroring mcp.NormalizeClientID: alias variants map to
// short canonical IDs, everything else slugifies to lowercase hyphenated
// form. Used only to warn when a configured key differs from the form the
// gateway resolves at call time — the cost path itself never re-normalizes.
func normalizedClientModelKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if alias, ok := clientNameAliases[lower]; ok {
		return alias
	}
	return slugifyClientModelKey(lower)
}

// slugifyClientModelKey mirrors slugifyClientName in pkg/mcp/clientid.go:
// lowercase letters, digits, and dots pass through, runs of any other rune
// collapse to a single hyphen, and leading/trailing hyphens are stripped.
func slugifyClientModelKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSep := true
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.':
			b.WriteRune(r)
			prevSep = false
		default:
			if !prevSep {
				b.WriteRune('-')
				prevSep = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// NormalizedClientModelKeyForTest exposes normalizedClientModelKey to the
// external parity test (clientmodels_parity_test.go), which asserts this
// package's copy of the normalization logic matches mcp.NormalizeClientID.
// Not for production use — the cost path never re-normalizes keys.
func NormalizedClientModelKeyForTest(raw string) string {
	return normalizedClientModelKey(raw)
}
