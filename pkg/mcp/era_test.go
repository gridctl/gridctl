package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEraOfVersion(t *testing.T) {
	tests := []struct {
		version string
		want    ProtocolEra
	}{
		{"2026-07-28", EraStateless},
		{"2025-11-25", EraHandshake},
		{"2025-06-18", EraHandshake},
		{"2024-11-05", EraHandshake},
		{"", EraHandshake},
		{"9999-12-31", EraHandshake}, // unknown never classifies stateless
	}
	for _, tc := range tests {
		if got := EraOfVersion(tc.version); got != tc.want {
			t.Errorf("EraOfVersion(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestStatelessVersionIsSupported(t *testing.T) {
	if !IsSupportedProtocolVersion(StatelessProtocolVersion) {
		t.Fatalf("%s must be in the supported set", StatelessProtocolVersion)
	}
	// The handshake counter-offer default must stay handshake-era: a
	// legacy client requesting an unknown version must never be
	// counter-offered a stateless revision it cannot speak.
	if got := NegotiateProtocolVersion("1900-01-01"); EraOfVersion(got) != EraHandshake {
		t.Fatalf("counter-offer %q is not handshake-era", got)
	}
}

func TestParseRequestMeta(t *testing.T) {
	tests := []struct {
		name       string
		params     string
		wantModern bool
		wantVer    string
		wantClient string
	}{
		{
			name: "full modern meta",
			params: `{"name":"echo","arguments":{},"_meta":{
				"io.modelcontextprotocol/protocolVersion":"2026-07-28",
				"io.modelcontextprotocol/clientInfo":{"name":"ExampleClient","version":"1.0.0"},
				"io.modelcontextprotocol/clientCapabilities":{}}}`,
			wantModern: true,
			wantVer:    "2026-07-28",
			wantClient: "ExampleClient",
		},
		{
			name:       "no meta",
			params:     `{"name":"echo","arguments":{}}`,
			wantModern: false,
		},
		{
			name:       "meta without protocolVersion is not modern",
			params:     `{"_meta":{"traceparent":"00-abc-def-01"}}`,
			wantModern: false,
		},
		{
			name:       "empty params",
			params:     "",
			wantModern: false,
		},
		{
			name:       "non-object params",
			params:     `[1,2,3]`,
			wantModern: false,
		},
		{
			name:       "non-string version stays empty for rejection",
			params:     `{"_meta":{"io.modelcontextprotocol/protocolVersion":42}}`,
			wantModern: true,
			wantVer:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta, modern := parseRequestMeta(json.RawMessage(tc.params))
			if modern != tc.wantModern {
				t.Fatalf("modern = %v, want %v", modern, tc.wantModern)
			}
			if !modern {
				return
			}
			if meta.ProtocolVersion != tc.wantVer {
				t.Errorf("ProtocolVersion = %q, want %q", meta.ProtocolVersion, tc.wantVer)
			}
			if tc.wantClient != "" && meta.ClientInfo.Name != tc.wantClient {
				t.Errorf("ClientInfo.Name = %q, want %q", meta.ClientInfo.Name, tc.wantClient)
			}
		})
	}
}

func TestDiscoverResultWireShape(t *testing.T) {
	res := DiscoverResult{
		ResultType:        ResultTypeComplete,
		SupportedVersions: []string{"2026-07-28"},
		Capabilities:      Capabilities{Tools: &ToolsCapability{}},
		TTLMs:             0,
		CacheScope:        CacheScopePrivate,
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	// Required fields must be present even at zero values.
	for _, key := range []string{`"resultType":"complete"`, `"supportedVersions":["2026-07-28"]`, `"ttlMs":0`, `"cacheScope":"private"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("discover result missing %s in %s", key, data)
		}
	}
}

func TestStatelessResultFieldsStayOffLegacyWire(t *testing.T) {
	// Handshake-era results must serialize byte-identically to the
	// pre-dual-stack shape: empty stateless fields never hit the wire.
	data, err := json.Marshal(ToolsListResult{Tools: []Tool{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"tools":[]}` {
		t.Fatalf("legacy tools/list wire shape changed: %s", data)
	}
}

func TestRequestStateEnvelopeRoundTrip(t *testing.T) {
	// Origin bytes must survive byte-exact, including sentinel-shaped
	// and binary-ish state.
	states := []string{
		"AEAD-protected blob",
		"",
		"gridctl-mrtr-v1:not-actually-ours",
		`{"nested":"json"}`,
		"\x00\x01\xffbinary",
	}
	for _, state := range states {
		wrapped := wrapRequestState("github", state)
		server, got, ok := unwrapRequestState(wrapped)
		if !ok {
			t.Fatalf("unwrap failed for state %q", state)
		}
		if server != "github" {
			t.Errorf("server = %q, want github", server)
		}
		if got != state {
			t.Errorf("state round-trip mismatch: got %q, want %q", got, state)
		}
	}
}

func TestUnwrapRequestStateRejectsForeignValues(t *testing.T) {
	for _, v := range []string{"", "opaque-origin-state", "gridctl-mrtr-v1:!!!not-base64!!!", "gridctl-mrtr-v1:" + "e30="} {
		if _, _, ok := unwrapRequestState(v); ok {
			t.Errorf("unwrapRequestState(%q) must not succeed", v)
		}
	}
}
