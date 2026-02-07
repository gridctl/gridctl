package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
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
	g := NewGateway()

	// Add a mock client with tools
	client := NewMockAgentClient("agent1", []Tool{
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
	g := NewGateway()
	ctx := context.Background()

	client := NewMockAgentClient("agent1", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	client.SetCallToolFn(func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
		msg := args["message"].(string)
		return &ToolCallResult{
			Content: []Content{NewTextContent("Echo: " + msg)},
		}, nil
	})
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
	g := NewGateway()
	ctx := context.Background()

	client := NewMockAgentClient("agent1", []Tool{
		{Name: "fail", Description: "Failing tool"},
	})
	client.SetCallToolFn(func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
		return nil, fmt.Errorf("agent error")
	})
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
	client := NewMockAgentClient("agent1", []Tool{
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
	g := NewGateway()

	client := NewMockAgentClient("agent1", []Tool{
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
	g := NewGateway()

	// Add two mock clients with tools
	client1 := NewMockAgentClient("server1", []Tool{
		{Name: "read", Description: "Read tool"},
		{Name: "write", Description: "Write tool"},
		{Name: "delete", Description: "Delete tool"},
	})
	client2 := NewMockAgentClient("server2", []Tool{
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
	g := NewGateway()
	ctx := context.Background()

	// Add a mock client with tools
	client := NewMockAgentClient("server1", []Tool{
		{Name: "allowed", Description: "Allowed tool"},
		{Name: "restricted", Description: "Restricted tool"},
	})
	client.SetCallToolFn(func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
		return &ToolCallResult{
			Content: []Content{NewTextContent("called " + name)},
		}, nil
	})
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
	g := NewGateway()
	ctx := context.Background()

	// Set up log buffer to capture logs
	logBuffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(logBuffer, nil)
	g.SetLogger(slog.New(handler))

	// Add a mock client
	client := NewMockAgentClient("server1", []Tool{
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
	client := NewMockAgentClient("test-server", []Tool{
		{Name: "echo", Description: "Echo tool"},
	})
	client.SetCallToolFn(func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
		return &ToolCallResult{
			Content: []Content{NewTextContent("ok")},
		}, nil
	})
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
