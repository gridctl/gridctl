package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func TestClient_SendsNegotiatedProtocolVersionHeader(t *testing.T) {
	var mu sync.Mutex
	headersByMethod := make(map[string]string)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		headersByMethod[req.Method] = r.Header.Get("MCP-Protocol-Version")
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			result := InitializeResult{
				ProtocolVersion: "2025-06-18",
				ServerInfo:      ServerInfo{Name: "test", Version: "1.0"},
			}
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, result))
		case "tools/list":
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, ToolsListResult{}))
		default:
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
		}
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.RefreshTools(context.Background()); err != nil {
		t.Fatalf("RefreshTools: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if v := headersByMethod["initialize"]; v != "" {
		t.Errorf("initialize must not carry a negotiated version header, got %q", v)
	}
	if v := headersByMethod["tools/list"]; v != "2025-06-18" {
		t.Errorf("expected post-initialize requests to carry the negotiated version, got %q", v)
	}
}

func TestClient_ParseSSEResponse_Notifications(t *testing.T) {
	// Simulate an SSE stream with a notification followed by a result
	sseBody := `event: message
data: {"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","data":{"msg":"some log"}}}

event: message
data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"success"}]}}
`

	client := &Client{}
	resp, err := client.parseSSEResponse(strings.NewReader(sseBody))
	if err != nil {
		t.Fatalf("parseSSEResponse failed: %v", err)
	}

	if resp.ID == nil {
		t.Fatal("expected response to have ID")
	}

	// Verify it picked the result, not the notification
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Check content
	// {"content":[{"type":"text","text":"success"}]}
	content, ok := result["content"].([]any)
	if !ok {
		t.Fatalf("expected content array")
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 content item")
	}
}

func TestClient_ParseSSEResponse_OnlyNotification(t *testing.T) {
	// Simulate an SSE stream with only a notification
	sseBody := `event: message
data: {"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}
`

	client := &Client{}
	_, err := client.parseSSEResponse(strings.NewReader(sseBody))
	if err == nil {
		t.Fatal("expected error when no response with ID is found")
	}
	if !strings.Contains(err.Error(), "no response with ID") {
		t.Errorf("expected error message 'no response with ID', got: %v", err)
	}
}

func TestClient_ParseSSEResponse_MalformedData(t *testing.T) {
	// Simulate malformed data lines
	sseBody := `event: message
data: not-json

event: message
data: {"jsonrpc":"2.0","id":1,"result":{}}
`

	client := &Client{}
	resp, err := client.parseSSEResponse(strings.NewReader(sseBody))
	if err != nil {
		t.Fatalf("parseSSEResponse failed with malformed data skipped: %v", err)
	}
	if resp.ID == nil {
		t.Fatal("expected valid response despite previous malformed line")
	}
}

func TestClientPing_StatelessUsesServerDiscover(t *testing.T) {
	// The stateless generation removed ping; the health check must
	// exercise server/discover (the stdio/process precedent) so a
	// generation flip surfaces through the health channel.
	var gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("stateless health check must POST, got %s", r.Method)
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding health request: %v", err)
		}
		gotMethod = req.Method
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","supportedVersions":["2026-07-28"],"capabilities":{},"ttlMs":0,"cacheScope":"private"}}`, req.ID)
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraStateless)
	c.SetProtocolVersion(StatelessProtocolVersion)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if gotMethod != "server/discover" {
		t.Errorf("health check method = %q, want server/discover", gotMethod)
	}
}

func TestClientPing_StatelessEraFlipFails(t *testing.T) {
	// A server that flipped back to the handshake generation answers
	// server/discover with -32601; the health check must fail into the
	// health channel rather than reporting a reachable-but-wrong peer
	// as healthy.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"Method not found"}}`, req.ID)
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraStateless)
	c.SetProtocolVersion(StatelessProtocolVersion)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must fail when the server no longer speaks the stateless generation")
	}
}

