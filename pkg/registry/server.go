package registry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Server is an in-process MCP server that serves Agent Skills as prompts
// and executable workflow skills as MCP tools.
// It implements mcp.AgentClient so it can be registered with the gateway router,
// and mcp.PromptProvider so the gateway can serve skills via MCP prompts and resources.
//
// Skills without a workflow are knowledge documents exposed as prompts.
// Skills with a workflow are executable and exposed as MCP tools that
// route through the gateway's ToolCaller for step execution.
type Server struct {
	store    *Store
	executor *Executor // nil if no ToolCaller was provided

	mu          sync.RWMutex
	initialized bool
	serverInfo  mcp.ServerInfo
}

// Compile-time checks.
var (
	_ mcp.AgentClient    = (*Server)(nil)
	_ mcp.PromptProvider = (*Server)(nil)
)

// New creates a registry server. If caller is non-nil, executable
// skills (those with workflows) are exposed as MCP tools that route
// through the caller. Skills without workflows remain knowledge documents.
func New(store *Store, opts ...ServerOption) *Server {
	s := &Server{
		store: store,
		serverInfo: mcp.ServerInfo{
			Name:    "registry",
			Version: "1.0.0",
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ServerOption configures the registry server.
type ServerOption func(*Server)

// WithToolCaller enables workflow execution through the given ToolCaller.
func WithToolCaller(caller ToolCaller, logger *slog.Logger, opts ...ExecutorOption) ServerOption {
	return func(s *Server) {
		if caller != nil {
			s.executor = NewExecutor(caller, logger, opts...)
		}
	}
}

// SetLogger updates the executor's logger if one is configured.
func (s *Server) SetLogger(logger *slog.Logger) {
	if s.executor != nil {
		s.executor.SetLogger(logger)
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

// Tools returns executable skills as MCP tools.
// Skills without workflows are not included (they remain prompts only).
func (s *Server) Tools() []mcp.Tool {
	if s.executor == nil {
		return nil
	}
	var tools []mcp.Tool
	for _, sk := range s.store.ActiveSkills() {
		if !sk.IsExecutable() {
			continue
		}
		tools = append(tools, sk.ToMCPTool())
	}
	return tools
}

// CallTool dispatches to the executor for executable skills.
// Non-executable skills return an informational error.
func (s *Server) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	sk, err := s.store.GetSkill(name)
	if err != nil {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	if !sk.IsExecutable() {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent("This skill is a knowledge document, not executable.")},
			IsError: true,
		}, nil
	}
	if s.executor == nil {
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent("Workflow execution is not available (no ToolCaller configured).")},
			IsError: true,
		}, nil
	}
	return s.executor.Execute(ctx, sk, arguments)
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
