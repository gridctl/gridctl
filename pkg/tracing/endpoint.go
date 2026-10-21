package tracing

import (
	"fmt"
	"net/url"
	"strings"
)

// ResolveOTLPEndpoint parses raw and, when the path is empty or "/", appends
// signalPath ("/v1/traces" or "/v1/metrics"). Any other path is used verbatim.
// Both exporters call this: WithEndpointURL uses the path as-is and never
// appends a signal path itself.
func ResolveOTLPEndpoint(raw, signalPath string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("endpoint is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("endpoint must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("endpoint must include a host")
	}
	if u.Path == "" || u.Path == "/" {
		if !strings.HasPrefix(signalPath, "/") {
			return "", fmt.Errorf("signal path must be absolute")
		}
		u.Path = signalPath
	}
	return u.String(), nil
}

// EndpointHost returns the host (and port) of an OTLP endpoint for logs.
// It never returns userinfo, path, or query. An unparseable value yields "".
func EndpointHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Host
}
