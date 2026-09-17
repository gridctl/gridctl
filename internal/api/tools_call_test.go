package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

type callStubClient struct {
	name    string
	tools   []mcp.Tool
	calls   int
	lastArg map[string]any
	result  *mcp.ToolCallResult
	err     error
}

func (c *callStubClient) Name() string                       { return c.name }
func (c *callStubClient) Initialize(context.Context) error   { return nil }
func (c *callStubClient) RefreshTools(context.Context) error { return nil }
func (c *callStubClient) Tools() []mcp.Tool                  { return c.tools }
func (c *callStubClient) IsInitialized() bool                { return true }
func (c *callStubClient) ServerInfo() mcp.ServerInfo {
	return mcp.ServerInfo{Name: c.name, Version: "1"}
}
func (c *callStubClient) CallTool(_ context.Context, _ string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	c.calls++
	c.lastArg = arguments
	if c.err != nil {
		return nil, c.err
	}
	if c.result != nil {
		return c.result, nil
	}
	return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("ok")}}, nil
}

func newCallServer(t *testing.T, client mcp.AgentClient) *Server {
	t.Helper()
	srv := newTestServer(t)
	if client != nil {
		srv.gateway.Router().AddClient(client)
		srv.gateway.Router().RefreshTools()
	}
	return srv
}

