package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/docker/docker/api/types/container"
	"github.com/gridctl/gridctl/pkg/dockerclient"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

// pendingTestMCPHandler is a minimal MCP HTTP server: enough for Ping,
// Initialize, and tools/list, mirroring the legacy branch of
// newGenerationFlipServer.
func pendingTestMCPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "pending-session")
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, InitializeResult{
				ProtocolVersion: "2025-11-25",
				ServerInfo:      ServerInfo{Name: "pending-test", Version: "1.0"},
			}))
		case "tools/list":
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, ToolsListResult{Tools: []Tool{{Name: "pending-tool"}}}))
		default:
			_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(req.ID, nil))
		}
	})
}

// reserveAddr returns a localhost address that had a free port at call time
// and has no listener on it afterward.
func reserveAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// startServerAt starts an MCP test server on the exact address a prior
// registration attempt failed against, modeling a backend that comes up late.
func startServerAt(t *testing.T, addr string) *httptest.Server {
	t.Helper()
	var l net.Listener
	var err error
	for i := 0; i < 40; i++ {
		l, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: pendingTestMCPHandler(), ReadHeaderTimeout: 5 * time.Second}}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func externalHTTPConfig(name, addr string) MCPServerConfig {
	return MCPServerConfig{
		Name:      name,
		Transport: TransportHTTP,
		Endpoint:  "http://" + addr,
		External:  true,
		// >= 1s: waitForHTTPServer's first ping lands at the 500ms poll
		// interval, so a shorter window can never observe a live server.
		ReadyTimeout: 1 * time.Second,
	}
}

// statusFor returns the status row for name, or nil.
func statusFor(g *Gateway, name string) *MCPServerStatus {
	for _, s := range g.Status() {
		if s.Name == name {
			out := s
			return &out
		}
	}
	return nil
}

func TestGateway_PendingRetry_RecoversWhenServerComesUp(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()
	ctx := context.Background()

	err := g.RegisterMCPServer(ctx, externalHTTPConfig("late", addr))
	if err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}
	// Mirror the controller's recordOutcome: the caller surfaces failures.
	g.RecordRegistrationFailure("late", err)

	st := statusFor(g, "late")
	if st == nil || !st.RegistrationFailed {
		t.Fatal("expected a RegistrationFailed status row after the failed attempt")
	}
	if !strings.Contains(st.HealthError, "retrying") {
		t.Errorf("expected the failure message to carry a retry hint, got %q", st.HealthError)
	}

	startServerAt(t, addr)
	g.retryPendingRegistrations(ctx)

	deadline := time.Now().Add(15 * time.Second)
	for {
		st = statusFor(g, "late")
		if st != nil && !st.RegistrationFailed && st.Initialized && st.ToolCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never recovered; last status: %+v", st)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if g.Router().GetReplicaSet("late") == nil {
		t.Error("expected the recovered server to be routable")
	}
	if _, _, ok := g.pendingSnapshot("late"); ok {
		t.Error("expected the pending entry to be cleared after recovery")
	}
}

