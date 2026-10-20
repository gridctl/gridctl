package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/gridctl/gridctl/pkg/resourceauth"
	"github.com/stretchr/testify/require"
)

func TestBearerChallenge(t *testing.T) {
	got := bearerChallenge("http://localhost/.well-known/oauth-protected-resource", nil, "")
	require.Equal(t, `Bearer resource_metadata="http://localhost/.well-known/oauth-protected-resource"`, got)
	got = bearerChallenge("http://localhost/.well-known/oauth-protected-resource", []string{"mcp:tools", "openid"}, "invalid_token")
	require.Contains(t, got, `resource_metadata="http://localhost/.well-known/oauth-protected-resource"`)
	require.Contains(t, got, `error="invalid_token"`)
	require.Contains(t, got, `scope="mcp:tools openid"`)
	got = bearerChallenge("http://localhost/meta", []string{"mcp:tools"}, "insufficient_scope")
	require.Contains(t, got, `error="insufficient_scope"`)
	require.NotContains(t, got, "Authorization")
}

func TestOAuthMetadataPath_Exact(t *testing.T) {
	require.True(t, oauthMetadataPath("/.well-known/oauth-protected-resource"))
	require.True(t, oauthMetadataPath("/.well-known/oauth-protected-resource/mcp"))
	require.True(t, oauthMetadataPath("/.well-known/oauth-protected-resource/groups/release/mcp"))
	require.False(t, oauthMetadataPath("/.well-known/agent.json"))
	require.False(t, oauthMetadataPath("/.well-known/oauth-protected-resource/extra"))
	require.False(t, oauthMetadataPath("/.well-known/oauth-protected-resource/groups/release/mcp/extra"))
	require.False(t, oauthMetadataPath("/.well-known/oauth-protected-resource/groups/mcp"))
	require.True(t, isProtectedPath("/.well-known/agent.json"))
	require.True(t, isProtectedPath("/.well-known/oauth-protected-resource/extra"))
}

func TestOAuthHandler_MetadataCORSAndChallenge(t *testing.T) {
	iss := startAPIIssuer(t)
	s := NewServer(groupsGateway(), fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>ui</html>")}})
	t.Cleanup(s.Close)
	s.SetAllowedOrigins([]string{"https://ui.example"})
	s.SetServerCard("", "")
	verifier, err := resourceauth.New(context.Background(), resourceauth.Config{
		Issuer: iss.URL, Audience: "gridctl", RequiredScopes: []string{"mcp:tools"},
	}, iss.Client())
	require.NoError(t, err)
	s.SetOAuth(verifier, resourceauth.Config{
		Issuer: iss.URL, Audience: "gridctl", RequiredScopes: []string{"mcp:tools"},
	})
	h := s.Handler()

	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-protected-resource/groups/release/mcp",
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, loopbackRequest(method, path, nil))
			require.Equal(t, http.StatusMethodNotAllowed, w.Code, method+" "+path)
			require.Equal(t, "GET, OPTIONS", w.Header().Get("Allow"))
		}
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			badHost := loopbackRequest(method, path, nil)
			badHost.Host = "untrusted.invalid"
			badHost.Header.Set("Origin", "https://unlisted.example")
			denied := httptest.NewRecorder()
			h.ServeHTTP(denied, badHost)
			require.Equal(t, http.StatusForbidden, denied.Code)
		}
		req := loopbackRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "https://unlisted.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, path)
		require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		var doc map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
		require.Equal(t, []any{iss.URL}, doc["authorization_servers"])
		require.Equal(t, []any{"header"}, doc["bearer_methods_supported"])
		require.Equal(t, []any{"mcp:tools"}, doc["scopes_supported"])
		require.Contains(t, doc["resource"], "/mcp")

		opt := loopbackRequest(http.MethodOptions, path, nil)
		opt.Header.Set("Origin", "https://unlisted.example")
		ow := httptest.NewRecorder()
		h.ServeHTTP(ow, opt)
		require.Equal(t, http.StatusOK, ow.Code, "OPTIONS "+path)
		require.Equal(t, "*", ow.Header().Get("Access-Control-Allow-Origin"))
		require.Contains(t, ow.Header().Get("Access-Control-Allow-Methods"), "GET")
		require.NotContains(t, ow.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	}

	for _, path := range []string{"/mcp", "/api/status"} {
		opt := loopbackRequest(http.MethodOptions, path, nil)
		opt.Header.Set("Origin", "https://unlisted.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, opt)
		require.Equal(t, http.StatusOK, w.Code)
		require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), path)
	}

	post := loopbackRequest(http.MethodPost, "/.well-known/oauth-protected-resource", nil)
	pw := httptest.NewRecorder()
	h.ServeHTTP(pw, post)
	require.Equal(t, http.StatusMethodNotAllowed, pw.Code)

	unknown := loopbackRequest(http.MethodGet, "/.well-known/oauth-protected-resource/groups/missing/mcp", nil)
	uw := httptest.NewRecorder()
	h.ServeHTTP(uw, unknown)
	require.Equal(t, http.StatusNotFound, uw.Code)

	for _, path := range []string{"/mcp", "/groups/release/mcp", "/api/status", "/sse", "/message", "/a2a/x", "/.well-known/oauth-protected-resource/groups/mcp"} {
		req := loopbackRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code, path)
		require.Equal(t, "1", w.Header().Get("Gridctl-Auth-Rejected"))
		require.Empty(t, w.Header().Get("Mcp-Session-Id"))
		challenge := w.Header().Get("WWW-Authenticate")
		require.Contains(t, challenge, "resource_metadata=")
		if strings.HasPrefix(path, "/groups/") {
			require.Contains(t, challenge, "/groups/release/mcp")
		} else {
			require.NotContains(t, challenge, "/groups/")
		}
		require.Contains(t, w.Body.String(), "Unauthorized")
	}

	bad := loopbackRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	bad.Header.Set("Authorization", "Bearer not-a-jwt")
	bw := httptest.NewRecorder()
	h.ServeHTTP(bw, bad)
	require.Equal(t, http.StatusUnauthorized, bw.Code)
	require.Contains(t, bw.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
	require.Equal(t, "1", bw.Header().Get("Gridctl-Auth-Rejected"))

	token := iss.mint(t, map[string]any{"scope": "openid"})
	scoped := loopbackRequest(http.MethodGet, "/api/status", nil)
	scoped.Header.Set("Authorization", "Bearer "+token)
	sw := httptest.NewRecorder()
	h.ServeHTTP(sw, scoped)
	require.Equal(t, http.StatusForbidden, sw.Code)
	require.Contains(t, sw.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`)
	require.Contains(t, sw.Header().Get("WWW-Authenticate"), `scope="mcp:tools"`)
	require.Equal(t, "1", sw.Header().Get("Gridctl-Auth-Rejected"))
	require.Contains(t, sw.Body.String(), "Forbidden")

	okToken := iss.mint(t, map[string]any{"scope": "mcp:tools"})
	for _, path := range []string{"/api/status", "/mcp"} {
		req := loopbackRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"cursor"}}}`))
		if path == "/api/status" {
			req = loopbackRequest(http.MethodGet, path, nil)
		}
		req.Header.Set("Authorization", "Bearer "+okToken)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, path)
		if path == "/mcp" {
			require.NotEmpty(t, w.Header().Get("Mcp-Session-Id"))
		}
	}
	health := loopbackRequest(http.MethodGet, "/health", nil)
	hw := httptest.NewRecorder()
	h.ServeHTTP(hw, health)
	require.Equal(t, http.StatusOK, hw.Code)
}

