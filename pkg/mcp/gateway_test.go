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

	result, err := g.HandleInitialize(params)
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

	// Should create a session
	sessions := g.Sessions().List()
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
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

	_, err := g.HandleInitialize(InitializeParams{
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

	result, err := g.HandleInitialize(params)
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

	result, err := g.HandleInitialize(params)
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
		if r.MimeType != "text/plain" {
			t.Errorf("expected mimeType 'text/plain', got %q", r.MimeType)
		}
		if r.URI != "prompt://"+r.Name {
			t.Errorf("expected URI 'prompt://%s', got %q", r.Name, r.URI)
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

	result, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://code-review"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Contents))
	}
	if result.Contents[0].URI != "prompt://code-review" {
		t.Errorf("expected URI 'prompt://code-review', got %q", result.Contents[0].URI)
	}
	if result.Contents[0].MimeType != "text/plain" {
		t.Errorf("expected mimeType 'text/plain', got %q", result.Contents[0].MimeType)
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

	_, err := g.HandleResourcesRead(ResourcesReadParams{URI: "prompt://"})
	if err == nil {
		t.Fatal("expected error for empty prompt name")
	}
	if !strings.Contains(err.Error(), "empty prompt name") {
		t.Errorf("expected 'empty prompt name' in error, got %q", err.Error())
	}
}

