package mcp

import (
	"context"
)

// MockAgentClient is a test double for the AgentClient interface.
type MockAgentClient struct {
	name        string
	tools       []Tool
	initialized bool
	serverInfo  ServerInfo
	callToolFn  func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
}

// NewMockAgentClient creates a new mock agent client for testing.
func NewMockAgentClient(name string, tools []Tool) *MockAgentClient {
	return &MockAgentClient{
		name:        name,
		tools:       tools,
		initialized: true,
		serverInfo:  ServerInfo{Name: name, Version: "1.0.0"},
	}
}

func (m *MockAgentClient) Name() string {
	return m.name
}

func (m *MockAgentClient) Initialize(ctx context.Context) error {
	m.initialized = true
	return nil
}

func (m *MockAgentClient) RefreshTools(ctx context.Context) error {
	return nil
}

func (m *MockAgentClient) Tools() []Tool {
	return m.tools
}

func (m *MockAgentClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	if m.callToolFn != nil {
		return m.callToolFn(ctx, name, arguments)
	}
	return &ToolCallResult{
		Content: []Content{NewTextContent("mock result for " + name)},
	}, nil
}

func (m *MockAgentClient) IsInitialized() bool {
	return m.initialized
}

func (m *MockAgentClient) ServerInfo() ServerInfo {
	return m.serverInfo
}

// SetCallToolFn sets a custom function to handle CallTool invocations.
func (m *MockAgentClient) SetCallToolFn(fn func(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)) {
	m.callToolFn = fn
}
