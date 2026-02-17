package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Server is an in-process MCP server that provides skill management.
// It implements mcp.AgentClient so it can be registered with the gateway router.
type Server struct {
	store      *Store
	toolCaller mcp.ToolCaller

	mu          sync.RWMutex
	initialized bool
	tools       []mcp.Tool
	serverInfo  mcp.ServerInfo
}

// Compile-time check that Server implements required interfaces.
var _ mcp.AgentClient = (*Server)(nil)

// New creates a registry server.
// The toolCaller parameter allows the registry to execute tools from other
// MCP servers when running skills. Pass nil if skill execution is not needed.
func New(store *Store, toolCaller mcp.ToolCaller) *Server {
	return &Server{
		store:      store,
		toolCaller: toolCaller,
		serverInfo: mcp.ServerInfo{
			Name:    "registry",
			Version: "1.0.0",
		},
	}
}

// Name returns "registry".
func (s *Server) Name() string {
	return "registry"
}

// Initialize loads the store and builds the MCP tool list from active skills.
func (s *Server) Initialize(ctx context.Context) error {
	if err := s.store.Load(); err != nil {
		return fmt.Errorf("loading registry store: %w", err)
	}

	s.refreshTools()

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	return nil
}

// RefreshTools re-scans the store and rebuilds the tools list.
func (s *Server) RefreshTools(ctx context.Context) error {
	if err := s.store.Load(); err != nil {
		return fmt.Errorf("reloading registry store: %w", err)
	}
	s.refreshTools()
	return nil
}

// Tools returns the cached tools list.
func (s *Server) Tools() []mcp.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tools
}

// CallTool looks up a skill by name and returns its body as content.
func (s *Server) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	skill, err := s.store.GetSkill(name)
	if err != nil {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("skill not found: %s", name))},
			IsError: true,
		}, nil
	}

	if skill.State != StateActive {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("skill is not active: %s (state: %s)", name, skill.State))},
			IsError: true,
		}, nil
	}

	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(skill.Body)},
	}, nil
}

// IsInitialized returns whether the server has been initialized.
func (s *Server) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

// ServerInfo returns server information.
func (s *Server) ServerInfo() mcp.ServerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serverInfo
}

// Store returns the underlying store for REST API access.
func (s *Server) Store() *Store {
	return s.store
}

// HasContent returns true if the registry has any skills.
func (s *Server) HasContent() bool {
	return s.store.HasContent()
}

// refreshTools rebuilds the MCP tool list from active skills in the store.
func (s *Server) refreshTools() {
	skills := s.store.ActiveSkills()
	tools := make([]mcp.Tool, len(skills))
	for i, sk := range skills {
		tools[i] = skillToTool(sk)
	}

	s.mu.Lock()
	s.tools = tools
	s.mu.Unlock()
}

// skillToTool converts an AgentSkill to an MCP Tool.
func skillToTool(sk *AgentSkill) mcp.Tool {
	schema := mcp.InputSchemaObject{
		Type:       "object",
		Properties: make(map[string]mcp.Property),
	}
	schemaBytes, _ := json.Marshal(schema)
	return mcp.Tool{
		Name:        sk.Name,
		Description: sk.Description,
		InputSchema: schemaBytes,
	}
}
