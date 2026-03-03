package logging

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// Patterns that match sensitive values in log output.
// Each pattern uses a capture group to preserve the prefix (e.g., "Bearer ")
// while replacing only the secret value with [REDACTED].
var defaultRedactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(Authorization:\s*)\S+(\s+\S+)?`),
	regexp.MustCompile(`(?i)(Bearer\s+)\S+`),
	regexp.MustCompile(`(?i)((?:password|passwd|secret|api[_-]?key|token|credentials?|auth[_-]?token)\s*[=:]\s*)\S+`),
}

// RedactingHandler is a slog.Handler that redacts sensitive values from all
// log records before forwarding them to an inner handler. It scans string
// values in the log message and all attributes for patterns that look like
// secrets (bearer tokens, authorization headers, passwords, API keys).
type RedactingHandler struct {
	inner        slog.Handler
	patterns     []*regexp.Regexp
	redactValues []string // exact string values to redact (sorted longest-first)
}

// NewRedactingHandler wraps an inner handler with secret redaction.
func NewRedactingHandler(inner slog.Handler) *RedactingHandler {
	return &RedactingHandler{
		inner:    inner,
		patterns: defaultRedactPatterns,
	}
}

// RegisterRedactValues adds exact string values that should be redacted in logs.
// Values are sorted longest-first to avoid partial matches.
// This method creates a new slice to avoid data races with concurrent Handle() calls.
func (h *RedactingHandler) RegisterRedactValues(values []string) {
	// Build new slice: existing + new, deduplicated
	seen := make(map[string]bool, len(h.redactValues)+len(values))
	merged := make([]string, 0, len(h.redactValues)+len(values))
	for _, v := range h.redactValues {
		if v != "" && !seen[v] {
			merged = append(merged, v)
			seen[v] = true
		}
	}
	for _, v := range values {
		if v != "" && !seen[v] {
			merged = append(merged, v)
			seen[v] = true
		}
	}
	// Sort longest-first to prevent partial replacement
	sort.Slice(merged, func(i, j int) bool {
		return len(merged[i]) > len(merged[j])
	})
	// Atomic swap: assign new slice (safe for concurrent readers)
	h.redactValues = merged
}

// Enabled delegates to the inner handler.
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle redacts sensitive values in the record before forwarding.
func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Redact message
	r.Message = h.redactString(r.Message)

	// Build new attrs with redacted values
	var redacted []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		redacted = append(redacted, h.redactAttr(a))
		return true
	})

	// Create a new record with redacted attrs
	newRecord := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	newRecord.AddAttrs(redacted...)

	return h.inner.Handle(ctx, newRecord)
}

// WithAttrs returns a new handler with redacted persistent attributes.
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &RedactingHandler{
		inner:        h.inner.WithAttrs(redacted),
		patterns:     h.patterns,
		redactValues: h.redactValues,
	}
}

// WithGroup returns a new handler with the given group name.
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{
		inner:        h.inner.WithGroup(name),
		patterns:     h.patterns,
		redactValues: h.redactValues,
	}
}

// redactAttr redacts sensitive values in an attribute.
func (h *RedactingHandler) redactAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.redactString(a.Value.String()))
	case slog.KindGroup:
		attrs := a.Value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, ga := range attrs {
			redacted[i] = h.redactAttr(ga)
		}
		return slog.Group(a.Key, attrsToAny(redacted)...)
	case slog.KindAny:
		return h.redactAnyAttr(a)
	default:
		return a
	}
}

// redactAnyAttr handles KindAny values like []string (used for command arrays),
// maps (used for env vars), and error types.
func (h *RedactingHandler) redactAnyAttr(a slog.Attr) slog.Attr {
	v := a.Value.Any()
	switch val := v.(type) {
	case []string:
		redacted := make([]string, len(val))
		for i, s := range val {
			redacted[i] = h.redactString(s)
		}
		return slog.Any(a.Key, redacted)
	case map[string]string:
		redacted := make(map[string]string, len(val))
		for k, v := range val {
			if isSensitiveKey(k) {
				redacted[k] = "[REDACTED]"
			} else {
				redacted[k] = h.redactString(v)
			}
		}
		return slog.Any(a.Key, redacted)
	case error:
		return slog.String(a.Key, h.redactString(val.Error()))
	case fmt.Stringer:
		return slog.String(a.Key, h.redactString(val.String()))
	default:
		return a
	}
}

// redactString applies all redaction patterns and exact value matches to a string.
func (h *RedactingHandler) redactString(s string) string {
	for _, p := range h.patterns {
		s = p.ReplaceAllString(s, "${1}[REDACTED]")
	}
	for _, v := range h.redactValues {
		s = strings.ReplaceAll(s, v, "[REDACTED]")
	}
	return s
}

// RedactString applies the default redaction patterns to a string.
// Use this for redacting secrets in non-slog output (e.g., verbose JSON dumps).
func RedactString(s string) string {
	for _, p := range defaultRedactPatterns {
		s = p.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}

// attrsToAny converts []slog.Attr to []any for slog.Group().
func attrsToAny(attrs []slog.Attr) []any {
	result := make([]any, len(attrs))
	for i, a := range attrs {
		result[i] = a
	}
	return result
}

// RedactEnv returns a copy of the env map with sensitive values redacted.
// Keys matching common secret patterns have their values replaced with [REDACTED].
func RedactEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	redacted := make(map[string]string, len(env))
	for k, v := range env {
		if isSensitiveKey(k) {
			redacted[k] = "[REDACTED]"
		} else {
			redacted[k] = v
		}
	}
	return redacted
}

var sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|key|credential|auth|api[_-]?key)`)

// isSensitiveKey returns true if the key name suggests it holds a secret.
func isSensitiveKey(key string) bool {
	return sensitiveKeyPattern.MatchString(strings.ToLower(key))
}
