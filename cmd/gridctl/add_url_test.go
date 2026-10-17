package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/servercard"
)

func TestResolveURLAdd_MixedOriginCatalogProbes(t *testing.T) {
	prev := addNoCard
	addNoCard = false
	t.Cleanup(func() { addNoCard = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/mcp/server-card":
			http.NotFound(w, r)
		case servercard.CatalogWellKnown:
			w.Header().Set("Content-Type", servercard.MediaTypeCatalog)
			_, _ = w.Write([]byte(`{"specVersion":"1.0","entries":[{"identifier":"urn:air:other.example:mcp:other","type":"application/mcp-server-card+json","url":"https://other.example/mcp/server-card"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, _, err := resolveURLAdd(context.Background(), output.NewWithWriter(io.Discard), srv.URL+"/api/mcp")
	if err == nil {
		t.Fatal("expected probe failure")
	}
	if strings.Contains(err.Error(), "off-origin") {
		t.Fatalf("foreign catalog entry refused instead of probing: %v", err)
	}
	if !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("err = %v", err)
	}
}