func TestClientPing_StatelessRejectsJunkDiscover(t *testing.T) {
	// A bare RPC success is not health: the discover result must still
	// look like a stateless-era peer (resultType complete, a mutually
	// supported version), or a lax peer answering junk reads healthy
	// while every real call fails.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{}}`, req.ID)
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraStateless)
	c.SetProtocolVersion(StatelessProtocolVersion)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must fail when the discover result is not a stateless-era answer")
	}
}

func TestClient_ReprobeAfterGenerationFlip(t *testing.T) {
	// Wire-level regression for the #1086 re-probe defect: after a
	// handshake-era negotiation, a server redeployed as stateless-only
	// rejects any request whose MCP-Protocol-Version header contradicts
	// its _meta with -32020. Re-Initialize must clear the stale
	// negotiated version so the probe's header matches its stamped
	// _meta and the flip re-negotiates instead of wedging.
	var mu sync.Mutex
	modern := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		isModern := modern
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		if !isModern {
			switch req.Method {
			case "initialize":
				_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, InitializeResult{
					ProtocolVersion: "2025-11-25",
					ServerInfo:      ServerInfo{Name: "flip", Version: "1.0"},
				}))
			default:
				_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
			}
			return
		}

		// Strict stateless-only server: the header must match _meta.
		meta, _ := parseRequestMeta(req.Params)
		if hv := r.Header.Get("MCP-Protocol-Version"); hv != meta.ProtocolVersion {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, ErrCodeHeaderMismatch,
				fmt.Sprintf("header %q does not match _meta %q", hv, meta.ProtocolVersion)))
			return
		}
		if req.Method != "server/discover" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, "Method not found"))
			return
		}
		_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, DiscoverResult{
			ResultType:        ResultTypeComplete,
			SupportedVersions: []string{StatelessProtocolVersion},
			Capabilities:      Capabilities{Tools: &ToolsCapability{}},
			CacheScope:        CacheScopePrivate,
		}))
	}))
	defer ts.Close()

	c := NewClient("flip", ts.URL)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("legacy Initialize: %v", err)
	}
	if c.Era() != EraHandshake {
		t.Fatalf("era = %q, want handshake before the flip", c.Era())
	}

	mu.Lock()
	modern = true
	mu.Unlock()

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("re-Initialize after generation flip: %v", err)
	}
	if c.Era() != EraStateless {
		t.Fatalf("era = %q, want stateless after the flip", c.Era())
	}
}

func TestClientPing_HandshakeSendsProtocolPing(t *testing.T) {
	// The handshake-era health check must exercise the protocol (a
	// JSON-RPC ping, the stdio/process precedent), not bare reachability:
	// a bare GET cannot see a generation flip (#1088).
	var mu sync.Mutex
	var gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("handshake health check must POST, got %s", r.Method)
		}
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		gotMethod = req.Method
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraHandshake)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotMethod != "ping" {
		t.Errorf("health check method = %q, want ping", gotMethod)
	}
}

func TestClientPing_HandshakeEraFlipFails(t *testing.T) {
	// #1088 regression: a server that negotiated the handshake era and
	// redeployed as stateless-only rejects ping (removed method) but
	// answers server/discover as a modern peer. The health check must
	// fail into the health channel; the old bare-GET check read this
	// server as healthy forever while every tool call failed.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method != "server/discover" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, "Method not found"))
			return
		}
		_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, DiscoverResult{
			ResultType:        ResultTypeComplete,
			SupportedVersions: []string{StatelessProtocolVersion},
			CacheScope:        CacheScopePrivate,
		}))
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraHandshake)
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() must fail when the server no longer speaks the handshake generation")
	}
	if !strings.Contains(err.Error(), "generation flip") {
		t.Errorf("flip error must name the generation flip, got %v", err)
	}
}

func TestClientPing_HandshakeLaxLegacyStaysHealthy(t *testing.T) {
	// A legacy server that rejects ping with -32601 and answers junk to
	// server/discover is reachable and fine; #1088's flip detection must
	// not turn the long-standing reachability tolerance into false
	// unhealthy.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, "Method not found"))
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraHandshake)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("lax legacy server must stay healthy, got %v", err)
	}
}

