package probe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestProbe_ExternalSendsAuthAndExtraHeaders(t *testing.T) {
	var mu sync.Mutex
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"0"}}}`)
	}))
	defer srv.Close()

	p := NewProber(NewCache(0))
	_, _ = p.Probe(context.Background(), config.MCPServer{
		Name: "api",
		URL:  srv.URL,
		Auth: &config.ServerAuth{Type: "bearer", Token: "probe-token"},
		Headers: map[string]string{
			"X-Tenant": "acme",
		},
	})
	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatal("probe made no request")
	}
	if got.Get("Authorization") != "Bearer probe-token" {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("X-Tenant") != "acme" {
		t.Fatalf("X-Tenant = %q", got.Get("X-Tenant"))
	}
}
