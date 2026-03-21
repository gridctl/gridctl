package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/token"
	"go.uber.org/mock/gomock"
)

func TestNewGateway(t *testing.T) {
	g := NewGateway()
	if g == nil {
		t.Fatal("NewGateway returned nil")
	}
	if g.Router() == nil {
		t.Error("Router should not be nil")
	}
	if g.Sessions() == nil {
		t.Error("Sessions should not be nil")
	}

	info := g.ServerInfo()
	if info.Name != "gridctl-gateway" {
		t.Errorf("expected server name 'gridctl-gateway', got '%s'", info.Name)
	}
	if info.Version != "dev" {
		t.Errorf("expected version 'dev', got '%s'", info.Version)
	}
}

func TestGateway_SetVersion(t *testing.T) {
	g := NewGateway()
	g.SetVersion("v0.1.0-alpha.2")

	info := g.ServerInfo()
	if info.Version != "v0.1.0-alpha.2" {
		t.Errorf("expected version 'v0.1.0-alpha.2', got '%s'", info.Version)
	}
}

func TestGateway_HasAgent(t *testing.T) {
	g := NewGateway()

	if g.HasAgent("test-agent") {
		t.Error("expected HasAgent to return false for unregistered agent")
	}

	g.RegisterAgent("test-agent", []config.ToolSelector{{Server: "server1"}})

	if !g.HasAgent("test-agent") {
		t.Error("expected HasAgent to return true for registered agent")
	}

	if g.HasAgent("unknown") {
		t.Error("expected HasAgent to return false for unknown agent")
	}

	g.UnregisterAgent("test-agent")

	if g.HasAgent("test-agent") {
		t.Error("expected HasAgent to return false after unregister")
	}
}

func TestGateway_HandleInitialize(t *testing.T) {
	g := NewGateway()
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
		Capabilities:    Capabilities{},
	}

	result, _, err := g.HandleInitialize(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected protocol version '2024-11-05', got '%s'", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "gridctl-gateway" {
		t.Errorf("expected server name 'gridctl-gateway', got '%s'", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected Tools capability to be set")
	}
	if !result.Capabilities.Tools.ListChanged {
		t.Error("expected Tools.ListChanged to be true")
	}
}

func TestGateway_HandleToolsList(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add a mock client with tools
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.HandleToolsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestGateway_HandleToolsCall(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	// Override default CallTool with custom echo behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			msg := args["message"].(string)
			return &ToolCallResult{
				Content: []Content{NewTextContent("Echo: " + msg)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	params := ToolCallParams{
		Name:      "agent1__echo",
		Arguments: map[string]any{"message": "hello"},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Error("expected successful result, got error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	if result.Content[0].Text != "Echo: hello" {
		t.Errorf("expected 'Echo: hello', got '%s'", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_UnknownTool(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()

	params := ToolCallParams{
		Name:      "unknown__tool",
		Arguments: map[string]any{},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for unknown tool")
	}
}

func TestGateway_HandleToolsCall_InvalidFormat(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()

	params := ToolCallParams{
		Name:      "invalidformat",
		Arguments: map[string]any{},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for invalid format")
	}
}

func TestGateway_HandleToolsCall_AgentError(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "fail", Description: "Failing tool"},
	})
	// Override default CallTool to return error
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("agent error")).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	params := ToolCallParams{
		Name:      "agent1__fail",
		Arguments: map[string]any{},
	}

	result, err := g.HandleToolsCall(ctx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result when agent fails")
	}
	if len(result.Content) == 0 {
		t.Error("expected error content")
	}
}

func TestGateway_Status(t *testing.T) {
	g := NewGateway()

	// Initially no servers
	statuses := g.Status()
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}

	// Add a mock client
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.Router().AddClient(client)

	// Store metadata manually (normally done by RegisterMCPServer)
	g.mu.Lock()
	g.serverMeta["agent1"] = MCPServerConfig{
		Name:      "agent1",
		Transport: TransportHTTP,
		Endpoint:  "http://localhost:9000/mcp",
	}
	g.mu.Unlock()

	statuses = g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}

	status := statuses[0]
	if status.Name != "agent1" {
		t.Errorf("expected name 'agent1', got '%s'", status.Name)
	}
	if status.Transport != TransportHTTP {
		t.Errorf("expected transport 'http', got '%s'", status.Transport)
	}
	if status.ToolCount != 1 {
		t.Errorf("expected 1 tool, got %d", status.ToolCount)
	}
	if !status.Initialized {
		t.Error("expected initialized to be true")
	}
}

func TestGateway_Status_IncludesOutputFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Server with explicit output format
	client1 := setupMockAgentClient(ctrl, "toon-server", []Tool{{Name: "tool1"}})
	g.Router().AddClient(client1)
	g.SetServerMeta(MCPServerConfig{
		Name:         "toon-server",
		Transport:    TransportHTTP,
		OutputFormat: "toon",
	})

	// Server without output format (should inherit gateway default)
	client2 := setupMockAgentClient(ctrl, "default-server", []Tool{{Name: "tool2"}})
	g.Router().AddClient(client2)
	g.SetServerMeta(MCPServerConfig{
		Name:      "default-server",
		Transport: TransportStdio,
	})

	// Set gateway default
	g.SetDefaultOutputFormat("csv")

	statuses := g.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Statuses are sorted by name
	defaultStatus := statuses[0] // "default-server"
	toonStatus := statuses[1]    // "toon-server"

	if toonStatus.OutputFormat != "toon" {
		t.Errorf("toon-server output format = %q, want %q", toonStatus.OutputFormat, "toon")
	}
	if defaultStatus.OutputFormat != "csv" {
		t.Errorf("default-server output format = %q, want %q (gateway default)", defaultStatus.OutputFormat, "csv")
	}
}

func TestGateway_Status_OutputFormat_NoDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "plain-server", []Tool{{Name: "tool1"}})
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{
		Name:      "plain-server",
		Transport: TransportHTTP,
	})

	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].OutputFormat != "" {
		t.Errorf("output format = %q, want empty (no override, no default)", statuses[0].OutputFormat)
	}
}

func TestGateway_UnregisterMCPServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Verify exists
	if len(g.Router().AggregatedTools()) != 1 {
		t.Fatal("expected 1 tool before unregister")
	}

	g.UnregisterMCPServer("agent1")

	if len(g.Router().AggregatedTools()) != 0 {
		t.Error("expected 0 tools after unregister")
	}
	if g.Router().GetClient("agent1") != nil {
		t.Error("expected client to be removed")
	}
}

// closableClient wraps a MockAgentClient to implement io.Closer.
type closableClient struct {
	AgentClient
	closeFn func() error
}

func (c *closableClient) Close() error {
	return c.closeFn()
}

func TestGateway_RestartMCPServer_NotFound(t *testing.T) {
	g := NewGateway()
	err := g.RestartMCPServer(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
	if !strings.Contains(err.Error(), "unknown MCP server") {
		t.Errorf("expected 'unknown MCP server' in error, got: %s", err)
	}
}

func TestGateway_RestartMCPServer_ClosesExistingClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var closed atomic.Bool
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &closableClient{
		AgentClient: mock,
		closeFn:     func() error { closed.Store(true); return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP, Endpoint: "http://localhost:9999", External: true})

	// Restart will fail at re-registration (no real server), but close should be called
	_ = g.RestartMCPServer(context.Background(), "server1")

	if !closed.Load() {
		t.Error("expected existing client to be closed")
	}
}

