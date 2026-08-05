package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// discoverAnswer builds a callFn that answers server/discover like a
// stateless-era server and fails the test on any other method.
func discoverAnswer(t *testing.T, versions []string) func(context.Context, string, any, any) error {
	t.Helper()
	return func(_ context.Context, method string, params any, result any) error {
		if method != "server/discover" {
			t.Fatalf("unexpected downstream call %q after modern verdict", method)
		}
		// The probe must carry the required modern _meta.
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if meta, modern := parseRequestMeta(raw); !modern || meta.ProtocolVersion != StatelessProtocolVersion {
			t.Fatalf("probe params lack modern _meta: %s", raw)
		}
		if r, ok := result.(*DiscoverResult); ok {
			r.ResultType = ResultTypeComplete
			r.SupportedVersions = versions
			r.Capabilities = Capabilities{Tools: &ToolsCapability{}}
			r.TTLMs = 3600000
			r.CacheScope = CacheScopePublic
			r.Meta = map[string]any{
				metaKeyServerInfo: map[string]any{"name": "modern-server", "version": "2.0"},
			}
		}
		return nil
	}
}

func TestInitialize_AdoptsStatelessServer(t *testing.T) {
	ft := &fakeTransport{callFn: discoverAnswer(t, []string{StatelessProtocolVersion})}
	r := newFakeRPCClient("test", ft)

	if err := r.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if r.Era() != EraStateless {
		t.Fatalf("era = %q, want stateless", r.Era())
	}
	if r.ProtocolVersion() != StatelessProtocolVersion {
		t.Errorf("protocol version = %q, want %s", r.ProtocolVersion(), StatelessProtocolVersion)
	}
	if !r.IsInitialized() {
		t.Error("client must be initialized after discover adoption")
	}
	if r.ServerInfo().Name != "modern-server" {
		t.Errorf("server info name = %q, want modern-server", r.ServerInfo().Name)
	}
}

func TestInitialize_FallsBackOnMethodNotFound(t *testing.T) {
	var calls []string
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			calls = append(calls, method)
			if method == "server/discover" {
				return &RPCError{Code: -32601, Message: "Unknown method"}
			}
			if r, ok := result.(*InitializeResult); ok {
				r.ProtocolVersion = MCPProtocolVersion
				r.ServerInfo = ServerInfo{Name: "legacy", Version: "1.0"}
			}
			return nil
		},
		sendFn: func(_ context.Context, _ string, _ any) error { return nil },
	}
	r := newFakeRPCClient("test", ft)
	if err := r.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if r.Era() != EraHandshake {
		t.Fatalf("era = %q, want handshake", r.Era())
	}
	if calls[0] != "server/discover" || calls[1] != "initialize" {
		t.Fatalf("call order = %v", calls)
	}
}

func TestInitialize_ProbeRejectsAuthAndServerErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"auth challenge", &AuthRequiredError{Status: 401}},
		{"probe 5xx", &HTTPStatusError{Status: 502, Body: "bad gateway"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTransport{
				callFn: func(_ context.Context, method string, _ any, _ any) error {
					if method != "server/discover" {
						t.Fatalf("must not fall back to %q on %s", method, tc.name)
					}
					return tc.err
				},
			}
			r := newFakeRPCClient("test", ft)
			if err := r.Initialize(context.Background()); err == nil {
				t.Fatal("expected probe rejection")
			}
		})
	}
}

func TestInitialize_UnsupportedVersionErrorFallsBackWhenHandshakeMutual(t *testing.T) {
	var calls []string
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			calls = append(calls, method)
			if method == "server/discover" {
				return &RPCError{
					Code:    ErrCodeUnsupportedProtocolVersion,
					Message: "Unsupported protocol version",
					Data:    json.RawMessage(`{"supported":["2025-11-25"],"requested":"2026-07-28"}`),
				}
			}
			if r, ok := result.(*InitializeResult); ok {
				r.ProtocolVersion = "2025-11-25"
			}
			return nil
		},
		sendFn: func(_ context.Context, _ string, _ any) error { return nil },
	}
	r := newFakeRPCClient("test", ft)
	if err := r.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if r.Era() != EraHandshake {
		t.Fatalf("era = %q, want handshake", r.Era())
	}
	if len(calls) < 2 || calls[1] != "initialize" {
		t.Fatalf("expected handshake fallback, calls = %v", calls)
	}
}

func TestInitialize_UnsupportedVersionErrorWithNoMutualVersionFails(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, _ any) error {
			if method != "server/discover" {
				t.Fatalf("must not fall back when no mutual version exists, called %q", method)
			}
			return &RPCError{
				Code:    ErrCodeUnsupportedProtocolVersion,
				Message: "Unsupported protocol version",
				Data:    json.RawMessage(`{"supported":["2099-01-01"],"requested":"2026-07-28"}`),
			}
		},
	}
	r := newFakeRPCClient("test", ft)
	err := r.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no mutually supported protocol version") {
		t.Fatalf("expected no-mutual-version error, got %v", err)
	}
}

