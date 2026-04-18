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
	logger      *slog.Logger
	noExpand    bool
	vault       config.VaultLookup
	vaultSet    config.VaultSetLookup

	// Callback for registering new MCP servers with gateway. replicas carries
	// one entry per replica in replica-id order (ContainerID and HostPort per
	// replica, empty for non-container replicas). stackPath is the live active
	// stack path; passed through to the registrar so the gateway_builder closure
	// does not need to re-enter the Handler mutex to look it up.
	registerServer func(ctx context.Context, server config.MCPServer, replicas []ReplicaRuntime, stackPath string) error
}

// ReplicaRuntime carries runtime handles for one replica that the reload
// handler has provisioned. Container replicas populate both fields; local-
// process, SSH, external, and OpenAPI replicas pass zero-valued entries.
type ReplicaRuntime struct {
	HostPort    int
	ContainerID string
}

// NewHandler creates a reload handler.
func NewHandler(stackPath string, currentCfg *config.Stack, gateway *mcp.Gateway, rt *runtime.Orchestrator, port, basePort int, v config.VaultLookup, vs config.VaultSetLookup) *Handler {
	return &Handler{
		stackPath:  stackPath,
		currentCfg: currentCfg,
		gateway:    gateway,
		runtime:    rt,
		port:       port,
		basePort:   basePort,
		logger:     logging.NewDiscardLogger(),
		vault:      v,
		vaultSet:   vs,
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
func (h *Handler) SetRegisterServerFunc(fn func(ctx context.Context, server config.MCPServer, replicas []ReplicaRuntime, stackPath string) error) {
	h.registerServer = fn
}

// CurrentConfig returns the current stack configuration.
func (h *Handler) CurrentConfig() *config.Stack {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentCfg
}

// Initialize cold-loads a stack into a running stackless daemon.
// It sets the stack path, resets currentCfg to nil so that ComputeDiff treats
// every server and resource as newly added, then calls Reload.
func (h *Handler) Initialize(ctx context.Context, stackPath string) (*ReloadResult, error) {
	h.mu.Lock()
	h.stackPath = stackPath
	h.currentCfg = nil
	h.mu.Unlock()
	return h.Reload(ctx)
}

// Reload reloads the configuration from disk and applies changes.
func (h *Handler) Reload(ctx context.Context) (*ReloadResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Info("reloading configuration", "path", h.stackPath)

	// Build load options
	var loadOpts []config.LoadOption
	if h.vault != nil {
		loadOpts = append(loadOpts, config.WithVault(h.vault))
	}
	if h.vaultSet != nil {
		loadOpts = append(loadOpts, config.WithVaultSets(h.vaultSet))
	}

	// Load new config
	newCfg, err := config.LoadStack(h.stackPath, loadOpts...)
	if err != nil {
		return &ReloadResult{
			Success: false,
			Message: fmt.Sprintf("failed to load config: %v", err),
		}, nil
	}

	// Compute diff; treat a nil currentCfg as an empty stack (initial load).
	isInitial := h.currentCfg == nil
	prevCfg := h.currentCfg
	if isInitial {
		prevCfg = &config.Stack{}
	}
	diff := ComputeDiff(prevCfg, newCfg)

	if diff.IsEmpty() {
		h.logger.Info("no configuration changes detected")
		return &ReloadResult{
			Success: true,
			Message: "no changes detected",
		}, nil
	}

	// Network changes require full restart — skip this check on initial load
	// because there is no previous network config to compare against.
	if diff.NetworkChanged && !isInitial {
		return &ReloadResult{
			Success: false,
			Message: "network configuration changed - full restart required (run gridctl destroy && gridctl apply)",
		}, nil
	}

	result := &ReloadResult{Success: true}

	// On initial load (stackless serve → /api/stack/initialize), the daemon
	// started without running orchestrator.Up, so the stack's network(s) have
	// never been created. Ensure them now before applyMCPServerChanges tries
	// to start any container — otherwise Docker rejects ContainerCreate with
	// "network X not found".
	if isInitial && newCfg.NeedsContainerRuntime() {
		if err := h.ensureNetworks(ctx, newCfg); err != nil {
			return &ReloadResult{
				Success: false,
				Message: fmt.Sprintf("failed to ensure network: %v", err),
			}, nil
		}
	}

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

	// Update current config
	h.currentCfg = newCfg

	// Per-item failures collected during applyMCPServerChanges /
	// applyResourceChanges must flip Success so callers (HTTP handler, file
	// watcher) see the reload as failed instead of a silent green 200.
	if len(result.Errors) > 0 {
		result.Success = false
		if result.Message == "" {
			if len(result.Errors) == 1 {
				result.Message = result.Errors[0]
			} else {
				result.Message = fmt.Sprintf("%d errors during reload", len(result.Errors))
			}
		}
	} else if result.Message == "" {
		result.Message = "configuration reloaded successfully"
	}

	h.logger.Info("reload complete",
		"added", len(result.Added),
		"removed", len(result.Removed),
		"modified", len(result.Modified),
		"errors", len(result.Errors))
	for _, e := range result.Errors {
		h.logger.Warn("reload: per-item error", "error", e)
	}

	return result, nil
}

func (h *Handler) applyMCPServerChanges(ctx context.Context, diff MCPServerDiff, newCfg *config.Stack, result *ReloadResult) error {
	// Remove old servers
	for _, server := range diff.Removed {
		h.logger.Info("removing MCP server", "name", server.Name)

		// Unregister from gateway
		h.gateway.UnregisterMCPServer(server.Name)

		// Clear stored pins so a future re-add of the same server name starts fresh.
		if err := h.gateway.ResetServerPins(server.Name); err != nil {
			h.logger.Warn("failed to reset schema pins for removed server", "name", server.Name, "error", err)
		}

		// Stop and remove container(s) if the server was container-based. For
		// multi-replica servers we iterate replica containers; single-replica
		// preserves the pre-replicas naming (no suffix).
		if !server.IsExternal() && !server.IsLocalProcess() && !server.IsSSH() && !server.IsOpenAPI() {
			for _, name := range replicaContainerNames(h.currentCfg.Name, &server) {
				if err := h.stopAndRemoveContainer(ctx, name); err != nil {
					h.logger.Warn("failed to remove container", "name", name, "error", err)
					result.Errors = append(result.Errors, fmt.Sprintf("failed to remove container %s: %v", name, err))
				}
			}
		}

		result.Removed = append(result.Removed, "mcp-server:"+server.Name)
	}

	// Handle modified servers (stop old, start new)
	for _, change := range diff.Modified {
		h.logger.Info("reloading MCP server", "name", change.Name)

		// Unregister from gateway
		h.gateway.UnregisterMCPServer(change.Name)

		// Stop old container(s) if it was container-based.
		if !change.Old.IsExternal() && !change.Old.IsLocalProcess() && !change.Old.IsSSH() && !change.Old.IsOpenAPI() {
			for _, name := range replicaContainerNames(h.currentCfg.Name, &change.Old) {
				if err := h.stopAndRemoveContainer(ctx, name); err != nil {
					h.logger.Warn("failed to stop container", "name", name, "error", err)
				}
			}
		}

		// Clear stale pins: the server config changed, so existing pins are invalid.
		// The next RegisterMCPServer call will re-pin the new tool definitions from scratch.
		if err := h.gateway.ResetServerPins(change.Name); err != nil {
			h.logger.Warn("failed to reset schema pins for modified server", "name", change.Name, "error", err)
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

// ensureNetworks creates the stack's network(s) if they do not already exist.
// Mirrors the network setup in pkg/runtime/orchestrator.go Up, but is only
// invoked during the stackless initial-load path where Up was never called.
func (h *Handler) ensureNetworks(ctx context.Context, stack *config.Stack) error {
	if h.runtime == nil {
		return fmt.Errorf("container runtime unavailable (Docker/Podman not detected); load the stack via 'gridctl apply' instead")
	}
	rt := h.runtime.Runtime()
	if rt == nil {
		return fmt.Errorf("container runtime unavailable (Docker/Podman not detected); load the stack via 'gridctl apply' instead")
	}

	if len(stack.Networks) > 0 {
		for _, net := range stack.Networks {
			h.logger.Info("creating network", "name", net.Name)
			if err := rt.EnsureNetwork(ctx, net.Name, runtime.NetworkOptions{
				Driver: net.Driver,
				Stack:  stack.Name,
			}); err != nil {
				return fmt.Errorf("ensuring network %s: %w", net.Name, err)
			}
		}
		return nil
	}

	h.logger.Info("creating network", "name", stack.Network.Name)
	if err := rt.EnsureNetwork(ctx, stack.Network.Name, runtime.NetworkOptions{
		Driver: stack.Network.Driver,
		Stack:  stack.Name,
	}); err != nil {
		return fmt.Errorf("ensuring network: %w", err)
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
	replicas := effectiveReplicas(&server)

	// Skip container creation for non-container servers. Still produce N
	// placeholder ReplicaRuntime entries so the registrar creates N clients.
	if server.IsExternal() || server.IsLocalProcess() || server.IsSSH() || server.IsOpenAPI() {
		if h.registerServer != nil {
			return h.registerServer(ctx, server, make([]ReplicaRuntime, replicas), h.stackPath)
		}
		return nil
	}

	// Start container
	if h.runtime == nil {
		return fmt.Errorf("container runtime unavailable (Docker/Podman not detected); load the stack via 'gridctl apply' instead")
	}
	rt := h.runtime.Runtime()
	if rt == nil {
		return fmt.Errorf("container runtime unavailable (Docker/Podman not detected); load the stack via 'gridctl apply' instead")
	}

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

	// Start one container per replica. Each replica gets its own host port;
	// for multi-replica servers the workload name gets a "-replica-<id>"
	// suffix so container names don't collide.
	runtimes := make([]ReplicaRuntime, 0, replicas)
	for replicaID := 0; replicaID < replicas; replicaID++ {
		hostPort := h.allocatePort(ctx)
		workloadName := server.Name
		if replicas > 1 {
			workloadName = fmt.Sprintf("%s-replica-%d", server.Name, replicaID)
		}
		cfg := runtime.WorkloadConfig{
			Name:        workloadName,
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
			return fmt.Errorf("starting container replica %d: %w", replicaID, err)
		}

		actualHostPort := status.HostPort
		if actualHostPort == 0 {
			actualHostPort = hostPort
		}
		runtimes = append(runtimes, ReplicaRuntime{HostPort: actualHostPort, ContainerID: string(status.ID)})
	}

	if h.registerServer != nil {
		return h.registerServer(ctx, server, runtimes, h.stackPath)
	}

	return nil
}

// effectiveReplicas returns the replica count, treating zero/negative as 1.
func effectiveReplicas(server *config.MCPServer) int {
	if server.Replicas <= 0 {
		return 1
	}
	return server.Replicas
}

func (h *Handler) startResource(ctx context.Context, res config.Resource, stack *config.Stack) error {
	if h.runtime == nil {
		return fmt.Errorf("container runtime unavailable (Docker/Podman not detected); load the stack via 'gridctl apply' instead")
	}
	rt := h.runtime.Runtime()
	if rt == nil {
		return fmt.Errorf("container runtime unavailable (Docker/Podman not detected); load the stack via 'gridctl apply' instead")
	}

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

// replicaContainerNames returns the container names for each replica of a
// server. Single-replica servers keep the pre-replicas name (no suffix) so
// backward-compatible stacks see no change.
func replicaContainerNames(stack string, server *config.MCPServer) []string {
	n := effectiveReplicas(server)
	if n <= 1 {
		return []string{containerName(stack, server.Name)}
	}
	names := make([]string, 0, n)
	base := containerName(stack, server.Name)
	for i := 0; i < n; i++ {
		names = append(names, fmt.Sprintf("%s-replica-%d", base, i))
	}
	return names
}
