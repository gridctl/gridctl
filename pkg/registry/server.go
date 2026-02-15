package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Server is an in-process MCP server that provides prompt and skill management.
// It implements mcp.AgentClient so it can be registered with the gateway router.
type Server struct {
	store      *Store
	toolCaller mcp.ToolCaller

	mu          sync.RWMutex
	initialized bool
	tools       []mcp.Tool
	serverInfo  mcp.ServerInfo
}

// Compile-time checks that Server implements required interfaces.
var (
	_ mcp.AgentClient    = (*Server)(nil)
	_ mcp.PromptProvider = (*Server)(nil)
)

// New creates a registry server.
// The toolCaller parameter allows the registry to execute tools from other
// MCP servers when running skill chains. Pass nil if skill execution is
// not needed (e.g., prompts-only mode).
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

// CallTool looks up a skill by name and executes the tool chain.
// Stub: full execution will be built in a future prompt.
func (s *Server) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	return nil, fmt.Errorf("skill execution not yet implemented")
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

// Prompts returns all active prompts (for MCP prompts/list).
func (s *Server) Prompts() []*Prompt {
	return s.store.ActivePrompts()
}

// GetPrompt returns a specific prompt by name (for MCP prompts/get).
func (s *Server) GetPrompt(name string) (*Prompt, error) {
	return s.store.GetPrompt(name)
}

// ListPromptData returns all active prompts as MCP PromptData (implements mcp.PromptProvider).
func (s *Server) ListPromptData() []mcp.PromptData {
	prompts := s.store.ActivePrompts()
	result := make([]mcp.PromptData, len(prompts))
	for i, p := range prompts {
		args := make([]mcp.PromptArgumentData, len(p.Arguments))
		for j, a := range p.Arguments {
			args[j] = mcp.PromptArgumentData{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
				Default:     a.Default,
			}
		}
		result[i] = mcp.PromptData{
			Name:        p.Name,
			Description: p.Description,
			Content:     p.Content,
			Arguments:   args,
		}
	}
	return result
}

// GetPromptData returns a prompt by name as MCP PromptData (implements mcp.PromptProvider).
func (s *Server) GetPromptData(name string) (*mcp.PromptData, error) {
	p, err := s.store.GetPrompt(name)
	if err != nil {
		return nil, err
	}
	args := make([]mcp.PromptArgumentData, len(p.Arguments))
	for j, a := range p.Arguments {
		args[j] = mcp.PromptArgumentData{
			Name:        a.Name,
			Description: a.Description,
			Required:    a.Required,
			Default:     a.Default,
		}
	}
	return &mcp.PromptData{
		Name:        p.Name,
		Description: p.Description,
		Content:     p.Content,
		Arguments:   args,
	}, nil
}

// Store returns the underlying store for REST API access.
func (s *Server) Store() *Store {
	return s.store
}

// HasContent returns true if the registry has any prompts or skills.
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

// skillToTool converts a Skill to an MCP Tool with a JSON Schema input.
func skillToTool(sk *Skill) mcp.Tool {
	schema := mcp.InputSchemaObject{
		Type:       "object",
		Properties: make(map[string]mcp.Property),
	}
	for _, arg := range sk.Input {
		schema.Properties[arg.Name] = mcp.Property{
			Type:        "string",
			Description: arg.Description,
		}
		if arg.Required {
			schema.Required = append(schema.Required, arg.Name)
		}
	}
	schemaBytes, _ := json.Marshal(schema)
	return mcp.Tool{
		Name:        sk.Name,
		Description: sk.Description,
		InputSchema: schemaBytes,
	}
}