func TestGateway_PendingRetry_ImmediateScanOnMonitorStart(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := g.RegisterMCPServer(ctx, externalHTTPConfig("early-bird", addr)); err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}
	startServerAt(t, addr)

	// A one-hour interval proves recovery comes from the immediate first
	// scan, not from a ticker fire.
	g.StartHealthMonitor(ctx, time.Hour)

	deadline := time.Now().Add(15 * time.Second)
	for {
		if st := statusFor(g, "early-bird"); st != nil && !st.RegistrationFailed && st.Initialized {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("immediate pending scan on monitor start never recovered the server")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestGateway_PendingRetry_StaleGenerationDoesNotResurrect(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()
	ctx := context.Background()
	cfg := externalHTTPConfig("removed", addr)

	if err := g.RegisterMCPServer(ctx, cfg); err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}

	// Capture the generation a retry launched now would hold, then remove
	// the server while that attempt is conceptually in flight.
	g.pendingMu.Lock()
	gen := g.regGen["removed"]
	g.pendingMu.Unlock()
	g.UnregisterMCPServer("removed")

	startServerAt(t, addr)
	err := g.registerReplicaSet(ctx, "removed", ReplicaPolicyRoundRobin, []MCPServerConfig{cfg}, gen)
	if !errors.Is(err, errStaleRegistration) {
		t.Fatalf("expected errStaleRegistration, got %v", err)
	}

	g.mu.RLock()
	_, hasMeta := g.serverMeta["removed"]
	g.mu.RUnlock()
	if hasMeta {
		t.Error("stale retry must not write serverMeta for a removed server")
	}
	if g.Router().GetReplicaSet("removed") != nil {
		t.Error("stale retry must not register a removed server with the router")
	}
}

func TestGateway_Unregister_DropsPending(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()
	ctx := context.Background()

	if err := g.RegisterMCPServer(ctx, externalHTTPConfig("gone", addr)); err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}
	if _, _, ok := g.pendingSnapshot("gone"); !ok {
		t.Fatal("expected a pending entry after the failed attempt")
	}
	g.UnregisterMCPServer("gone")
	if _, _, ok := g.pendingSnapshot("gone"); ok {
		t.Error("expected UnregisterMCPServer to drop the pending entry")
	}
}

func TestGateway_RestartMCPServer_PendingAttemptsImmediately(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()
	ctx := context.Background()

	if err := g.RegisterMCPServer(ctx, externalHTTPConfig("late-restart", addr)); err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}

	// Restart while the endpoint is still down: an immediate attempt that
	// fails, never an "unknown MCP server" 404, and the server stays pending.
	err := g.RestartMCPServer(ctx, "late-restart")
	if err == nil {
		t.Fatal("expected restart to fail while the endpoint is down")
	}
	if strings.Contains(err.Error(), "unknown MCP server") {
		t.Fatalf("restart of a pending server must not report unknown, got %v", err)
	}
	if _, _, ok := g.pendingSnapshot("late-restart"); !ok {
		t.Fatal("expected the server to stay pending after a failed restart")
	}

	startServerAt(t, addr)
	if err := g.RestartMCPServer(ctx, "late-restart"); err != nil {
		t.Fatalf("restart with the endpoint up: %v", err)
	}
	st := statusFor(g, "late-restart")
	if st == nil || st.RegistrationFailed || !st.Initialized {
		t.Fatalf("expected a registered server after restart, got %+v", st)
	}

	// Genuinely unknown names still error the old way.
	if err := g.RestartMCPServer(ctx, "never-heard-of-it"); err == nil || !strings.Contains(err.Error(), "unknown MCP server") {
		t.Errorf("expected unknown MCP server error, got %v", err)
	}
}

