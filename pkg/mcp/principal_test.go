package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/stretchr/testify/require"
)

const testPrincipal = "Alice+/Subject="

func principalScopeGateway(t *testing.T) *Gateway {
	t.Helper()
	g := newScopeTestGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"alice":  {Aliases: []string{testPrincipal}, Servers: []string{"github"}},
			"cursor": {Servers: []string{"gitlab"}},
			// Exact aliases must win even over a matching normalized key.
			NormalizeClientID(testPrincipal): {Servers: []string{"gitlab"}},
		},
	}))
	return g
}

func requirePrincipalTools(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response jsonrpc.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Nil(t, response.Error)
	var result ToolsListResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	require.Equal(t, []string{"github__create-issue", "github__search-repos"}, toolNames(result.Tools))
}

func TestPrincipal_HandshakeBindingAndScope(t *testing.T) {
	g := principalScopeGateway(t)
	srv := NewStreamableHTTPServer(g, nil)
	init := loopbackRequest(http.MethodPost, "/mcp?client=cursor", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"Cursor"}}}`))
	init.Header.Set(ClientAccessIDHeader, "header-claim")
	init = init.WithContext(WithPrincipal(init.Context(), testPrincipal))
	require.Equal(t, testPrincipal, clientAccessIDFromRequest(init))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, init)
	require.Equal(t, http.StatusOK, w.Code)
	id := w.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, id)
	session := g.sessions.Get(id)
	require.Equal(t, testPrincipal, session.AccessID)
	require.Equal(t, testPrincipal, session.Principal)
	require.Equal(t, "cursor", session.ClientID)
	ctx := withSessionIdentity(init.Context(), session)
	require.Equal(t, testPrincipal, ClientAccessIDFromContext(ctx))

	request := func(method, principal string) *httptest.ResponseRecorder {
		r := loopbackRequest(method, "/mcp?client=cursor", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
		r.Header.Set("Mcp-Session-Id", id)
		r = r.WithContext(WithPrincipal(r.Context(), principal))
		out := httptest.NewRecorder()
		srv.ServeHTTP(out, r)
		return out
	}
	requirePrincipalTools(t, request(http.MethodPost, testPrincipal))
	before := g.sessions.Get(id).LastSeen
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		for _, principal := range []string{"Bob+/Subject=", ""} {
			out := request(method, principal)
			require.Equal(t, http.StatusNotFound, out.Code, method)
			require.Equal(t, "session expired\n", out.Body.String())
			require.Equal(t, before, g.sessions.Get(id).LastSeen)
		}
	}
	requirePrincipalTools(t, request(http.MethodPost, testPrincipal))
	require.Equal(t, http.StatusOK, request(http.MethodDelete, testPrincipal).Code)
	require.Nil(t, g.sessions.Get(id))
}

func TestPrincipal_AliasPrecedencePreservesLegacy(t *testing.T) {
	g := NewGateway()
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Servers: []string{"alpha"}},
			"alice":  {Aliases: []string{"cursor"}, Servers: []string{"beta"}},
		},
	}))
	legacy := WithClientAccessID(context.Background(), "cursor")
	require.True(t, g.clientAllowsToolCall(legacy, "alpha__echo"))
	require.False(t, g.clientAllowsToolCall(legacy, "beta__echo"))
	authenticated := WithPrincipal(legacy, "cursor")
	require.True(t, g.clientAllowsToolCall(authenticated, "beta__echo"))
	require.False(t, g.clientAllowsToolCall(authenticated, "alpha__echo"))
	// Per-request selection must not mutate the policy used by other callers.
	require.True(t, g.clientAllowsToolCall(legacy, "alpha__echo"))
}

func TestPrincipal_StatelessScope(t *testing.T) {
	g := principalScopeGateway(t)
	srv := NewStreamableHTTPServer(g, nil)
	r := statelessRequest(t, statelessBody(t, 1, "tools/list", nil))
	r.URL.RawQuery = "client=cursor"
	r.Header.Set(ClientAccessIDHeader, "cursor")
	r = r.WithContext(WithPrincipal(r.Context(), testPrincipal))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	requirePrincipalTools(t, w)
	require.Empty(t, w.Header().Get("Mcp-Session-Id"))
}

func TestPrincipal_DeleteExpiredSession(t *testing.T) {
	g := NewGateway()
	srv := NewStreamableHTTPServer(g, nil)
	id := initializeStreamable(t, srv)
	g.sessions.Delete(id)
	r := loopbackRequest(http.MethodDelete, "/mcp", nil)
	r.Header.Set("Mcp-Session-Id", id)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "session expired\n", w.Body.String())
}
