package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/gridctl/gridctl/pkg/agent/skill"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// SkillRegistry is the typed-skill surface the registry server consults
// to expose typed skills as MCP tools. The registry server holds a
// concrete *skill.Registry (from pkg/agent/skill) wrapped in this
// interface so the registry package does not have to import pkg/agent
// directly — that would push pkg/agent into the dependency closure of
// every consumer of pkg/registry.
//
// Implementations MUST be safe for concurrent reads.
type SkillRegistry interface {
	Tools() []mcp.Tool
	CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error)
}

// TSDispatcher is the runtime hook for executing TypeScript-handler
// skills the walker discovered on disk. The registry server hands a
// dispatch each call by handler-path; the dispatcher reads the file,
// runs it through the agent sandbox, and returns the typed-skill
// result. Set via Server.SetTSDispatcher; nil is a valid configuration
// — TS skills then surface as a "no dispatcher wired" error at call
// time rather than failing registry load.
type TSDispatcher interface {
	Dispatch(ctx context.Context, name, sourcePath string, arguments map[string]any) (*mcp.ToolCallResult, error)
}

// Server is an in-process MCP server that serves Agent Skills as prompts.
// It implements mcp.AgentClient so it can be registered with the gateway router,
// and mcp.PromptProvider so the gateway can serve skills via MCP prompts and resources.
type Server struct {
	store *Store

	mu            sync.RWMutex
	initialized   bool
	serverInfo    mcp.ServerInfo
	skillRegistry SkillRegistry
	tsDispatcher  TSDispatcher
}

// Compile-time checks.
var (
	_ mcp.AgentClient    = (*Server)(nil)
	_ mcp.PromptProvider = (*Server)(nil)
)

// New creates a registry server that serves skills as MCP prompts.
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

// SetSkillRegistry installs the typed-skill registry the server
// consults for programmatically registered skills (Go skills, plus
// any other in-process Definitions). Pass nil to detach.
func (s *Server) SetSkillRegistry(r SkillRegistry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skillRegistry = r
}

// SetTSDispatcher installs the TypeScript dispatcher the server uses
// to execute skill.ts handlers discovered by the walker. Pass nil to
// detach.
func (s *Server) SetTSDispatcher(d TSDispatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tsDispatcher = d
}

// Tools returns the registered typed skills as MCP tool entries — the
// programmatically registered skills first, then any TS-handler
// skills the walker found on disk. The returned slice is sorted by
// name so the wire output is deterministic across reloads.
//
// Phase C: Markdown-only skills (no skill.go / skill.ts sibling)
// remain prompts and do not appear here. Go-handler skills are not
// exposed by the walker either — they require an explicit
// registration through SetSkillRegistry, which Phase G will do as
// part of `gridctl agent build`.
func (s *Server) Tools() []mcp.Tool {
	s.mu.RLock()
	skillRegistry := s.skillRegistry
	tsDispatcher := s.tsDispatcher
	s.mu.RUnlock()

	out := []mcp.Tool{}
	seen := make(map[string]bool)

	if skillRegistry != nil {
		for _, t := range skillRegistry.Tools() {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			out = append(out, t)
		}
	}

	if tsDispatcher != nil {
		for _, sk := range s.store.ListSkills() {
			if sk.State != StateActive || sk.HandlerLanguage != "ts" || seen[sk.Name] {
				continue
			}
			seen[sk.Name] = true
			out = append(out, mcp.Tool{
				Name:        sk.Name,
				Description: sk.Description,
				InputSchema: defaultInputSchema(),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CallTool dispatches a registered typed skill. The lookup order is:
//
//  1. The typed-skill registry (programmatic registrations — Go skills
//     etc.). This is the path Phase D's orchestrator and Phase G's
//     CLI will most often hit.
//  2. The TS dispatcher (skill.ts handlers discovered by the walker).
//
// Tools that are not registered surface the same error every other
// AgentClient does, so the gateway router's "no such tool" path works
// unchanged.
func (s *Server) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	s.mu.RLock()
	skillRegistry := s.skillRegistry
	tsDispatcher := s.tsDispatcher
	s.mu.RUnlock()

	if skillRegistry != nil {
		res, err := skillRegistry.CallTool(ctx, name, arguments)
		if err == nil {
			return res, nil
		}
		if !errors.Is(err, skill.ErrSkillNotRegistered) {
			return nil, err
		}
		slog.Debug("registry skill miss; falling through to ts dispatcher", "skill", name)
	}

	if tsDispatcher != nil {
		path, ok := s.store.HandlerPath(name)
		if ok {
			sk, err := s.store.GetSkill(name)
			if err != nil {
				return nil, fmt.Errorf("registry: %w", err)
			}
			if sk.HandlerLanguage != "ts" {
				return nil, fmt.Errorf("skill %q: handler language %q is not runnable in Phase C (only ts is)", name, sk.HandlerLanguage)
			}
			return tsDispatcher.Dispatch(ctx, name, path, arguments)
		}
	}

	return nil, fmt.Errorf("registry: %q is not a registered tool", name)
}

// defaultInputSchema is the permissive "any object" schema TS skills
// surface until the loader infers a typed schema from the source.
// Phase C skills opt into typed input by registering a Go counterpart
// with skill.Define; the TS-only path accepts any object. A typed-TS
// schema-inference path (Phase F) will populate per-skill schemas
// without changing the call site here.
func defaultInputSchema() []byte {
	return []byte(`{"type":"object"}`)
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
