package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func getDiscover(handler http.Handler, rawQuery string) *httptest.ResponseRecorder {
	req := loopbackRequest(http.MethodGet, "/api/tools/discover"+rawQuery, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHandleToolsDiscover_SearchAndNotFound(t *testing.T) {
	stub := &callStubClient{
		name: "echo",
		tools: []mcp.Tool{
			{Name: "echo", Description: "repeat a payload", InputSchema: []byte(`{"type":"object","properties":{"message":{"type":"string"}}}`)},
			{Name: "sum", Description: "add", InputSchema: []byte(`{"type":"object","properties":{"left":{"type":"number"}}}`)},
		},
	}
	handler := newCallServer(t, stub).Handler()
	rec := getDiscover(handler, `?query=mcp`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var env toolsDiscoverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.SchemaVersion != 1 || env.Client != "cli" || env.Matched != 2 || env.Returned != 2 || env.Tools == nil {
		t.Fatalf("env = %+v", env)
	}

	rec = getDiscover(handler, `?name=echo__missing`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	callEnv := decodeCallEnvelope(t, rec)
	if callEnv.Error == nil || callEnv.Error.Code != errCodeNotFound {
		t.Fatalf("error = %+v", callEnv.Error)
	}

	rec = getDiscover(handler, `?name=echo__echo&query=x`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}

	req := loopbackRequest(http.MethodGet, "/api/tools/discover?limit=1&limit=2", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate status = %d", rec.Code)
	}

	rec = getDiscover(handler, `?limit=0`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit status = %d", rec.Code)
	}

	rec = getDiscover(handler, `?client=`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty client status = %d", rec.Code)
	}
}

func TestHandleToolsDiscover_ScopedCounts(t *testing.T) {
	stub := &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo", Description: "x"}}}
	srv := newCallServer(t, stub)
	srv.gateway.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{Default: "deny"}))
	rec := getDiscover(srv.Handler(), `?query=mcp`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var env toolsDiscoverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.TotalVisible != 0 || env.Matched != 0 || env.Returned != 0 || env.Tools == nil {
		t.Fatalf("hidden tools counted: %+v", env)
	}
	if stub.calls != 0 {
		t.Fatal("discovery invoked a tool")
	}
}

func TestHandleToolsDiscover_CodeModeOn(t *testing.T) {
	stub := &callStubClient{name: "echo", tools: []mcp.Tool{{Name: "echo", Description: "payload"}}}
	srv := newCallServer(t, stub)
	srv.gateway.SetCodeMode(0)
	rec := getDiscover(srv.Handler(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env toolsDiscoverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Returned != 1 || env.Tools[0].Name != "echo__echo" {
		t.Fatalf("code mode changed discovery: %+v", env)
	}
	if strings.Contains(strings.ToLower(env.Tools[0].Name), "search") {
		t.Fatal("meta-tool leaked")
	}
}
