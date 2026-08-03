package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

// statelessBody builds a JSON-RPC request body carrying modern _meta.
func statelessBody(t *testing.T, id any, method string, params map[string]any) []byte {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		metaKeyProtocolVersion:    StatelessProtocolVersion,
		metaKeyClientInfo:         map[string]any{"name": "Example Client", "version": "1.0.0"},
		metaKeyClientCapabilities: map[string]any{},
	}
	m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != nil {
		m["id"] = id
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// statelessRequest builds a POST with the required request-metadata
// headers derived from the body.
func statelessRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	// httptest defaults Host to example.com, which Host validation rejects.
	r.Host = "localhost:8180"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("MCP-Protocol-Version", StatelessProtocolVersion)
	r.Header.Set(headerMcpMethod, req.Method)
	if name := mcpNameForRequest(req.Method, req.Params); name != "" {
		r.Header.Set(headerMcpName, encodeHeaderValue(name))
	}
	return r
}

func doStateless(t *testing.T, srv *StreamableHTTPServer, r *http.Request) (*httptest.ResponseRecorder, jsonrpc.Response) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	var resp jsonrpc.Response
	if w.Body.Len() > 0 {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decoding response (%d): %v", w.Code, err)
		}
	}
	return w, resp
}

func TestStateless_ServerDiscover(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, "server/discover", nil)))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Error("stateless path must not mint sessions")
	}
	var result DiscoverResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ResultType != ResultTypeComplete {
		t.Errorf("resultType = %q, want complete", result.ResultType)
	}
	found := false
	for _, v := range result.SupportedVersions {
		if v == StatelessProtocolVersion {
			found = true
		}
	}
	if !found {
		t.Errorf("supportedVersions %v missing %s", result.SupportedVersions, StatelessProtocolVersion)
	}
	if result.CacheScope != CacheScopePrivate || result.TTLMs != 0 {
		t.Errorf("empty fleet must aggregate to 0/private, got %d/%s", result.TTLMs, result.CacheScope)
	}
	if result.Capabilities.Tools == nil || result.Capabilities.Tools.ListChanged {
		t.Error("discover capabilities must declare tools without listChanged")
	}
}

func TestStateless_DiscoverWithoutMetaStillAnswers(t *testing.T) {
	// server/discover doubles as the backward-compatibility probe; a
	// probe without modern _meta must be answered, not 404ed into a
	// legacy verdict.
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "server/discover"})
	r := loopbackRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	w, resp := doStateless(t, srv, r)
	if w.Code != http.StatusOK || resp.Error != nil {
		t.Fatalf("probe without _meta must succeed, got %d %v", w.Code, resp.Error)
	}
}

func TestStateless_ToolsListNoSessionRequired(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, "tools/list", nil)))
	if w.Code != http.StatusOK || resp.Error != nil {
		t.Fatalf("tools/list: %d %v", w.Code, resp.Error)
	}
	var result ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ResultType != ResultTypeComplete {
		t.Errorf("resultType = %q, want complete", result.ResultType)
	}
	if result.TTLMs == nil || *result.TTLMs != 0 || result.CacheScope != CacheScopePrivate {
		t.Errorf("empty fleet cache meta must be 0/private, got %+v", result.StatelessResultFields)
	}
}