func TestGateway_RestartMCPServer_UnregistersBeforeReregister(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP, Endpoint: "http://localhost:9999", External: true})

	// Verify tools exist before restart
	if len(g.Router().AggregatedTools()) != 1 {
		t.Fatal("expected 1 tool before restart")
	}

	// Restart will fail at re-registration, but unregister should have cleared the router
	_ = g.RestartMCPServer(context.Background(), "server1")

	if g.Router().GetClient("server1") != nil {
		t.Error("expected client to be removed after failed restart")
	}
	if len(g.Router().AggregatedTools()) != 0 {
		t.Error("expected 0 tools after failed restart")
	}
}

func TestGateway_AgentToolFiltering(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add two mock clients with tools
	client1 := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "read", Description: "Read tool"},
		{Name: "write", Description: "Write tool"},
		{Name: "delete", Description: "Delete tool"},
	})
	client2 := setupMockAgentClient(ctrl, "server2", []Tool{
		{Name: "list", Description: "List tool"},
		{Name: "create", Description: "Create tool"},
	})
	g.Router().AddClient(client1)
	g.Router().AddClient(client2)
	g.Router().RefreshTools()

	tests := []struct {
		name           string
		agentName      string
		uses           []config.ToolSelector
		wantToolCount  int
		wantToolNames  []string
	}{
		{
			name:      "no registration returns all tools",
			agentName: "unregistered-agent",
			uses:      nil, // not registered
			wantToolCount: 5,
		},
		{
			name:      "server access without tool filter",
			agentName: "viewer-agent",
			uses: []config.ToolSelector{
				{Server: "server1"},
			},
			wantToolCount: 3,
			wantToolNames: []string{"server1__read", "server1__write", "server1__delete"},
		},
		{
			name:      "server access with tool filter",
			agentName: "restricted-agent",
			uses: []config.ToolSelector{
				{Server: "server1", Tools: []string{"read"}},
			},
			wantToolCount: 1,
			wantToolNames: []string{"server1__read"},
		},
		{
			name:      "multiple servers with mixed filtering",
			agentName: "mixed-agent",
			uses: []config.ToolSelector{
				{Server: "server1", Tools: []string{"read", "write"}},
				{Server: "server2"}, // all tools from server2
			},
			wantToolCount: 4,
			wantToolNames: []string{"server1__read", "server1__write", "server2__list", "server2__create"},
		},
		{
			name:      "empty selectors returns nothing",
			agentName: "no-access-agent",
			uses:      []config.ToolSelector{},
			wantToolCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.uses != nil {
				g.RegisterAgent(tc.agentName, tc.uses)
			}

			result, err := g.HandleToolsListForAgent(tc.agentName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result.Tools) != tc.wantToolCount {
				t.Errorf("expected %d tools, got %d", tc.wantToolCount, len(result.Tools))
				for _, tool := range result.Tools {
					t.Logf("  got tool: %s", tool.Name)
				}
			}

			if len(tc.wantToolNames) > 0 {
				gotNames := make(map[string]bool)
				for _, tool := range result.Tools {
					gotNames[tool.Name] = true
				}
				for _, name := range tc.wantToolNames {
					if !gotNames[name] {
						t.Errorf("expected tool %s to be present", name)
					}
				}
			}
		})
	}
}

func TestGateway_AgentToolCallFiltering(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	// Add a mock client with tools
	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "allowed", Description: "Allowed tool"},
		{Name: "restricted", Description: "Restricted tool"},
	})
	// Override default CallTool with custom behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("called " + name)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Register agent with only "allowed" tool
	g.RegisterAgent("restricted-agent", []config.ToolSelector{
		{Server: "server1", Tools: []string{"allowed"}},
	})

	// Call allowed tool - should succeed
	result, err := g.HandleToolsCallForAgent(ctx, "restricted-agent", ToolCallParams{
		Name: "server1__allowed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected allowed tool call to succeed")
	}

	// Call restricted tool - should fail with access denied
	result, err = g.HandleToolsCallForAgent(ctx, "restricted-agent", ToolCallParams{
		Name: "server1__restricted",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected restricted tool call to fail")
	}
	if len(result.Content) == 0 || result.Content[0].Text == "" {
		t.Error("expected access denied message")
	}
}

