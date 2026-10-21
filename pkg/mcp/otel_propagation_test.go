package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/mock/gomock"

	"github.com/gridctl/gridctl/pkg/a2aclient"
	"github.com/gridctl/gridctl/pkg/tracing"
)

func TestSensitiveExecution_ToolNameIsCategory(t *testing.T) {
	buf := tracing.NewBuffer(10, time.Hour)
	restore := installTracer(t, buf)
	defer restore()

	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "agent", []Tool{{Name: "create_issue"}})
	client.EXPECT().CallTool(gomock.Any(), "create_issue", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
	)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.serverMeta["agent"] = MCPServerConfig{sensitiveCalls: true}
	var seen []DispatchObservation
	g.SetDispatchObserver(dispatchSpy{fn: func(obs DispatchObservation) { seen = append(seen, obs) }})

	if _, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent__create_issue"}); err != nil {
		t.Fatal(err)
	}
	tr := buf.GetRecent(1)[0]
	for _, sp := range tr.Spans {
		for key, value := range sp.Attrs {
			if strings.Contains(value, "create_issue") {
				t.Errorf("span %s attr %s = %q contains the real tool name", sp.Name, key, value)
			}
		}
		if sp.ParentID == "" || strings.HasPrefix(sp.Name, "tools/call") {
			if sp.Attrs[string(tracing.AttrGenAIToolName)] != "skill" {
				t.Errorf("span %s gen_ai.tool.name = %q, want skill", sp.Name, sp.Attrs[string(tracing.AttrGenAIToolName)])
			}
			if sp.Attrs[string(tracing.AttrLegacyToolName)] != "skill" || sp.Attrs[string(tracing.AttrLegacyBareToolName)] != "skill" {
				t.Errorf("legacy tool aliases = %v", sp.Attrs)
			}
		}
	}
	if len(seen) != 1 {
		t.Fatalf("observations = %d, want 1", len(seen))
	}
	if seen[0].ToolName != "skill" || strings.Contains(seen[0].ServerName, "create_issue") {
		t.Fatalf("observation tool = %q server = %q", seen[0].ToolName, seen[0].ServerName)
	}
	blob, _ := json.Marshal(seen[0])
	if strings.Contains(string(blob), "create_issue") {
		t.Fatalf("observation leaked the tool name: %s", blob)
	}
}

func TestResolveTransportAttrs(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MCPServerConfig
		hasMeta bool
		gridctl string
		network string
		addr    string
		port    int
	}{
		{"stdio", MCPServerConfig{Transport: TransportStdio}, true, tracing.TransportStdio, tracing.NetworkPipe, "", 0},
		{"process", MCPServerConfig{LocalProcess: true}, true, tracing.TransportProcess, tracing.NetworkPipe, "", 0},
		{"http", MCPServerConfig{Transport: TransportHTTP, Endpoint: "http://example.test:8080/mcp"}, true, tracing.TransportHTTP, tracing.NetworkTCP, "example.test", 8080},
		{"sse", MCPServerConfig{Transport: TransportSSE, Endpoint: "http://example.test/sse"}, true, tracing.TransportSSE, tracing.NetworkTCP, "example.test", 0},
		{"openapi", MCPServerConfig{OpenAPI: true, OpenAPIConfig: &OpenAPIClientConfig{BaseURL: "https://api.example.test/v1"}}, true, tracing.TransportOpenAPI, tracing.NetworkTCP, "api.example.test", 0},
		{"a2a", MCPServerConfig{A2A: true, A2AConfig: &A2AClientConfig{Endpoint: "https://agent.example.test/rpc"}}, true, tracing.TransportA2A, tracing.NetworkTCP, "agent.example.test", 0},
		{"ssh", MCPServerConfig{SSH: true, SSHHost: "bastion.internal", SSHPort: 2222}, true, tracing.TransportSSH, tracing.NetworkTCP, "bastion.internal", 2222},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTransportAttrs(tt.cfg, tt.hasMeta)
			if got.Gridctl != tt.gridctl || got.Network != tt.network || got.Address != tt.addr || got.Port != tt.port {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestRootSpan_NoSessionIDAndUsageAliasOnly(t *testing.T) {
	buf := tracing.NewBuffer(10, time.Hour)
	restore := installTracer(t, buf)
	defer restore()
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "github", []Tool{{Name: "create_issue"}})
	client.EXPECT().CallTool(gomock.Any(), "create_issue", gomock.Any()).Return(&ToolCallResult{
		Content: []Content{NewTextContent("ok")},
		Usage:   &CallUsage{CacheReadTokens: 3, CacheCreationTokens: 4},
	}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetToolCallObserver(usageObserver{})
	if _, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "github__create_issue"}); err != nil {
		t.Fatal(err)
	}
	tr := buf.GetRecent(1)[0]
	for _, sp := range tr.Spans {
		if _, ok := sp.Attrs["mcp.session.id"]; ok {
			t.Fatalf("span %s carries mcp.session.id", sp.Name)
		}
		for key := range sp.Attrs {
			if strings.HasPrefix(key, "gen_ai.usage.") {
				found := false
				for _, alias := range []string{
					string(tracing.AttrLegacyUsageInput),
					string(tracing.AttrLegacyUsageOutput),
					string(tracing.AttrLegacyUsageCacheRead),
					string(tracing.AttrLegacyUsageCacheCreation),
				} {
					if key == alias {
						found = true
					}
				}
				if !found {
					t.Fatalf("unexpected gen_ai.usage key %s", key)
				}
			}
		}
	}
}

