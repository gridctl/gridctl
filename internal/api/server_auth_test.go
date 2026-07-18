package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/mcpauth"
)

// newAuthTestServer wires an API server with a broker configured against a
// minimal fake OAuth environment (resource + AS on one httptest server).
func newAuthTestServer(t *testing.T) (*Server, *mcpauth.Broker, *httptest.Server) {
	t.Helper()

	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate",
			fmt.Sprintf("Bearer resource_metadata=%q", base+"/.well-known/oauth-protected-resource"))
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              base + "/mcp",
			"authorization_servers": []string{base},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                           base,
			"authorization_endpoint":           base + "/authorize",
			"token_endpoint":                   base + "/token",
			"registration_endpoint":            base + "/register",
			"jwks_uri":                         base + "/jwks",
			"response_types_supported":         []string{"code"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"client_id": "dyn-client"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-1",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	as := httptest.NewServer(mux)
	t.Cleanup(as.Close)
	base = as.URL

	store, err := mcpauth.NewTokenStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	broker := mcpauth.NewBroker(store, "http://localhost:8180"+mcpauth.CallbackPath, nil)

	gateway := mcp.NewGateway()
	broker.SetStateSink(gateway)
	if err := broker.Configure("notion", as.URL+"/mcp", nil); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(gateway, nil)
	srv.SetOAuthBroker(broker)
	return srv, broker, as
}

func TestHandleAuthServers(t *testing.T) {
	srv, _, _ := newAuthTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var infos []mcpauth.ServerAuthInfo
	if err := json.NewDecoder(rec.Body).Decode(&infos); err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Server != "notion" || infos[0].Status != mcp.AuthStatusNeedsAuth {
		t.Fatalf("unexpected payload: %+v", infos)
	}
}

func TestHandleAuthLoginAndManual(t *testing.T) {
	srv, _, as := newAuthTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/servers/notion/auth/login", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
	}
	var loginResp struct {
		AuthorizeURL string `json:"authorize_url"`
		State        string `json:"state"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatal(err)
	}
	if loginResp.State == "" || !strings.HasPrefix(loginResp.AuthorizeURL, as.URL+"/authorize") {
		t.Fatalf("unexpected login payload: %+v", loginResp)
	}

	// Complete via the manual endpoint with a pasted redirect URL.
	redirect := "http://localhost:8180/oauth/callback?code=any&state=" + url.QueryEscape(loginResp.State)
	body := fmt.Sprintf(`{"redirectUrl": %q}`, redirect)
	req = httptest.NewRequest(http.MethodPost, "/api/servers/notion/auth/manual", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manual status = %d: %s", rec.Code, rec.Body.String())
	}

	// Status should now be authorized.
	req = httptest.NewRequest(http.MethodGet, "/api/auth/servers", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var infos []mcpauth.ServerAuthInfo
	if err := json.NewDecoder(rec.Body).Decode(&infos); err != nil {
		t.Fatal(err)
	}
	if infos[0].Status != mcp.AuthStatusAuthorized {
		t.Fatalf("expected authorized, got %+v", infos[0])
	}
}

func TestHandleAuthWaitUnknownState(t *testing.T) {
	srv, _, _ := newAuthTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/servers/notion/auth/wait?state=bogus", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleAuthLogout(t *testing.T) {
	srv, _, _ := newAuthTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/servers/notion/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthEndpointsDisabledWithoutBroker(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestOAuthCallbackMountedOutsideAuthMiddleware(t *testing.T) {
	srv, _, _ := newAuthTestServer(t)
	srv.SetAuth("bearer", "gateway-token", "")
	handler := srv.Handler()

	// API routes require the gateway token.
	req := httptest.NewRequest(http.MethodGet, "/api/auth/servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on API without token, got %d", rec.Code)
	}

	// The OAuth callback must be reachable with NO gateway token: the
	// browser redirect carries none. A bad state is a 400 from the
	// callback page, not a 401 from the middleware.
	req = httptest.NewRequest(http.MethodGet, "/oauth/callback?code=x&state=unknown", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("callback must bypass the inbound auth middleware")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown state, got %d", rec.Code)
	}
}
