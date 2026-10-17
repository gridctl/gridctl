package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/secreport"
	"github.com/gridctl/gridctl/pkg/servercard"
)

func TestPublicDiscoveryRoutes_Invariants(t *testing.T) {
	s := NewServer(groupsGateway(), nil)
	t.Cleanup(s.Close)
	s.SetAuth("bearer", "secret", "")
	s.SetAllowedOrigins([]string{"https://ui.example"})
	s.SetStackName("lab")
	handler := s.Handler()

	for _, pattern := range discoveryRoutePatterns() {
		route := strings.ReplaceAll(pattern, "{name}", "release")
		t.Run(pattern, func(t *testing.T) {
			if isProtectedPath(route) || isProtectedPath(path.Clean(route)) {
				t.Fatalf("isProtectedPath(%s) = true", route)
			}
			req := loopbackRequest(http.MethodGet, route, nil)
			req.Header.Set("Origin", "https://other.example")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, body %s", route, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("ACAO = %q", got)
			}
			post := loopbackRequest(http.MethodPost, route, nil)
			post.Header.Set("Origin", "https://other.example")
			pw := httptest.NewRecorder()
			handler.ServeHTTP(pw, post)
			if pw.Code != http.StatusMethodNotAllowed {
				t.Fatalf("POST %s = %d", route, pw.Code)
			}
		})
	}

	// path.Clean("/api/../mcp/server-card") is the discovery route. A
	// dot-segment that cleans to an operational route stays protected.
	if isProtectedPath("/api/../mcp/server-card") || isProtectedPath(path.Clean("/api/../mcp/server-card")) {
		t.Fatal("dot-segment discovery path is protected")
	}
	if !isProtectedPath(path.Clean("/mcp/server-card/../../mcp")) {
		t.Fatal("cleaned operational path is not protected")
	}

	bad := []discoveryEntry{
		{pattern: "/mcp", method: http.MethodGet},
		{pattern: "/sse", method: http.MethodGet},
		{pattern: "/message", method: http.MethodGet},
		{pattern: "/groups/{name}/mcp", method: http.MethodGet},
		{pattern: "/groups/{name}/sse", method: http.MethodGet},
		{pattern: "/api/status", method: http.MethodGet},
		{pattern: "/a2a/x", method: http.MethodGet},
		{pattern: "/mcp/server-card", method: http.MethodPost},
	}
	for _, entry := range bad {
		if _, err := newDiscoveryTable([]discoveryEntry{entry}); err == nil {
			t.Errorf("newDiscoveryTable(%s %s) succeeded", entry.method, entry.pattern)
		}
	}
}

func TestCORSMiddleware_AllowlistUnlistedOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware([]string{"https://ui.example"}, nil, next)
	for _, path := range []string{"/mcp", "/api/status"} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", "https://untrusted.invalid")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("OPTIONS %s set ACAO %q", path, w.Header().Get("Access-Control-Allow-Origin"))
		}
		get := httptest.NewRequest(http.MethodGet, path, nil)
		get.Header.Set("Origin", "https://untrusted.invalid")
		gw := httptest.NewRecorder()
		h.ServeHTTP(gw, get)
		if gw.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("GET %s set ACAO %q", path, gw.Header().Get("Access-Control-Allow-Origin"))
		}
	}
}

func TestSecurityReport_OriginsMode(t *testing.T) {
	s := NewServer(mcp.NewGateway(), nil)
	t.Cleanup(s.Close)
	s.SetStartupOrigins("allowlist", 1)
	s.SetGatewayAddr("http://localhost:8180")
	report := s.passiveSecurityReport(context.Background())
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "https://ui.example") || strings.Contains(string(raw), "allowed_origins") {
		t.Fatalf("report contains an origin string: %s", raw)
	}
	var found bool
	for _, check := range report.Checks {
		if check.Predicate != secreport.PredGatewayOriginsStartup {
			continue
		}
		found = true
		if check.Facts.OriginsMode != "allowlist" {
			t.Fatalf("mode = %q", check.Facts.OriginsMode)
		}
		if len(check.Facts.DiscoveryRoutes) != 3 {
			t.Fatalf("routes = %#v", check.Facts.DiscoveryRoutes)
		}
	}
	if !found {
		t.Fatal("missing gateway.origins.startup")
	}

	fileReport := secreport.Assemble(report.GeneratedAt, secreport.Inputs{})
	found = false
	for _, check := range fileReport.Checks {
		if check.Predicate == secreport.PredGatewayOriginsStartup {
			found = true
			if check.Outcome != secreport.OutcomeUnknown || check.ReasonCode != "startup_unavailable" {
				t.Fatalf("file-only check = %s %s", check.Outcome, check.ReasonCode)
			}
		}
	}
	if !found {
		t.Fatal("nil startup omitted gateway.origins.startup")
	}
}

func TestCatalogIdentifier_KeepsDottedSegment(t *testing.T) {
	gw := mcp.NewGateway()
	gw.SetName("acme.mcp")
	s := NewServer(gw, nil)
	t.Cleanup(s.Close)
	s.SetServerCard("", "https://gw.example")
	req := loopbackRequest(http.MethodGet, "/.well-known/ai-catalog.json", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"identifier":"urn:air:gw.example:mcp:acme.mcp"`) {
		t.Fatalf("catalog = %s", w.Body.String())
	}
}

func TestServerCard_UntrustedHostOmitsRemotes(t *testing.T) {
	s := NewServer(groupsGateway(), nil)
	t.Cleanup(s.Close)
	s.SetAuth("bearer", "secret", "")
	s.SetStackName("lab")
	req := httptest.NewRequest(http.MethodGet, "/mcp/server-card", nil)
	req.Host = "evil.example"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 8180}))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var card servercard.ServerCard
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if len(card.Remotes) != 0 {
		t.Fatalf("remotes = %#v", card.Remotes)
	}
	cw := httptest.NewRecorder()
	creq := req.Clone(req.Context())
	creq.URL.Path = "/.well-known/ai-catalog.json"
	s.Handler().ServeHTTP(cw, creq)
	if !strings.Contains(cw.Body.String(), `"entries":[]`) {
		t.Fatalf("catalog = %s", cw.Body.String())
	}

	s.SetServerCard("", "https://gw.example/prefix")
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, req)
	var pinned servercard.ServerCard
	if err := json.Unmarshal(w2.Body.Bytes(), &pinned); err != nil {
		t.Fatal(err)
	}
	if len(pinned.Remotes) != 1 || !strings.HasPrefix(pinned.Remotes[0].URL, "https://gw.example/prefix/mcp") {
		t.Fatalf("public_url remote = %#v", pinned.Remotes)
	}
	if w2.Header().Get("Vary") != "" {
		t.Fatalf("public_url response varied: %q", w2.Header().Get("Vary"))
	}
}
