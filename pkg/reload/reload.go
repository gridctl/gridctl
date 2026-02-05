package reload

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// ReloadResult contains the result of a reload operation.
type ReloadResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Added    []string `json:"added,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Modified []string `json:"modified,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

// Handler manages hot reload for a running stack.
type Handler struct {
	mu          sync.Mutex
	stackPath   string
	currentCfg  *config.Stack
	gateway     *mcp.Gateway
	runtime     *runtime.Orchestrator
	port        int
	basePort    int
	gatewayPort int
	logger      *slog.Logger
	noExpand    bool

	// Callback for registering new MCP servers with gateway
	registerServer func(ctx context.Context, server config.MCPServer, hostPort int) error
}

// NewHandler creates a reload handler.
func NewHandler(stackPath string, currentCfg *config.Stack, gateway *mcp.Gateway, rt *runtime.Orchestrator, port, basePort, gatewayPort int) *Handler {
	return &Handler{
		stackPath:   stackPath,
		currentCfg:  currentCfg,
		gateway:     gateway,
		runtime:     rt,
		port:        port,
		basePort:    basePort,
		gatewayPort: gatewayPort,
		logger:      logging.NewDiscardLogger(),
	}
}

// SetLogger sets the logger.
func (h *Handler) SetLogger(logger *slog.Logger) {
	if logger != nil {
		h.logger = logger
	}
}

// SetNoExpand sets whether to skip OpenAPI env var expansion.
func (h *Handler) SetNoExpand(noExpand bool) {
	h.noExpand = noExpand
}

// SetRegisterServerFunc sets the callback for registering MCP servers.
func (h *Handler) SetRegisterServerFunc(fn func(ctx context.Context, server config.MCPServer, hostPort int) error) {
	h.registerServer = fn
}

// CurrentConfig returns the current stack configuration.
func (h *Handler) CurrentConfig() *config.Stack {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentCfg
}

// Reload reloads the configuration from disk and applies changes.
func (h *Handler) Reload(ctx context.Context) (*ReloadResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Info("reloading configuration", "path", h.stackPath)

	// Load new config
	newCfg, err := config.LoadStack(h.stackPath)
	if err != nil {
		return &ReloadResult{
			Success: false,
			Message: fmt.Sprintf("failed to load config: %v", err),
		}, nil
	}

	// Compute diff
	diff := ComputeDiff(h.currentCfg, newCfg)

	if diff.IsEmpty() {
		h.logger.Info("no configuration changes detected")
		return &ReloadResult{
			Success: true,
			Message: "no changes detected",
		}, nil
	}

	// Network changes require full restart
	if diff.NetworkChanged {
		return &ReloadResult{
			Success: false,
			Message: "network configuration changed - full restart required (run gridctl destroy && gridctl deploy)",
		}, nil
	}

	result := &ReloadResult{Success: true}

	// Apply MCP server changes
	if err := h.applyMCPServerChanges(ctx, diff.MCPServers, newCfg, result); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("failed to apply MCP server changes: %v", err)
		return result, nil
	}

	// Apply resource changes
	if err := h.applyResourceChanges(ctx, diff.Resources, newCfg, result); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("failed to apply resource changes: %v", err)
		return result, nil
	}

	// Apply agent changes
	if err := h.applyAgentChanges(ctx, diff.Agents, newCfg, result); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("failed to apply agent changes: %v", err)
		return result, nil
	}

	// Update current config
	h.currentCfg = newCfg

	if result.Message == "" {
		result.Message = "configuration reloaded successfully"
	}

	h.logger.Info("reload complete",
		"added", len(result.Added),
		"removed", len(result.Removed),
		"modified", len(result.Modified))

	return result, nil
}

