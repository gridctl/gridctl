package a2aclient

import "testing"

func TestParseURL(t *testing.T) {
	for _, raw := range []string{"https://EXAMPLE.com:443", "http://localhost", "http://127.0.0.1:1234/card", "http://[::1]/card", "https://example.com/rpc?q=a%20b"} {
		if _, err := ParseURL(raw); err != nil {
			t.Errorf("valid URL rejected: %s", raw)
		}
	}
	for _, raw := range []string{"file:///card", "http://example.com", "http://127.0.0.2", "https://user:password@example.com", "https://example.com/#", "https://example.com/#frag", "https://example.com:", "https://example.com:0", "https://example.com:65536", "//example.com", "https:opaque", "https://"} {
		if _, err := ParseURL(raw); err == nil {
			t.Errorf("invalid URL accepted: %s", raw)
		}
	}
	a, _ := ParseURL("https://EXAMPLE.com:0443/card")
	b, _ := ParseURL("https://example.com/rpc")
	if !sameOrigin(a, b) || a.String() != "https://example.com/card" {
		t.Fatal("origin normalization failed")
	}
}
