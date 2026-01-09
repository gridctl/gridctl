package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"agentlab/pkg/dockerclient"
	"agentlab/pkg/logging"
)

// MCPServerConfig contains configuration for connecting to an MCP server.
type MCPServerConfig struct {
	Name        string
	Transport   Transport
	Endpoint    string // For HTTP transport
	ContainerID string // For Stdio transport
}

// Gateway aggregates multiple MCP servers into a single endpoint.
type Gateway struct {
	router    *Router
	sessions  *SessionManager
	dockerCli dockerclient.DockerClient
	logger    *slog.Logger

	mu          sync.RWMutex
	serverInfo  ServerInfo
	serverMeta  map[string]MCPServerConfig // name -> config for status reporting
	agentAccess map[string][]string        // agent name -> allowed MCP server names
}

// NewGateway creates a new MCP gateway.
func NewGateway() *Gateway {
	return &Gateway{
		router:   NewRouter(),
		sessions: NewSessionManager(),
		logger:   logging.NewDiscardLogger(),
		serverInfo: ServerInfo{
			Name:    "agentlab-gateway",
			Version: "1.0.0",
		},
		serverMeta:  make(map[string]MCPServerConfig),
		agentAccess: make(map[string][]string),
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

	switch cfg.Transport {
	case TransportStdio:
		if g.dockerCli == nil {
			return fmt.Errorf("Docker client not set for stdio transport")
		}
		stdioClient := NewStdioClient(cfg.Name, cfg.ContainerID, g.dockerCli)
		if err := stdioClient.Connect(ctx); err != nil {
			return fmt.Errorf("connecting to container: %w", err)
		}
		agentClient = stdioClient
	case TransportHTTP, "": // Default to HTTP
		httpClient := NewClient(cfg.Name, cfg.Endpoint)
		// Wait for MCP server to be ready with retries
		if err := g.waitForHTTPServer(ctx, httpClient); err != nil {
			return fmt.Errorf("MCP server %s not ready: %w", cfg.Name, err)
		}
		agentClient = httpClient
	default:
		return fmt.Errorf("unknown transport: %s", cfg.Transport)
	}

	// Initialize MCP connection
	if err := agentClient.Initialize(ctx); err != nil {
		return fmt.Errorf("initializing MCP server %s: %w", cfg.Name, err)
	}

	// Fetch tools
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

// RegisterAgent registers an agent and its allowed MCP servers.
func (g *Gateway) RegisterAgent(name string, uses []string) {
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
func (g *Gateway) GetAgentAllowedServers(agentName string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.agentAccess[agentName]
}

// isServerAllowedForAgent checks if an agent can access tools from a specific MCP server.
func (g *Gateway) isServerAllowedForAgent(agentName, serverName string) bool {
	allowed := g.GetAgentAllowedServers(agentName)
	if allowed == nil {
		// Agent not registered - allow all (backward compatibility)
		return true
	}
	for _, s := range allowed {
		if s == serverName {
			return true
		}
	}
	return false
}

// HandleToolsListForAgent returns tools filtered by agent access permissions.
func (g *Gateway) HandleToolsListForAgent(agentName string) (*ToolsListResult, error) {
	allowed := g.GetAgentAllowedServers(agentName)
	if allowed == nil {
		// Agent not registered - return all tools
		return g.HandleToolsList()
	}

	// Build set of allowed servers for fast lookup
	allowedSet := make(map[string]bool)
	for _, name := range allowed {
		allowedSet[name] = true
	}

	// Filter tools by allowed MCP servers
	allTools := g.router.AggregatedTools()
	var filteredTools []Tool
	for _, tool := range allTools {
		serverName, _, err := ParsePrefixedTool(tool.Name)
		if err != nil {
			g.logger.Warn("skipping tool with invalid name format", "name", tool.Name, "error", err)
			continue
		}
		if allowedSet[serverName] {
			filteredTools = append(filteredTools, tool)
		}
	}

	return &ToolsListResult{Tools: filteredTools}, nil
}

// HandleToolsCallForAgent routes a tool call with agent access validation.
func (g *Gateway) HandleToolsCallForAgent(ctx context.Context, agentName string, params ToolCallParams) (*ToolCallResult, error) {
	// Parse the tool name to get the MCP server
	serverName, _, err := ParsePrefixedTool(params.Name)
	if err != nil {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Invalid tool name: %v", err))},
			IsError: true,
		}, nil
	}

	// Check if agent has access to this server's tools
	if !g.isServerAllowedForAgent(agentName, serverName) {
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Access denied: agent '%s' cannot use tools from '%s'", agentName, serverName))},
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
	Name        string    `json:"name"`
	Transport   Transport `json:"transport"`
	Endpoint    string    `json:"endpoint,omitempty"`
	ContainerID string    `json:"containerId,omitempty"`
	Initialized bool      `json:"initialized"`
	ToolCount   int       `json:"toolCount"`
	Tools       []string  `json:"tools"`
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
			Name:        client.Name(),
			Transport:   meta.Transport,
			Endpoint:    meta.Endpoint,
			ContainerID: meta.ContainerID,
			Initialized: client.IsInitialized(),
			ToolCount:   len(tools),
			Tools:       toolNames,
		})
	}

	return statuses
}
