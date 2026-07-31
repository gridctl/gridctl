package mcp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Stateless-era request metadata headers (2026-07-28). The transport
// mirrors selected body fields into headers so intermediaries can route
// without parsing bodies; any processing server must validate the
// mirror against the body and reject mismatches with -32020.
const (
	headerMcpMethod = "Mcp-Method"
	headerMcpName   = "Mcp-Name"
)

// base64 sentinel markers for header values that cannot be represented
// as plain ASCII field values. Case-sensitive per spec.
const (
	headerSentinelPrefix = "=?base64?"
	headerSentinelSuffix = "?="
)

// mcpNameForRequest returns the Mcp-Name source value for a method, per
// the spec's standard-header table: params.name for tools/call and
// prompts/get, params.uri for resources/read, "" for everything else.
func mcpNameForRequest(method string, params json.RawMessage) string {
	var key string
	switch method {
	case "tools/call", "prompts/get":
		key = "name"
	case "resources/read":
		key = "uri"
	default:
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return ""
	}
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return value
}

// headerValueSafe reports whether v can travel as a plain ASCII header
// value: visible ASCII plus space and tab, no leading/trailing
// whitespace, and not itself shaped like the sentinel.
func headerValueSafe(v string) bool {
	if v == "" {
		return true
	}
	if v != strings.TrimSpace(v) {
		return false
	}
	if strings.HasPrefix(v, headerSentinelPrefix) && strings.HasSuffix(v, headerSentinelSuffix) {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\t' && (c < 0x20 || c > 0x7e) {
			return false
		}
	}
	return true
}

// encodeHeaderValue renders v as a header value, applying the base64
// sentinel when it is not header-safe.
func encodeHeaderValue(v string) string {
	if headerValueSafe(v) {
		return v
	}
	return headerSentinelPrefix + base64.StdEncoding.EncodeToString([]byte(v)) + headerSentinelSuffix
}

// decodeHeaderValue reverses encodeHeaderValue. Non-sentinel values
// pass through; a malformed sentinel payload reports ok=false so
// validation can reject rather than compare garbage.
func decodeHeaderValue(v string) (string, bool) {
	if !strings.HasPrefix(v, headerSentinelPrefix) || !strings.HasSuffix(v, headerSentinelSuffix) {
		return v, true
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(v, headerSentinelPrefix), headerSentinelSuffix)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}
