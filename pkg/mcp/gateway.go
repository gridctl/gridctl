package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"
)

// MCPServerConfig contains configuration for connecting to an MCP server.
type MCPServerConfig struct {
	Name            string
	Transport       Transport
	Endpoint        string            // For HTTP/SSE transport
	ContainerID     string            // For Docker Stdio transport
	External        bool              // True for external URL servers (no container)
	LocalProcess    bool              // True for local process servers (no container)
	SSH             bool              // True for SSH servers (remote process over SSH)
	Command         []string          // For local process or SSH transport
	WorkDir         string            // For local process transport
	Env             map[string]string // For local process or SSH transport
	SSHHost         string            // SSH hostname (for SSH servers)
	SSHUser         string            // SSH username (for SSH servers)
	SSHPort         int               // SSH port (for SSH servers, 0 = default 22)
	SSHIdentityFile string            // SSH identity file path (for SSH servers)
	Tools           []string          // Tool whitelist (empty = all tools)
}

// Gateway aggregates multiple MCP servers into a single endpoint.
type Gateway struct {
	router    *Router
	sessions  *SessionManager
	dockerCli dockerclient.DockerClient
	logger    *slog.Logger

	mu          sync.RWMutex
	serverInfo  ServerInfo
	serverMeta  map[string]MCPServerConfig       // name -> config for status reporting
	agentAccess map[string][]config.ToolSelector // agent name -> allowed MCP servers with tool filtering
}

// NewGateway creates a new MCP gateway.
func NewGateway() *Gateway {
	return &Gateway{
		router:   NewRouter(),
		sessions: NewSessionManager(),
		logger:   logging.NewDiscardLogger(),
		serverInfo: ServerInfo{
			Name:    "gridctl-gateway",
			Version: "dev",
		},
		serverMeta:  make(map[string]MCPServerConfig),
		agentAccess: make(map[string][]config.ToolSelector),
	}
}

// SetLogger sets the logger for gateway operations.
// If nil is passed, logging is disabled (default).
func (g *Gateway) SetLogger(logger *slog.Logger) {
	if logger != nil {
		g.logger = logger
	}
}

// SetDockerClient sets the Docker client for stdio transport.
func (g *Gateway) SetDockerClient(cli dockerclient.DockerClient) {
	g.dockerCli = cli
}

// SetVersion sets the gateway version string.
func (g *Gateway) SetVersion(version string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.serverInfo.Version = version
}

// Router returns the tool router.
func (g *Gateway) Router() *Router {
	return g.router
}

// Sessions returns the session manager.
func (g *Gateway) Sessions() *SessionManager {
	return g.sessions
}

// ServerInfo returns the gateway server info.
func (g *Gateway) ServerInfo() ServerInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.serverInfo
}

// RegisterMCPServer registers and initializes an MCP server.
func (g *Gateway) RegisterMCPServer(ctx context.Context, cfg MCPServerConfig) error {
	var agentClient AgentClient

	// Handle SSH servers (they use stdio over SSH)
	if cfg.SSH {
		sshCommand := buildSSHCommand(cfg)
		processClient := NewProcessClient(cfg.Name, sshCommand, cfg.WorkDir, cfg.Env)
		if len(cfg.Tools) > 0 {
			processClient.SetToolWhitelist(cfg.Tools)
		}
		if err := processClient.Connect(ctx); err != nil {
			return fmt.Errorf("starting SSH process %s: %w", cfg.Name, err)
		}
		agentClient = processClient
	} else if cfg.LocalProcess {
		// Handle local process servers (they use stdio but not Docker)
		processClient := NewProcessClient(cfg.Name, cfg.Command, cfg.WorkDir, cfg.Env)
		if len(cfg.Tools) > 0 {
			processClient.SetToolWhitelist(cfg.Tools)
		}
		if err := processClient.Connect(ctx); err != nil {
			return fmt.Errorf("starting process %s: %w", cfg.Name, err)
		}
		agentClient = processClient
	} else {
		switch cfg.Transport {
		case TransportStdio:
			if g.dockerCli == nil {
				return fmt.Errorf("Docker client not set for stdio transport")
			}
			stdioClient := NewStdioClient(cfg.Name, cfg.ContainerID, g.dockerCli)
			if len(cfg.Tools) > 0 {
				stdioClient.SetToolWhitelist(cfg.Tools)
			}
			if err := stdioClient.Connect(ctx); err != nil {
				return fmt.Errorf("connecting to container: %w", err)
			}
			agentClient = stdioClient
		case TransportSSE:
			// SSE transport - uses same HTTP client which handles text/event-stream responses
			httpClient := NewClient(cfg.Name, cfg.Endpoint)
			if len(cfg.Tools) > 0 {
				httpClient.SetToolWhitelist(cfg.Tools)
			}
			// Wait for MCP server to be ready with retries
			if err := g.waitForHTTPServer(ctx, httpClient); err != nil {
				return fmt.Errorf("MCP server %s not ready: %w", cfg.Name, err)
			}
			agentClient = httpClient
		case TransportHTTP, "": // Default to HTTP
			httpClient := NewClient(cfg.Name, cfg.Endpoint)
			if len(cfg.Tools) > 0 {
				httpClient.SetToolWhitelist(cfg.Tools)
			}
			// Wait for MCP server to be ready with retries
			if err := g.waitForHTTPServer(ctx, httpClient); err != nil {
				return fmt.Errorf("MCP server %s not ready: %w", cfg.Name, err)
			}
			agentClient = httpClient
		default:
			return fmt.Errorf("unknown transport: %s", cfg.Transport)
		}
	}

	// Initialize MCP connection
	if err := agentClient.Initialize(ctx); err != nil {
		return fmt.Errorf("initializing MCP server %s: %w", cfg.Name, err)
	}

	// Fetch tools (will be filtered by whitelist if set)
	if err := agentClient.RefreshTools(ctx); err != nil {
		return fmt.Errorf("fetching tools from %s: %w", cfg.Name, err)
	}

	// Store metadata
	func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.serverMeta[cfg.Name] = cfg
	}()

	// Add to router
	g.router.AddClient(agentClient)
	g.router.RefreshTools()

	g.logger.Info("registered MCP server", "name", cfg.Name, "transport", cfg.Transport, "tools", len(agentClient.Tools()))
	return nil
}