func TestGateway_AccessDenialLogging(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	// Set up log buffer to capture logs
	logBuffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	// Add a mock client
	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "secret-tool", Description: "Secret tool"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Register agent with no access to server1
	g.RegisterAgent("limited-agent", []config.ToolSelector{
		{Server: "other-server"},
	})

	// Attempt denied tool call
	result, err := g.HandleToolsCallForAgent(ctx, "limited-agent", ToolCallParams{
		Name: "server1__secret-tool",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected access denied error")
	}

	// Verify WARN log was emitted
	entries := logBuffer.GetRecent(10)
	found := false
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "tool access denied" {
			if entry.Attrs["agent"] != "limited-agent" {
				t.Errorf("expected agent 'limited-agent', got %v", entry.Attrs["agent"])
			}
			if entry.Attrs["tool"] != "secret-tool" {
				t.Errorf("expected tool 'secret-tool', got %v", entry.Attrs["tool"])
			}
			if entry.Attrs["server"] != "server1" {
				t.Errorf("expected server 'server1', got %v", entry.Attrs["server"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected WARN log entry for tool access denial")
		for _, entry := range entries {
			t.Logf("  log: level=%s msg=%s attrs=%v", entry.Level, entry.Message, entry.Attrs)
		}
	}
}

func TestGateway_SessionCount(t *testing.T) {
	g := NewGateway()

	if g.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", g.SessionCount())
	}

	_, _, err := g.HandleInitialize(InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "client1", Version: "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if g.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", g.SessionCount())
	}
}

func TestGateway_Close(t *testing.T) {
	g := NewGateway()

	// Close without StartCleanup should not panic
	g.Close()

	// Start and close
	ctx := context.Background()
	g.StartCleanup(ctx)
	g.Close()
}

// pingableClient wraps a MockAgentClient to also implement Pingable.
type pingableClient struct {
	AgentClient
	pingFn func(ctx context.Context) error
}

func (p *pingableClient) Ping(ctx context.Context) error {
	return p.pingFn(ctx)
}

func TestGateway_HealthMonitor_DetectsUnhealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	// Run a single health check
	ctx := context.Background()
	g.checkHealth(ctx)

	// Verify health status
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if hs.Healthy {
		t.Error("expected server to be unhealthy")
	}
	if hs.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got '%s'", hs.Error)
	}
	if hs.LastCheck.IsZero() {
		t.Error("expected LastCheck to be set")
	}

	// Verify WARN log
	entries := logBuffer.GetRecent(20)
	found := false
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "MCP server unhealthy" {
			if entry.Attrs["name"] != "server1" {
				t.Errorf("expected name 'server1', got %v", entry.Attrs["name"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected WARN log for unhealthy server")
	}
}

func TestGateway_HealthMonitor_DetectsHealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()
	g.checkHealth(ctx)

	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if !hs.Healthy {
		t.Error("expected server to be healthy")
	}
	if hs.Error != "" {
		t.Errorf("expected empty error, got '%s'", hs.Error)
	}
	if hs.LastHealthy.IsZero() {
		t.Error("expected LastHealthy to be set")
	}
}

func TestGateway_HealthMonitor_Recovery(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	pingErr := fmt.Errorf("connection refused")
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return pingErr },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()

	// First check: unhealthy
	g.checkHealth(ctx)
	hs := g.GetHealthStatus("server1")
	if hs == nil || hs.Healthy {
		t.Fatal("expected unhealthy after first check")
	}

	// Server recovers
	client.pingFn = func(ctx context.Context) error { return nil }
	g.checkHealth(ctx)

	hs = g.GetHealthStatus("server1")
	if hs == nil || !hs.Healthy {
		t.Fatal("expected healthy after recovery")
	}

	// Verify recovery log
	entries := logBuffer.GetRecent(20)
	found := false
	for _, entry := range entries {
		if entry.Level == "INFO" && entry.Message == "MCP server recovered" {
			if entry.Attrs["name"] != "server1" {
				t.Errorf("expected name 'server1', got %v", entry.Attrs["name"])
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected INFO log for server recovery")
	}
}

func TestGateway_HealthMonitor_SkipsNonPingable(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Use a regular mock (not pingable)
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Should have no health status since client is not Pingable
	hs := g.GetHealthStatus("server1")
	if hs != nil {
		t.Error("expected no health status for non-pingable client")
	}
}

func TestGateway_HealthMonitor_SkipsNonMCPServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add a client without server metadata (e.g., A2A adapter)
	mock := setupMockAgentClient(ctrl, "a2a-adapter", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	// Deliberately not calling SetServerMeta

	ctx := context.Background()
	g.checkHealth(ctx)

	hs := g.GetHealthStatus("a2a-adapter")
	if hs != nil {
		t.Error("expected no health status for client without server meta")
	}
}

func TestGateway_HealthMonitor_MultipleServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Server 1: healthy
	mock1 := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client1 := &pingableClient{
		AgentClient: mock1,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client1)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	// Server 2: unhealthy
	mock2 := setupMockAgentClient(ctrl, "server2", []Tool{{Name: "tool2"}})
	client2 := &pingableClient{
		AgentClient: mock2,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("timeout") },
	}
	g.Router().AddClient(client2)
	g.SetServerMeta(MCPServerConfig{Name: "server2", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	hs1 := g.GetHealthStatus("server1")
	if hs1 == nil || !hs1.Healthy {
		t.Error("expected server1 to be healthy")
	}

	hs2 := g.GetHealthStatus("server2")
	if hs2 == nil || hs2.Healthy {
		t.Error("expected server2 to be unhealthy")
	}
	if hs2.Error != "timeout" {
		t.Errorf("expected error 'timeout', got '%s'", hs2.Error)
	}
}

func TestGateway_Status_IncludesHealth(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	// Before health check, status should have no health data
	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Healthy != nil {
		t.Error("expected Healthy to be nil before health check")
	}

	// After health check
	g.checkHealth(context.Background())

	statuses = g.Status()
	if statuses[0].Healthy == nil {
		t.Fatal("expected Healthy to be set after health check")
	}
	if !*statuses[0].Healthy {
		t.Error("expected Healthy to be true")
	}
	if statuses[0].LastCheck == nil {
		t.Error("expected LastCheck to be set")
	}
}

func TestGateway_StartHealthMonitor_Lifecycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var pingCount atomic.Int32
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn: func(ctx context.Context) error {
			pingCount.Add(1)
			return nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx, cancel := context.WithCancel(context.Background())

	// Start with a very short interval for testing
	g.StartHealthMonitor(ctx, 50*time.Millisecond)

	// Wait for at least 2 checks
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Wait for goroutine to clean up
	time.Sleep(20 * time.Millisecond)

	if pingCount.Load() < 2 {
		t.Errorf("expected at least 2 health checks, got %d", pingCount.Load())
	}

	hs := g.GetHealthStatus("server1")
	if hs == nil || !hs.Healthy {
		t.Error("expected server1 to be healthy")
	}
}

func TestGateway_HealthMonitor_NoRepeatWarnings(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("down") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()

	// Run multiple health checks while server stays unhealthy
	g.checkHealth(ctx)
	g.checkHealth(ctx)
	g.checkHealth(ctx)

	// Should only log WARN once (on first detection)
	entries := logBuffer.GetRecent(20)
	warnCount := 0
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "MCP server unhealthy" {
			warnCount++
		}
	}
	if warnCount != 1 {
		t.Errorf("expected exactly 1 unhealthy WARN log, got %d", warnCount)
	}
}

func TestGateway_GetHealthStatus_NotFound(t *testing.T) {
	g := NewGateway()

	hs := g.GetHealthStatus("nonexistent")
	if hs != nil {
		t.Error("expected nil health status for unknown server")
	}
}

// reconnectableClient wraps a MockAgentClient to implement both Pingable and Reconnectable.
type reconnectableClient struct {
	AgentClient
	pingFn      func(ctx context.Context) error
	reconnectFn func(ctx context.Context) error
}

func (r *reconnectableClient) Ping(ctx context.Context) error {
	return r.pingFn(ctx)
}

func (r *reconnectableClient) Reconnect(ctx context.Context) error {
	return r.reconnectFn(ctx)
}

func TestGateway_HealthMonitor_ReconnectsUnhealthyClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	var reconnected atomic.Int32
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(ctx context.Context) error {
			reconnected.Add(1)
			return nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Verify reconnection was attempted
	if reconnected.Load() != 1 {
		t.Errorf("expected 1 reconnection attempt, got %d", reconnected.Load())
	}

	// After successful reconnection, health should be updated to healthy
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if !hs.Healthy {
		t.Error("expected server to be healthy after successful reconnection")
	}

	// Verify reconnection log
	entries := logBuffer.GetRecent(20)
	foundAttempt := false
	foundReconnected := false
	for _, entry := range entries {
		if entry.Level == "INFO" && entry.Message == "attempting reconnection" {
			foundAttempt = true
		}
		if entry.Level == "INFO" && entry.Message == "MCP server reconnected" {
			foundReconnected = true
		}
	}
	if !foundAttempt {
		t.Error("expected 'attempting reconnection' log entry")
	}
	if !foundReconnected {
		t.Error("expected 'MCP server reconnected' log entry")
	}
}

func TestGateway_HealthMonitor_ReconnectionFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
		reconnectFn: func(ctx context.Context) error { return fmt.Errorf("container not found") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Health should remain unhealthy after failed reconnection
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if hs.Healthy {
		t.Error("expected server to remain unhealthy after failed reconnection")
	}

	// Verify failure log
	entries := logBuffer.GetRecent(20)
	foundFailed := false
	for _, entry := range entries {
		if entry.Level == "WARN" && entry.Message == "reconnection failed" {
			if entry.Attrs["name"] != "server1" {
				t.Errorf("expected name 'server1', got %v", entry.Attrs["name"])
			}
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Error("expected 'reconnection failed' WARN log entry")
	}
}

func TestGateway_HealthMonitor_SkipsReconnectForNonReconnectable(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Use pingableClient (not reconnectable)
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &pingableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return fmt.Errorf("connection refused") },
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportHTTP})

	ctx := context.Background()
	g.checkHealth(ctx)

	// Server should be unhealthy but no reconnection attempted (no panic, no error)
	hs := g.GetHealthStatus("server1")
	if hs == nil {
		t.Fatal("expected health status for server1")
	}
	if hs.Healthy {
		t.Error("expected server to be unhealthy")
	}
}

func TestGateway_HealthMonitor_SkipsReconnectForHealthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	var reconnectCount atomic.Int32
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	client := &reconnectableClient{
		AgentClient: mock,
		pingFn:      func(ctx context.Context) error { return nil }, // healthy
		reconnectFn: func(ctx context.Context) error {
			reconnectCount.Add(1)
			return nil
		},
	}
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{Name: "server1", Transport: TransportStdio})

	ctx := context.Background()
	g.checkHealth(ctx)

	// No reconnection should be attempted for healthy server
	if reconnectCount.Load() != 0 {
		t.Errorf("expected 0 reconnection attempts for healthy server, got %d", reconnectCount.Load())
	}
}

