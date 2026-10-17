package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func TestClient_ExtraHeadersBeforeAuth(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"0"}}}`)
	}))
	defer ts.Close()

	c := NewClient("api", ts.URL)
	c.SetExtraHeaders(map[string]string{
		"X-Tenant":      "acme",
		"Authorization": "Bearer from-headers",
	})
	c.SetHeaderSource(NewStaticHeaderSource("Authorization", "Bearer from-auth"))

	req, err := c.newPOSTRequest(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`), "initialize", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-Tenant") != "acme" {
		t.Fatalf("X-Tenant = %q", req.Header.Get("X-Tenant"))
	}
	if req.Header.Get("Authorization") != "Bearer from-auth" {
		t.Fatalf("auth did not win: %q", req.Header.Get("Authorization"))
	}

	if err := c.replyToServer(context.Background(), jsonrpc.Response{JSONRPC: "2.0"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("reply requests = %d", len(seen))
	}
	if seen[0].Get("X-Tenant") != "acme" || seen[0].Get("Authorization") != "Bearer from-auth" {
		t.Fatalf("reply headers tenant=%q auth=%q", seen[0].Get("X-Tenant"), seen[0].Get("Authorization"))
	}
}

func TestApplyDownstreamHeaders_HeaderSource(t *testing.T) {
	var mu sync.Mutex
	var seen http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"0"}}}`)
	}))
	defer ts.Close()

	c := NewClient("api", ts.URL)
	applyDownstreamHeaders(c, MCPServerConfig{
		Headers:      map[string]string{"X-Portainer-API-Key": "k1"},
		HeaderSource: NewStaticHeaderSource("Authorization", "Bearer synthetic"),
	})
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen.Get("X-Portainer-API-Key") != "k1" {
		t.Fatalf("X-Portainer-API-Key = %q", seen.Get("X-Portainer-API-Key"))
	}
	if seen.Get("Authorization") != "Bearer synthetic" {
		t.Fatalf("Authorization = %q", seen.Get("Authorization"))
	}
}

func TestOpenAPIClient_HeadersOnOperationAndHeadNotSpec(t *testing.T) {
	var mu sync.Mutex
	specHeaders := http.Header{}
	opHeaders := http.Header{}
	headHeaders := http.Header{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodHead {
			headHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			return
		}
		opHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer api.Close()
	spec := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "` + api.URL + `"}],
		"paths": {
			"/echo": {
				"post": {
					"operationId": "echo",
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`
	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		specHeaders = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, spec)
	}))
	defer specSrv.Close()

	client, err := NewOpenAPIClient("api", &OpenAPIClientConfig{
		Spec:      specSrv.URL + "/openapi.json",
		BaseURL:   api.URL,
		Headers:   map[string]string{"X-Tenant": "acme", "Authorization": "Bearer extra"},
		AuthType:  "bearer",
		AuthToken: "from-auth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.RefreshTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool call error: %#v", result.Content)
	}
	if err := client.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if specHeaders.Get("X-Tenant") != "" || specHeaders.Get("Authorization") != "" {
		t.Fatalf("spec fetch carried headers: %#v", specHeaders)
	}
	if opHeaders.Get("X-Tenant") != "acme" || !strings.HasPrefix(opHeaders.Get("Authorization"), "Bearer from-auth") {
		t.Fatalf("operation headers tenant=%q auth=%q", opHeaders.Get("X-Tenant"), opHeaders.Get("Authorization"))
	}
	if headHeaders.Get("X-Tenant") != "acme" || !strings.HasPrefix(headHeaders.Get("Authorization"), "Bearer from-auth") {
		t.Fatalf("head headers tenant=%q auth=%q", headHeaders.Get("X-Tenant"), headHeaders.Get("Authorization"))
	}
}