func TestOAuthHandler_BearerModeKeepsMetadataProtected(t *testing.T) {
	s := NewServer(groupsGateway(), nil)
	t.Cleanup(s.Close)
	s.SetAuth("bearer", "secret", "")
	s.SetAllowedOrigins([]string{"https://ui.example"})
	h := s.Handler()
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-protected-resource/groups/release/mcp",
		"/.well-known/agent.json",
		"/.well-known/oauth-protected-resource/extra",
	} {
		req := loopbackRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code, path)
		opt := loopbackRequest(http.MethodOptions, path, nil)
		opt.Header.Set("Origin", "https://unlisted.example")
		ow := httptest.NewRecorder()
		h.ServeHTTP(ow, opt)
		require.Equal(t, http.StatusOK, ow.Code)
		require.Empty(t, ow.Header().Get("Access-Control-Allow-Origin"), path)
	}
}

func TestOAuthHandler_DiscoveryDownIs503(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(down.Close)
	s := NewServer(groupsGateway(), nil)
	t.Cleanup(s.Close)
	verifier, err := resourceauth.New(context.Background(), resourceauth.Config{
		Issuer: down.URL, Audience: "gridctl",
	}, down.Client())
	require.Error(t, err)
	s.SetOAuth(verifier, resourceauth.Config{Issuer: down.URL, Audience: "gridctl"})
	h := s.Handler()
	req := loopbackRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Empty(t, w.Header().Get("WWW-Authenticate"))
	require.Empty(t, w.Header().Get("Gridctl-Auth-Rejected"))
	require.Contains(t, w.Body.String(), "authorization server unavailable")
}

type apiIssuer struct {
	*httptest.Server
	key *rsa.PrivateKey
	kid string
}

func startAPIIssuer(t *testing.T) *apiIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	iss := &apiIssuer{key: key, kid: "kid-1"}
	mux := http.NewServeMux()
	serveMeta := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.URL,
			"authorization_endpoint":                iss.URL + "/auth",
			"token_endpoint":                        iss.URL + "/token",
			"jwks_uri":                              iss.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"code_challenge_methods_supported":      []string{"S256"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}
	mux.HandleFunc("/.well-known/openid-configuration", serveMeta)
	mux.HandleFunc("/.well-known/oauth-authorization-server", serveMeta)
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &key.PublicKey, KeyID: iss.kid, Algorithm: "RS256", Use: "sig",
		}}})
	})
	iss.Server = httptest.NewServer(mux)
	t.Cleanup(iss.Close)
	return iss
}

func (iss *apiIssuer) mint(t *testing.T, extra map[string]any) string {
	t.Helper()
	claims := map[string]any{
		"iss": iss.URL,
		"sub": "user-1",
		"aud": "gridctl",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range extra {
		claims[k] = v
	}
	opts := (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", iss.kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: iss.key}, opts)
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signed, err := signer.Sign(payload)
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	return compact
}