func TestStateless_UnsupportedVersionRejected(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{
		"io.modelcontextprotocol/protocolVersion":"2099-01-01",
		"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	r := loopbackRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	r.Header.Set("MCP-Protocol-Version", "2099-01-01")
	r.Header.Set(headerMcpMethod, "tools/list")
	w, resp := doStateless(t, srv, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if resp.Error == nil || resp.Error.Code != ErrCodeUnsupportedProtocolVersion {
		t.Fatalf("expected -32022, got %+v", resp.Error)
	}
	data, _ := json.Marshal(resp.Error.Data)
	if !strings.Contains(string(data), StatelessProtocolVersion) || !strings.Contains(string(data), `"requested":"2099-01-01"`) {
		t.Errorf("error data must list supported and requested versions: %s", data)
	}
}

func TestStateless_HeaderValidation(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"missing version header", func(r *http.Request) { r.Header.Del("MCP-Protocol-Version") }},
		{"version header mismatch", func(r *http.Request) { r.Header.Set("MCP-Protocol-Version", "2025-11-25") }},
		{"missing Mcp-Method", func(r *http.Request) { r.Header.Del(headerMcpMethod) }},
		{"Mcp-Method mismatch", func(r *http.Request) { r.Header.Set(headerMcpMethod, "tools/list") }},
		{"Mcp-Name mismatch", func(r *http.Request) { r.Header.Set(headerMcpName, "other_tool") }},
		{"missing Mcp-Name", func(r *http.Request) { r.Header.Del(headerMcpName) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := statelessRequest(t, statelessBody(t, 1, "tools/call", map[string]any{"name": "srv__echo", "arguments": map[string]any{}}))
			tc.mutate(r)
			w, resp := doStateless(t, srv, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if resp.Error == nil || resp.Error.Code != ErrCodeHeaderMismatch {
				t.Fatalf("expected -32020, got %+v", resp.Error)
			}
		})
	}
}

func TestStateless_EncodedMcpNameValidates(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	r := statelessRequest(t, statelessBody(t, 1, "tools/call", map[string]any{"name": "srv__echo", "arguments": map[string]any{}}))
	// Base64-sentinel encoding of the same name must validate.
	r.Header.Set(headerMcpName, "=?base64?c3J2X19lY2hv?=")
	w, resp := doStateless(t, srv, r)
	if w.Code == http.StatusBadRequest && resp.Error != nil && resp.Error.Code == ErrCodeHeaderMismatch {
		t.Fatalf("sentinel-encoded matching Mcp-Name must validate: %+v", resp.Error)
	}
}

func TestStateless_MissingClientCapabilitiesRejected(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{
		"io.modelcontextprotocol/protocolVersion":"` + StatelessProtocolVersion + `"}}}`)
	r := loopbackRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	r.Header.Set("MCP-Protocol-Version", StatelessProtocolVersion)
	r.Header.Set(headerMcpMethod, "tools/list")
	w, resp := doStateless(t, srv, r)
	if w.Code != http.StatusBadRequest || resp.Error == nil || resp.Error.Code != jsonrpc.InvalidRequest {
		t.Fatalf("expected 400 -32600 for missing clientCapabilities, got %d %+v", w.Code, resp.Error)
	}
}

func TestStateless_RemovedMethodsAre404(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	for _, method := range []string{"ping", "logging/setLevel", "initialize", "subscriptions/listen", "no/such/method"} {
		t.Run(method, func(t *testing.T) {
			w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, method, nil)))
			if w.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d", w.Code)
			}
			if resp.Error == nil || resp.Error.Code != jsonrpc.MethodNotFound {
				t.Fatalf("expected -32601, got %+v", resp.Error)
			}
		})
	}
}

func TestStateless_NotificationAccepted(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	w, _ := doStateless(t, srv, statelessRequest(t, statelessBody(t, nil, "notifications/whatever", nil)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("notification acknowledgment must have no body, got %s", w.Body.String())
	}
}

func TestStateless_GetAndDeleteAre405(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		r := loopbackRequest(method, "/mcp", nil)
		r.Header.Set("MCP-Protocol-Version", StatelessProtocolVersion)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s with stateless version: expected 405, got %d", method, w.Code)
		}
	}
}

func TestStateless_LegacySessionFlowUnchanged(t *testing.T) {
	// A handshake-era client on the same endpoint keeps its full flow.
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv)
	resp := streamablePost(t, srv, sessionID, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("legacy tools/list failed: %+v", resp.Error)
	}
	// Legacy results must not grow stateless fields.
	if strings.Contains(string(resp.Result), "resultType") {
		t.Errorf("legacy result carries stateless fields: %s", resp.Result)
	}
}

func TestStateless_MRTRForeignRequestStateRejected(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	params := map[string]any{
		"name":         "srv__echo",
		"arguments":    map[string]any{},
		"requestState": "not-a-gridctl-envelope",
	}
	w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, "tools/call", params)))
	if w.Code != http.StatusOK || resp.Error == nil || resp.Error.Code != jsonrpc.InvalidParams {
		t.Fatalf("foreign requestState must be rejected with -32602, got %d %+v", w.Code, resp.Error)
	}
}

// taskCapableFake builds a stateless-era downstream client declaring
// the tasks extension whose transport relays tasks methods verbatim.
func taskCapableFake(name string) *RPCClient {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, params any, result any) error {
			if raw, ok := result.(*json.RawMessage); ok {
				*raw = json.RawMessage(`{"resultType":"complete","task":{"taskId":"t1","status":"working","echoedMethod":"` + method + `"}}`)
			}
			return nil
		},
	}
	r := newFakeRPCClient(name, ft)
	r.SetEra(EraStateless)
	r.SetDownstreamCapabilities(Capabilities{
		Extensions: map[string]json.RawMessage{TasksExtensionID: json.RawMessage(`{}`)},
	})
	return r
}

func TestStateless_TasksProxyRelaysToSingleCapableServer(t *testing.T) {
	g := NewGateway()
	g.Router().AddClient(taskCapableFake("tasky"))
	srv := NewStreamableHTTPServer(g, nil)

	w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, "tasks/get", map[string]any{"taskId": "t1"})))
	if w.Code != http.StatusOK || resp.Error != nil {
		t.Fatalf("tasks/get: %d %+v", w.Code, resp.Error)
	}
	if !strings.Contains(string(resp.Result), `"echoedMethod":"tasks/get"`) {
		t.Fatalf("result not relayed verbatim: %s", resp.Result)
	}
	// The single capable server also surfaces the extension in discover.
	_, resp = doStateless(t, srv, statelessRequest(t, statelessBody(t, 2, "server/discover", nil)))
	if !strings.Contains(string(resp.Result), TasksExtensionID) {
		t.Errorf("discover must advertise the tasks extension: %s", resp.Result)
	}
}

func TestStateless_TasksAmbiguousWithTwoCapableServers(t *testing.T) {
	g := NewGateway()
	g.Router().AddClient(taskCapableFake("tasky1"))
	g.Router().AddClient(taskCapableFake("tasky2"))
	srv := NewStreamableHTTPServer(g, nil)

	w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, "tasks/get", map[string]any{"taskId": "t1"})))
	if w.Code != http.StatusOK || resp.Error == nil || !strings.Contains(resp.Error.Message, "multiple servers") {
		t.Fatalf("expected ambiguity error, got %d %+v", w.Code, resp.Error)
	}
	// Ambiguity also suppresses the discover advertisement.
	_, resp = doStateless(t, srv, statelessRequest(t, statelessBody(t, 2, "server/discover", nil)))
	if strings.Contains(string(resp.Result), TasksExtensionID) {
		t.Errorf("ambiguous tasks fleet must not advertise the extension: %s", resp.Result)
	}
}

func TestSessionEntries(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	sessionID := initializeStreamable(t, srv)

	entries := srv.SessionEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != sessionID {
		t.Errorf("entry ID = %q, want %q", entries[0].ID, sessionID)
	}
	if entries[0].Generation != string(EraHandshake) {
		t.Errorf("generation = %q, want handshake", entries[0].Generation)
	}
	if entries[0].ProtocolVersion == "" {
		t.Error("entry must carry the negotiated protocol version")
	}
}

func TestStateless_TasksWithoutCapableServer(t *testing.T) {
	srv := NewStreamableHTTPServer(NewGateway(), nil)
	w, resp := doStateless(t, srv, statelessRequest(t, statelessBody(t, 1, "tasks/get", map[string]any{"taskId": "t1"})))
	if w.Code != http.StatusOK || resp.Error == nil {
		t.Fatalf("tasks/get with no capable server must error, got %d %+v", w.Code, resp.Error)
	}
	if !strings.Contains(resp.Error.Message, TasksExtensionID) {
		t.Errorf("error should name the extension: %s", resp.Error.Message)
	}
}
