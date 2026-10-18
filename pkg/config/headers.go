package config

import (
	"fmt"
	"strings"
)

// reservedHeaderNames are gateway-owned field names that are not covered by
// the Mcp- prefix. Comparison is case-insensitive.
var reservedHeaderNames = map[string]struct{}{
	"content-type":      {},
	"accept":            {},
	"host":              {},
	"content-length":    {},
	"transfer-encoding": {},
	"connection":        {},
}

func validateHeaders(server MCPServer, prefix string) ValidationErrors {
	if len(server.Headers) == 0 {
		return nil
	}
	var errs ValidationErrors
	if !server.IsExternal() && !server.IsOpenAPI() {
		errs = append(errs, ValidationError{prefix + ".headers", "only valid for external URL and OpenAPI servers"})
	}
	for name, value := range server.Headers {
		field := fmt.Sprintf("%s.headers[%s]", prefix, name)
		if name == "" {
			errs = append(errs, ValidationError{prefix + ".headers[]", "header name is empty"})
			continue
		}
		if !isHTTPToken(name) {
			errs = append(errs, ValidationError{field, "header name must be an HTTP token"})
		}
		if headerNameReserved(name) {
			errs = append(errs, ValidationError{field, fmt.Sprintf("header '%s' is reserved and set by the gateway", name)})
		}
		if strings.EqualFold(name, "Authorization") && server.Auth != nil {
			errs = append(errs, ValidationError{prefix + ".headers[Authorization]", "conflicts with auth; set the credential in auth or in headers, not both"})
		}
		if value == "" {
			errs = append(errs, ValidationError{field, "header value is empty"})
			continue
		}
		if !headerValueOK(value) {
			errs = append(errs, ValidationError{field, "header value cannot be sent as an HTTP field value"})
		}
	}
	return errs
}

func headerNameReserved(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "mcp-") {
		return true
	}
	_, ok := reservedHeaderNames[lower]
	return ok
}

// isHTTPToken reports whether name is an RFC 9110 token.
func isHTTPToken(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isTChar(name[i]) {
			return false
		}
	}
	return true
}

func isTChar(c byte) bool {
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// headerValueOK copies the control-character check used for MCP header
// values. pkg/config cannot import pkg/mcp. Empty is rejected separately.
func headerValueOK(v string) bool {
	if v == "" || v != strings.TrimSpace(v) {
		return false
	}
	if strings.HasPrefix(v, "=?base64?") && strings.HasSuffix(v, "?=") {
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
