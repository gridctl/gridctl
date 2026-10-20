//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/resourceauth"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// Resolved with docker buildx imagetools inspect ghcr.io/dexidp/dex:v2.46.0.
const oauthDexImage = "ghcr.io/dexidp/dex:v2.46.0@sha256:933fcd3f523338c847b88ef84a7145fb2fcb7b9917f08418612afeaaa2001777"

type oauthDex struct {
	issuer   string
	password string
}

func startOAuthDex(t *testing.T, ctx context.Context) oauthDex {
	t.Helper()
	if testing.Short() {
		t.Skip("Dex requires Docker")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI unavailable")
	}
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skip("Docker daemon unavailable")
	}
	port := freePort(t)
	dex := oauthDex{issuer: fmt.Sprintf("http://127.0.0.1:%d/dex", port), password: rand.Text()}
	hash, err := bcrypt.GenerateFromPassword([]byte(dex.password), bcrypt.DefaultCost)
	require.NoError(t, err)
	config := map[string]any{
		"issuer":           dex.issuer,
		"storage":          map[string]any{"type": "memory"},
		"web":              map[string]any{"http": "0.0.0.0:5556"},
		"enablePasswordDB": true,
		"oauth2":           map[string]any{"passwordConnector": "local"},
		"staticClients": []map[string]any{
			{"id": "gridctl-gateway", "name": "Gridctl", "public": true, "redirectURIs": []string{"http://localhost/callback"}},
			{"id": "other-client", "name": "Other", "public": true, "redirectURIs": []string{"http://localhost/callback"}},
		},
		"staticPasswords": []map[string]any{
			{"email": "alice@example.test", "username": "Alice", "userID": "Alice+/Subject=1", "hash": string(hash)},
			{"email": "bob@example.test", "username": "Bob", "userID": "Bob+/Subject=", "hash": string(hash)},
		},
	}
	data, err := json.Marshal(config)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "dex.json")
	require.NoError(t, os.WriteFile(file, data, 0600))
	name := "gridctl-oauth-" + strings.ToLower(rand.Text()[:10])
	// Copy rather than bind-mount so the fixture works with a remote Docker daemon.
	out, err := exec.CommandContext(ctx, "docker", "create", "--user", "0:0", "--name", name, "-p", fmt.Sprintf("127.0.0.1:%d:5556", port), oauthDexImage, "dex", "serve", "/tmp/dex.json").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if t.Failed() {
			logs, _ := exec.CommandContext(cleanup, "docker", "logs", name).CombinedOutput()
			t.Logf("Dex container log: %s", logs)
		}
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
	})
	out, err = exec.CommandContext(ctx, "docker", "cp", file, name+":/tmp/dex.json").CombinedOutput()
	require.NoError(t, err, string(out))
	out, err = exec.CommandContext(ctx, "docker", "start", name).CombinedOutput()
	require.NoError(t, err, string(out))
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dex.issuer+"/.well-known/openid-configuration", nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 100*time.Millisecond, "Dex did not become ready")
	return dex
}