func (h *Handler) applyMCPServerChanges(ctx context.Context, diff MCPServerDiff, newCfg *config.Stack, result *ReloadResult) error {
	// Remove old servers
	for _, server := range diff.Removed {
		h.logger.Info("removing MCP server", "name", server.Name)

		// Unregister from gateway
		h.gateway.UnregisterMCPServer(server.Name)

		// Stop and remove container if it exists
		if !server.IsExternal() && !server.IsLocalProcess() && !server.IsSSH() && !server.IsOpenAPI() {
			containerName := containerName(h.currentCfg.Name, server.Name)
			if err := h.stopAndRemoveContainer(ctx, containerName); err != nil {
				h.logger.Warn("failed to remove container", "name", server.Name, "error", err)
				result.Errors = append(result.Errors, fmt.Sprintf("failed to remove container %s: %v", server.Name, err))
			}
		}

		result.Removed = append(result.Removed, "mcp-server:"+server.Name)
	}

	// Handle modified servers (stop old, start new)
	for _, change := range diff.Modified {
		h.logger.Info("reloading MCP server", "name", change.Name)

		// Unregister from gateway
		h.gateway.UnregisterMCPServer(change.Name)

		// Stop old container if it was container-based
		if !change.Old.IsExternal() && !change.Old.IsLocalProcess() && !change.Old.IsSSH() && !change.Old.IsOpenAPI() {
			containerName := containerName(h.currentCfg.Name, change.Name)
			if err := h.stopAndRemoveContainer(ctx, containerName); err != nil {
				h.logger.Warn("failed to stop container", "name", change.Name, "error", err)
			}
		}

		// Start new server
		if err := h.startMCPServer(ctx, change.New, newCfg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to reload %s: %v", change.Name, err))
			continue
		}

		result.Modified = append(result.Modified, "mcp-server:"+change.Name)
	}

	// Add new servers
	for _, server := range diff.Added {
		h.logger.Info("adding MCP server", "name", server.Name)

		if err := h.startMCPServer(ctx, server, newCfg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to add %s: %v", server.Name, err))
			continue
		}

		result.Added = append(result.Added, "mcp-server:"+server.Name)
	}

	return nil
}

func (h *Handler) applyResourceChanges(ctx context.Context, diff ResourceDiff, newCfg *config.Stack, result *ReloadResult) error {
	// Remove old resources
	for _, res := range diff.Removed {
		h.logger.Info("removing resource", "name", res.Name)

		containerName := containerName(h.currentCfg.Name, res.Name)
		if err := h.stopAndRemoveContainer(ctx, containerName); err != nil {
			h.logger.Warn("failed to remove container", "name", res.Name, "error", err)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to remove resource %s: %v", res.Name, err))
		}

		result.Removed = append(result.Removed, "resource:"+res.Name)
	}

	// Handle modified resources
	for _, change := range diff.Modified {
		h.logger.Info("reloading resource", "name", change.Name)

		containerName := containerName(h.currentCfg.Name, change.Name)
		if err := h.stopAndRemoveContainer(ctx, containerName); err != nil {
			h.logger.Warn("failed to stop container", "name", change.Name, "error", err)
		}

		if err := h.startResource(ctx, change.New, newCfg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to reload %s: %v", change.Name, err))
			continue
		}

		result.Modified = append(result.Modified, "resource:"+change.Name)
	}

	// Add new resources
	for _, res := range diff.Added {
		h.logger.Info("adding resource", "name", res.Name)

		if err := h.startResource(ctx, res, newCfg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to add %s: %v", res.Name, err))
			continue
		}

		result.Added = append(result.Added, "resource:"+res.Name)
	}

	return nil
}

func (h *Handler) applyAgentChanges(ctx context.Context, diff AgentDiff, newCfg *config.Stack, result *ReloadResult) error {
	// Remove old agents
	for _, agent := range diff.Removed {
		h.logger.Info("removing agent", "name", agent.Name)

		// Unregister from gateway
		h.gateway.UnregisterAgent(agent.Name)

		containerName := containerName(h.currentCfg.Name, agent.Name)
		if err := h.stopAndRemoveContainer(ctx, containerName); err != nil {
			h.logger.Warn("failed to remove container", "name", agent.Name, "error", err)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to remove agent %s: %v", agent.Name, err))
		}

		result.Removed = append(result.Removed, "agent:"+agent.Name)
	}

	// Handle modified agents
	for _, change := range diff.Modified {
		h.logger.Info("reloading agent", "name", change.Name)

		// Unregister from gateway
		h.gateway.UnregisterAgent(change.Name)

		containerName := containerName(h.currentCfg.Name, change.Name)
		if err := h.stopAndRemoveContainer(ctx, containerName); err != nil {
			h.logger.Warn("failed to stop container", "name", change.Name, "error", err)
		}

		if err := h.startAgent(ctx, change.New, newCfg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to reload %s: %v", change.Name, err))
			continue
		}

		result.Modified = append(result.Modified, "agent:"+change.Name)
	}

	// Add new agents
	for _, agent := range diff.Added {
		h.logger.Info("adding agent", "name", agent.Name)

		if err := h.startAgent(ctx, agent, newCfg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to add %s: %v", agent.Name, err))
			continue
		}

		result.Added = append(result.Added, "agent:"+agent.Name)
	}

	return nil
}