func TestClientPing_HandshakePinSkipsFlipConfirm(t *testing.T) {
	// Under a handshake generation pin (the SSE shape) Initialize can
	// never adopt the stateless era, so a flip verdict from Ping would
	// strand the server unhealthy with no recovery path (#1088). The
	// pin must skip flip detection entirely: no server/discover probe.
	var mu sync.Mutex
	discoverCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "server/discover" {
			mu.Lock()
			discoverCalls++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, "Method not found"))
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetGenerationPin(GenerationHandshake)
	c.SetEra(EraHandshake)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("pinned client must stay healthy on -32601, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if discoverCalls != 0 {
		t.Errorf("pinned client must never probe server/discover, got %d probes", discoverCalls)
	}
}

func TestClientPing_UnresolvedEraToleratesModernServer(t *testing.T) {
	// Registration readiness probes Ping before Initialize resolves the
	// era. A stateless-only server rejecting ping is up and adoptable,
	// so an unresolved, never-initialized era must not run #1088's flip
	// detection: failing health here deadlocks registration of every
	// modern server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method != "server/discover" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, "Method not found"))
			return
		}
		_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, DiscoverResult{
			ResultType:        ResultTypeComplete,
			SupportedVersions: []string{StatelessProtocolVersion},
			CacheScope:        CacheScopePrivate,
		}))
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("pre-Initialize Ping against a modern server must pass, got %v", err)
	}
}

func TestClientPing_UnresolvedEraAfterFailedReconnectFails(t *testing.T) {
	// The wedge latch for #1088's recovery path: a client that once
	// negotiated but whose Reconnect failed mid-way sits at era "" with
	// initialized still true. Tolerating that state would re-create the
	// forever-healthy bug through the fix itself (the reachable server
	// answers, flip detection is skipped, and the gateway resets the
	// reconnect backoff), so Ping must fail until Reconnect converges.
	c := NewClient("test", "http://127.0.0.1:0")
	c.SetInitialized(ServerInfo{Name: "wedged", Version: "1.0"})
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must fail on an initialized client with an unresolved era")
	}
}

// failingHeaderSource models a broker whose token machinery errors before
// any HTTP exchange (token endpoint outage, malformed token response).
type failingHeaderSource struct{ err error }

func (f failingHeaderSource) AuthHeader(context.Context) (string, string, error) {
	return "", "", f.err
}

func TestClientPing_AuthSourceErrorFails(t *testing.T) {
	// A header-source failure happens before any request reaches the
	// server, so it must not read as a healthy server: every tool call
	// fails on the same error, and the old bare-GET Ping surfaced it.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request must reach the server when the auth source fails")
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetEra(EraHandshake)
	c.SetHeaderSource(failingHeaderSource{err: errors.New("token refresh failed: HTTP 502")})
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() must fail when the auth source errors")
	}
	var srcErr *AuthSourceError
	if !errors.As(err, &srcErr) {
		t.Fatalf("expected AuthSourceError, got %T: %v", err, err)
	}
}

func TestClient_ReconnectPartialFailureStaysUnhealthy(t *testing.T) {
	// If Initialize succeeds but the tool refresh fails, Reconnect must
	// leave the era unresolved: a half-reconnected client reading
	// healthy would serve stale pre-flip tools and skip the gateway's
	// post-reconnect pin verification forever (#1088).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "server/discover" {
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, DiscoverResult{
				ResultType:        ResultTypeComplete,
				SupportedVersions: []string{StatelessProtocolVersion},
				CacheScope:        CacheScopePrivate,
			}))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	if err := c.Reconnect(context.Background()); err == nil {
		t.Fatal("Reconnect must fail when the tool refresh fails")
	}
	if era := c.Era(); era != "" {
		t.Fatalf("era = %q, want unresolved after a partial Reconnect", era)
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must keep failing until a full Reconnect converges")
	}
}

