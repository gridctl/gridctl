package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/probe"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// recordingClient is a minimal AgentClient for handler tests that need the
// real probe.Prober path (cache behavior, error codes). For tests where the
// probe orchestration itself is already covered, we use stubProber below.
type recordingClient struct {
	tools    []mcp.Tool
	initErr  error
	listErr  error
	initWait time.Duration
}

func (c *recordingClient) Name() string { return "rec" }
func (c *recordingClient) Initialize(ctx context.Context) error {
	if c.initWait > 0 {
		select {
		case <-time.After(c.initWait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.initErr
}
func (c *recordingClient) RefreshTools(context.Context) error { return c.listErr }
func (c *recordingClient) Tools() []mcp.Tool                   { return c.tools }
func (c *recordingClient) IsInitialized() bool                 { return true }
func (c *recordingClient) ServerInfo() mcp.ServerInfo          { return mcp.ServerInfo{} }
func (c *recordingClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return nil, errors.New("not implemented")
}

type fixedFactory struct{ client mcp.AgentClient }

func (f fixedFactory) NewHTTP(string, string) mcp.AgentClient { return f.client }
func (f fixedFactory) NewProcess(string, []string, string, map[string]string) mcp.AgentClient {
	return f.client
}

func newProbeServer(t *testing.T, client mcp.AgentClient) *Server {
	t.Helper()
	srv := newTestServer(t)
	p := probe.NewProber(probe.NewCache(time.Minute), nil)
	p.SetClientFactory(fixedFactory{client: client})
	srv.SetProber(p)
	return srv
}

func postProbe(t *testing.T, handler http.Handler, body any, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/servers/probe", bytes.NewReader(b))
	if sessionID != "" {
		req.Header.Set("X-Session-ID", sessionID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestProbeHandler_ExternalURL_Success(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	srv := newProbeServer(t, &recordingClient{tools: []mcp.Tool{
		{Name: "search", Description: "web search", InputSchema: schema},
	}})
	rec := postProbe(t, srv.Handler(), map[string]any{
		"url": "https://example.com/mcp",
	}, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var got probeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "search" {
		t.Fatalf("tools mismatch: %+v", got.Tools)
	}
	if got.ProbedAt == "" {
		t.Fatalf("probedAt missing")
	}
	if got.Cached {
		t.Fatalf("first probe should not be cached")
	}
}

func TestProbeHandler_CacheHit(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{tools: []mcp.Tool{{Name: "a"}}})
	handler := srv.Handler()
	body := map[string]any{"url": "https://example.com/mcp"}
	_ = postProbe(t, handler, body, "")
	rec := postProbe(t, handler, body, "")
	var got probeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Cached {
		t.Fatalf("expected cached=true on second call")
	}
}

func TestProbeHandler_InitializeFailure_422WithCode(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{initErr: errors.New("bad")})
	rec := postProbe(t, srv.Handler(), map[string]any{"url": "https://example.com/mcp"}, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
	var errBody probeErrorWire
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errBody.Error.Code != probe.CodeInitializeFailed {
		t.Fatalf("want code=%q, got %q", probe.CodeInitializeFailed, errBody.Error.Code)
	}
}

func TestProbeHandler_InvalidConfig_400(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{})
	rec := postProbe(t, srv.Handler(), map[string]any{"name": "no-transport"}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var errBody probeErrorWire
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Code != probe.CodeInvalidConfig {
		t.Fatalf("want %q, got %q", probe.CodeInvalidConfig, errBody.Error.Code)
	}
}

func TestProbeHandler_Timeout(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{initWait: 500 * time.Millisecond})
	rec := postProbe(t, srv.Handler(), map[string]any{
		"url":           "https://example.com/mcp",
		"ready_timeout": "30ms",
	}, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
	var errBody probeErrorWire
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Code != probe.CodeProbeTimeout {
		t.Fatalf("want probe_timeout, got %q (msg=%q)", errBody.Error.Code, errBody.Error.Message)
	}
}

func TestProbeHandler_UnsupportedTransport_SSH(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{})
	rec := postProbe(t, srv.Handler(), map[string]any{
		"ssh":     map[string]any{"host": "h", "user": "u"},
		"command": []string{"/bin/sh"},
	}, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
	var errBody probeErrorWire
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if errBody.Error.Code != probe.CodeUnsupportedTransport {
		t.Fatalf("want %q, got %q", probe.CodeUnsupportedTransport, errBody.Error.Code)
	}
}

func TestProbeHandler_UnsupportedTransport_OpenAPI(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{})
	rec := postProbe(t, srv.Handler(), map[string]any{
		"openapi": map[string]any{"spec": "https://api.example.com/openapi.json"},
	}, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
}

func TestProbeHandler_SecretScrubbing(t *testing.T) {
	// An error string that includes the env value must have that value
	// replaced with *** before the response is serialized.
	srv := newProbeServer(t, &recordingClient{initErr: errors.New("token=supersecret-123 rejected")})
	rec := postProbe(t, srv.Handler(), map[string]any{
		"url": "https://example.com/mcp",
		"env": map[string]string{"API_TOKEN": "supersecret-123"},
	}, "")
	var errBody probeErrorWire
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if strings.Contains(errBody.Error.Message, "supersecret-123") {
		t.Fatalf("secret leaked into message: %q", errBody.Error.Message)
	}
	if !strings.Contains(errBody.Error.Message, "***") {
		t.Fatalf("expected *** in scrubbed message, got %q", errBody.Error.Message)
	}
}

func TestProbeHandler_MethodNotAllowed(t *testing.T) {
	srv := newProbeServer(t, &recordingClient{})
	req := httptest.NewRequest(http.MethodGet, "/api/servers/probe", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestProbeHandler_NoProber_503(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/servers/probe", strings.NewReader(`{"url":"x"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when prober not configured, got %d", rec.Code)
	}
}

func TestProbeHandler_SessionConcurrencyCap(t *testing.T) {
	// A client that blocks on Initialize lets us observe the semaphore: 3
	// in-flight are accepted, the 4th gets 429.
	block := make(chan struct{})
	client := &slowClient{started: &atomic.Int32{}, release: block}
	srv := newProbeServer(t, client)
	handler := srv.Handler()

	body, _ := json.Marshal(map[string]any{"url": "https://example.com/mcp"})
	var wg sync.WaitGroup
	results := make(chan int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/servers/probe", bytes.NewReader(body))
			req.Header.Set("X-Session-ID", "s1")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			results <- rec.Code
		}()
	}

	// Wait for 3 to be in-flight, then release all 4 (including the rejected one).
	for client.started.Load() < 3 {
		time.Sleep(time.Millisecond)
	}
	close(block)
	wg.Wait()
	close(results)

	codes := map[int]int{}
	for c := range results {
		codes[c]++
	}
	if codes[http.StatusTooManyRequests] == 0 {
		t.Fatalf("expected at least one 429, got %+v", codes)
	}
}

// slowClient blocks Initialize until release is closed. Used to hold slots
// open while exercising the concurrency cap.
type slowClient struct {
	started *atomic.Int32
	release chan struct{}
}

func (s *slowClient) Name() string { return "slow" }
func (s *slowClient) Initialize(ctx context.Context) error {
	s.started.Add(1)
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *slowClient) RefreshTools(context.Context) error { return nil }
func (s *slowClient) Tools() []mcp.Tool                  { return nil }
func (s *slowClient) IsInitialized() bool                { return true }
func (s *slowClient) ServerInfo() mcp.ServerInfo         { return mcp.ServerInfo{} }
func (s *slowClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return nil, errors.New("not implemented")
}

// Round-trip check: the snake_case wire fields the frontend sends must decode
// into the handler's probeRequest and convert to a config.MCPServer with the
// values intact.
func TestProbeHandler_RequestShape(t *testing.T) {
	req := map[string]any{
		"name":          "web",
		"url":           "https://example.com/mcp",
		"transport":     "http",
		"tools":         []string{"a", "b"},
		"ready_timeout": "2s",
		"build_args":    map[string]string{"FOO": "1"},
	}
	b, _ := json.Marshal(req)
	var pr probeRequest
	if err := json.Unmarshal(b, &pr); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	cfg := pr.toMCPServer()
	if cfg.URL != "https://example.com/mcp" || cfg.Name != "web" {
		t.Fatalf("fields didn't decode: %+v", cfg)
	}
	if cfg.ReadyTimeout != "2s" {
		t.Fatalf("ready_timeout didn't map: got %q", cfg.ReadyTimeout)
	}
	if cfg.BuildArgs["FOO"] != "1" {
		t.Fatalf("build_args didn't map: %+v", cfg.BuildArgs)
	}
}
