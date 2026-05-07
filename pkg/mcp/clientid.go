package mcp

import (
	"context"
	"strings"
	"unicode"
)

// clientIDKey is the context key under which the gateway propagates a
// normalized client ID through the tool-call path. Tool-call observers
// read the value via ClientIDFromContext to attribute calls per client.
type clientIDKey struct{}

// WithClientID returns a child context carrying the given normalized client ID.
// An empty id leaves the context unchanged so callers do not have to pre-check.
func WithClientID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIDKey{}, id)
}

// ClientIDFromContext returns the normalized client ID previously stored on
// ctx via WithClientID, or "" when no client attribution is available.
func ClientIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(clientIDKey{}).(string)
	return v
}

// clientNameAliases maps the noisy `clientInfo.name` strings emitted by
// common MCP clients to stable short identifiers. Keys are matched
// case-insensitively against the raw name.
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

// NormalizeClientID returns a stable, lowercase, hyphenated identifier for
// the supplied raw client name from a `clientInfo.name` field. Common
// alias variants ("Claude Code", "claude-ai", "Cursor") map to short
// canonical IDs ("claude-code", "claude-desktop", "cursor"); unknown
// clients pass through as best-effort slugs (lowercased, with whitespace
// and underscores collapsed to single hyphens).
//
// An empty input returns "". The function never errors and never panics —
// the cost path treats unknown clients as opaque attribution dimensions
// rather than rejecting them.
func NormalizeClientID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if alias, ok := clientNameAliases[lower]; ok {
		return alias
	}
	return slugifyClientName(lower)
}

// slugifyClientName converts a lowercased client name to a hyphen-delimited
// slug. Allowed runes pass through; any other rune is treated as a
// separator. Consecutive separators collapse, and leading/trailing
// hyphens are stripped.
func slugifyClientName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSep := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevSep = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSep = false
		case r == '.':
			b.WriteRune(r)
			prevSep = false
		case unicode.IsSpace(r), r == '_', r == '-', r == '/':
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		default:
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	return out
}