// UnregisterMCPServer removes an MCP server from the gateway.
func (g *Gateway) UnregisterMCPServer(name string) {
	g.router.RemoveClient(name)
	g.router.RefreshTools()
}

// RegisterAgent registers an agent and its allowed MCP servers with optional tool filtering.
func (g *Gateway) RegisterAgent(name string, uses []config.ToolSelector) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.agentAccess[name] = uses
}

// UnregisterAgent removes an agent's access configuration.
func (g *Gateway) UnregisterAgent(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.agentAccess, name)
}

// GetAgentAllowedServers returns the MCP servers an agent can access.
// Returns nil if the agent is not registered (allows all for backward compatibility).
func (g *Gateway) GetAgentAllowedServers(agentName string) []config.ToolSelector {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.agentAccess[agentName]
}

// getAgentServerAccess returns the ToolSelector for a specific server if the agent has access.
// Returns nil if the agent doesn't have access to this server, or if the agent is not registered
// (in which case all access is allowed for backward compatibility).
func (g *Gateway) getAgentServerAccess(agentName, serverName string) (*config.ToolSelector, bool) {
	allowed := g.GetAgentAllowedServers(agentName)
	if allowed == nil {
		// Agent not registered - allow all (backward compatibility)
		return nil, true
	}
	for i := range allowed {
		if allowed[i].Server == serverName {
			return &allowed[i], true
		}
	}
	return nil, false
}

// isToolAllowedForAgent checks if an agent can access a specific tool from a server.
// This checks both server-level access and tool-level filtering.
func (g *Gateway) isToolAllowedForAgent(agentName, serverName, toolName string) bool {
	selector, allowed := g.getAgentServerAccess(agentName, serverName)
	if !allowed {
		return false
	}
	if selector == nil {
		// Agent not registered - allow all (backward compatibility)
		return true
	}
	// If no tool list specified, all tools from this server are allowed
	if len(selector.Tools) == 0 {
		return true
	}
	// Check if tool is in the whitelist
	for _, t := range selector.Tools {
		if t == toolName {
			return true
		}
	}
	return false
}

// HandleToolsListForAgent returns tools filtered by agent access permissions.
// This applies both server-level and tool-level filtering.
func (g *Gateway) HandleToolsListForAgent(agentName string) (*ToolsListResult, error) {
	allowed := g.GetAgentAllowedServers(agentName)
	if allowed == nil {
		// Agent not registered - return all tools
		return g.HandleToolsList()
	}

	// Build map of allowed servers to their tool selectors for fast lookup
	serverSelectors := make(map[string]config.ToolSelector)
	for _, selector := range allowed {
		serverSelectors[selector.Server] = selector
	}

	// Filter tools by allowed MCP servers and tool whitelists
	allTools := g.router.AggregatedTools()
	var filteredTools []Tool
	for _, tool := range allTools {
		serverName, originalToolName, err := ParsePrefixedTool(tool.Name)
		if err != nil {
			g.logger.Warn("skipping tool with invalid name format", "name", tool.Name, "error", err)
			continue
		}

		selector, hasServer := serverSelectors[serverName]
		if !hasServer {
			continue
		}

		// If no tool whitelist, include all tools from this server
		if len(selector.Tools) == 0 {
			filteredTools = append(filteredTools, tool)
			continue
		}

		// Check if this specific tool is in the whitelist
		for _, allowedTool := range selector.Tools {
			if allowedTool == originalToolName {
				filteredTools = append(filteredTools, tool)
				break
			}
		}
	}

	return &ToolsListResult{Tools: filteredTools}, nil
}