func TestGateway_RegisterMCPServer_LogsTiming(t *testing.T) {
	g := NewGateway()

	// Set up log buffer to capture logs
	logBuffer := logging.NewLogBuffer(20)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	// Add a mock client directly to test that RegisterMCPServer logs
	// We can't fully test RegisterMCPServer without real transport,
	// but we can verify the gateway logger is wired up by checking
	// other logged operations. Instead, verify the log methods work
	// by checking tool call logging (which uses the same logger).
	ctrl := gomock.NewController(t)
	client := setupMockAgentClient(ctrl, "test-server", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	// Override default CallTool with custom behavior
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("ok")},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	ctx := context.Background()
	_, _ = g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "test-server__echo",
		Arguments: map[string]any{},
	})

	// Verify tool call logging includes timing info
	entries := logBuffer.GetRecent(20)
	foundStarted := false
	foundFinished := false
	for _, entry := range entries {
		if entry.Message == "tool call started" {
			foundStarted = true
		}
		if entry.Message == "tool call finished" {
			foundFinished = true
			if entry.Attrs["duration"] == nil {
				t.Error("expected duration attribute on tool call finished log")
			}
		}
	}
	if !foundStarted {
		t.Error("expected 'tool call started' log entry")
	}
	if !foundFinished {
		t.Error("expected 'tool call finished' log entry")
	}
}

func TestGateway_ImplementsToolCaller(t *testing.T) {
	var _ ToolCaller = (*Gateway)(nil) // compile-time check
}

func TestGateway_CallTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "agent1", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			msg := args["message"].(string)
			return &ToolCallResult{
				Content: []Content{NewTextContent("Echo: " + msg)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.CallTool(ctx, "agent1__echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected successful result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Echo: hello" {
		t.Errorf("unexpected result: %+v", result)
	}
}

// promptProviderClient wraps a MockAgentClient to also implement PromptProvider.
type promptProviderClient struct {
	AgentClient
	prompts []PromptData
}

func (p *promptProviderClient) ListPromptData() []PromptData {
	return p.prompts
}

func (p *promptProviderClient) GetPromptData(name string) (*PromptData, error) {
	for _, pd := range p.prompts {
		if pd.Name == name {
			return &pd, nil
		}
	}
	return nil, fmt.Errorf("prompt %q: not found", name)
}

func TestGateway_HandleInitialize_WithRegistry(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Register a registry client that implements PromptProvider
	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{{Name: "test-prompt"}},
	}
	g.Router().AddClient(client)

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	}

	result, _, err := g.HandleInitialize(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Capabilities.Tools == nil {
		t.Error("expected Tools capability to be set")
	}
	if result.Capabilities.Prompts == nil {
		t.Error("expected Prompts capability to be set")
	}
	if result.Capabilities.Prompts != nil && !result.Capabilities.Prompts.ListChanged {
		t.Error("expected Prompts.ListChanged to be true")
	}
	if result.Capabilities.Resources == nil {
		t.Error("expected Resources capability to be set")
	}
	if result.Capabilities.Resources != nil && !result.Capabilities.Resources.ListChanged {
		t.Error("expected Resources.ListChanged to be true")
	}
}

func TestGateway_HandleInitialize_WithoutRegistry(t *testing.T) {
	g := NewGateway()

	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      ClientInfo{Name: "test-client", Version: "1.0"},
	}

	result, _, err := g.HandleInitialize(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Capabilities.Tools == nil {
		t.Error("expected Tools capability to be set")
	}
	if result.Capabilities.Prompts != nil {
		t.Error("expected Prompts capability to be nil without registry")
	}
	if result.Capabilities.Resources != nil {
		t.Error("expected Resources capability to be nil without registry")
	}
}

func TestGateway_HandlePromptsList_Empty(t *testing.T) {
	g := NewGateway()

	result, err := g.HandlePromptsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Prompts == nil {
		t.Fatal("expected non-nil prompts slice")
	}
	if len(result.Prompts) != 0 {
		t.Errorf("expected 0 prompts, got %d", len(result.Prompts))
	}
}

func TestGateway_HandlePromptsList_WithPrompts(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:        "code-review",
				Description: "Review code for issues",
				Arguments: []PromptArgumentData{
					{Name: "language", Description: "Programming language", Required: true},
					{Name: "style", Description: "Review style", Required: false},
				},
			},
			{
				Name:        "summarize",
				Description: "Summarize content",
			},
		},
	}
	g.Router().AddClient(client)

	result, err := g.HandlePromptsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(result.Prompts))
	}

	// Find the code-review prompt
	var found bool
	for _, p := range result.Prompts {
		if p.Name == "code-review" {
			found = true
			if p.Description != "Review code for issues" {
				t.Errorf("expected description 'Review code for issues', got %q", p.Description)
			}
			if len(p.Arguments) != 2 {
				t.Errorf("expected 2 arguments, got %d", len(p.Arguments))
			}
			if p.Arguments[0].Name != "language" || !p.Arguments[0].Required {
				t.Errorf("unexpected first argument: %+v", p.Arguments[0])
			}
			break
		}
	}
	if !found {
		t.Error("expected 'code-review' prompt to be present")
	}
}

func TestGateway_HandlePromptsGet_ArgumentSubstitution(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:        "greet",
				Description: "Greeting prompt",
				Content:     "Hello {{name}}, welcome to {{place}}!",
				Arguments: []PromptArgumentData{
					{Name: "name", Description: "User name", Required: true},
					{Name: "place", Description: "Location", Required: false, Default: "the world"},
				},
			},
		},
	}
	g.Router().AddClient(client)

	// Test with all arguments provided
	result, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "greet",
		Arguments: map[string]string{"name": "Alice", "place": "Wonderland"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Description != "Greeting prompt" {
		t.Errorf("expected description 'Greeting prompt', got %q", result.Description)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", result.Messages[0].Role)
	}
	if result.Messages[0].Content.Text != "Hello Alice, welcome to Wonderland!" {
		t.Errorf("expected substituted content, got %q", result.Messages[0].Content.Text)
	}
}

func TestGateway_HandlePromptsGet_DefaultArguments(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "greet",
				Content: "Hello {{name}}, welcome to {{place}}!",
				Arguments: []PromptArgumentData{
					{Name: "name", Description: "User name", Required: true},
					{Name: "place", Description: "Location", Default: "the world"},
				},
			},
		},
	}
	g.Router().AddClient(client)

	// Test with missing "place" argument — should use default
	result, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "greet",
		Arguments: map[string]string{"name": "Bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Messages[0].Content.Text != "Hello Bob, welcome to the world!" {
		t.Errorf("expected default substitution, got %q", result.Messages[0].Content.Text)
	}
}

func TestGateway_HandlePromptsGet_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandlePromptsGet(PromptsGetParams{Name: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent prompt")
	}
}

func TestGateway_HandlePromptsGet_NoRegistry(t *testing.T) {
	g := NewGateway()

	_, err := g.HandlePromptsGet(PromptsGetParams{Name: "anything"})
	if err == nil {
		t.Fatal("expected error when no registry")
	}
	if err.Error() != "registry not available" {
		t.Errorf("expected 'registry not available' error, got %q", err.Error())
	}
}

func TestGateway_HandleResourcesList(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{Name: "code-review", Description: "Review code"},
			{Name: "summarize", Description: "Summarize content"},
		},
	}
	g.Router().AddClient(client)

	result, err := g.HandleResourcesList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result.Resources))
	}

	for _, r := range result.Resources {
		if r.MimeType != "text/markdown" {
			t.Errorf("expected mimeType 'text/markdown', got %q", r.MimeType)
		}
		if r.URI != "skills://registry/"+r.Name {
			t.Errorf("expected URI 'skills://registry/%s', got %q", r.Name, r.URI)
		}
	}
}

func TestGateway_HandleResourcesList_Empty(t *testing.T) {
	g := NewGateway()

	result, err := g.HandleResourcesList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resources == nil {
		t.Fatal("expected non-nil resources slice")
	}
	if len(result.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(result.Resources))
	}
}