func TestClientPing_HandshakeStalledBodyFails(t *testing.T) {
	// A server that returns headers and then stalls the body times out
	// after the transport dial succeeded, so the failure surfaces as a
	// bare context error rather than a url.Error. That is
	// unreachability, not body-junk tolerance.
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release
	}))
	defer func() {
		close(release)
		ts.Close()
	}()

	c := NewClient("test", ts.URL)
	c.SetEra(EraHandshake)
	c.SetPingTimeout(100 * time.Millisecond)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must fail when the response body stalls past the deadline")
	}
}

func TestClientPing_HandshakeUnreachableFails(t *testing.T) {
	// Transport-level unreachability must keep failing health under the
	// protocol-level handshake check (#1088), exactly as the bare GET did.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := ts.URL
	ts.Close()

	c := NewClient("test", endpoint)
	c.SetEra(EraHandshake)
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping() must fail when the server is unreachable")
	}
}

// newGenerationFlipServer returns a test server that starts as a lax
// legacy handshake-era peer (minting a session at initialize) and, after
// flip() is called, behaves as a strict stateless-only peer, modeling a
// redeploy across protocol generations (#1088).
func newGenerationFlipServer(t *testing.T) (ts *httptest.Server, flip func()) {
	t.Helper()
	var mu sync.Mutex
	modern := false

	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		isModern := modern
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		if !isModern {
			switch req.Method {
			case "initialize":
				w.Header().Set("Mcp-Session-Id", "legacy-session")
				_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, InitializeResult{
					ProtocolVersion: "2025-11-25",
					ServerInfo:      ServerInfo{Name: "flip", Version: "1.0"},
				}))
			case "tools/list":
				_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, ToolsListResult{Tools: []Tool{{Name: "legacy-tool"}}}))
			default:
				_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
			}
			return
		}

		// Strict stateless-only server: the header must match _meta.
		meta, _ := parseRequestMeta(req.Params)
		if hv := r.Header.Get("MCP-Protocol-Version"); hv != meta.ProtocolVersion {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, ErrCodeHeaderMismatch,
				fmt.Sprintf("header %q does not match _meta %q", hv, meta.ProtocolVersion)))
			return
		}
		switch req.Method {
		case "server/discover":
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, DiscoverResult{
				ResultType:        ResultTypeComplete,
				SupportedVersions: []string{StatelessProtocolVersion},
				Capabilities:      Capabilities{Tools: &ToolsCapability{}},
				CacheScope:        CacheScopePrivate,
			}))
		case "tools/list":
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, ToolsListResult{Tools: []Tool{{Name: "modern-tool"}}}))
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(req.ID, jsonrpc.MethodNotFound, "Method not found"))
		}
	}))

	return ts, func() {
		mu.Lock()
		modern = true
		mu.Unlock()
	}
}

func TestClient_Reconnect(t *testing.T) {
	// #1088: Reconnect must re-resolve the era on the live client (the
	// stdio/process precedent), refresh the tool cache before returning,
	// and clear the stale handshake-era session ID.
	ts, flip := newGenerationFlipServer(t)
	defer ts.Close()

	c := NewClient("flip", ts.URL)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("legacy Initialize: %v", err)
	}
	if err := c.RefreshTools(context.Background()); err != nil {
		t.Fatalf("legacy RefreshTools: %v", err)
	}
	if c.Era() != EraHandshake {
		t.Fatalf("era = %q, want handshake before the flip", c.Era())
	}
	c.mu.RLock()
	sid := c.sessionID
	c.mu.RUnlock()
	if sid != "legacy-session" {
		t.Fatalf("sessionID = %q, want the minted legacy session before the flip", sid)
	}

	flip()

	if err := c.Reconnect(context.Background()); err != nil {
		t.Fatalf("Reconnect after generation flip: %v", err)
	}
	if c.Era() != EraStateless {
		t.Fatalf("era = %q, want stateless after Reconnect", c.Era())
	}
	tools := c.Tools()
	if len(tools) != 1 || tools[0].Name != "modern-tool" {
		t.Errorf("Tools() = %v, want the post-flip tool list", tools)
	}
	c.mu.RLock()
	sid = c.sessionID
	c.mu.RUnlock()
	if sid != "" {
		t.Errorf("sessionID = %q, want cleared after Reconnect", sid)
	}
}