func TestGateway_PendingSkip_AuthTerminalAndCancel(t *testing.T) {
	g := NewGateway()
	cfgs := []MCPServerConfig{{Name: "x", Transport: TransportHTTP, Endpoint: "http://127.0.0.1:1"}}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"auth-required", &AuthRequiredError{Status: 401}, false},
		{"needs-auth", &NeedsAuthError{Server: "x"}, false},
		{"terminal", terminalRegistration(errors.New("unknown transport: bogus")), false},
		{"cancelled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"retryable", errors.New("connection refused"), true},
	}
	for _, tc := range cases {
		g.notePendingRegistrationFailure(tc.name, ReplicaPolicyRoundRobin, cfgs, tc.err)
		_, _, got := g.pendingSnapshot(tc.name)
		if got != tc.want {
			t.Errorf("%s: pending = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestGateway_PendingSkip_TerminalConfigEndToEnd(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()

	if err := g.RegisterMCPServer(ctx, MCPServerConfig{Name: "weird", Transport: Transport("bogus")}); err == nil {
		t.Fatal("expected unknown-transport registration to fail")
	}
	if _, _, ok := g.pendingSnapshot("weird"); ok {
		t.Error("unknown transport is terminal and must not enter the retry loop")
	}

	if err := g.RegisterMCPServer(ctx, MCPServerConfig{Name: "oapi", OpenAPI: true}); err == nil {
		t.Fatal("expected OpenAPI registration without config to fail")
	}
	if _, _, ok := g.pendingSnapshot("oapi"); ok {
		t.Error("missing OpenAPI config is terminal and must not enter the retry loop")
	}
}

func TestGateway_PendingSkip_WhenReadyCleanupRan(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()
	ctx := context.Background()

	var cleaned atomic.Bool
	cfg := externalHTTPConfig("cleaned-up", addr)
	cfg.External = false
	cfg.CleanupOnReadyFailure = func(context.Context) error {
		cleaned.Store(true)
		return nil
	}

	if err := g.RegisterMCPServer(ctx, cfg); err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}
	if !cleaned.Load() {
		t.Fatal("expected the ready-failure cleanup to run")
	}
	if _, _, ok := g.pendingSnapshot("cleaned-up"); ok {
		t.Error("a server whose container was cleaned up must not enter the retry loop")
	}
	g.pendingMu.Lock()
	leftover := len(g.cleanupRan)
	g.pendingMu.Unlock()
	if leftover != 0 {
		t.Errorf("expected the cleanupRan flag to be consumed, %d left", leftover)
	}
}

func TestGateway_PendingKept_WhenCleanupDidNotRun(t *testing.T) {
	// The endpoint answers pings (readiness passes) but returns garbage to
	// initialize: the failure happens after readiness, cleanup never runs,
	// and the server stays retryable even though a cleanup closure exists.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	g := NewGateway()
	var cleaned atomic.Bool
	cfg := MCPServerConfig{
		Name:                  "still-up",
		Transport:             TransportHTTP,
		Endpoint:              srv.URL,
		ReadyTimeout:          1 * time.Second,
		CleanupOnReadyFailure: func(context.Context) error { cleaned.Store(true); return nil },
	}

	if err := g.RegisterMCPServer(context.Background(), cfg); err == nil {
		t.Fatal("expected registration to fail at initialize")
	}
	if cleaned.Load() {
		t.Error("cleanup must not run for a failure after readiness")
	}
	if _, _, ok := g.pendingSnapshot("still-up"); !ok {
		t.Error("an initialize failure with the container still up must stay retryable")
	}
}

func TestGateway_PendingSkip_MixedReplicaSuccess(t *testing.T) {
	srv := httptest.NewServer(pendingTestMCPHandler())
	defer srv.Close()
	deadAddr := reserveAddr(t)

	g := NewGateway()
	good := MCPServerConfig{Name: "mixed", Transport: TransportHTTP, Endpoint: srv.URL, External: true, ReadyTimeout: 1 * time.Second}
	bad := externalHTTPConfig("mixed", deadAddr)

	if err := g.RegisterMCPReplicaSet(context.Background(), "mixed", ReplicaPolicyRoundRobin, []MCPServerConfig{good, bad}); err != nil {
		t.Fatalf("partial-startup tolerance should register the surviving replica: %v", err)
	}
	if _, _, ok := g.pendingSnapshot("mixed"); ok {
		t.Error("a mixed-success replica set must not enter the retry loop")
	}
	if g.Router().GetReplicaSet("mixed") == nil {
		t.Error("expected the surviving replica to be routable")
	}
}

func TestGateway_RegisterReplaceClosesPreviousClients(t *testing.T) {
	// A commit that replaces a live replica set for the same name (a manual
	// restart of a pending server racing the background retry) must close
	// the replaced set's clients rather than leak them.
	srv := httptest.NewServer(pendingTestMCPHandler())
	defer srv.Close()

	ctrl := gomock.NewController(t)
	g := NewGateway()

	var closed atomic.Bool
	mock := setupMockAgentClient(ctrl, "dup", []Tool{{Name: "old-tool"}})
	g.Router().AddClient(&closableClient{
		AgentClient: mock,
		closeFn:     func() error { closed.Store(true); return nil },
	})
	g.SetServerMeta(MCPServerConfig{Name: "dup", Transport: TransportHTTP})

	cfg := MCPServerConfig{Name: "dup", Transport: TransportHTTP, Endpoint: srv.URL, External: true, ReadyTimeout: 1 * time.Second}
	if err := g.RegisterMCPServer(context.Background(), cfg); err != nil {
		t.Fatalf("re-register over a live set: %v", err)
	}
	if !closed.Load() {
		t.Error("expected the replaced set's client to be closed")
	}
}

func TestGateway_RestartMCPServer_CancelKeepsPending(t *testing.T) {
	addr := reserveAddr(t)
	g := NewGateway()

	if err := g.RegisterMCPServer(context.Background(), externalHTTPConfig("flaky-restart", addr)); err == nil {
		t.Fatal("expected registration to fail while the endpoint is down")
	}

	// A restart whose request context dies mid-attempt (client disconnect)
	// must not permanently exit the retry loop.
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.RestartMCPServer(cctx, "flaky-restart"); err == nil {
		t.Fatal("expected cancelled restart to fail")
	}
	if _, _, ok := g.pendingSnapshot("flaky-restart"); !ok {
		t.Fatal("cancelled restart must not drop the pending entry")
	}

	startServerAt(t, addr)
	if err := g.RestartMCPServer(context.Background(), "flaky-restart"); err != nil {
		t.Fatalf("restart after the endpoint came up: %v", err)
	}
}

func TestGateway_Unregister_RacingRetryCommitDoesNotResurrect(t *testing.T) {
	// Unregister must take ownership (generation bump) before teardown so a
	// retry commit racing it can never re-add the server after removal.
	srv := httptest.NewServer(pendingTestMCPHandler())
	defer srv.Close()
	ctx := context.Background()

	for i := 0; i < 15; i++ {
		g := NewGateway()
		cfg := MCPServerConfig{Name: "race", Transport: TransportHTTP, Endpoint: srv.URL, External: true, ReadyTimeout: 1 * time.Second}
		g.pendingMu.Lock()
		g.regGen["race"] = 1
		g.pending["race"] = &pendingRegistration{policy: ReplicaPolicyRoundRobin, cfgs: []MCPServerConfig{cfg}, backoff: &backoffState{}}
		g.pendingMu.Unlock()

		g.retryPendingRegistrations(ctx)
		time.Sleep(time.Duration(i%5) * time.Millisecond)
		g.UnregisterMCPServer("race")

		// Once Unregister returns, any later commit must abort on the bumped
		// generation; the settle sleep catches a late resurrection.
		time.Sleep(200 * time.Millisecond)
		g.mu.RLock()
		_, hasMeta := g.serverMeta["race"]
		g.mu.RUnlock()
		if hasMeta || g.Router().GetReplicaSet("race") != nil {
			t.Fatalf("round %d: unregistered server was resurrected by a racing retry commit", i)
		}
	}
}

// restartRecorder stubs just the container-restart call the pending retry
// path makes; every other DockerClient method panics via the nil embed,
// which the failure-path test never reaches.
type restartRecorder struct {
	dockerclient.DockerClient
	restarted atomic.Bool
	err       error
}

func (r *restartRecorder) ContainerRestart(_ context.Context, _ string, _ container.StopOptions) error {
	r.restarted.Store(true)
	return r.err
}

func TestGateway_PendingRetry_RestartsStdioContainer(t *testing.T) {
	g := NewGateway()
	rec := &restartRecorder{err: errors.New("daemon busy")}
	g.SetDockerClient(rec)

	cfg := MCPServerConfig{Name: "stdio-pending", Transport: TransportStdio, ContainerID: "abc123"}
	g.pendingMu.Lock()
	g.regGen["stdio-pending"] = 1
	g.pending["stdio-pending"] = &pendingRegistration{policy: ReplicaPolicyRoundRobin, cfgs: []MCPServerConfig{cfg}, backoff: &backoffState{}}
	g.pendingMu.Unlock()

	g.retryPendingRegistrations(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	for !rec.restarted.Load() {
		if time.Now().After(deadline) {
			t.Fatal("pending retry never attempted the container restart")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The failed restart advances backoff and keeps the entry pending.
	deadline = time.Now().Add(5 * time.Second)
	for {
		g.pendingMu.Lock()
		pe := g.pending["stdio-pending"]
		settled := pe != nil && !pe.inFlight && pe.backoff.Attempts() > 0
		g.pendingMu.Unlock()
		if settled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed container restart must advance backoff and keep the entry pending")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