func TestGateway_HandleResourcesRead(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "code-review",
				Content: "Please review the following code.",
			},
		},
	}
	g.Router().AddClient(client)

	result, err := g.HandleResourcesRead(ResourcesReadParams{URI: "skills://registry/code-review"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Contents))
	}
	if result.Contents[0].URI != "skills://registry/code-review" {
		t.Errorf("expected URI 'skills://registry/code-review', got %q", result.Contents[0].URI)
	}
	if result.Contents[0].MimeType != "text/markdown" {
		t.Errorf("expected mimeType 'text/markdown', got %q", result.Contents[0].MimeType)
	}
	if result.Contents[0].Text != "Please review the following code." {
		t.Errorf("expected prompt content, got %q", result.Contents[0].Text)
	}
}

func TestGateway_HandleResourcesRead_InvalidURI(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "https://example.com/foo"})
	if err == nil {
		t.Fatal("expected error for non-prompt:// URI")
	}
	if !strings.Contains(err.Error(), "unsupported URI scheme") {
		t.Errorf("expected 'unsupported URI scheme' error, got %q", err.Error())
	}
}

func TestGateway_HandleResourcesRead_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent prompt")
	}
}

func TestGateway_HandleResourcesRead_NoRegistry(t *testing.T) {
	g := NewGateway()

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://anything"})
	if err == nil {
		t.Fatal("expected error when no registry")
	}
}

func TestGateway_HandlePromptsGet_NilArguments(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "simple",
				Content: "Hello world",
			},
		},
	}
	g.Router().AddClient(client)

	// nil arguments map should work for prompts without required args
	result, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "simple",
		Arguments: nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Messages[0].Content.Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", result.Messages[0].Content.Text)
	}
}

func TestGateway_HandlePromptsGet_RequiredArgumentMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts: []PromptData{
			{
				Name:    "greet",
				Content: "Hello {{name}}!",
				Arguments: []PromptArgumentData{
					{Name: "name", Description: "User name", Required: true},
				},
			},
		},
	}
	g.Router().AddClient(client)

	_, err := g.HandlePromptsGet(PromptsGetParams{
		Name:      "greet",
		Arguments: map[string]string{},
	})
	if err == nil {
		t.Fatal("expected error for missing required argument")
	}
	if !strings.Contains(err.Error(), "required argument") {
		t.Errorf("expected 'required argument' in error, got %q", err.Error())
	}
}

func TestGateway_HandleResourcesRead_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "registry", nil)
	client := &promptProviderClient{
		AgentClient: mock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(client)

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "skills://registry/"})
	if err == nil {
		t.Fatal("expected error for empty resource name")
	}
	if !strings.Contains(err.Error(), "empty resource name") {
		t.Errorf("expected 'empty resource name' in error, got %q", err.Error())
	}
}

func TestGateway_SetCodeMode(t *testing.T) {
	g := NewGateway()

	// Default is off
	if g.CodeModeStatus() != "off" {
		t.Errorf("expected initial code mode 'off', got %q", g.CodeModeStatus())
	}

	// Enable code mode
	g.SetCodeMode(30 * time.Second)
	if g.CodeModeStatus() != "on" {
		t.Errorf("expected code mode 'on', got %q", g.CodeModeStatus())
	}
}

func TestGateway_CodeModeStatus_Default(t *testing.T) {
	g := NewGateway()
	if g.CodeModeStatus() != "off" {
		t.Errorf("expected 'off', got %q", g.CodeModeStatus())
	}
}

func TestGateway_HandleToolsList_CodeMode(t *testing.T) {
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)

	result, err := g.HandleToolsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Code mode should return meta-tools instead of real tools
	if len(result.Tools) == 0 {
		t.Error("expected code mode meta-tools")
	}

	// Meta-tools should include "search" and "execute"
	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}
	if !toolNames["search"] && !toolNames["execute"] {
		t.Errorf("expected meta-tools 'search' and 'execute', got %v", toolNames)
	}
}

func TestGateway_HandleToolsListForAgent_CodeMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Register a server and agent
	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)
	g.RegisterAgent("agent1", []config.ToolSelector{{Server: "server1"}})

	// Enable code mode
	g.SetCodeMode(30 * time.Second)

	// When code mode is active, HandleToolsListForAgent should return meta-tools
	result, err := g.HandleToolsListForAgent("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return meta-tools, not real tools
	for _, tool := range result.Tools {
		if tool.Name == "server1__tool1" {
			t.Error("expected meta-tools in code mode, not real tools")
		}
	}
}

