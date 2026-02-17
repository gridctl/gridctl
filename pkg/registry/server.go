package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Server is an in-process MCP server that serves Agent Skills as prompts.
// It implements mcp.AgentClient so it can be registered with the gateway router,
// and mcp.PromptProvider so the gateway can serve skills via MCP prompts and resources.
//
// Agent Skills are knowledge documents, not executable tools. The server exposes
// them as MCP prompts (prompts/list, prompts/get) and resources (resources/list,
// resources/read) rather than as MCP tools.
type Server struct {
	store *Store

	mu          sync.RWMutex
	initialized bool
	serverInfo  mcp.ServerInfo
}

// Compile-time checks.
var (
	_ mcp.AgentClient    = (*Server)(nil)
	_ mcp.PromptProvider = (*Server)(nil)
)

// New creates a registry server.
// Unlike the previous version, no ToolCaller is needed — Agent Skills
// are knowledge documents served to clients, not executable operations.
func New(store *Store) *Server {
	return &Server{
		store: store,
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

// Initialize loads the store.
func (s *Server) Initialize(ctx context.Context) error {
	if err := s.store.Load(); err != nil {
		return fmt.Errorf("loading registry store: %w", err)
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	return nil
}

// RefreshTools reloads the store from disk.
func (s *Server) RefreshTools(ctx context.Context) error {
	if err := s.store.Load(); err != nil {
		return fmt.Errorf("reloading registry store: %w", err)
	}
	return nil
}

// Tools returns an empty list. Agent Skills are not exposed as MCP tools.
func (s *Server) Tools() []mcp.Tool {
	return nil
}

// CallTool returns an error — Agent Skills are knowledge documents, not executable tools.
func (s *Server) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent("Agent Skills are knowledge documents, not executable tools. Read the skill content via prompts/get or resources/read instead.")},
		IsError: true,
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

// ListPromptData returns active Agent Skills as MCP PromptData.
// Each skill gets a single optional "context" argument for clients to pass
// additional context when requesting the skill via prompts/get.
func (s *Server) ListPromptData() []mcp.PromptData {
	skills := s.store.ActiveSkills()
	result := make([]mcp.PromptData, len(skills))
	for i, sk := range skills {
		result[i] = mcp.PromptData{
			Name:        sk.Name,
			Description: sk.Description,
			Content:     sk.Body,
			Arguments: []mcp.PromptArgumentData{
				{
					Name:        "context",
					Description: "Additional context for the skill",
					Required:    false,
				},
			},
		}
	}
	return result
}

// GetPromptData returns a specific active skill's content as MCP PromptData.
func (s *Server) GetPromptData(name string) (*mcp.PromptData, error) {
	sk, err := s.store.GetSkill(name)
	if err != nil {
		return nil, err
	}
	if sk.State != StateActive {
		return nil, fmt.Errorf("skill %q is not active (state: %s)", name, sk.State)
	}
	return &mcp.PromptData{
		Name:        sk.Name,
		Description: sk.Description,
		Content:     sk.Body,
		Arguments: []mcp.PromptArgumentData{
			{
				Name:        "context",
				Description: "Additional context for the skill",
				Required:    false,
			},
		},
	}, nil
}
