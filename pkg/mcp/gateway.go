package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
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
	OpenAPI         bool              // True for OpenAPI-based servers
	Command         []string          // For local process or SSH transport
	WorkDir         string            // For local process transport
	Env             map[string]string // For local process or SSH transport
	SSHHost         string            // SSH hostname (for SSH servers)
	SSHUser         string            // SSH username (for SSH servers)
	SSHPort         int               // SSH port (for SSH servers, 0 = default 22)
	SSHIdentityFile string            // SSH identity file path (for SSH servers)
	OpenAPIConfig   *OpenAPIClientConfig // OpenAPI configuration (for OpenAPI servers)
	Tools           []string          // Tool whitelist (empty = all tools)
}

// OpenAPIClientConfig contains configuration for an OpenAPI-backed MCP client.
type OpenAPIClientConfig struct {
	Spec       string   // URL or local file path to OpenAPI spec
	BaseURL    string   // Override server URL from spec
	AuthType   string   // "bearer" or "header"
	AuthToken  string   // Resolved bearer token (from env)
	AuthHeader string   // Header name for header-based auth
	AuthValue  string   // Resolved header value (from env)
	Include    []string // Operation IDs to include
	Exclude    []string // Operation IDs to exclude
	NoExpand   bool     // If true, skip environment variable expansion in spec file
}

// HealthStatus tracks the health state of a downstream MCP server.
type HealthStatus struct {
	Healthy     bool      // Whether the server is responding to pings
	LastCheck   time.Time // When the last health check ran
	LastHealthy time.Time // When the server was last seen healthy
	Error       string    // Error message if unhealthy (empty when healthy)
}

// DefaultHealthCheckInterval is the default interval between health checks.
const DefaultHealthCheckInterval = 30 * time.Second

// Gateway aggregates multiple MCP servers into a single endpoint.
type Gateway struct {
	router    *Router
	sessions  *SessionManager
	dockerCli dockerclient.DockerClient
	logger    *slog.Logger
	cancel    context.CancelFunc

	mu          sync.RWMutex
	serverInfo  ServerInfo
	serverMeta  map[string]MCPServerConfig       // name -> config for status reporting
	agentAccess map[string][]config.ToolSelector // agent name -> allowed MCP servers with tool filtering

	healthMu sync.RWMutex
	health   map[string]*HealthStatus // name -> health status
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
		health:      make(map[string]*HealthStatus),
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

// SessionCount returns the number of active sessions.
func (g *Gateway) SessionCount() int {
	return g.sessions.Count()
}

// StartCleanup starts periodic session cleanup. Call Close() to stop.
func (g *Gateway) StartCleanup(ctx context.Context) {
	ctx, g.cancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed := g.sessions.Cleanup(30 * time.Minute)
				if removed > 0 {
					g.logger.Info("cleaned up stale sessions", "removed", removed)
				}
			}
		}
	}()
}

// StartHealthMonitor starts periodic health checking for all registered MCP servers.
// It runs alongside StartCleanup and stops when the gateway context is cancelled.
func (g *Gateway) StartHealthMonitor(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.checkHealth(ctx)
			}
		}
	}()
}

// checkHealth pings all registered MCP servers and updates their health status.
// If a server is unhealthy and implements Reconnectable, it attempts reconnection.
func (g *Gateway) checkHealth(ctx context.Context) {
	clients := g.router.Clients()

	for _, client := range clients {
		// Only check clients that have server metadata (actual MCP servers)
		g.mu.RLock()
		_, isMCPServer := g.serverMeta[client.Name()]
		g.mu.RUnlock()
		if !isMCPServer {
			continue
		}

		pingable, ok := client.(Pingable)
		if !ok {
			continue
		}

		now := time.Now()
		err := pingable.Ping(ctx)

		g.healthMu.Lock()
		prev := g.health[client.Name()]

		status := &HealthStatus{
			Healthy:   err == nil,
			LastCheck: now,
		}

		if err == nil {
			status.LastHealthy = now
			if prev != nil && !prev.Healthy {
				g.logger.Info("MCP server recovered", "name", client.Name())
			}
		} else {
			status.Error = err.Error()
			if prev != nil {
				status.LastHealthy = prev.LastHealthy
			}
			if prev == nil || prev.Healthy {
				g.logger.Warn("MCP server unhealthy", "name", client.Name(), "error", err)
			}
		}

		g.health[client.Name()] = status
		g.healthMu.Unlock()

		// Attempt reconnection for unhealthy clients that support it
		if err != nil {
			if rc, ok := client.(Reconnectable); ok {
				g.logger.Info("attempting reconnection", "name", client.Name())
				if reconnErr := rc.Reconnect(ctx); reconnErr != nil {
					g.logger.Warn("reconnection failed", "name", client.Name(), "error", reconnErr)
				} else {
					// Reconnection succeeded — update health status and refresh router
					g.healthMu.Lock()
					g.health[client.Name()] = &HealthStatus{
						Healthy:     true,
						LastCheck:   time.Now(),
						LastHealthy: time.Now(),
					}
					g.healthMu.Unlock()
					g.router.RefreshTools()
					g.logger.Info("MCP server reconnected", "name", client.Name())
				}
			}
		}
	}
}

