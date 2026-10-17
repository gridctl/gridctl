package servercard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolve_MixedOriginCatalogIsNoCard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/mcp/server-card":
			http.NotFound(w, r)
		case CatalogWellKnown:
			w.Header().Set("Content-Type", MediaTypeCatalog)
			_, _ = w.Write([]byte(`{"specVersion":"1.0","entries":[{"identifier":"urn:air:other.example:mcp:other","type":"application/mcp-server-card+json","url":"https://other.example/mcp/server-card"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := Resolve(context.Background(), srv.URL+"/api/mcp", FetchOptions{})
	if !errors.Is(err, ErrNoCard) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolve_CatalogSkipsForeignThenRefusesOffOriginRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/mcp/server-card":
			http.NotFound(w, r)
		case "/listed/server-card":
			card := ServerCard{
				Schema:      SchemaURL,
				Name:        "io.example/lab",
				Description: "Lab",
				Version:     "1",
				Remotes:     []Remote{{Type: "streamable-http", URL: "https://other.example/mcp"}},
			}
			w.Header().Set("Content-Type", MediaTypeServerCard)
			_ = json.NewEncoder(w).Encode(card)
		case CatalogWellKnown:
			body, _ := json.Marshal(Catalog{
				SpecVersion: "1.0",
				Entries: []CatalogEntry{
					{Identifier: "urn:air:other.example:mcp:other", Type: MediaTypeServerCard, URL: "https://other.example/mcp/server-card"},
					{Identifier: "urn:air:local:mcp:lab", Type: MediaTypeServerCard, URL: "http://" + r.Host + "/listed/server-card"},
				},
			})
			w.Header().Set("Content-Type", MediaTypeCatalog)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := Resolve(context.Background(), srv.URL+"/api/mcp", FetchOptions{})
	if !errors.Is(err, ErrOffOrigin) {
		t.Fatalf("err = %v", err)
	}
}