func TestGateway_RefreshAllTools(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	mock := setupMockAgentClient(ctrl, "server1", []Tool{{Name: "tool1"}})
	g.Router().AddClient(mock)

	ctx := context.Background()
	err := g.RefreshAllTools(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_HandleToolsCallForAgent_MetaTool(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "A tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Register agent
	g.RegisterAgent("my-agent", []config.ToolSelector{{Server: "server1"}})

	ctx := context.Background()

	// Search meta-tool should work via agent path
	result, err := g.HandleToolsCallForAgent(ctx, "my-agent", ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{"query": "tool"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected search to succeed")
	}
}

func TestGateway_HandleToolsCallForAgent_InvalidToolName(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "A tool"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	g.RegisterAgent("my-agent", []config.ToolSelector{{Server: "server1"}})

	ctx := context.Background()

	// Tool name without double underscore should return error
	result, err := g.HandleToolsCallForAgent(ctx, "my-agent", ToolCallParams{
		Name: "invalid-name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for invalid tool name format")
	}
}

func TestGateway_HandleToolsCallForAgent_AccessDenied(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "secret-tool", Description: "Restricted"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Agent only has access to server2, not server1
	g.RegisterAgent("limited-agent", []config.ToolSelector{{Server: "server2"}})

	ctx := context.Background()

	result, err := g.HandleToolsCallForAgent(ctx, "limited-agent", ToolCallParams{
		Name:      "server1__secret-tool",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for access denied")
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "Access denied") {
		t.Error("expected 'Access denied' in error content")
	}
}

func TestGateway_HandleToolsCallForAgent_UnregisteredAgent(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "A tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	ctx := context.Background()

	// Unregistered agent should be allowed (backward compatibility)
	result, err := g.HandleToolsCallForAgent(ctx, "unregistered", ToolCallParams{
		Name:      "server1__tool1",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected unregistered agent to be allowed (backward compat)")
	}
}

func TestGateway_getAgentServerAccess(t *testing.T) {
	g := NewGateway()

	// Unregistered agent returns nil selector and true (allow all)
	selector, allowed := g.getAgentServerAccess("unregistered", "server1")
	if !allowed {
		t.Error("expected allowed=true for unregistered agent")
	}
	if selector != nil {
		t.Error("expected nil selector for unregistered agent")
	}

	// Registered agent with specific server access
	g.RegisterAgent("agent1", []config.ToolSelector{
		{Server: "server1", Tools: []string{"tool1"}},
		{Server: "server2"},
	})

	selector, allowed = g.getAgentServerAccess("agent1", "server1")
	if !allowed {
		t.Error("expected allowed=true for server1")
	}
	if selector == nil {
		t.Fatal("expected non-nil selector for server1")
	}
	if selector.Server != "server1" {
		t.Errorf("expected server 'server1', got '%s'", selector.Server)
	}

	// Access to server2 (no tool whitelist)
	selector, allowed = g.getAgentServerAccess("agent1", "server2")
	if !allowed {
		t.Error("expected allowed=true for server2")
	}
	if selector == nil {
		t.Fatal("expected non-nil selector for server2")
	}

	// Access to server3 (not in agent's allowed list)
	selector, allowed = g.getAgentServerAccess("agent1", "server3")
	if allowed {
		t.Error("expected allowed=false for server3")
	}
	if selector != nil {
		t.Error("expected nil selector for denied server")
	}
}

func TestGateway_isToolAllowedForAgent(t *testing.T) {
	g := NewGateway()

	// Unregistered agent: all tools allowed
	if !g.isToolAllowedForAgent("unregistered", "server1", "any-tool") {
		t.Error("expected all tools allowed for unregistered agent")
	}

	g.RegisterAgent("agent1", []config.ToolSelector{
		{Server: "server1", Tools: []string{"read", "write"}},
		{Server: "server2"}, // all tools allowed
	})

	// Tool in whitelist
	if !g.isToolAllowedForAgent("agent1", "server1", "read") {
		t.Error("expected 'read' to be allowed")
	}

	// Tool not in whitelist
	if g.isToolAllowedForAgent("agent1", "server1", "delete") {
		t.Error("expected 'delete' to be denied")
	}

	// Server with no tool whitelist: all tools allowed
	if !g.isToolAllowedForAgent("agent1", "server2", "anything") {
		t.Error("expected all tools allowed for server2")
	}

	// Server not in allowed list
	if g.isToolAllowedForAgent("agent1", "server3", "tool") {
		t.Error("expected tool denied for disallowed server")
	}
}

func TestGateway_logToolCountHint(t *testing.T) {
	g := NewGateway()
	logBuf := logging.NewLogBuffer(100)
	g.SetLogger(slog.New(logging.NewBufferHandler(logBuf, nil)))

	// Should not warn for <= 50 tools
	g.logToolCountHint(50)
	if g.toolCountWarned {
		t.Error("should not warn for exactly 50 tools")
	}

	// Should warn for > 50 tools
	g.logToolCountHint(51)
	if !g.toolCountWarned {
		t.Error("should warn for 51 tools")
	}

	// Should not warn again (already warned)
	g.logToolCountHint(100)
	// No panic or double warning
}

func TestGateway_HandleToolsList_LogsHint(t *testing.T) {
	g := NewGateway()

	// Add >50 mock tools by creating a mock with many tools
	ctrl := gomock.NewController(t)
	tools := make([]Tool, 55)
	for i := range tools {
		tools[i] = Tool{Name: fmt.Sprintf("tool%d", i), Description: "desc"}
	}
	client := setupMockAgentClient(ctrl, "server1", tools)
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	result, err := g.HandleToolsList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 55 {
		t.Errorf("expected 55 tools, got %d", len(result.Tools))
	}
	if !g.toolCountWarned {
		t.Error("expected toolCountWarned to be true after listing >50 tools")
	}
}

func TestGateway_HandleToolsCall_CodeMode(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetCodeMode(30 * time.Second)

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "A test tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	ctx := context.Background()

	// Meta-tool search should be handled by code mode
	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{"query": ""},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected search to succeed")
	}
	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}
}

func TestGateway_HandleResourcesRead_LegacyPromptURI(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Set up registry provider
	registryMock := setupMockAgentClient(ctrl, "registry", nil)
	pp := &gatewayTestPromptProvider{
		AgentClient: registryMock,
		prompts: []PromptData{
			{
				Name:        "test-prompt",
				Description: "A prompt",
				Content:     "Hello world",
			},
		},
	}
	g.Router().AddClient(pp)

	// Legacy prompt:// URI should work
	result, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://test-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}
	if result.Contents[0].Text != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", result.Contents[0].Text)
	}
}

func TestGateway_HandleResourcesRead_EmptyNameInURI(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	registryMock := setupMockAgentClient(ctrl, "registry", nil)
	pp := &gatewayTestPromptProvider{
		AgentClient: registryMock,
		prompts:     []PromptData{},
	}
	g.Router().AddClient(pp)

	// Empty name after prefix strip
	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "skills://registry/"})
	if err == nil {
		t.Fatal("expected error for empty resource name")
	}
	if !strings.Contains(err.Error(), "empty resource name") {
		t.Errorf("expected 'empty resource name' error, got: %v", err)
	}
}

// gatewayTestPromptProvider wraps a MockAgentClient for gateway-level prompt tests.
type gatewayTestPromptProvider struct {
	AgentClient
	prompts []PromptData
}

func (p *gatewayTestPromptProvider) ListPromptData() []PromptData {
	return p.prompts
}

func (p *gatewayTestPromptProvider) GetPromptData(name string) (*PromptData, error) {
	for _, pd := range p.prompts {
		if pd.Name == name {
			return &pd, nil
		}
	}
	return nil, fmt.Errorf("prompt %q: not found", name)
}

func TestGateway_HandleToolsListForAgent_AllServers(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Agent with access to server1 (all tools)
	g.RegisterAgent("agent1", []config.ToolSelector{{Server: "server1"}})

	result, err := g.HandleToolsListForAgent("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(result.Tools))
	}
}

func TestGateway_HandleToolsListForAgent_ToolWhitelist(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "read", Description: "Read"},
		{Name: "write", Description: "Write"},
		{Name: "delete", Description: "Delete"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Agent with access to only read and write tools
	g.RegisterAgent("agent1", []config.ToolSelector{
		{Server: "server1", Tools: []string{"read", "write"}},
	})

	result, err := g.HandleToolsListForAgent("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Errorf("expected 2 filtered tools, got %d", len(result.Tools))
	}
	for _, tool := range result.Tools {
		if strings.Contains(tool.Name, "delete") {
			t.Error("should not include 'delete' tool")
		}
	}
}

func TestGateway_HandleToolsListForAgent_Unregistered(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Unregistered agent gets all tools
	result, err := g.HandleToolsListForAgent("unregistered")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Errorf("expected 1 tool for unregistered agent, got %d", len(result.Tools))
	}
}

func TestGateway_HandleToolsListForAgent_NoServerAccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Agent with access to server2 only (no server1)
	g.RegisterAgent("agent1", []config.ToolSelector{{Server: "server2"}})

	result, err := g.HandleToolsListForAgent("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(result.Tools))
	}
}

func TestGateway_HandleToolsCallForAgent_NonMetaTool_Succeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "echo", Description: "Echo"},
	})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent("echoed")}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Agent with access
	g.RegisterAgent("agent1", []config.ToolSelector{{Server: "server1"}})

	ctx := context.Background()
	result, err := g.HandleToolsCallForAgent(ctx, "agent1", ToolCallParams{
		Name:      "server1__echo",
		Arguments: map[string]any{"msg": "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected successful tool call")
	}
}

func TestGateway_SetLogger_Nil(t *testing.T) {
	g := NewGateway()
	// Should not panic when passing nil
	g.SetLogger(nil)
	// Logger should remain the default discard logger
	if g.logger == nil {
		t.Error("logger should not be nil after SetLogger(nil)")
	}
}

