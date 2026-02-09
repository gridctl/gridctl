package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/logging"
)

func TestClientBase_Tools_Empty(t *testing.T) {
	b := &ClientBase{}
	if got := b.Tools(); got != nil {
		t.Errorf("expected nil tools, got %v", got)
	}
}

func TestClientBase_SetTools_NoWhitelist(t *testing.T) {
	b := &ClientBase{}
	tools := []Tool{
		{Name: "tool-a"},
		{Name: "tool-b"},
	}
	b.SetTools(tools)

	got := b.Tools()
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	if got[0].Name != "tool-a" || got[1].Name != "tool-b" {
		t.Errorf("unexpected tools: %v", got)
	}
}

func TestClientBase_SetTools_WithWhitelist(t *testing.T) {
	b := &ClientBase{}
	b.SetToolWhitelist([]string{"allowed"})
	b.SetTools([]Tool{
		{Name: "allowed"},
		{Name: "blocked"},
	})

	got := b.Tools()
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if got[0].Name != "allowed" {
		t.Errorf("expected 'allowed' tool, got %q", got[0].Name)
	}
}

func TestClientBase_SetTools_WhitelistFiltersAll(t *testing.T) {
	b := &ClientBase{}
	b.SetToolWhitelist([]string{"nonexistent"})
	b.SetTools([]Tool{
		{Name: "tool-a"},
		{Name: "tool-b"},
	})

	got := b.Tools()
	if len(got) != 0 {
		t.Errorf("expected 0 tools, got %d", len(got))
	}
}

func TestClientBase_IsInitialized_Default(t *testing.T) {
	b := &ClientBase{}
	if b.IsInitialized() {
		t.Error("expected not initialized by default")
	}
}

func TestClientBase_SetInitialized(t *testing.T) {
	b := &ClientBase{}
	info := ServerInfo{Name: "test-server", Version: "1.0.0"}
	b.SetInitialized(info)

	if !b.IsInitialized() {
		t.Error("expected initialized after SetInitialized")
	}
	if got := b.ServerInfo(); got != info {
		t.Errorf("expected server info %v, got %v", info, got)
	}
}

func TestClientBase_ServerInfo_Default(t *testing.T) {
	b := &ClientBase{}
	if got := b.ServerInfo(); got != (ServerInfo{}) {
		t.Errorf("expected zero ServerInfo, got %v", got)
	}
}

func TestClientBase_SetToolWhitelist(t *testing.T) {
	b := &ClientBase{}

	// Set whitelist then add tools
	b.SetToolWhitelist([]string{"x", "y"})
	b.SetTools([]Tool{
		{Name: "x"},
		{Name: "y"},
		{Name: "z"},
	})

	got := b.Tools()
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}

	// Clear whitelist and reset tools
	b.SetToolWhitelist(nil)
	b.SetTools([]Tool{
		{Name: "x"},
		{Name: "y"},
		{Name: "z"},
	})

	got = b.Tools()
	if len(got) != 3 {
		t.Fatalf("expected 3 tools after clearing whitelist, got %d", len(got))
	}
}

func TestFilterTools(t *testing.T) {
	tests := []struct {
		name      string
		tools     []Tool
		whitelist []string
		want      int
	}{
		{
			name:      "all match",
			tools:     []Tool{{Name: "a"}, {Name: "b"}},
			whitelist: []string{"a", "b"},
			want:      2,
		},
		{
			name:      "partial match",
			tools:     []Tool{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			whitelist: []string{"a", "c"},
			want:      2,
		},
		{
			name:      "no match",
			tools:     []Tool{{Name: "a"}, {Name: "b"}},
			whitelist: []string{"x"},
			want:      0,
		},
		{
			name:      "empty tools",
			tools:     nil,
			whitelist: []string{"a"},
			want:      0,
		},
		{
			name:      "nil whitelist matches nothing",
			tools:     []Tool{{Name: "a"}},
			whitelist: nil,
			want:      0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterTools(tc.tools, tc.whitelist)
			if len(got) != tc.want {
				t.Errorf("filterTools() returned %d tools, want %d", len(got), tc.want)
			}
		})
	}
}

func TestClientBase_ConcurrentAccess(t *testing.T) {
	b := &ClientBase{}
	var wg sync.WaitGroup

	// Concurrent writers and readers
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			b.SetTools([]Tool{{Name: "tool"}})
		}()
		go func() {
			defer wg.Done()
			b.SetInitialized(ServerInfo{Name: "server", Version: "1.0"})
		}()
		go func() {
			defer wg.Done()
			_ = b.Tools()
			_ = b.IsInitialized()
			_ = b.ServerInfo()
		}()
	}

	wg.Wait()
}

// --- RPCClient tests ---

// fakeTransport implements transporter for testing RPCClient shared methods.
type fakeTransport struct {
	callFn func(ctx context.Context, method string, params any, result any) error
	sendFn func(ctx context.Context, method string, params any) error
}