// GetHealthStatus returns the health status for a named MCP server.
// Returns nil if no health data is available.
func (g *Gateway) GetHealthStatus(name string) *HealthStatus {
	g.healthMu.RLock()
	defer g.healthMu.RUnlock()
	return g.health[name]
}

// Close stops the cleanup goroutine and closes all agent client connections.
func (g *Gateway) Close() {
	if g.cancel != nil {
		g.cancel()
	}

	for _, client := range g.router.Clients() {
		if closer, ok := client.(io.Closer); ok {
			closer.Close()
		}
	}
}

// ServerInfo returns the gateway server info.
func (g *Gateway) ServerInfo() ServerInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.serverInfo
}

// RegisterMCPServer registers and initializes an MCP server.
func (g *Gateway) RegisterMCPServer(ctx context.Context, cfg MCPServerConfig) error {
	g.logger.Info("connecting to MCP server", "name", cfg.Name, "transport", cfg.Transport)
	start := time.Now()

	var agentClient AgentClient
	clientLogger := g.logger.With("server", cfg.Name)

	// Handle OpenAPI servers
	if cfg.OpenAPI {
		if cfg.OpenAPIConfig == nil {
			return fmt.Errorf("OpenAPI config required for OpenAPI server %s", cfg.Name)
		}
		openAPIClient, err := NewOpenAPIClient(cfg.Name, cfg.OpenAPIConfig)
		if err != nil {
			return fmt.Errorf("creating OpenAPI client %s: %w", cfg.Name, err)
		}
		openAPIClient.SetLogger(clientLogger)
		if len(cfg.Tools) > 0 {
			openAPIClient.SetToolWhitelist(cfg.Tools)
		}
		agentClient = openAPIClient
	} else if cfg.SSH {
		// Handle SSH servers (they use stdio over SSH)
		sshCommand := buildSSHCommand(cfg)
		processClient := NewProcessClient(cfg.Name, sshCommand, cfg.WorkDir, cfg.Env)
		processClient.SetLogger(clientLogger)
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
		processClient.SetLogger(clientLogger)
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
			stdioClient.SetLogger(clientLogger)
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
			httpClient.SetLogger(clientLogger)
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
			httpClient.SetLogger(clientLogger)
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

	g.logger.Info("registered MCP server", "name", cfg.Name, "transport", cfg.Transport, "tools", len(agentClient.Tools()), "duration", time.Since(start))
	return nil
}

// SetServerMeta stores metadata for an MCP server without connecting to it.
// This is used by tests and by internal registration paths that manage
// their own client connections.
func (g *Gateway) SetServerMeta(cfg MCPServerConfig) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.serverMeta[cfg.Name] = cfg
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

// HasAgent returns true if the named agent is registered with the gateway.
func (g *Gateway) HasAgent(name string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.agentAccess[name]
	return ok
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
		g.logger.Warn("tool access denied", "agent", agentName, "tool", originalToolName, "server", serverName)
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
	start := time.Now()
	ticker := time.NewTicker(DefaultReadyPollInterval)
	defer ticker.Stop()

	timeout := time.After(DefaultReadyTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for MCP server")
		case <-ticker.C:
			if err := client.Ping(ctx); err == nil {
				g.logger.Debug("MCP server ready", "name", client.Name(), "wait", time.Since(start))
				return nil
			}
		}
	}
}

// HandleInitialize handles the initialize request.
func (g *Gateway) HandleInitialize(params InitializeParams) (*InitializeResult, error) {
	// Create a session for this client
	g.sessions.Create(params.ClientInfo)

	caps := Capabilities{
		Tools: &ToolsCapability{
			ListChanged: true,
		},
	}

	// Advertise Prompts and Resources if registry is available
	if g.promptProvider() != nil {
		caps.Prompts = &PromptsCapability{
			ListChanged: true,
		}
		caps.Resources = &ResourcesCapability{
			ListChanged: true,
		}
	}

	return &InitializeResult{
		ProtocolVersion: MCPProtocolVersion,
		ServerInfo:      g.ServerInfo(),
		Capabilities:    caps,
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

	g.logger.Info("tool call started", "server", client.Name(), "tool", toolName)
	start := time.Now()

	result, err := client.CallTool(ctx, toolName, params.Arguments)
	duration := time.Since(start)

	if err != nil {
		g.logger.Warn("tool call failed", "server", client.Name(), "tool", toolName, "duration", duration, "error", err)
		return &ToolCallResult{
			Content: []Content{NewTextContent(fmt.Sprintf("Error calling tool: %v", err))},
			IsError: true,
		}, nil
	}

	g.logger.Info("tool call finished", "server", client.Name(), "tool", toolName, "duration", duration, "is_error", result.IsError)
	return result, nil
}

// CallTool implements the ToolCaller interface, allowing components to call
// tools through the gateway without a direct reference to the router.
func (g *Gateway) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	return g.HandleToolsCall(ctx, ToolCallParams{
		Name:      name,
		Arguments: arguments,
	})
}