func TestGateway_buildSSHCommand(t *testing.T) {
	tests := []struct {
		name     string
		cfg      MCPServerConfig
		wantLen  int
		contains []string
	}{
		{
			name: "basic SSH",
			cfg: MCPServerConfig{
				SSHUser: "user",
				SSHHost: "host.example.com",
				Command: []string{"/opt/server"},
			},
			wantLen:  7,
			contains: []string{"ssh", "user@host.example.com", "/opt/server"},
		},
		{
			name: "SSH with identity file",
			cfg: MCPServerConfig{
				SSHUser:         "admin",
				SSHHost:         "10.0.0.1",
				SSHIdentityFile: "~/.ssh/id_ed25519",
				Command:         []string{"/opt/server"},
			},
			contains: []string{"-i", "~/.ssh/id_ed25519"},
		},
		{
			name: "SSH with custom port",
			cfg: MCPServerConfig{
				SSHUser: "admin",
				SSHHost: "10.0.0.1",
				SSHPort: 2222,
				Command: []string{"/opt/server"},
			},
			contains: []string{"-p", "2222"},
		},
		{
			name: "SSH with default port (22) should not add -p",
			cfg: MCPServerConfig{
				SSHUser: "admin",
				SSHHost: "10.0.0.1",
				SSHPort: 22,
				Command: []string{"/opt/server"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := buildSSHCommand(tc.cfg)
			if tc.wantLen > 0 && len(result) != tc.wantLen {
				t.Errorf("expected %d args, got %d: %v", tc.wantLen, len(result), result)
			}
			for _, want := range tc.contains {
				found := false
				for _, arg := range result {
					if arg == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected command to contain '%s', got: %v", want, result)
				}
			}
			// Default port 22 should not have -p
			if tc.cfg.SSHPort == 22 {
				for _, arg := range result {
					if arg == "-p" {
						t.Error("should not include -p for default port 22")
					}
				}
			}
		})
	}
}

func TestGateway_getAgentFilteredTools(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Unregistered agent: get all tools
	tools, err := g.getAgentFilteredTools("unregistered")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	// Registered agent with tool whitelist
	g.RegisterAgent("agent1", []config.ToolSelector{
		{Server: "server1", Tools: []string{"tool1"}},
	})
	tools, err = g.getAgentFilteredTools("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}

	// Agent with no server access
	g.RegisterAgent("agent2", []config.ToolSelector{
		{Server: "other-server"},
	})
	tools, err = g.getAgentFilteredTools("agent2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestGateway_Status_SortsAlphabetically(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	clientB := setupMockAgentClient(ctrl, "bravo", []Tool{{Name: "t1"}})
	clientA := setupMockAgentClient(ctrl, "alpha", []Tool{{Name: "t2"}})
	g.Router().AddClient(clientB)
	g.Router().AddClient(clientA)
	g.SetServerMeta(MCPServerConfig{Name: "bravo"})
	g.SetServerMeta(MCPServerConfig{Name: "alpha"})

	statuses := g.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0].Name != "alpha" {
		t.Errorf("expected first status 'alpha', got '%s'", statuses[0].Name)
	}
	if statuses[1].Name != "bravo" {
		t.Errorf("expected second status 'bravo', got '%s'", statuses[1].Name)
	}
}

func TestGateway_Status_ExcludesNonMCPClients(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	// Add a client without server metadata (e.g., an A2A adapter)
	client := setupMockAgentClient(ctrl, "adapter1", []Tool{{Name: "t1"}})
	g.Router().AddClient(client)
	// Don't call SetServerMeta for adapter1

	statuses := g.Status()
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses (non-MCP client excluded), got %d", len(statuses))
	}
}

func TestGateway_Status_OpenAPISpec(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()

	client := setupMockAgentClient(ctrl, "api-server", []Tool{{Name: "get"}})
	g.Router().AddClient(client)
	g.SetServerMeta(MCPServerConfig{
		Name:    "api-server",
		OpenAPI: true,
		OpenAPIConfig: &OpenAPIClientConfig{
			Spec: "https://api.example.com/spec.json",
		},
	})

	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].OpenAPI {
		t.Error("expected OpenAPI=true")
	}
	if statuses[0].OpenAPISpec != "https://api.example.com/spec.json" {
		t.Errorf("expected OpenAPI spec URL, got '%s'", statuses[0].OpenAPISpec)
	}
}

func TestSearchIndex_ToolCount(t *testing.T) {
	tools := []Tool{
		{Name: "tool1"},
		{Name: "tool2"},
		{Name: "tool3"},
	}
	idx := NewSearchIndex(tools)
	if idx.ToolCount() != 3 {
		t.Errorf("expected ToolCount=3, got %d", idx.ToolCount())
	}

	emptyIdx := NewSearchIndex(nil)
	if emptyIdx.ToolCount() != 0 {
		t.Errorf("expected ToolCount=0 for empty index, got %d", emptyIdx.ToolCount())
	}
}

func TestCodeMode_HandleCallWithScope_UnknownTool(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)

	// A tool name that is neither search nor execute
	params := ToolCallParams{
		Name:      "unknown_meta_tool",
		Arguments: map[string]any{},
	}

	result, err := cm.HandleCallWithScope(context.Background(), params, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for unknown code mode tool")
	}
	if !strings.Contains(result.Content[0].Text, "Unknown code mode tool") {
		t.Errorf("expected 'Unknown code mode tool' message, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_SyntaxError(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": "const x = {;"},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for syntax error")
	}
	if !strings.Contains(result.Content[0].Text, "syntax") {
		t.Errorf("expected syntax error hint, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_AccessDenied(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			t.Fatal("should not call tool")
			return nil, nil
		},
	}

	allowedTools := []Tool{{Name: "server__allowed"}}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": `mcp.callTool("server", "forbidden", {});`},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, allowedTools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for access denied")
	}
	if !strings.Contains(result.Content[0].Text, "access denied") {
		t.Errorf("expected 'access denied' hint, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_Timeout(t *testing.T) {
	cm := NewCodeMode(100 * time.Millisecond)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": "while(true) {}"},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for timeout")
	}
}

func TestCodeMode_HandleExecute_CodeTooLarge(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": strings.Repeat("x", MaxCodeSize+1)},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for code too large")
	}
	if !strings.Contains(result.Content[0].Text, "code too large") {
		t.Errorf("expected 'code too large' hint, got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_NoOutput(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": "undefined;"},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected success for undefined result")
	}
	if result.Content[0].Text != "(no output)" {
		t.Errorf("expected '(no output)', got: %s", result.Content[0].Text)
	}
}

func TestCodeMode_HandleExecute_WithConsole(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	caller := &mockToolCaller{
		callFn: func(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{}, nil
		},
	}

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{"code": `console.log("hello"); "result";`},
	}

	result, err := cm.HandleCall(context.Background(), params, caller, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected success")
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "result") {
		t.Errorf("expected 'result' in output, got: %s", text)
	}
	if !strings.Contains(text, "Console Output") {
		t.Errorf("expected 'Console Output' in output, got: %s", text)
	}
}

func TestCodeMode_HandleExecute_MissingCodeParam(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)

	params := ToolCallParams{
		Name:      MetaToolExecute,
		Arguments: map[string]any{}, // no "code" key
	}

	result, err := cm.HandleCall(context.Background(), params, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for missing code param")
	}
	if !strings.Contains(result.Content[0].Text, "'code' parameter is required") {
		t.Errorf("expected 'code parameter required' message, got: %s", result.Content[0].Text)
	}
}

func TestGateway_SetDockerClient(t *testing.T) {
	g := NewGateway()
	g.SetDockerClient(nil) // Should not panic
}

func TestGateway_Close_WithoutStartCleanup(t *testing.T) {
	g := NewGateway()
	// Close without StartCleanup should not panic (cancel is nil)
	g.Close()
}

func TestGateway_Close_WithStartCleanup(t *testing.T) {
	g := NewGateway()
	ctx := context.Background()
	g.StartCleanup(ctx)
	// Close should cancel the cleanup goroutine
	g.Close()
}

func TestCodeMode_HandleSearch_NoQuery(t *testing.T) {
	cm := NewCodeMode(5 * time.Second)
	tools := []Tool{
		{Name: "server__tool1", Description: "Tool 1"},
		{Name: "server__tool2", Description: "Tool 2"},
	}

	params := ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{},
	}

	result, err := cm.HandleCall(context.Background(), params, nil, tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected success for search with no query")
	}
	if !strings.Contains(result.Content[0].Text, "Found 2 tool(s)") {
		t.Errorf("expected all tools returned, got: %s", result.Content[0].Text)
	}
}

// --- Format Conversion Tests ---

