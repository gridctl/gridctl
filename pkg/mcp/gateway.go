package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"agentlab/pkg/dockerclient"
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

	mu         sync.RWMutex
	serverInfo ServerInfo
	serverMeta map[string]MCPServerConfig // name -> config for status reporting
}

// NewGateway creates a new MCP gateway.
func NewGateway() *Gateway {
	return &Gateway{
		router:   NewRouter(),
		sessions: NewSessionManager(),
		serverInfo: ServerInfo{
			Name:    "agentlab-gateway",
			Version: "1.0.0",
		},
		serverMeta: make(map[string]MCPServerConfig),
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
	g.mu.Lock()
	g.serverMeta[cfg.Name] = cfg
	g.mu.Unlock()

	// Add to router
	g.router.AddClient(agentClient)
	g.router.RefreshTools()

	fmt.Printf("  Registered MCP server '%s' (%s) with %d tools\n", cfg.Name, cfg.Transport, len(agentClient.Tools()))
	return nil
}

// UnregisterMCPServer removes an MCP server from the gateway.
func (g *Gateway) UnregisterMCPServer(name string) {
	g.router.RemoveClient(name)
	g.router.RefreshTools()
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
			fmt.Printf("Warning: failed to refresh tools for %s: %v\n", client.Name(), err)
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
func (g *Gateway) Status() []MCPServerStatus {
	clients := g.router.Clients()
	statuses := make([]MCPServerStatus, 0, len(clients))

	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, client := range clients {
		tools := client.Tools()
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}

		meta := g.serverMeta[client.Name()]
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
