package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostInAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		allowed []string
		want    bool
	}{
		{"host:port entry", "gw.example:443", []string{"gw.example:443"}, true},
		{"case fold", "GW.Example:443", []string{"gw.example:443"}, true},
		{"hostname against host:port entry", "gw.example", []string{"gw.example:443"}, true},
		{"unlisted host", "evil.example", []string{"gw.example"}, false},
		{"empty allowlist", "evil.example", nil, false},
		{"empty entry", "evil.example", []string{""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HostInAllowlist(tt.host, tt.allowed); got != tt.want {
				t.Fatalf("HostInAllowlist(%q, %v) = %v, want %v", tt.host, tt.allowed, got, tt.want)
			}
		})
	}

	// ValidateRequestHost returns nil for any non-loopback arrival.
	// HostInAllowlist must not apply that short-circuit.
	req := httptest.NewRequest(http.MethodGet, "http://evil.example/mcp", nil)
	req.Host = "evil.example"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 8180}))
	if err := ValidateRequestHost(req, []string{"gw.example"}); err != nil {
		t.Fatalf("non-loopback arrival was checked: %v", err)
	}
	if HostInAllowlist(req.Host, []string{"gw.example"}) {
		t.Fatal("non-loopback arrival short-circuited the allowlist")
	}
}
