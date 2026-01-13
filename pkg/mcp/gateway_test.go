package mcp

import (
	"context"
	"fmt"
	"testing"
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
	if info.Name != "agentlab-gateway" {
		t.Errorf("expected server name 'agentlab-gateway', got '%s'", info.Name)
	}
	if info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", info.Version)
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
	if result.ServerInfo.Name != "agentlab-gateway" {
		t.Errorf("expected server name 'agentlab-gateway', got '%s'", result.ServerInfo.Name)
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
