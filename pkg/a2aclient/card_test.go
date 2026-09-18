package a2aclient

import "testing"

func TestParseCard_DialectEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, body, dialect, want string
		fail                      bool
	}{
		{"modern", `{"supportedInterfaces":[{"url":"https://example.com/rpc","protocolBinding":"JSONRPC","protocolVersion":"1.0.7"}]}`, "auto", "1.0", false},
		{"legacy default", `{"url":"https://example.com/rpc","protocolVersion":"0.3.0"}`, "auto", "0.3", false},
		{"legacy additional", `{"url":"https://example.com/grpc","preferredTransport":"GRPC","protocolVersion":"0.3.1","additionalInterfaces":[{"url":"https://example.com/rpc","transport":"JSONRPC"}]}`, "0.3", "0.3", false},
		{"prefer modern", `{"url":"https://example.com/old","protocolVersion":"0.3.0","supportedInterfaces":[{"url":"https://example.com/new","protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`, "auto", "1.0", false},
		{"constrain legacy", `{"url":"https://example.com/old","protocolVersion":"0.3.0","supportedInterfaces":[{"url":"https://example.com/new","protocolBinding":"JSONRPC","protocolVersion":"1.0"}]}`, "0.3", "0.3", false},
		{"contradiction", `{"url":"https://example.com/rpc","protocolVersion":"0.3.0"}`, "1.0", "", true},
		{"absent evidence", `{"url":"https://example.com/rpc"}`, "0.3", "", true},
		{"grpc only", `{"url":"https://example.com/grpc","preferredTransport":"GRPC","protocolVersion":"0.3.0"}`, "auto", "", true},
		{"tenant", `{"supportedInterfaces":[{"url":"https://example.com/rpc","protocolBinding":"JSONRPC","protocolVersion":"1.0","tenant":"required"}]}`, "auto", "", true},
		{"extension", `{"url":"https://example.com/rpc","protocolVersion":"0.3.0","capabilities":{"extensions":[{"required":true}]}}`, "auto", "", true},
		{"unsafe unselected", `{"url":"https://example.com/rpc","protocolVersion":"0.3.0","additionalInterfaces":[{"url":"http://example.com/grpc","transport":"GRPC"}]}`, "auto", "", true},
		{"wrong minor", `{"url":"https://example.com/rpc","protocolVersion":"0.30.0"}`, "auto", "", true},
		{"invalid", `{`, "auto", "", true},
		{"null", `null`, "auto", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, iface, err := ParseCard([]byte(tt.body), tt.dialect)
			if (err != nil) != tt.fail || !tt.fail && iface.ProtocolVersion != tt.want {
				t.Fatalf("version=%s err=%v", iface.ProtocolVersion, err)
			}
		})
	}
}