func TestParentContextFromRequest(t *testing.T) {
	restore := installPropagator(t)
	defer restore()
	metaParent := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	headerParent := "00-11111111111111111111111111111111-2222222222222222-01"
	params := json.RawMessage(`{"name":"x","_meta":{"traceparent":"` + metaParent + `","n":9007199254740993}}`)
	headers := http.Header{}
	headers.Set("traceparent", headerParent)

	ctx := parentContextFromRequest(context.Background(), params, headers)
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || sc.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("parent trace = %s, want meta trace", sc.TraceID())
	}
	links := traceLinksFromContext(ctx)
	if len(links) != 1 || links[0].SpanContext.TraceID().String() != "11111111111111111111111111111111" {
		t.Fatalf("links = %+v", links)
	}
	// Extraction must not re-encode the big integer.
	if !strings.Contains(string(params), "9007199254740993") {
		t.Fatal("extraction rewrote params")
	}

	headerOnly := parentContextFromRequest(context.Background(), json.RawMessage(`{"name":"x"}`), headers)
	if got := trace.SpanContextFromContext(headerOnly).TraceID().String(); got != "11111111111111111111111111111111" {
		t.Fatalf("header-only parent = %s", got)
	}
}

func TestInjectMeta_BaggageCapAndRoundTrip(t *testing.T) {
	restore := installPropagator(t)
	defer restore()
	ctx, span := otel.Tracer("test").Start(context.Background(), "call")
	defer span.End()
	member, err := baggage.NewMember("tenant", "acme")
	if err != nil {
		t.Fatal(err)
	}
	state, err := baggage.NewMember("state", "opaque")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	_ = state
	ctx = baggage.ContextWithBaggage(ctx, bag)
	// tracestate is injected by the propagator when the span context has it.
	// The traceparent round trip is the span we just started.
	out := injectMetaTraceparent(ctx, json.RawMessage(`{"name":"echo"}`))
	var obj struct {
		Meta map[string]string `json:"_meta"`
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if obj.Meta["traceparent"] == "" || !strings.Contains(obj.Meta["baggage"], "tenant=acme") {
		t.Fatalf("meta = %v", obj.Meta)
	}

	// The W3C library rejects a member this large, so the cap is exercised on
	// a pre-existing _meta value the propagator does not replace.
	bareCtx, bareSpan := otel.Tracer("test").Start(context.Background(), "bare")
	defer bareSpan.End()
	capped := injectMetaTraceparent(bareCtx, json.RawMessage(`{"name":"echo","_meta":{"baggage":"`+strings.Repeat("b", tracing.BaggageMaxBytes+1)+`"}}`))
	var cappedObj struct {
		Meta map[string]string `json:"_meta"`
	}
	if err := json.Unmarshal(capped, &cappedObj); err != nil {
		t.Fatal(err)
	}
	if _, ok := cappedObj.Meta["baggage"]; ok {
		t.Fatalf("oversized baggage was forwarded: len %d", len(cappedObj.Meta["baggage"]))
	}
}

func TestClientCall_StatelessMetaComposesTraceContext(t *testing.T) {
	restore := installPropagator(t)
	defer restore()
	var body []byte
	var header http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	defer ts.Close()

	ctx, span := otel.Tracer("test").Start(context.Background(), "upstream")
	defer span.End()
	member, err := baggage.NewMember("tenant", "acme")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx = baggage.ContextWithBaggage(ctx, bag)

	c := NewClient("echo", ts.URL)
	c.SetEra(EraStateless)
	c.SetProtocolVersion(StatelessProtocolVersion)
	var result ToolCallResult
	if err := c.call(ctx, "tools/call", ToolCallParams{Name: "echo"}, &result); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Meta   json.RawMessage `json:"_meta"`
		Params struct {
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if len(envelope.Meta) != 0 {
		t.Fatalf("envelope root has _meta: %s", body)
	}
	meta := envelope.Params.Meta
	for _, key := range []string{"traceparent", "baggage", metaKeyProtocolVersion} {
		if meta[key] == nil || meta[key] == "" {
			t.Fatalf("params._meta missing %s: %s", key, body)
		}
	}
	if header.Get("traceparent") == "" || !strings.Contains(header.Get("baggage"), "tenant=acme") {
		t.Fatalf("headers traceparent=%q baggage=%q", header.Get("traceparent"), header.Get("baggage"))
	}
}

func TestOpenAPIAndA2A_InjectHeaders(t *testing.T) {
	restore := installPropagator(t)
	defer restore()
	ctx, span := otel.Tracer("test").Start(context.Background(), "down")
	defer span.End()
	member, err := baggage.NewMember("tenant", "acme")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx = baggage.ContextWithBaggage(ctx, bag)

	var got http.Header
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()
	client, err := NewOpenAPIClient("api", &OpenAPIClientConfig{Spec: api.URL + "/openapi.json", BaseURL: api.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = client.executeOperation(ctx, &OpenAPIOperation{Method: "POST", Path: "/echo"}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("traceparent") == "" || !strings.Contains(got.Get("baggage"), "tenant=acme") {
		t.Fatalf("openapi headers traceparent=%q baggage=%q", got.Get("traceparent"), got.Get("baggage"))
	}

	var a2aHeader http.Header
	a2aSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a2aHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer a2aSrv.Close()
	ac, err := a2aclient.New(a2aclient.Options{Card: a2aSrv.URL + "/card", Endpoint: a2aSrv.URL + "/rpc"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ac.RPC(ctx, a2aSrv.URL+"/rpc", "1.0", "", []byte(`{"jsonrpc":"2.0","id":1,"method":"tasks/get"}`)); err != nil {
		t.Fatal(err)
	}
	if a2aHeader.Get("traceparent") == "" || !strings.Contains(a2aHeader.Get("baggage"), "tenant=acme") {
		t.Fatalf("a2a headers traceparent=%q baggage=%q", a2aHeader.Get("traceparent"), a2aHeader.Get("baggage"))
	}
}

func TestHeaderBaggageCap(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://example.test", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	carrier := tracing.NewCappedCarrier(propagation.HeaderCarrier(req.Header))
	carrier.Set("baggage", strings.Repeat("a", tracing.BaggageMaxBytes+1))
	if req.Header.Get("baggage") != "" {
		t.Fatalf("oversized header baggage forwarded, len %d", len(req.Header.Get("baggage")))
	}
	if req.Header.Get("traceparent") == "" {
		t.Fatal("traceparent should still be present")
	}
	carrier.Set("baggage", "tenant=acme")
	if req.Header.Get("baggage") != "tenant=acme" {
		t.Fatalf("small baggage = %q", req.Header.Get("baggage"))
	}
}

type dispatchSpy struct{ fn func(DispatchObservation) }

func (d dispatchSpy) ObserveDispatch(_ context.Context, obs DispatchObservation) {
	if d.fn != nil {
		d.fn(obs)
	}
}

type usageObserver struct{}

func (usageObserver) ObserveToolCall(string, int, map[string]any, *ToolCallResult) {}
func (usageObserver) ObserveToolCallWithClient(context.Context, ToolCallObservation) ToolCallSummary {
	return ToolCallSummary{InputTokens: 2, OutputTokens: 3, CacheReadTokens: 3, CacheCreationTokens: 4}
}

func installTracer(t *testing.T, buf *tracing.Buffer) func() {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buf)))
	old := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(oldProp)
	}
}

func TestStateless_UpstreamTraceContextReachesDownstream(t *testing.T) {
	rec := &spanRecorder{}
	restore := installRecordingTracer(t, rec)
	defer restore()

	const (
		metaParent  = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
		wantParent  = "b7ad6b7169203331"
		wantTrace   = "0af7651916cd43dd8448eb211c80319c"
		wantState   = "vendor=1"
		wantBaggage = "tenant=acme"
	)
	var (
		body   []byte
		header http.Header
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`))
	}))
	defer ts.Close()

	downstream := NewClient("echo", ts.URL)
	downstream.allTools = []Tool{{Name: "ping"}}
	downstream.initialized = true
	downstream.SetEra(EraStateless)
	downstream.SetProtocolVersion(StatelessProtocolVersion)

	g := NewGateway()
	g.Router().AddClient(downstream)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "echo", Transport: TransportHTTP, Endpoint: ts.URL})

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo__ping",
			"arguments": map[string]any{},
			"_meta": map[string]any{
				metaKeyProtocolVersion:    StatelessProtocolVersion,
				metaKeyClientInfo:         map[string]any{"name": "Example Client", "version": "1.0.0"},
				metaKeyClientCapabilities: map[string]any{},
				"traceparent":             metaParent,
				"tracestate":              wantState,
				"baggage":                 wantBaggage,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewStreamableHTTPServer(g, nil)
	w, resp := doStateless(t, srv, statelessRequest(t, raw))
	if w.Code != http.StatusOK || resp.Error != nil {
		t.Fatalf("status %d error %v body %s", w.Code, resp.Error, w.Body.String())
	}

	var got struct {
		Meta   json.RawMessage `json:"_meta"`
		Params struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("downstream body %s: %v", body, err)
	}
	if len(got.Meta) != 0 {
		t.Fatalf("envelope root has _meta: %s", body)
	}
	if metaString(t, got.Params.Meta, "tracestate") != wantState {
		t.Fatalf("params._meta tracestate = %q, body %s", metaString(t, got.Params.Meta, "tracestate"), body)
	}
	if !strings.Contains(metaString(t, got.Params.Meta, "baggage"), wantBaggage) {
		t.Fatalf("params._meta baggage = %q", metaString(t, got.Params.Meta, "baggage"))
	}
	if header.Get("tracestate") != wantState {
		t.Fatalf("header tracestate = %q", header.Get("tracestate"))
	}
	if !strings.Contains(header.Get("baggage"), wantBaggage) {
		t.Fatalf("header baggage = %q", header.Get("baggage"))
	}

	if !rec.hasRemoteParent(wantTrace, wantParent) {
		t.Fatalf("no root span parented on %s in %+v", wantParent, rec.snapshot())
	}
}

func TestHandshake_MetaTraceparentParentsRoot(t *testing.T) {
	rec := &spanRecorder{}
	restore := installRecordingTracer(t, rec)
	defer restore()

	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	srv := NewStreamableHTTPServer(g, nil)
	sessionID := initializeStreamable(t, srv)

	const (
		metaParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
		wantTrace  = "0af7651916cd43dd8448eb211c80319c"
		wantParent = "b7ad6b7169203331"
	)
	params, err := json.Marshal(map[string]any{
		"name":      "server1__echo",
		"arguments": map[string]any{},
		"_meta":     map[string]any{"traceparent": metaParent},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  json.RawMessage(params),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := loopbackRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Mcp-Session-Id", sessionID)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}

	if !rec.hasRemoteParent(wantTrace, wantParent) {
		t.Fatalf("handshake root was not parented on %s: %+v", wantParent, rec.snapshot())
	}
}

func TestCodeMode_HeaderLinkStaysOnOuterRoot(t *testing.T) {
	exp := &linkExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)))
	old := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(oldProp)
	})

	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetCodeMode(5 * time.Second)
	client := setupMockAgentClient(ctrl, "echo", []Tool{{Name: "ping"}})
	client.EXPECT().CallTool(gomock.Any(), "ping", gomock.Any()).Return(&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	metaParent := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	headerParent := "00-11111111111111111111111111111111-2222222222222222-01"
	params := json.RawMessage(`{"name":"execute","_meta":{"traceparent":"` + metaParent + `"}}`)
	headers := http.Header{}
	headers.Set("traceparent", headerParent)
	ctx := parentContextFromRequest(context.Background(), params, headers)

	if _, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "execute",
		Arguments: map[string]any{"code": `mcp.callTool("echo", "ping", {}); "ok";`},
	}); err != nil {
		t.Fatal(err)
	}

	exp.mu.Lock()
	defer exp.mu.Unlock()
	var outer, inner int
	for _, sp := range exp.spans {
		switch sp.name {
		case tracing.SpanNameToolsCall:
			outer++
			if sp.links != 1 {
				t.Fatalf("outer root links = %d, want 1", sp.links)
			}
		case tracing.SpanNameToolsCall + " ping":
			inner++
			if sp.links != 0 {
				t.Fatalf("inner span %q links = %d, want 0", sp.name, sp.links)
			}
		}
	}
	if outer == 0 || inner == 0 {
		t.Fatalf("spans = %+v", exp.spans)
	}
}

func TestPinBlock_DoesNotAttemptDownstream(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	client := setupMockAgentClient(ctrl, "echo", []Tool{{Name: "ping"}})
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.blockedMu.Lock()
	g.blockedServers["echo"] = true
	g.blockedMu.Unlock()

	var seen []DispatchObservation
	g.SetDispatchObserver(dispatchSpy{fn: func(obs DispatchObservation) { seen = append(seen, obs) }})
	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "echo__ping"})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("result = %+v, want pin denial", result)
	}
	if len(seen) != 1 {
		t.Fatalf("observations = %d", len(seen))
	}
	if !seen[0].HasReplica || seen[0].DownstreamAttempted {
		t.Fatalf("hasReplica=%v downstreamAttempted=%v", seen[0].HasReplica, seen[0].DownstreamAttempted)
	}
}

func metaString(t *testing.T, meta map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("%s is not a string: %s", key, raw)
	}
	return value
}

type recordedSpan struct {
	name         string
	traceID      string
	parentID     string
	parentRemote bool
}

type spanRecorder struct {
	mu    sync.Mutex
	spans []recordedSpan
}

func (r *spanRecorder) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sp := range spans {
		parent := sp.Parent()
		r.spans = append(r.spans, recordedSpan{
			name:         sp.Name(),
			traceID:      sp.SpanContext().TraceID().String(),
			parentID:     parent.SpanID().String(),
			parentRemote: parent.IsRemote(),
		})
	}
	return nil
}

func (r *spanRecorder) Shutdown(context.Context) error { return nil }

func (r *spanRecorder) hasRemoteParent(traceID, parentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sp := range r.spans {
		if sp.traceID == traceID && sp.parentRemote && sp.parentID == parentID && strings.HasPrefix(sp.name, tracing.SpanNameToolsCall) {
			return true
		}
	}
	return false
}

func (r *spanRecorder) snapshot() []recordedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedSpan, len(r.spans))
	copy(out, r.spans)
	return out
}

func installRecordingTracer(t *testing.T, rec *spanRecorder) func() {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(rec)))
	old := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(oldProp)
	}
}

type linkSpan struct {
	name  string
	links int
}

type linkExporter struct {
	mu    sync.Mutex
	spans []linkSpan
}

func (e *linkExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, sp := range spans {
		e.spans = append(e.spans, linkSpan{name: sp.Name(), links: len(sp.Links())})
	}
	return nil
}

func (e *linkExporter) Shutdown(context.Context) error { return nil }

func installPropagator(t *testing.T) func() {
	t.Helper()
	tp := sdktrace.NewTracerProvider()
	old := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(oldProp)
	}
}
