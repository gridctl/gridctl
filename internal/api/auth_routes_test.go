package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/mcpauth"
	"github.com/stretchr/testify/require"
)

// Walk the production registrations rather than keep a second route inventory.
// Unknown registration expressions fail closed until the test can exercise them.
func TestAuthHandler_RegisteredRoutes(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "api.go", nil, 0)
	require.NoError(t, err)
	var patterns []string
	var patternStrings func(ast.Expr, string) string
	patternStrings = func(expr ast.Expr, prefix string) string {
		switch e := expr.(type) {
		case *ast.BasicLit:
			s, err := strconv.Unquote(e.Value)
			require.NoError(t, err)
			return s
		case *ast.BinaryExpr:
			require.Equal(t, token.ADD, e.Op)
			return patternStrings(e.X, prefix) + patternStrings(e.Y, prefix)
		case *ast.Ident:
			require.Equal(t, "prefix", e.Name)
			return prefix
		case *ast.SelectorExpr:
			require.Equal(t, "mcpauth", e.X.(*ast.Ident).Name)
			require.Equal(t, "CallbackPath", e.Sel.Name)
			return mcpauth.CallbackPath
		default:
			t.Fatalf("unhandled route expression %T", expr)
			return ""
		}
	}
	var prefixes []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok {
			if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "registerVarRoutes" {
				prefixes = append(prefixes, patternStrings(call.Args[0], ""))
			}
		}
		return true
	})
	require.NotEmpty(t, prefixes)
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		for _, prefix := range prefixes {
			patterns = append(patterns, patternStrings(call.Args[0], prefix))
		}
		return true
	})
	require.NotEmpty(t, patterns)
	s := NewServer(groupsGateway(), nil)
	t.Cleanup(s.Close)
	s.SetAuth("bearer", rand.Text(), "")
	handler := s.Handler()
	t.Run("grouped_mcp", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, loopbackRequest(http.MethodPost, "/groups/release/mcp", nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})
	for _, pattern := range patterns {
		method, path, found := strings.Cut(pattern, " ")
		if !found {
			method, path = http.MethodGet, pattern
		}
		switch path {
		case "/", "/health", "/ready", "/oauth/callback":
			continue // Public contracts are exercised separately below.
		}
		for strings.Contains(path, "{") {
			start, end := strings.Index(path, "{"), strings.Index(path, "}")
			path = path[:start] + "release" + path[end+1:]
		}
		t.Run(pattern, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, loopbackRequest(method, path, nil))
			require.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

func TestAuthHandler_RouteMatrix(t *testing.T) {
	for _, mode := range []struct{ name, kind, header string }{
		{"bearer", "bearer", ""},
		{"api-key", "api_key", ""},
		{"custom-header", "api_key", "X-Gateway-Key"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			s := NewServer(groupsGateway(), fstest.MapFS{
				"index.html":    {Data: []byte("public shell")},
				"assets/app.js": {Data: []byte("public asset")},
			})
			t.Cleanup(s.Close)
			credential := rand.Text()
			s.SetAuth(mode.kind, credential, mode.header)
			h := s.Handler()
			header := mode.header
			if header == "" {
				header = "Authorization"
			}
			for _, path := range []string{
				"/mcp", "/sse", "/message", "/api/status",
				"/groups/release/mcp", "/groups/release/sse", "/groups/unknown/mcp",
				"/groups/unknown/sse", "/groups/release/mcp/", "/groups/release/sse/",
				"/groups/release/%6dcp", "/%67roups/release/sse", "/groups/%72elease/mcp",
				"/groups/release%2Fother/mcp", "/groups/%2e%2e/mcp", "/groups%2Frelease/mcp",
				"//groups/release/mcp", "/x/../groups/release/mcp", "/%6dcp", "/api%2fstatus",
				"/api/var/%2e%2e", "/groups/%2e%2e/health", "/x/../mcp", "/%2fmcp",
			} {
				for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete, http.MethodHead, http.MethodPut, http.MethodPatch} {
					for _, value := range []string{"", rand.Text()} {
						t.Run(method+path, func(t *testing.T) {
							r := loopbackRequest(method, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
							if value != "" {
								if mode.kind == "bearer" {
									value = "Bearer " + value
								}
								r.Header.Set(header, value)
							}
							w := httptest.NewRecorder()
							h.ServeHTTP(w, r)
							require.Equal(t, http.StatusUnauthorized, w.Code)
							require.Empty(t, w.Header().Get("Location"))
							require.Empty(t, w.Header().Get("Mcp-Session-Id"))
						})
					}
				}
			}
			require.Zero(t, s.streamableServer.SessionCount())
			for _, path := range []string{"/", "/assets/app.js", "/tools", "/groups-lookalike/release/mcp", "/mcpx", "/apiary/status", "/health", "/ready"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, loopbackRequest(http.MethodGet, path, nil))
				want := http.StatusOK
				if path == "/ready" {
					want = http.StatusServiceUnavailable // No stack is loaded.
				}
				require.Equal(t, want, w.Code, path)
			}
			for _, path := range []string{"/mcp", "/groups/release/mcp", "/groups/release/sse", "/api/status"} {
				r := loopbackRequest(http.MethodOptions, path, strings.NewReader(`{"method":"initialize"}`))
				r.Header.Set("Origin", "https://untrusted.invalid")
				r.Header.Set("Access-Control-Request-Method", http.MethodPost)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				require.Equal(t, http.StatusOK, w.Code)
				require.Empty(t, w.Body.String())
				require.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
				require.Empty(t, w.Header().Get("Content-Type"))
			}
			require.Zero(t, s.streamableServer.SessionCount())
			for _, route := range []struct {
				method, path, body string
				status             int
			}{
				{http.MethodGet, "/sse", "POST /mcp", http.StatusOK},
				{http.MethodGet, "/groups/release/sse", "POST /groups/release/mcp", http.StatusOK},
				{http.MethodHead, "/groups/release/sse", "", http.StatusOK},
				{http.MethodPost, "/message", "legacy SSE", http.StatusGone},
				{http.MethodGet, "/api/status", `"gateway"`, http.StatusOK},
				{http.MethodPost, "/groups/unknown/mcp", "", http.StatusNotFound},
				{http.MethodGet, "/groups/unknown/sse", "", http.StatusNotFound},
				{http.MethodPost, "/groups/release/%6dcp", "Invalid JSON", http.StatusOK},
				{http.MethodGet, "/%67roups/release/sse", "POST /groups/release/mcp", http.StatusOK},
			} {
				r := loopbackRequest(route.method, route.path, nil)
				value := credential
				if mode.kind == "bearer" {
					value = "Bearer " + value
				}
				r.Header.Set(header, value)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				require.Equal(t, route.status, w.Code, route.path)
				require.Contains(t, w.Body.String(), route.body)
			}
			for _, path := range []string{"/mcp", "/groups/release/mcp"} {
				for _, defense := range []string{"Host", "Origin"} {
					r := loopbackRequest(http.MethodPost, path, strings.NewReader(`{"method":"initialize"}`))
					value := credential
					if mode.kind == "bearer" {
						value = "Bearer " + value
					}
					r.Header.Set(header, value)
					if defense == "Host" {
						r.Host = "untrusted.invalid"
					} else {
						r.Header.Set("Origin", "https://untrusted.invalid")
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					require.Equal(t, http.StatusForbidden, w.Code, defense)
				}
			}
		})
	}
}

