package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func TestStaticHeaderSourceFor(t *testing.T) {
	tests := []struct {
		name      string
		auth      *ServerAuthConfig
		wantName  string
		wantValue string
		wantNil   bool
	}{
		{"nil config", nil, "", "", true},
		{"bearer", &ServerAuthConfig{Type: "bearer", Token: "tok"}, "Authorization", "Bearer tok", false},
		{"bearer empty token", &ServerAuthConfig{Type: "bearer"}, "", "", true},
		{"header", &ServerAuthConfig{Type: "header", Header: "X-API-Key", Value: "v"}, "X-API-Key", "v", false},
		{"header empty name", &ServerAuthConfig{Type: "header", Value: "v"}, "", "", true},
		{"oauth not static", &ServerAuthConfig{Type: "oauth"}, "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hs := StaticHeaderSourceFor(tt.auth)
			if tt.wantNil {
				if hs != nil {
					t.Fatalf("expected nil source, got %v", hs)
				}
				return
			}
			if hs == nil {
				t.Fatal("expected source, got nil")
			}
			name, value, err := hs.AuthHeader(context.Background())
			if err != nil {
				t.Fatalf("AuthHeader: %v", err)
			}
			if name != tt.wantName || value != tt.wantValue {
				t.Errorf("got (%q, %q), want (%q, %q)", name, value, tt.wantName, tt.wantValue)
			}
		})
	}
}

func TestClient_AttachesAuthHeader(t *testing.T) {
	// Ping is a protocol-level JSON-RPC request (#1088), so every request
	// including the health check must carry the configured auth header;
	// key the capture by JSON-RPC method, not HTTP verb.
	var mu sync.Mutex
	authByMethod := make(map[string]string)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		authByMethod[req.Method] = r.Header.Get("Authorization")
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			result := InitializeResult{
				ProtocolVersion: "2025-06-18",
				ServerInfo:      ServerInfo{Name: "test", Version: "1.0"},
			}
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, result))
			return
		}
		_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
	}))
	defer ts.Close()

	c := NewClient("test", ts.URL)
	c.SetHeaderSource(NewStaticHeaderSource("Authorization", "Bearer tok"))

	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := authByMethod["initialize"]; got != "Bearer tok" {
		t.Errorf("initialize Authorization = %q, want %q", got, "Bearer tok")
	}
	if got := authByMethod["ping"]; got != "Bearer tok" {
		t.Errorf("ping Authorization = %q, want %q", got, "Bearer tok")
	}
}