// promptProvider returns the PromptProvider from the router, if registered.
func (g *Gateway) promptProvider() PromptProvider {
	client := g.router.GetClient("registry")
	if client == nil {
		return nil
	}
	if pp, ok := client.(PromptProvider); ok {
		return pp
	}
	return nil
}

// HandlePromptsList returns all active prompts as MCP Prompts.
func (g *Gateway) HandlePromptsList() (*PromptsListResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return &PromptsListResult{Prompts: []MCPPrompt{}}, nil
	}

	prompts := pp.ListPromptData()
	result := make([]MCPPrompt, len(prompts))
	for i, p := range prompts {
		args := make([]PromptArgument, len(p.Arguments))
		for j, a := range p.Arguments {
			args[j] = PromptArgument{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
			}
		}
		result[i] = MCPPrompt{
			Name:        p.Name,
			Description: p.Description,
			Arguments:   args,
		}
	}
	return &PromptsListResult{Prompts: result}, nil
}

// HandlePromptsGet returns a specific prompt with argument substitution.
func (g *Gateway) HandlePromptsGet(params PromptsGetParams) (*PromptsGetResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return nil, fmt.Errorf("registry not available")
	}

	p, err := pp.GetPromptData(params.Name)
	if err != nil {
		return nil, err
	}

	// Perform argument substitution on content
	content := p.Content
	for _, arg := range p.Arguments {
		placeholder := "{{" + arg.Name + "}}"
		value, ok := params.Arguments[arg.Name]
		if !ok {
			if arg.Default != "" {
				value = arg.Default
			} else if arg.Required {
				return nil, fmt.Errorf("required argument %q not provided", arg.Name)
			}
		}
		content = strings.ReplaceAll(content, placeholder, value)
	}

	return &PromptsGetResult{
		Description: p.Description,
		Messages: []PromptMessage{
			{
				Role:    "user",
				Content: NewTextContent(content),
			},
		},
	}, nil
}

// HandleResourcesList returns prompts as MCP Resources.
func (g *Gateway) HandleResourcesList() (*ResourcesListResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return &ResourcesListResult{Resources: []MCPResource{}}, nil
	}

	prompts := pp.ListPromptData()
	resources := make([]MCPResource, len(prompts))
	for i, p := range prompts {
		resources[i] = MCPResource{
			URI:         "prompt://" + p.Name,
			Name:        p.Name,
			Description: p.Description,
			MimeType:    "text/plain",
		}
	}
	return &ResourcesListResult{Resources: resources}, nil
}

// HandleResourcesRead returns the content of a prompt resource.
func (g *Gateway) HandleResourcesRead(params ResourcesReadParams) (*ResourcesReadResult, error) {
	pp := g.promptProvider()
	if pp == nil {
		return nil, fmt.Errorf("registry not available")
	}

	// Parse prompt:// URI
	name := strings.TrimPrefix(params.URI, "prompt://")
	if name == params.URI {
		return nil, fmt.Errorf("unsupported URI scheme: %s", params.URI)
	}
	if name == "" {
		return nil, fmt.Errorf("empty prompt name in URI: %s", params.URI)
	}

	p, err := pp.GetPromptData(name)
	if err != nil {
		return nil, err
	}

	return &ResourcesReadResult{
		Contents: []ResourceContents{
			{
				URI:      params.URI,
				MimeType: "text/plain",
				Text:     p.Content,
			},
		},
	}, nil
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
	OpenAPI      bool      `json:"openapi"`      // True for OpenAPI servers
	OpenAPISpec  string    `json:"openapiSpec,omitempty"` // OpenAPI spec location
	Healthy      *bool      `json:"healthy,omitempty"`      // Health check result (nil if not yet checked)
	LastCheck    *time.Time `json:"lastCheck,omitempty"`    // When last health check ran
	HealthError  string     `json:"healthError,omitempty"`  // Error message if unhealthy
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

		status := MCPServerStatus{
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
			OpenAPI:      meta.OpenAPI,
		}
		if meta.OpenAPIConfig != nil {
			status.OpenAPISpec = meta.OpenAPIConfig.Spec
		}

		// Include health status if available
		g.healthMu.RLock()
		if hs, ok := g.health[client.Name()]; ok {
			status.Healthy = &hs.Healthy
			status.LastCheck = &hs.LastCheck
			status.HealthError = hs.Error
		}
		g.healthMu.RUnlock()

		statuses = append(statuses, status)
	}

	return statuses
}
