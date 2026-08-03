package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// foreignHostRequest builds a request whose Host names somewhere other than
// this machine, which is what a DNS-rebound browser sends.
func foreignHostRequest(t *testing.T, target string, local net.Addr) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = "evil.example.com"
	if local != nil {
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))
	}
	return r
}

func TestAPIHostGate_RejectsForeignHostOnRESTRoutes(t *testing.T) {
	// Before the mux-wide gate these answered 200, which is the whole defect:
	// /mcp was protected while ~90 REST routes were not.
	srv := NewServer(mcp.NewGateway(), nil)
	handler := srv.Handler()

	for _, path := range []string{"/api/status", "/api/mcp-servers", "/api/tools"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, foreignHostRequest(t, path, nil))

			if w.Code != http.StatusForbidden {
				t.Errorf("expected 403 for foreign Host on %s, got %d", path, w.Code)
			}
		})
	}
}

func TestAPIHostGate_AllowsLoopbackHost(t *testing.T) {
	srv := NewServer(mcp.NewGateway(), nil)
	handler := srv.Handler()

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, loopbackRequest(http.MethodGet, "/api/status", nil))

	if w.Code == http.StatusForbidden {
		t.Errorf("loopback Host must not be rejected, got 403: %s", w.Body.String())
	}
}

func TestAPIHostGate_LivenessProbesExempt(t *testing.T) {
	// The daemon parent polls these before anything else is known to work,
	// and it must not need to guess the right Host to do so.
	srv := NewServer(mcp.NewGateway(), nil)
	handler := srv.Handler()

	for _, path := range []string{"/health", "/ready"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, foreignHostRequest(t, path, nil))

			if w.Code == http.StatusForbidden {
				t.Errorf("%s must answer regardless of Host, got 403", path)
			}
		})
	}
}

func TestAPIHostGate_NonLoopbackArrivalUnaffected(t *testing.T) {
	// A deployment deliberately widened with --bind must keep serving remote
	// clients, who legitimately address it by their own name for it.
	srv := NewServer(mcp.NewGateway(), nil)
	handler := srv.Handler()

	local := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 5), Port: 8180}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, foreignHostRequest(t, "/api/status", local))

	if w.Code == http.StatusForbidden {
		t.Errorf("non-loopback arrival must not be gated on Host, got 403")
	}
}

func TestAPIHostGate_AllowedHostsAccepted(t *testing.T) {
	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetAllowedHosts([]string{"evil.example.com"})
	handler := srv.Handler()

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, foreignHostRequest(t, "/api/status", nil))

	if w.Code == http.StatusForbidden {
		t.Errorf("configured allowed host must be accepted, got 403")
	}
}
