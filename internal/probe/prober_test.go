package probe

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// stubClient is a controllable AgentClient for probe tests. It records which
// lifecycle methods the prober calls so we can assert on them.
type stubClient struct {
	name         string
	initErr      error
	listErr      error
	initDelay    time.Duration
	listDelay    time.Duration
	tools        []mcp.Tool
	initialized  bool
	closeCalled  atomic.Bool
	initCalled   atomic.Bool
	listCalled   atomic.Bool
}

func (c *stubClient) Name() string { return c.name }
func (c *stubClient) Initialize(ctx context.Context) error {
	c.initCalled.Store(true)
	if c.initDelay > 0 {
		select {
		case <-time.After(c.initDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if c.initErr != nil {
		return c.initErr
	}
	c.initialized = true
	return nil
}
func (c *stubClient) RefreshTools(ctx context.Context) error {
	c.listCalled.Store(true)
	if c.listDelay > 0 {
		select {
		case <-time.After(c.listDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return c.listErr
}
func (c *stubClient) Tools() []mcp.Tool                          { return c.tools }
func (c *stubClient) IsInitialized() bool                        { return c.initialized }
func (c *stubClient) ServerInfo() mcp.ServerInfo                 { return mcp.ServerInfo{} }
func (c *stubClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return nil, errors.New("not implemented")
}
func (c *stubClient) Close() error { c.closeCalled.Store(true); return nil }

// stubFactory returns the same stubClient for both HTTP and process
// constructors so tests don't need to discriminate.
type stubFactory struct{ client *stubClient }

func (f *stubFactory) NewHTTP(string, string) mcp.AgentClient { return f.client }
func (f *stubFactory) NewProcess(string, []string, string, map[string]string) mcp.AgentClient {
	return f.client
}

// fakeSpawner is a Spawner stub that records spawn / release calls.
type fakeSpawner struct {
	spawnErr      error
	releaseCalled atomic.Int32
	endpoint      string
}

func (s *fakeSpawner) SpawnContainer(context.Context, config.MCPServer) (string, string, func(context.Context), error) {
	release := func(context.Context) { s.releaseCalled.Add(1) }
	if s.spawnErr != nil {
		return "", "", release, s.spawnErr
	}
	endpoint := s.endpoint
	if endpoint == "" {
		endpoint = "http://127.0.0.1:0"
	}
	return endpoint, "fake-container-id", release, nil
}

func rawSchema(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	return b
}

func newProberWithStub(t *testing.T, client *stubClient, spawner Spawner) *Prober {
	t.Helper()
	p := NewProber(NewCache(time.Minute), spawner)
	p.SetClientFactory(&stubFactory{client: client})
	return p
}

func TestProbe_ExternalURL_HappyPath(t *testing.T) {
	client := &stubClient{
		tools: []mcp.Tool{
			{Name: "search", Description: "search the web", InputSchema: rawSchema(t)},
			{Name: "fetch", Description: "fetch a URL", InputSchema: rawSchema(t)},
		},
	}
	p := newProberWithStub(t, client, nil)
	res, err := p.Probe(context.Background(), config.MCPServer{Name: "web", URL: "https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(res.Tools))
	}
	if res.Cached {
		t.Errorf("first probe should not be cached")
	}
	if !client.initCalled.Load() || !client.listCalled.Load() {
		t.Errorf("expected Initialize and RefreshTools to be called")
	}
}

func TestProbe_LocalProcess_HappyPath(t *testing.T) {
	client := &stubClient{tools: []mcp.Tool{{Name: "run"}}}
	p := newProberWithStub(t, client, nil)
	res, err := p.Probe(context.Background(), config.MCPServer{
		Name:    "proc",
		Command: []string{"/usr/bin/my-server"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(res.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(res.Tools))
	}
}

func TestProbe_InitializeFailure(t *testing.T) {
	client := &stubClient{initErr: errors.New("bad handshake")}
	p := newProberWithStub(t, client, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{URL: "https://example.com/mcp"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Code != CodeInitializeFailed {
		t.Errorf("want %q, got %q", CodeInitializeFailed, err.Code)
	}
	if !client.closeCalled.Load() {
		t.Errorf("expected client Close to be called on init failure")
	}
}

func TestProbe_ListFailure(t *testing.T) {
	client := &stubClient{listErr: errors.New("no tools method")}
	p := newProberWithStub(t, client, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{URL: "https://example.com/mcp"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Code != CodeToolsListFailed {
		t.Errorf("want %q, got %q", CodeToolsListFailed, err.Code)
	}
	if !client.closeCalled.Load() {
		t.Errorf("expected client Close to be called on list failure")
	}
}

func TestProbe_Timeout(t *testing.T) {
	client := &stubClient{initDelay: 500 * time.Millisecond}
	p := newProberWithStub(t, client, nil)
	cfg := config.MCPServer{URL: "https://example.com/mcp", ReadyTimeout: "50ms"}
	_, err := p.Probe(context.Background(), cfg)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if err.Code != CodeProbeTimeout {
		t.Errorf("want %q, got %q (msg=%q)", CodeProbeTimeout, err.Code, err.Message)
	}
}

func TestProbe_CacheHit(t *testing.T) {
	client := &stubClient{tools: []mcp.Tool{{Name: "a"}, {Name: "b"}}}
	p := newProberWithStub(t, client, nil)
	cfg := config.MCPServer{URL: "https://example.com/mcp"}

	if _, err := p.Probe(context.Background(), cfg); err != nil {
		t.Fatalf("first probe: %+v", err)
	}
	// Reset so a second call would be observable if it actually ran.
	client.initCalled.Store(false)
	client.listCalled.Store(false)

	res, err := p.Probe(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second probe: %+v", err)
	}
	if !res.Cached {
		t.Errorf("expected cached=true on second probe")
	}
	if client.initCalled.Load() || client.listCalled.Load() {
		t.Errorf("cached probe should have short-circuited the client")
	}
}

func TestProbe_UnsupportedTransport_SSH(t *testing.T) {
	p := NewProber(nil, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{
		SSH:     &config.SSHConfig{Host: "host"},
		Command: []string{"/bin/mcp"},
	})
	if err == nil || err.Code != CodeUnsupportedTransport {
		t.Fatalf("want unsupported_transport, got %+v", err)
	}
}

func TestProbe_UnsupportedTransport_OpenAPI(t *testing.T) {
	p := NewProber(nil, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{
		OpenAPI: &config.OpenAPIConfig{Spec: "https://api.example.com/openapi.json"},
	})
	if err == nil || err.Code != CodeUnsupportedTransport {
		t.Fatalf("want unsupported_transport, got %+v", err)
	}
}

func TestProbe_InvalidConfig_ExternalMissingURL(t *testing.T) {
	p := NewProber(nil, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{Name: "web"})
	if err == nil || err.Code != CodeInvalidConfig {
		t.Fatalf("want invalid_config, got %+v", err)
	}
}

func TestProbe_InvalidConfig_ContainerMissingPort(t *testing.T) {
	p := NewProber(nil, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{Name: "c", Image: "mcp/foo"})
	if err == nil || err.Code != CodeInvalidConfig {
		t.Fatalf("want invalid_config, got %+v", err)
	}
}

func TestProbe_ContainerSpawner_ReleaseAlwaysRuns(t *testing.T) {
	// Even on initialize failure, the container must be released. This is the
	// leak-test requirement from the prompt.
	client := &stubClient{initErr: errors.New("boom")}
	spawner := &fakeSpawner{endpoint: "http://127.0.0.1:0"}
	p := newProberWithStub(t, client, spawner)
	_, err := p.Probe(context.Background(), config.MCPServer{
		Name:      "c",
		Image:     "mcp/foo:latest",
		Port:      8080,
		Transport: "http",
	})
	if err == nil || err.Code != CodeInitializeFailed {
		t.Fatalf("expected initialize_failed, got %+v", err)
	}
	if spawner.releaseCalled.Load() != 1 {
		t.Fatalf("expected release to be called exactly once, got %d", spawner.releaseCalled.Load())
	}
}

func TestProbe_ContainerSpawnFailure_StillReleases(t *testing.T) {
	spawner := &fakeSpawner{spawnErr: errors.New("no image")}
	p := newProberWithStub(t, nil, spawner)
	_, err := p.Probe(context.Background(), config.MCPServer{
		Name:      "c",
		Image:     "mcp/foo:latest",
		Port:      8080,
		Transport: "http",
	})
	if err == nil || err.Code != CodeInitializeFailed {
		t.Fatalf("expected initialize_failed from spawn error, got %+v", err)
	}
	if spawner.releaseCalled.Load() != 1 {
		t.Fatalf("expected release even on spawn failure, got %d", spawner.releaseCalled.Load())
	}
}

func TestProbe_ContainerWithoutSpawner_Unsupported(t *testing.T) {
	client := &stubClient{}
	p := newProberWithStub(t, client, nil)
	_, err := p.Probe(context.Background(), config.MCPServer{
		Name:      "c",
		Image:     "mcp/foo:latest",
		Port:      8080,
		Transport: "http",
	})
	if err == nil || err.Code != CodeUnsupportedTransport {
		t.Fatalf("expected unsupported_transport without spawner, got %+v", err)
	}
}

func TestProbe_ContainerStdio_Unsupported(t *testing.T) {
	spawner := &fakeSpawner{}
	p := newProberWithStub(t, &stubClient{}, spawner)
	_, err := p.Probe(context.Background(), config.MCPServer{
		Name:      "c",
		Image:     "mcp/foo:latest",
		Transport: "stdio",
	})
	if err == nil || err.Code != CodeUnsupportedTransport {
		t.Fatalf("expected unsupported_transport for stdio container, got %+v", err)
	}
	if spawner.releaseCalled.Load() != 0 {
		t.Errorf("spawner should not have been called for stdio unsupported path")
	}
}

func TestProbe_ContainerHappyPath(t *testing.T) {
	client := &stubClient{tools: []mcp.Tool{{Name: "exec"}}}
	spawner := &fakeSpawner{endpoint: "http://127.0.0.1:9999/mcp"}
	p := newProberWithStub(t, client, spawner)
	res, err := p.Probe(context.Background(), config.MCPServer{
		Name:      "c",
		Image:     "mcp/foo:latest",
		Port:      9999,
		Transport: "http",
	})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "exec" {
		t.Fatalf("tools mismatch: %+v", res.Tools)
	}
	if spawner.releaseCalled.Load() != 1 {
		t.Fatalf("expected release to be called, got %d", spawner.releaseCalled.Load())
	}
}