func TestAuthHandler_ProtocolEras(t *testing.T) {
	for _, mode := range []struct{ kind, header string }{{"bearer", ""}, {"api_key", ""}, {"api_key", "X-Gateway-Key"}, {"", ""}} {
		for _, path := range []string{"/mcp", "/groups/release/mcp"} {
			for _, version := range mcp.SupportedProtocolVersions {
				t.Run(mode.kind+mode.header+path+version, func(t *testing.T) {
					s := NewServer(groupsGateway(), nil)
					t.Cleanup(s.Close)
					client := &authCountingClient{mockAgentClient: newMockAgentClient("github", []mcp.Tool{{Name: "create_issue"}})}
					s.gateway.Router().AddClient(client)
					toolName := "github__create_issue"
					if path != "/mcp" {
						toolName = "create_issue"
					}
					credential := rand.Text()
					if mode.kind != "" {
						s.SetAuth(mode.kind, credential, mode.header)
					}
					h := s.Handler()
					session := ""
					do := func(method, rpc, value string) *httptest.ResponseRecorder {
						params := fmt.Sprintf(`{"protocolVersion":%q,"clientInfo":{"name":"test","version":"1"},"capabilities":{}}`, version)
						if mcp.EraOfVersion(version) == mcp.EraStateless {
							params = fmt.Sprintf(`{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}`, version)
						}
						if rpc == "tools/call" {
							params = strings.TrimSuffix(params, "}") + fmt.Sprintf(`,"name":%q,"arguments":{}}`, toolName)
						}
						r := loopbackRequest(method, path, strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":%s}`, rpc, params)))
						r.Header.Set("Content-Type", "application/json")
						r.Header.Set("MCP-Protocol-Version", version)
						r.Header.Set("Mcp-Method", rpc)
						if rpc == "tools/call" {
							r.Header.Set("Mcp-Name", toolName)
						}
						r.Header.Set("Mcp-Session-Id", session)
						r.Header.Set("Last-Event-ID", "0")
						if value != "" && mode.kind != "" {
							header := mode.header
							if header == "" {
								header = "Authorization"
							}
							if mode.kind == "bearer" {
								value = "Bearer " + value
							}
							r.Header.Set(header, value)
						}
						// A canceled context bounds valid GET streams without hiding stream setup.
						ctx, cancel := context.WithCancel(r.Context())
						defer cancel()
						if method == http.MethodGet {
							cancel()
						}
						w := httptest.NewRecorder()
						h.ServeHTTP(w, r.WithContext(ctx))
						return w
					}
					initial := "initialize"
					modern := mcp.EraOfVersion(version) == mcp.EraStateless
					if modern {
						initial = "server/discover"
					}
					if mode.kind != "" {
						for _, value := range []string{"", rand.Text()} {
							require.Equal(t, http.StatusUnauthorized, do(http.MethodPost, initial, value).Code)
						}
						require.Zero(t, s.streamableServer.SessionCount())
					}
					w := do(http.MethodPost, initial, credential)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.NotContains(t, w.Body.String(), `"error"`)
					session = w.Header().Get("Mcp-Session-Id")
					if modern {
						require.Empty(t, session)
					} else {
						require.NotEmpty(t, session)
					}
					if mode.kind != "" {
						for _, value := range []string{"", rand.Text()} {
							for _, rpc := range []string{"tools/list", "tools/call", "notifications/initialized", "server/discover"} {
								require.Equal(t, http.StatusUnauthorized, do(http.MethodPost, rpc, value).Code)
							}
							for _, method := range []string{http.MethodGet, http.MethodDelete} {
								w := do(method, "", value)
								require.Equal(t, http.StatusUnauthorized, w.Code)
								require.NotEqual(t, "text/event-stream", w.Header().Get("Content-Type"))
							}
						}
					}
					w = do(http.MethodPost, "tools/list", credential)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.NotContains(t, w.Body.String(), `"error"`)
					require.Zero(t, client.calls.Load(), "unauthorized requests reached tool dispatch")
					w = do(http.MethodPost, "tools/call", credential)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.NotContains(t, w.Body.String(), `"error"`)
					require.Equal(t, int64(1), client.calls.Load())
					want := http.StatusOK
					if modern {
						want = http.StatusMethodNotAllowed
					}
					w = do(http.MethodGet, "", credential)
					require.Equal(t, want, w.Code)
					if !modern {
						require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
					}
					require.Equal(t, want, do(http.MethodDelete, "", credential).Code)
					require.Zero(t, s.streamableServer.SessionCount())
				})
			}
		}
	}
}

type authCountingClient struct {
	*mockAgentClient
	calls atomic.Int64
}

func (c *authCountingClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	c.calls.Add(1)
	return &mcp.ToolCallResult{Content: []mcp.Content{mcp.NewTextContent("called")}}, nil
}

func TestAuthHandler_CallbackBoundary(t *testing.T) {
	store, err := mcpauth.NewTokenStore(t.TempDir())
	require.NoError(t, err)
	s := NewServer(groupsGateway(), nil)
	t.Cleanup(s.Close)
	s.SetAuth("bearer", rand.Text(), "")
	s.SetOAuthBroker(mcpauth.NewBroker(store, "http://localhost:8180"+mcpauth.CallbackPath, nil))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/oauth/callback?code=x&state=unknown", http.StatusBadRequest},
		{http.MethodGet, "/oauth/callback/extra?code=x&state=unknown", http.StatusNotFound},
		{http.MethodGet, "/oauth/callback-lookalike?code=x&state=unknown", http.StatusNotFound},
		{http.MethodPost, "/oauth/callback?code=x&state=unknown", http.StatusNotFound},
		{http.MethodGet, "/api/auth/servers", http.StatusUnauthorized},
		{http.MethodPost, "/api/servers/release/auth/login", http.StatusUnauthorized},
		{http.MethodGet, "/oauth/callback/../../groups/release/sse", http.StatusUnauthorized},
		{http.MethodPost, "/x/../groups/release/mcp", http.StatusUnauthorized},
		{http.MethodGet, "//groups/release/sse", http.StatusUnauthorized},
		{http.MethodGet, "/groups/release/%73se", http.StatusUnauthorized},
	} {
		r, err := http.NewRequestWithContext(t.Context(), route.method, ts.URL+route.path, nil)
		require.NoError(t, err)
		resp, err := ts.Client().Do(r) // Follow any outer mux normalization redirects.
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, route.status, resp.StatusCode, route.path)
	}
	require.Zero(t, s.streamableServer.SessionCount())
}