func (h *Handler) stopAndRemoveContainer(ctx context.Context, containerName string) error {
	rt := h.runtime.Runtime()

	exists, id, err := rt.Exists(ctx, containerName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if err := rt.Stop(ctx, id); err != nil {
		h.logger.Warn("failed to stop container", "name", containerName, "error", err)
	}

	return rt.Remove(ctx, id)
}

func (h *Handler) startMCPServer(ctx context.Context, server config.MCPServer, stack *config.Stack) error {
	// Skip container creation for non-container servers
	if server.IsExternal() || server.IsLocalProcess() || server.IsSSH() || server.IsOpenAPI() {
		// Just register with gateway
		if h.registerServer != nil {
			return h.registerServer(ctx, server, 0)
		}
		return nil
	}

	// Start container
	rt := h.runtime.Runtime()

	// Pull image if needed
	imageName := server.Image
	if server.Source != nil {
		// Source-based servers need to be built - for now, assume image is already built
		// Full build support during reload would require the Builder interface
		imageName = fmt.Sprintf("gridctl-%s-%s:latest", stack.Name, server.Name)
	}

	if err := rt.EnsureImage(ctx, imageName); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	// Determine network name
	networkName := stack.Network.Name
	if len(stack.Networks) > 0 && server.Network != "" {
		networkName = server.Network
	}

	// Allocate port - find next available port
	hostPort := h.allocatePort(ctx)

	// Create workload config
	cfg := runtime.WorkloadConfig{
		Name:        server.Name,
		Stack:       stack.Name,
		Type:        runtime.WorkloadTypeMCPServer,
		Image:       imageName,
		Command:     server.Command,
		Env:         server.Env,
		NetworkName: networkName,
		ExposedPort: server.Port,
		HostPort:    hostPort,
		Transport:   server.Transport,
		Labels: map[string]string{
			"gridctl.managed":    "true",
			"gridctl.stack":      stack.Name,
			"gridctl.mcp-server": server.Name,
		},
	}

	status, err := rt.Start(ctx, cfg)
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	actualHostPort := status.HostPort
	if actualHostPort == 0 {
		actualHostPort = hostPort
	}

	// Register with gateway
	if h.registerServer != nil {
		return h.registerServer(ctx, server, actualHostPort)
	}

	return nil
}

func (h *Handler) startResource(ctx context.Context, res config.Resource, stack *config.Stack) error {
	rt := h.runtime.Runtime()

	// Pull image
	if err := rt.EnsureImage(ctx, res.Image); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	// Determine network name
	networkName := stack.Network.Name
	if len(stack.Networks) > 0 && res.Network != "" {
		networkName = res.Network
	}

	cfg := runtime.WorkloadConfig{
		Name:        res.Name,
		Stack:       stack.Name,
		Type:        runtime.WorkloadTypeResource,
		Image:       res.Image,
		Env:         res.Env,
		NetworkName: networkName,
		Volumes:     res.Volumes,
		Labels: map[string]string{
			"gridctl.managed":  "true",
			"gridctl.stack":    stack.Name,
			"gridctl.resource": res.Name,
		},
	}

	_, err := rt.Start(ctx, cfg)
	return err
}

func (h *Handler) startAgent(ctx context.Context, agent config.Agent, stack *config.Stack) error {
	rt := h.runtime.Runtime()

	// Pull image
	imageName := agent.Image
	if err := rt.EnsureImage(ctx, imageName); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	// Determine network name
	networkName := stack.Network.Name
	if len(stack.Networks) > 0 && agent.Network != "" {
		networkName = agent.Network
	}

	// Build environment with MCP_ENDPOINT injection
	env := make(map[string]string)
	for k, v := range agent.Env {
		env[k] = v
	}
	if h.gatewayPort > 0 {
		env["MCP_ENDPOINT"] = fmt.Sprintf("http://host.docker.internal:%d", h.gatewayPort)
	}

	cfg := runtime.WorkloadConfig{
		Name:        agent.Name,
		Stack:       stack.Name,
		Type:        runtime.WorkloadTypeAgent,
		Image:       imageName,
		Command:     agent.Command,
		Env:         env,
		NetworkName: networkName,
		Labels: map[string]string{
			"gridctl.managed": "true",
			"gridctl.stack":   stack.Name,
			"gridctl.agent":   agent.Name,
		},
	}

	_, err := rt.Start(ctx, cfg)
	if err != nil {
		return err
	}

	// Register agent with gateway
	h.gateway.RegisterAgent(agent.Name, agent.Uses)

	return nil
}

func (h *Handler) allocatePort(ctx context.Context) int {
	// Get current servers to find next available port
	statuses := h.gateway.Status()
	maxPort := h.basePort - 1

	for _, s := range statuses {
		// Extract port from endpoint if available
		if s.Endpoint != "" {
			var port int
			if _, err := fmt.Sscanf(s.Endpoint, "http://localhost:%d", &port); err == nil {
				if port > maxPort {
					maxPort = port
				}
			}
		}
	}

	return maxPort + 1
}

func containerName(stack, name string) string {
	return "gridctl-" + stack + "-" + name
}