func (f *fakeTransport) call(ctx context.Context, method string, params any, result any) error {
	if f.callFn != nil {
		return f.callFn(ctx, method, params, result)
	}
	return nil
}

func (f *fakeTransport) send(ctx context.Context, method string, params any) error {
	if f.sendFn != nil {
		return f.sendFn(ctx, method, params)
	}
	return nil
}

func newFakeRPCClient(name string, ft *fakeTransport) *RPCClient {
	r := &RPCClient{}
	initRPCClient(r, name, ft)
	return r
}

func TestRPCClient_Name(t *testing.T) {
	ft := &fakeTransport{}
	r := newFakeRPCClient("test-server", ft)

	if got := r.Name(); got != "test-server" {
		t.Errorf("Name() = %q, want %q", got, "test-server")
	}
}

func TestRPCClient_SetLogger(t *testing.T) {
	ft := &fakeTransport{}
	r := newFakeRPCClient("test", ft)

	// Nil logger should not change the default
	r.SetLogger(nil)
	if r.logger == nil {
		t.Error("SetLogger(nil) should not set logger to nil")
	}

	// Non-nil logger should be set
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r.SetLogger(logger)
	if r.logger != logger {
		t.Error("SetLogger should update logger")
	}
}

func TestRPCClient_Initialize(t *testing.T) {
	var calledMethods []string
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, params any, result any) error {
			calledMethods = append(calledMethods, method)
			if method == "initialize" {
				// Populate result like a real MCP server would
				if r, ok := result.(*InitializeResult); ok {
					r.ProtocolVersion = MCPProtocolVersion
					r.ServerInfo = ServerInfo{Name: "test-mcp", Version: "1.0.0"}
				}
			}
			return nil
		},
		sendFn: func(_ context.Context, method string, _ any) error {
			calledMethods = append(calledMethods, method)
			return nil
		},
	}

	r := newFakeRPCClient("test", ft)

	err := r.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Verify initialize was called, then notification was sent
	if len(calledMethods) != 2 {
		t.Fatalf("expected 2 calls, got %d: %v", len(calledMethods), calledMethods)
	}
	if calledMethods[0] != "initialize" {
		t.Errorf("first call should be 'initialize', got %q", calledMethods[0])
	}
	if calledMethods[1] != "notifications/initialized" {
		t.Errorf("second call should be 'notifications/initialized', got %q", calledMethods[1])
	}

	// Verify state was updated
	if !r.IsInitialized() {
		t.Error("expected client to be initialized")
	}
	if r.ServerInfo().Name != "test-mcp" {
		t.Errorf("expected server name 'test-mcp', got %q", r.ServerInfo().Name)
	}
}

func TestRPCClient_Initialize_CallError(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, _ any) error {
			return fmt.Errorf("connection refused")
		},
	}

	r := newFakeRPCClient("test", ft)

	err := r.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if r.IsInitialized() {
		t.Error("should not be initialized after error")
	}
}

// fakeConnectorTransport implements both transporter and connector.
type fakeConnectorTransport struct {
	fakeTransport
	connectCalled bool
	connectErr    error
}

func (f *fakeConnectorTransport) Connect(_ context.Context) error {
	f.connectCalled = true
	return f.connectErr
}

func TestRPCClient_Initialize_WithConnector(t *testing.T) {
	ft := &fakeConnectorTransport{
		fakeTransport: fakeTransport{
			callFn: func(_ context.Context, method string, _ any, result any) error {
				if method == "initialize" {
					if r, ok := result.(*InitializeResult); ok {
						r.ServerInfo = ServerInfo{Name: "stdio-server", Version: "2.0"}
					}
				}
				return nil
			},
		},
	}

	r := &RPCClient{}
	initRPCClient(r, "test", ft)

	err := r.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !ft.connectCalled {
		t.Error("expected Connect() to be called for connector transport")
	}
}

func TestRPCClient_Initialize_ConnectError(t *testing.T) {
	ft := &fakeConnectorTransport{
		connectErr: fmt.Errorf("container not running"),
	}

	r := &RPCClient{}
	initRPCClient(r, "test", ft)

	err := r.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error from Connect()")
	}
	if err.Error() != "container not running" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRPCClient_RefreshTools(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			if method == "tools/list" {
				if r, ok := result.(*ToolsListResult); ok {
					r.Tools = []Tool{
						{Name: "read", Description: "Read a file"},
						{Name: "write", Description: "Write a file"},
					}
				}
			}
			return nil
		},
	}

	r := newFakeRPCClient("test", ft)

	err := r.RefreshTools(context.Background())
	if err != nil {
		t.Fatalf("RefreshTools() error = %v", err)
	}

	tools := r.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "read" || tools[1].Name != "write" {
		t.Errorf("unexpected tools: %v", tools)
	}
}