func TestInitialize_JunkDiscoverResultFallsBack(t *testing.T) {
	// A lax legacy server answering unknown methods with an empty 200
	// must not be classified modern.
	tests := []struct {
		name   string
		mutate func(*DiscoverResult)
	}{
		{"empty result", func(r *DiscoverResult) {}},
		{"missing resultType", func(r *DiscoverResult) { r.SupportedVersions = []string{StatelessProtocolVersion} }},
		{"handshake-only versions", func(r *DiscoverResult) {
			r.ResultType = ResultTypeComplete
			r.SupportedVersions = []string{"2025-11-25"}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTransport{
				callFn: func(_ context.Context, method string, _ any, result any) error {
					if method == "server/discover" {
						if r, ok := result.(*DiscoverResult); ok {
							tc.mutate(r)
						}
						return nil
					}
					if r, ok := result.(*InitializeResult); ok {
						r.ProtocolVersion = MCPProtocolVersion
					}
					return nil
				},
				sendFn: func(_ context.Context, _ string, _ any) error { return nil },
			}
			r := newFakeRPCClient("test", ft)
			if err := r.Initialize(context.Background()); err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}
			if r.Era() != EraHandshake {
				t.Fatalf("era = %q, want handshake fallback", r.Era())
			}
		})
	}
}

func TestInitialize_GenerationPinHandshakeSkipsProbe(t *testing.T) {
	var calls []string
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			calls = append(calls, method)
			if r, ok := result.(*InitializeResult); ok {
				r.ProtocolVersion = MCPProtocolVersion
			}
			return nil
		},
		sendFn: func(_ context.Context, _ string, _ any) error { return nil },
	}
	r := newFakeRPCClient("test", ft)
	r.SetGenerationPin(GenerationHandshake)
	if err := r.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	for _, m := range calls {
		if m == "server/discover" {
			t.Fatal("handshake pin must skip the probe")
		}
	}
}

func TestInitialize_GenerationPinStatelessNeverFallsBack(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, _ any) error {
			if method != "server/discover" {
				t.Fatalf("stateless pin must not fall back, called %q", method)
			}
			return &RPCError{Code: -32601, Message: "Unknown method"}
		},
	}
	r := newFakeRPCClient("test", ft)
	r.SetGenerationPin(GenerationStateless)
	err := r.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pinned to stateless") {
		t.Fatalf("expected stateless-pin failure, got %v", err)
	}
}

func TestStatelessClientStampsMetaOnEveryCall(t *testing.T) {
	var sawToolsList bool
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, params any, result any) error {
			if method == "server/discover" {
				return discoverAnswer(t, []string{StatelessProtocolVersion})(context.Background(), method, params, result)
			}
			if method == "tools/list" {
				sawToolsList = true
			}
			return nil
		},
	}
	r := newFakeRPCClient("test", ft)
	if err := r.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The fake transport bypasses the concrete transports' stamping
	// sites, so exercise the helper contract directly: RefreshTools must
	// run without a handshake, and stampStatelessMeta must merge without
	// clobbering unrelated _meta keys.
	if err := r.RefreshTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !sawToolsList {
		t.Fatal("tools/list not sent")
	}
	stamped := stampStatelessMeta(context.Background(), json.RawMessage(`{"name":"x","_meta":{"traceparent":"00-a-b-01"}}`), StatelessProtocolVersion)
	meta, modern := parseRequestMeta(stamped)
	if !modern || meta.ProtocolVersion != StatelessProtocolVersion {
		t.Fatalf("stamped params not modern: %s", stamped)
	}
	if !strings.Contains(string(stamped), "traceparent") {
		t.Fatalf("stamping clobbered existing _meta: %s", stamped)
	}
}

func TestStampStatelessMetaPreservesSiblingBytes(t *testing.T) {
	// Sibling values must pass through byte-exact: a decode through
	// map[string]any would rewrite 2^53+1 as a float64 and corrupt it.
	params := json.RawMessage(`{"name":"t","arguments":{"big_id":9007199254740993,"exp":1e2,"s":"x"}}`)
	stamped := stampStatelessMeta(context.Background(), params, StatelessProtocolVersion)
	for _, literal := range []string{"9007199254740993", "1e2"} {
		if !strings.Contains(string(stamped), literal) {
			t.Errorf("stamping corrupted sibling value %s: %s", literal, stamped)
		}
	}
}

// versionRecordingTransport is a fakeTransport that also implements the
// protocolVersionSetter seam, recording every call.
type versionRecordingTransport struct {
	fakeTransport
	versions []string
}

func (v *versionRecordingTransport) setProtocolVersion(s string) {
	v.versions = append(v.versions, s)
}

func TestInitialize_ClearsStaleProtocolVersionBeforeProbe(t *testing.T) {
	// A redeployed server may have changed generation. The re-probe
	// must not carry the previously negotiated version in its
	// MCP-Protocol-Version header: it would contradict the probe's
	// _meta, which a strict modern server rejects with -32020 instead
	// of answering (issue #1086).
	vt := &versionRecordingTransport{fakeTransport: fakeTransport{
		callFn: discoverAnswer(t, []string{StatelessProtocolVersion}),
	}}
	r := &RPCClient{}
	initRPCClient(r, "test", vt)
	// Simulate a prior handshake-era resolution.
	r.SetProtocolVersion("2025-11-25")
	vt.setProtocolVersion("2025-11-25")
	vt.versions = nil

	if err := r.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if len(vt.versions) == 0 || vt.versions[0] != "" {
		t.Fatalf("transport version calls = %q, want a clearing \"\" before the probe", vt.versions)
	}
	if r.ProtocolVersion() != StatelessProtocolVersion {
		t.Errorf("protocol version = %q, want re-negotiated %q", r.ProtocolVersion(), StatelessProtocolVersion)
	}
}