func (d oauthDex) token(t *testing.T, ctx context.Context, clientID, email string) (string, string) {
	t.Helper()
	form := url.Values{"grant_type": {"password"}, "client_id": {clientID}, "username": {email}, "password": {d.password}, "scope": {"openid profile email"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.issuer+"/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tokens))
	parts := strings.Split(tokens.AccessToken, ".")
	require.Len(t, parts, 3, "Dex must issue a JWT access token")
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var payload struct {
		Subject string `json:"sub"`
	}
	require.NoError(t, json.Unmarshal(claims, &payload))
	require.NotEmpty(t, payload.Subject)
	return tokens.AccessToken, payload.Subject
}

func TestOAuthResource_Dex(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	dex := startOAuthDex(t, ctx)
	alice, subject := dex.token(t, ctx, "gridctl-gateway", "alice@example.test")
	bob, _ := dex.token(t, ctx, "gridctl-gateway", "bob@example.test")
	wrongAudience, _ := dex.token(t, ctx, "other-client", "alice@example.test")
	other := startOAuthDex(t, ctx)
	wrongIssuer, _ := other.token(t, ctx, "gridctl-gateway", "alice@example.test")
	require.NotEqual(t, strings.ToLower(subject), subject, "subject must exercise case preservation")
	require.NotEqual(t, mcp.NormalizeClientID(subject), subject, "subject must exercise raw alias preservation")

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprint(port))
	waitForPort(t, ctx, port)
	newServer := func(scopes []string) *httptest.Server {
		gw := mcp.NewGateway()
		for _, name := range []string{"alpha", "beta"} {
			require.NoError(t, gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{Name: name, Transport: mcp.TransportHTTP, Endpoint: fmt.Sprintf("http://127.0.0.1:%d/mcp", port)}))
		}
		gw.SetGroupPolicy(groupsTestPolicy())
		gw.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{Default: "deny", Profiles: map[string]mcp.ClientProfileSpec{
			"alice":  {Aliases: []string{subject}, Servers: []string{"alpha"}},
			"cursor": {Servers: []string{"beta"}},
		}}))
		s := api.NewServer(gw, nil)
		t.Cleanup(s.Close)
		cfg := resourceauth.Config{Issuer: dex.issuer, Audience: "gridctl-gateway", RequiredScopes: scopes}
		v, err := resourceauth.New(ctx, cfg, nil)
		require.NoError(t, err)
		s.SetOAuth(v, cfg)
		ts := httptest.NewServer(s.Handler())
		t.Cleanup(ts.Close)
		return ts
	}
	ts := newServer(nil)
	do := func(server *httptest.Server, method, path, body, token, session string, want int) (http.Header, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if strings.Contains(body, "io.modelcontextprotocol/protocolVersion") {
			req.Header.Set("MCP-Protocol-Version", mcp.StatelessProtocolVersion)
			req.Header.Set("Mcp-Method", "tools/list")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, want, resp.StatusCode, "%s %s", method, path)
		return resp.Header, string(data)
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"Cursor","version":"1"}}}`
	h, _ := do(ts, http.MethodPost, "/mcp", init, "", "", http.StatusUnauthorized)
	require.Contains(t, h.Get("WWW-Authenticate"), "resource_metadata=")
	require.Empty(t, h.Get("Mcp-Session-Id"))
	for _, token := range []string{wrongAudience, wrongIssuer} {
		h, _ = do(ts, http.MethodGet, "/api/status", "", token, "", http.StatusUnauthorized)
		require.Contains(t, h.Get("WWW-Authenticate"), `error="invalid_token"`)
	}
	do(ts, http.MethodGet, "/api/status", "", alice, "", http.StatusOK)
	do(ts, http.MethodPost, "/groups/release/mcp", init, alice, "", http.StatusOK)
	h, _ = do(ts, http.MethodPost, "/mcp?client=cursor", init, alice, "", http.StatusOK)
	session := h.Get("Mcp-Session-Id")
	require.NotEmpty(t, session)
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	_, body := do(ts, http.MethodPost, "/mcp?client=cursor", list, alice, session, http.StatusOK)
	require.Contains(t, body, "alpha__")
	require.NotContains(t, body, "beta__")
	statelessList := `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"Cursor","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	_, body = do(ts, http.MethodPost, "/mcp?client=cursor", statelessList, alice, "", http.StatusOK)
	require.Contains(t, body, "alpha__")
	require.NotContains(t, body, "beta__")
	_, body = do(ts, http.MethodPost, "/mcp?client=cursor", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"beta__echo","arguments":{"message":"denied"}}}`, alice, session, http.StatusOK)
	require.Contains(t, body, `"isError":true`)
	require.Contains(t, body, "not in this client's access scope")
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		_, body = do(ts, method, "/mcp", list, bob, session, http.StatusNotFound)
		require.Equal(t, "session expired\n", body)
	}
	_, body = do(ts, http.MethodPost, "/mcp", list, alice, session, http.StatusOK)
	require.Contains(t, body, "alpha__")
	_, body = do(ts, http.MethodGet, "/api/tools/discover?client=cursor", "", alice, "", http.StatusOK)
	require.Contains(t, body, "alpha__")
	require.NotContains(t, body, "beta__")
	_, body = do(ts, http.MethodGet, "/.well-known/oauth-protected-resource", "", "", "", http.StatusOK)
	require.Contains(t, body, dex.issuer)
	scoped := newServer([]string{"mcp:tools"})
	h, _ = do(scoped, http.MethodGet, "/api/status", "", alice, "", http.StatusForbidden)
	require.Contains(t, h.Get("WWW-Authenticate"), `error="insufficient_scope"`)
}