func TestRPCClient_RefreshTools_Error(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, _ string, _ any, _ any) error {
			return fmt.Errorf("timeout")
		},
	}

	r := newFakeRPCClient("test", ft)

	err := r.RefreshTools(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRPCClient_RefreshTools_WithWhitelist(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			if method == "tools/list" {
				if r, ok := result.(*ToolsListResult); ok {
					r.Tools = []Tool{
						{Name: "read"},
						{Name: "write"},
						{Name: "delete"},
					}
				}
			}
			return nil
		},
	}

	r := newFakeRPCClient("test", ft)
	r.SetToolWhitelist([]string{"read", "write"})

	err := r.RefreshTools(context.Background())
	if err != nil {
		t.Fatalf("RefreshTools() error = %v", err)
	}

	tools := r.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (whitelist filtered), got %d", len(tools))
	}
}

func TestRPCClient_CallTool(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, params any, result any) error {
			if method == "tools/call" {
				if r, ok := result.(*ToolCallResult); ok {
					r.Content = []Content{{Type: "text", Text: "hello"}}
				}
			}
			return nil
		},
	}

	r := newFakeRPCClient("test", ft)

	result, err := r.CallTool(context.Background(), "greet", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestRPCClient_CallTool_Error(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, _ string, _ any, _ any) error {
			return fmt.Errorf("tool not found")
		},
	}

	r := newFakeRPCClient("test", ft)

	_, err := r.CallTool(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildNotification(t *testing.T) {
	req, err := buildNotification("notifications/initialized", nil)
	if err != nil {
		t.Fatalf("buildNotification() error = %v", err)
	}

	if req.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC '2.0', got %q", req.JSONRPC)
	}
	if req.Method != "notifications/initialized" {
		t.Errorf("expected method 'notifications/initialized', got %q", req.Method)
	}
	if req.ID != nil {
		t.Error("notification should not have an ID")
	}
	if req.Params != nil {
		t.Error("expected nil params")
	}
}

func TestBuildNotification_WithParams(t *testing.T) {
	params := map[string]string{"key": "value"}
	req, err := buildNotification("test/method", params)
	if err != nil {
		t.Fatalf("buildNotification() error = %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(req.Params, &got); err != nil {
		t.Fatalf("failed to unmarshal params: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("expected param key=value, got %v", got)
	}
}

func TestRPCClient_DefaultLogger(t *testing.T) {
	ft := &fakeTransport{}
	r := newFakeRPCClient("test", ft)

	// Default logger should be a discard logger (not nil)
	if r.logger == nil {
		t.Error("default logger should not be nil")
	}
}

func TestRPCClient_SatisfiesAgentClientInterface(t *testing.T) {
	// Verify that types embedding RPCClient satisfy the AgentClient interface.
	// This is a compile-time check via the test.
	ft := &fakeTransport{}
	r := newFakeRPCClient("test", ft)

	var _ AgentClient = r
}

func TestInitRPCClient(t *testing.T) {
	ft := &fakeTransport{}
	r := &RPCClient{}
	initRPCClient(r, "my-name", ft)

	if r.name != "my-name" {
		t.Errorf("name = %q, want %q", r.name, "my-name")
	}
	if r.logger == nil {
		t.Error("logger should not be nil after init")
	}
	if r.transport != ft {
		t.Error("transport not set correctly")
	}
}

// Compile-time assertions: concrete clients satisfy AgentClient.
var _ AgentClient = (*Client)(nil)
var _ AgentClient = (*StdioClient)(nil)
var _ AgentClient = (*ProcessClient)(nil)

// RPCClient also satisfies AgentClient (via embedded ClientBase + its own methods),
// though it is only used as an embedded base, not standalone.
var _ AgentClient = (*RPCClient)(nil)

// Compile-time assertions: concrete clients implement transporter.
var _ transporter = (*Client)(nil)
var _ transporter = (*StdioClient)(nil)
var _ transporter = (*ProcessClient)(nil)

// Compile-time assertions: stdio/process clients implement connector.
var _ connector = (*StdioClient)(nil)
var _ connector = (*ProcessClient)(nil)

// Verify Client does NOT implement connector (no Connect method).
func TestClient_DoesNotImplementConnector(t *testing.T) {
	c := NewClient("test", "http://localhost:3000")
	_, ok := c.transport.(connector)
	// Client wraps itself as transport, so check its own type
	_, isConnector := interface{}(c).(connector)
	if ok || isConnector {
		t.Error("Client should not implement connector")
	}
}

func TestRPCClient_SetLoggerToDiscard(t *testing.T) {
	ft := &fakeTransport{}
	r := newFakeRPCClient("test", ft)

	// Verify initial logger is a discard logger
	logger := logging.NewDiscardLogger()
	r.SetLogger(logger)
	if r.logger != logger {
		t.Error("expected logger to be set")
	}
}