// HandleToolsCallForAgent routes a tool call with agent access validation.
// This validates both server-level and tool-level access.
func (g *Gateway) HandleToolsCallForAgent(ctx context.Context, agentName string, params ToolCallParams) (*ToolCallResult, error) {
	// Parse the tool name to get the MCP server and original tool name
	serverName, originalToolName, err := ParsePrefixedTool(params.Name)
	if err != nil {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Invalid tool name: %v", err))},
			IsError: true,
		}, nil
	}

	// Check if agent has access to this specific tool
	if !g.isToolAllowedForAgent(agentName, serverName, originalToolName) {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Access denied: agent '%s' cannot use tool '%s' from '%s'", agentName, originalToolName, serverName))},
			IsError: true,
		}, nil
	}

	// Proceed with the tool call
	return g.HandleToolsCall(ctx, params)
}

// waitForHTTPServer waits for an HTTP MCP server to become available.
func (g *Gateway) waitForHTTPServer(ctx context.Context, client *Client) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for MCP server")
		case <-ticker.C:
			if err := client.Ping(ctx); err == nil {
				return nil
			}
		}
	}
}

// HandleInitialize handles the initialize request.
func (g *Gateway) HandleInitialize(params InitializeParams) (*InitializeResult, error) {
	// Create a session for this client
	g.sessions.Create(params.ClientInfo)

	return &InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      g.ServerInfo(),
		Capabilities: Capabilities{
			Tools: &ToolsCapability{
				ListChanged: true,
			},
		},
	}, nil
}

// HandleToolsList returns all aggregated tools.
func (g *Gateway) HandleToolsList() (*ToolsListResult, error) {
	tools := g.router.AggregatedTools()
	return &ToolsListResult{Tools: tools}, nil
}

// HandleToolsCall routes a tool call to the appropriate MCP server.
func (g *Gateway) HandleToolsCall(ctx context.Context, params ToolCallParams) (*ToolCallResult, error) {
	client, toolName, err := g.router.RouteToolCall(params.Name)
	if err != nil {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Error: %v", err))},
			IsError: true,
		}, nil
	}

	result, err := client.CallTool(ctx, toolName, params.Arguments)
	if err != nil {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Error calling tool: %v", err))},
			IsError: true,
		}, nil
	}

	return result, nil
}

// RefreshAllTools refreshes tools from all registered MCP servers.
func (g *Gateway) RefreshAllTools(ctx context.Context) error {
	for _, client := range g.router.Clients() {
		if err := client.RefreshTools(ctx); err != nil {
			g.logger.Warn("failed to refresh tools", "server", client.Name(), "error", err)
		}
	}
	g.router.RefreshTools()
	return nil
}

// MCPServerStatus returns status information about registered MCP servers.
type MCPServerStatus struct {
	Name         string    `json:"name"`
	Transport    Transport `json:"transport"`
	Endpoint     string    `json:"endpoint,omitempty"`
	ContainerID  string    `json:"containerId,omitempty"`
	Initialized  bool      `json:"initialized"`
	ToolCount    int       `json:"toolCount"`
	Tools        []string  `json:"tools"`
	External     bool      `json:"external"`     // True for external URL servers
	LocalProcess bool      `json:"localProcess"` // True for local process servers
	SSH          bool      `json:"ssh"`          // True for SSH servers
	SSHHost      string    `json:"sshHost,omitempty"` // SSH hostname
}

// buildSSHCommand constructs the ssh command with all options.
func buildSSHCommand(cfg MCPServerConfig) []string {
	args := []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new"}

	// Add identity file if specified
	if cfg.SSHIdentityFile != "" {
		args = append(args, "-i", cfg.SSHIdentityFile)
	}

	// Add port if non-default
	if cfg.SSHPort > 0 && cfg.SSHPort != 22 {
		args = append(args, "-p", strconv.Itoa(cfg.SSHPort))
	}

	// Add user@host
	args = append(args, cfg.SSHUser+"@"+cfg.SSHHost)

	// Add the remote command (as a single argument to be executed remotely)
	args = append(args, cfg.Command...)

	return args
}

// Status returns status of all registered MCP servers.
// Note: This only returns actual MCP servers, not A2A adapters or other
// clients added directly to the router.
func (g *Gateway) Status() []MCPServerStatus {
	clients := g.router.Clients()
	statuses := make([]MCPServerStatus, 0, len(clients))

	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, client := range clients {
		// Only include clients that were registered as MCP servers
		// (have metadata). This filters out A2A adapters which are
		// internal plumbing and shouldn't appear as MCP servers.
		meta, isMCPServer := g.serverMeta[client.Name()]
		if !isMCPServer {
			continue
		}

		tools := client.Tools()
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}

		statuses = append(statuses, MCPServerStatus{
			Name:         client.Name(),
			Transport:    meta.Transport,
			Endpoint:     meta.Endpoint,
			ContainerID:  meta.ContainerID,
			Initialized:  client.IsInitialized(),
			ToolCount:    len(tools),
			Tools:        toolNames,
			External:     meta.External,
			LocalProcess: meta.LocalProcess,
			SSH:          meta.SSH,
			SSHHost:      meta.SSHHost,
		})
	}

	return statuses
}
