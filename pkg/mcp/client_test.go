package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

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