func postToolsCall(handler http.Handler, body string, contentType string) *httptest.ResponseRecorder {
	req := loopbackRequest(http.MethodPost, "/api/tools/call", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeCallEnvelope(t *testing.T, rec *httptest.ResponseRecorder) toolsCallEnvelope {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var env toolsCallEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return env
}

func TestHandleToolsCall_SuccessAndToolError(t *testing.T) {
	stub := &callStubClient{
		name:  "echo",
		tools: []mcp.Tool{{Name: "echo", Description: "Echo"}},
		result: &mcp.ToolCallResult{
			Content:           []mcp.Content{mcp.NewTextContent("hello")},
			StructuredContent: json.RawMessage(`{"n":1}`),
			Meta:              map[string]any{"k": "v"},
		},
	}
	handler := newCallServer(t, stub).Handler()
	rec := postToolsCall(handler, `{"name":"echo__echo","arguments":{"message":"hello"}}`, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	env := decodeCallEnvelope(t, rec)
	if env.SchemaVersion != 1 || env.Client != "cli" || env.Name != "echo__echo" {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Outcome.Disposition != runs.DispositionCompleted || env.Outcome.Reason != runs.ReasonOK {
		t.Fatalf("outcome = %+v", env.Outcome)
	}
	if env.Error != nil || env.Result == nil || env.Result.Content[0].Text != "hello" {
		t.Fatalf("result = %+v error=%+v", env.Result, env.Error)
	}
	if string(env.Result.StructuredContent) != `{"n":1}` {
		t.Fatalf("structured = %s", env.Result.StructuredContent)
	}

	stub.result = &mcp.ToolCallResult{IsError: true, Content: []mcp.Content{mcp.NewTextContent("nope")}}
	rec = postToolsCall(handler, `{"name":"echo__echo"}`, "application/json")
	env = decodeCallEnvelope(t, rec)
	if rec.Code != http.StatusOK || env.Outcome.Reason != runs.ReasonToolError || env.Error == nil || env.Error.Code != errCodeToolError {
		t.Fatalf("tool error envelope status=%d env=%+v", rec.Code, env)
	}
	if env.Result == nil || env.Result.Content[0].Text != "nope" {
		t.Fatal("tool error must preserve result content")
	}
}

func TestHandleToolsCall_ValidationStatuses(t *testing.T) {
	handler := newCallServer(t, &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo"}}}).Handler()
	tests := []struct {
		name        string
		body        string
		ct          string
		wantStatus  int
		wantCode    string
		wantErrType string
	}{
		{name: "missing content type", body: `{"name":"echo__echo"}`, wantStatus: 415, wantCode: errCodeInvalidContentType},
		{name: "malformed json", body: `{`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidJSON},
		{name: "array", body: `[]`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidJSON},
		{name: "trailing", body: `{"name":"echo__echo"}{}`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidJSON},
		{name: "unknown field", body: `{"name":"echo__echo","group":"x"}`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidJSON},
		{name: "null arguments", body: `{"name":"echo__echo","arguments":null}`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidJSON},
		{name: "empty name", body: `{"name":"echo__"}`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidName},
		{name: "empty client", body: `{"name":"echo__echo","client":"  "}`, ct: "application/json", wantStatus: 400, wantCode: errCodeInvalidClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postToolsCall(handler, tt.body, tt.ct)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			env := decodeCallEnvelope(t, rec)
			if env.Error == nil || env.Error.Code != tt.wantCode || env.Error.Message == "" {
				t.Fatalf("error = %+v", env.Error)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(raw["error"], []byte(`"`)) || bytes.HasPrefix(bytes.TrimSpace(raw["error"]), []byte(`"`)) {
				t.Fatalf("legacy string error: %s", raw["error"])
			}
			if env.Result != nil {
				t.Fatal("result must be null")
			}
		})
	}
}

func TestHandleToolsCall_Oversized(t *testing.T) {
	handler := newCallServer(t, &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo"}}}).Handler()
	body := `{"name":"echo__echo","arguments":{"x":"` + strings.Repeat("a", toolsCallMaxBytes) + `"}}`
	rec := postToolsCall(handler, body, "application/json")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	env := decodeCallEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != errCodePayloadTooLarge {
		t.Fatalf("error = %+v", env.Error)
	}
}

func TestHandleToolsCall_GatewayUnavailable(t *testing.T) {
	srv := newTestServer(t)
	srv.gateway = nil
	rec := postToolsCall(srv.Handler(), `{"name":"echo__echo"}`, "application/json")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	env := decodeCallEnvelope(t, rec)
	if env.Error == nil || env.Error.Code != errCodeGatewayUnavailable {
		t.Fatalf("error = %+v", env.Error)
	}
}

func TestHandleToolsCall_ScopeAndUnknown(t *testing.T) {
	stub := &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo"}}}
	srv := newCallServer(t, stub)
	srv.gateway.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]mcp.ClientProfileSpec{
			"cli": {Servers: []string{"other"}},
		},
	}))
	rec := postToolsCall(srv.Handler(), `{"name":"echo__echo"}`, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	env := decodeCallEnvelope(t, rec)
	if env.Outcome.Reason != runs.ReasonClientScope || env.Error == nil || env.Error.Code != errCodeClientScope {
		t.Fatalf("env = %+v", env)
	}
	if stub.calls != 0 {
		t.Fatal("denied call invoked downstream")
	}

	rec = postToolsCall(srv.Handler(), `{"name":"echo__echo","client":"allowed"}`, "application/json")
	env = decodeCallEnvelope(t, rec)
	if env.Client != "allowed" {
		t.Fatalf("client = %s", env.Client)
	}
}

func TestHandleToolsCall_OmittedArguments(t *testing.T) {
	stub := &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo"}}}
	rec := postToolsCall(newCallServer(t, stub).Handler(), `{"name":"echo__echo"}`, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 1 || stub.lastArg == nil {
		t.Fatal("omitted arguments were not normalized to an object")
	}
}

func TestHandleToolsCall_DoesNotUseLegacyErrorShape(t *testing.T) {
	rec := postToolsCall(newCallServer(t, nil).Handler(), `{"name":`, "application/json")
	if rec.Code != 400 {
		t.Fatalf("status = %d", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw["error"]), []byte(`{`)) {
		t.Fatalf("error must be an object, got %s", raw["error"])
	}
	var obj toolsEndpointError
	if err := json.Unmarshal(raw["error"], &obj); err != nil || obj.Code == "" || obj.Message == "" {
		t.Fatalf("error object = %+v err=%v", obj, err)
	}
	if strings.Contains(strings.ToLower(obj.Message), "invalid json:") {
		t.Fatalf("unsafe or legacy message %q", obj.Message)
	}
}

func TestNormalizeDeclaredClient(t *testing.T) {
	got, err := normalizeDeclaredClient("", false)
	if err != nil || got != "cli" {
		t.Fatalf("default = %q %v", got, err)
	}
	if _, err := normalizeDeclaredClient("", true); err == nil {
		t.Fatal("empty present client must be invalid")
	}
	if _, err := normalizeDeclaredClient("\x00cli", true); err == nil {
		t.Fatal("control characters must be invalid")
	}
	if _, err := normalizeDeclaredClient(strings.Repeat("a", 129), true); err == nil {
		t.Fatal("oversize client must be invalid")
	}
	got, err = normalizeDeclaredClient("Claude Code", true)
	if err != nil || got != "claude-code" {
		t.Fatalf("normalized = %q %v", got, err)
	}
}
