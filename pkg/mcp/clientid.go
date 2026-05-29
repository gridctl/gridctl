package mcp

import (
	"context"
	"net/http"
	"strings"
	"unicode"
)

// clientIDKey is the context key under which the gateway propagates a
// normalized client ID through the tool-call path. Tool-call observers
// read the value via ClientIDFromContext to attribute calls per client.
type clientIDKey struct{}

// clientAccessIDKey is the context key under which the gateway propagates the
// connecting client's stable access identifier (see Session.AccessID). The
// per-client access filter reads it via ClientAccessIDFromContext to scope the
// exposed tool surface. It is kept distinct from clientIDKey: ClientID is the
// telemetry attribution dimension, while AccessID is the enforcement key the
// operator configures under stack.yaml `clients:`.
type clientAccessIDKey struct{}

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

// WithClientAccessID returns a child context carrying the connecting client's
// stable access identifier. An empty id leaves the context unchanged.
func WithClientAccessID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, clientAccessIDKey{}, id)
}

// ClientAccessIDFromContext returns the access identifier previously stored on
// ctx via WithClientAccessID, or "" when none is available.
func ClientAccessIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(clientAccessIDKey{}).(string)
	return v
}

// ClientAccessIDHeader is the HTTP header an upstream client may set to declare
// its stable access identifier explicitly, bypassing the clientInfo.name
// normalization heuristic. `gridctl link --client-id` embeds the same value as
// the `client` query parameter on the gateway URL it writes.
const ClientAccessIDHeader = "X-Gridctl-Client-Id"

// clientAccessIDFromRequest extracts the explicit, link-time-assigned client
// identifier from a request: the `client` query parameter takes precedence,
// then the X-Gridctl-Client-Id header. Returns "" when neither is present, in
// which case the gateway falls back to NormalizeClientID(clientInfo.name).
func clientAccessIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if r.URL != nil {
		if v := strings.TrimSpace(r.URL.Query().Get("client")); v != "" {
			return v
		}
	}
	return strings.TrimSpace(r.Header.Get(ClientAccessIDHeader))
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