func TestGateway_ResolveOutputFormat(t *testing.T) {
	g := NewGateway()

	// Default: no config → json
	if got := g.resolveOutputFormat("server1"); got != "json" {
		t.Errorf("default = %q, want %q", got, "json")
	}

	// Gateway default set
	g.SetDefaultOutputFormat("toon")
	if got := g.resolveOutputFormat("server1"); got != "toon" {
		t.Errorf("with gateway default = %q, want %q", got, "toon")
	}

	// Server override takes precedence
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "csv"})
	if got := g.resolveOutputFormat("server1"); got != "csv" {
		t.Errorf("with server override = %q, want %q", got, "csv")
	}

	// Other servers still use gateway default
	if got := g.resolveOutputFormat("server2"); got != "toon" {
		t.Errorf("other server = %q, want %q", got, "toon")
	}

	// Server with empty format uses gateway default
	g.SetServerMeta(MCPServerConfig{Name: "server3", OutputFormat: ""})
	if got := g.resolveOutputFormat("server3"); got != "toon" {
		t.Errorf("empty server format = %q, want %q", got, "toon")
	}
}

func TestGateway_HandleToolsCall_FormatConversion_TOON(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	g.SetTokenCounter(token.NewHeuristicCounter(4))
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fetch", Description: "Fetch data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`{"name":"John","age":30,"active":true}`)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fetch",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected success")
	}

	text := result.Content[0].Text
	// TOON output should contain key-value pairs (not JSON braces)
	if strings.Contains(text, "{") {
		t.Errorf("expected TOON format (no braces), got: %s", text)
	}
	if !strings.Contains(text, "name: John") {
		t.Errorf("expected 'name: John' in TOON output, got: %s", text)
	}
	if !strings.Contains(text, "age: 30") {
		t.Errorf("expected 'age: 30' in TOON output, got: %s", text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_CSV(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetTokenCounter(token.NewHeuristicCounter(4))
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "list", Description: "List items"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`[{"name":"Alice","age":25},{"name":"Bob","age":30}]`)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "csv"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].Text
	// CSV should have header row with sorted keys
	if !strings.Contains(text, "age,name") {
		t.Errorf("expected CSV header 'age,name', got: %s", text)
	}
	if !strings.Contains(text, "25,Alice") {
		t.Errorf("expected CSV row '25,Alice', got: %s", text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_NonJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "say", Description: "Say something"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent("Hello, this is plain text")},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__say",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-JSON text should pass through unchanged
	if result.Content[0].Text != "Hello, this is plain text" {
		t.Errorf("expected unchanged text, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_LargePayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	g.SetLogger(logging.NewDiscardLogger())
	// Disable truncation so this test focuses on format conversion skip behavior.
	g.SetMaxToolResultBytes(maxFormatPayloadSize * 10)
	ctx := context.Background()

	// Create a payload > 1MB
	largeJSON := `{"data":"` + strings.Repeat("x", maxFormatPayloadSize+1) + `"}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "big", Description: "Big response"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(largeJSON)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__big",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Large payload should be left unchanged
	if result.Content[0].Text != largeJSON {
		t.Error("expected large payload to be left unchanged")
	}
}

func TestGateway_HandleToolsCall_Truncation(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(100)
	ctx := context.Background()

	largeText := strings.Repeat("a", 500)

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fetch", Description: "Fetch data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(largeText)}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fetch",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := result.Content[0].Text
	if len(text) >= len(largeText) {
		t.Errorf("expected result to be truncated, got length %d", len(text))
	}
	if !strings.Contains(text, "[truncated: 500 bytes, showing first 100 bytes]") {
		t.Errorf("expected truncation suffix in result, got: %s", text)
	}
}

func TestGateway_HandleToolsCall_Truncation_UnderLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	g.SetMaxToolResultBytes(1000)
	ctx := context.Background()

	smallText := "small result"

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fetch", Description: "Fetch data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&ToolCallResult{Content: []Content{NewTextContent(smallText)}}, nil,
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fetch",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content[0].Text != smallText {
		t.Errorf("expected unchanged result, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_ServerOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	g.SetTokenCounter(token.NewHeuristicCounter(4))
	ctx := context.Background()

	jsonContent := `{"key":"value"}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "get", Description: "Get data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(jsonContent)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	// Server overrides to json — should skip conversion
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "json"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__get",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// JSON override: content unchanged
	if result.Content[0].Text != jsonContent {
		t.Errorf("expected JSON passthrough, got: %s", result.Content[0].Text)
	}
}

func TestGateway_HandleToolsCall_FormatConversion_ErrorResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	ctx := context.Background()

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "fail", Description: "Failing tool"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(`{"error":"something broke"}`)},
				IsError: true,
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__fail",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Error results should not be format-converted
	if !result.IsError {
		t.Error("expected error result")
	}
	if result.Content[0].Text != `{"error":"something broke"}` {
		t.Errorf("expected unchanged error content, got: %s", result.Content[0].Text)
	}
}

// mockFormatSavingsRecorder captures RecordWithSavings calls for testing.
type mockFormatSavingsRecorder struct {
	calls []formatSavingsCall
}

type formatSavingsCall struct {
	serverName      string
	originalTokens  int
	formattedTokens int
}

func (m *mockFormatSavingsRecorder) RecordFormatSavings(serverName string, originalTokens, formattedTokens int) {
	m.calls = append(m.calls, formatSavingsCall{serverName: serverName, originalTokens: originalTokens, formattedTokens: formattedTokens})
}

func TestGateway_HandleToolsCall_FormatConversion_RecordsSavings(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetDefaultOutputFormat("toon")
	counter := token.NewHeuristicCounter(4)
	g.SetTokenCounter(counter)
	recorder := &mockFormatSavingsRecorder{}
	g.SetFormatSavingsRecorder(recorder)
	ctx := context.Background()

	jsonContent := `{"name":"John Doe","email":"john@example.com","active":true,"count":42}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "get", Description: "Get user"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(jsonContent)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1"})

	_, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__get",
		Arguments: map[string]any{"id": "123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 RecordWithSavings call, got %d", len(recorder.calls))
	}

	call := recorder.calls[0]
	if call.serverName != "server1" {
		t.Errorf("serverName = %q, want %q", call.serverName, "server1")
	}
	if call.originalTokens <= 0 {
		t.Error("expected positive originalTokens")
	}
	if call.formattedTokens <= 0 {
		t.Error("expected positive formattedTokens")
	}
	// TOON is typically shorter than JSON
	if call.originalTokens <= call.formattedTokens {
		t.Errorf("expected originalTokens (%d) > formattedTokens (%d) for TOON conversion",
			call.originalTokens, call.formattedTokens)
	}
}

func TestGateway_SetDefaultOutputFormat(t *testing.T) {
	g := NewGateway()

	g.SetDefaultOutputFormat("toon")
	if got := g.resolveOutputFormat("any-server"); got != "toon" {
		t.Errorf("after SetDefaultOutputFormat = %q, want %q", got, "toon")
	}

	g.SetDefaultOutputFormat("")
	if got := g.resolveOutputFormat("any-server"); got != "json" {
		t.Errorf("empty format = %q, want %q", got, "json")
	}
}

func TestGateway_HandleToolsCall_FormatConversion_CSVNonTabular(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.SetLogger(logging.NewDiscardLogger())
	ctx := context.Background()

	// Non-tabular JSON (object, not array) should fail CSV and leave unchanged
	jsonContent := `{"key":"value","nested":{"a":1}}`

	client := setupMockAgentClient(ctrl, "server1", []Tool{
		{Name: "get", Description: "Get data"},
	})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
			return &ToolCallResult{
				Content: []Content{NewTextContent(jsonContent)},
			}, nil
		},
	).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "server1", OutputFormat: "csv"})

	result, err := g.HandleToolsCall(ctx, ToolCallParams{
		Name:      "server1__get",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CSV conversion should fail for non-tabular data, leaving content unchanged
	if result.Content[0].Text != jsonContent {
		t.Errorf("expected unchanged content on CSV failure, got: %s", result.Content[0].Text)
	}
}

