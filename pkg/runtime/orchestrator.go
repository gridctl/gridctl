package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
)

// Orchestrator manages the lifecycle of gridctl workloads.
// It uses a WorkloadRuntime to start/stop workloads and a Builder for image builds.
type Orchestrator struct {
	runtime WorkloadRuntime
	builder Builder
	logger  *slog.Logger
}

// Builder handles image/artifact building.
// This is kept separate from WorkloadRuntime as image building is a distinct concern.
type Builder interface {
	Build(ctx context.Context, opts BuildOptions) (*BuildResult, error)
}

// BuildOptions for the Builder interface.
type BuildOptions struct {
	SourceType string            // "git" or "local"
	URL        string            // Git URL
	Ref        string            // Git ref/branch
	Path       string            // Local path
	Dockerfile string            // Dockerfile path within context
	Tag        string            // Image tag
	BuildArgs  map[string]string // Build arguments
	NoCache    bool              // Force rebuild
}

// BuildResult from a build operation.
type BuildResult struct {
	ImageTag string // Image tag
	Cached   bool   // Whether build was cached
}

// UpOptions contains options for the Up operation.
type UpOptions struct {
	NoCache     bool // Force rebuild of source-based images
	BasePort    int  // Base port for host port allocation (default: 9000)
	GatewayPort int  // Port for MCP gateway (for agent MCP_ENDPOINT injection)
}

// UpResult contains the result of starting a stack.
type UpResult struct {
	MCPServers []MCPServerResult
	Agents     []AgentResult
}

// MCPServerResult is the runtime-agnostic result for an MCP server.
type MCPServerResult struct {
	Name       string     // Logical name
	WorkloadID WorkloadID // Runtime ID (empty for external/local/SSH/OpenAPI)
	Endpoint   string     // How to reach it (URL or host:port)
	HostPort   int        // Host port if applicable

	// Server type flags (exactly one should be true for non-container servers)
	External     bool // URL-based external server
	LocalProcess bool // Local stdio process
	SSH          bool // SSH-based remote process
	OpenAPI      bool // OpenAPI-based adapter server

	// For non-container servers
	URL             string   // External server URL
	Command         []string // Local process or SSH command
	SSHHost         string
	SSHUser         string
	SSHPort         int
	SSHIdentityFile string

	// For OpenAPI servers
	OpenAPIConfig *config.OpenAPIConfig // OpenAPI configuration for gateway to use
}

// AgentResult is the runtime-agnostic result for an agent.
type AgentResult struct {
	Name       string               // Logical name
	WorkloadID WorkloadID           // Runtime ID
	Uses       []config.ToolSelector // MCP servers this agent depends on
}

// NewOrchestrator creates an Orchestrator with the given runtime and builder.
func NewOrchestrator(runtime WorkloadRuntime, builder Builder) *Orchestrator {
	return &Orchestrator{
		runtime: runtime,
		builder: builder,
		logger:  logging.NewDiscardLogger(),
	}
}

// SetLogger sets the logger for orchestration operations.
func (o *Orchestrator) SetLogger(logger *slog.Logger) {
	if logger != nil {
		o.logger = logger
	}
}

// Close closes the runtime.
func (o *Orchestrator) Close() error {
	return o.runtime.Close()
}

// Runtime returns the underlying WorkloadRuntime for advanced use cases.
func (o *Orchestrator) Runtime() WorkloadRuntime {
	return o.runtime
}

// Up starts all MCP servers and resources defined in the stack.
func (o *Orchestrator) Up(ctx context.Context, stack *config.Stack, opts UpOptions) (*UpResult, error) {
	// Check runtime
	if err := o.runtime.Ping(ctx); err != nil {
		return nil, err
	}

	if opts.BasePort == 0 {
		opts.BasePort = 9000
	}

	o.logger.Info("starting stack", "name", stack.Name)

	// Create network(s)
	if len(stack.Networks) > 0 {
		// Advanced mode: create multiple networks
		for _, net := range stack.Networks {
			o.logger.Info("creating network", "name", net.Name)
			if err := o.runtime.EnsureNetwork(ctx, net.Name, NetworkOptions{
				Driver: net.Driver,
				Stack:  stack.Name,
			}); err != nil {
				return nil, fmt.Errorf("ensuring network %s: %w", net.Name, err)
			}
		}
	} else {
		// Simple mode: single network
		o.logger.Info("creating network", "name", stack.Network.Name)
		if err := o.runtime.EnsureNetwork(ctx, stack.Network.Name, NetworkOptions{
			Driver: stack.Network.Driver,
			Stack:  stack.Name,
		}); err != nil {
			return nil, fmt.Errorf("ensuring network: %w", err)
		}
	}

	// Start resources first (databases, etc.)
	for _, res := range stack.Resources {
		if err := o.startResource(ctx, stack, &res); err != nil {
			return nil, fmt.Errorf("starting resource %s: %w", res.Name, err)
		}
	}

	// Start MCP servers and collect info
	result := &UpResult{}
	containerIndex := 0 // Track container-based servers for port allocation
	for _, server := range stack.MCPServers {
		// Skip container creation for external servers
		if server.IsExternal() {
			o.logger.Info("registering external MCP server", "name", server.Name, "url", server.URL)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:     server.Name,
				External: true,
				URL:      server.URL,
			})
			continue
		}

		// Skip container creation for local process servers
		if server.IsLocalProcess() {
			o.logger.Info("registering local process MCP server", "name", server.Name, "command", server.Command)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:         server.Name,
				LocalProcess: true,
				Command:      server.Command,
			})
			continue
		}

		// Skip container creation for SSH servers
		if server.IsSSH() {
			o.logger.Info("registering SSH MCP server",
				"name", server.Name,
				"host", server.SSH.Host,
				"user", server.SSH.User,
				"command", server.Command)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:            server.Name,
				SSH:             true,
				Command:         server.Command,
				SSHHost:         server.SSH.Host,
				SSHUser:         server.SSH.User,
				SSHPort:         server.SSH.Port,
				SSHIdentityFile: server.SSH.IdentityFile,
			})
			continue
		}

		// Skip container creation for OpenAPI servers
		if server.IsOpenAPI() {
			o.logger.Info("registering OpenAPI MCP server",
				"name", server.Name,
				"spec", server.OpenAPI.Spec)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:          server.Name,
				OpenAPI:       true,
				OpenAPIConfig: server.OpenAPI,
			})
			continue
		}

		hostPort := opts.BasePort + containerIndex
		containerIndex++
		info, err := o.startMCPServer(ctx, stack, &server, opts, hostPort)
		if err != nil {
			return nil, fmt.Errorf("starting MCP server %s: %w", server.Name, err)
		}
		result.MCPServers = append(result.MCPServers, *info)
	}

	// Start agents in dependency order (topologically sorted)
	sortedAgents, err := sortAgentsByDependency(stack)
	if err != nil {
		return nil, fmt.Errorf("resolving agent dependencies: %w", err)
	}

	for _, agent := range sortedAgents {
		info, err := o.startAgent(ctx, stack, &agent, opts)
		if err != nil {
			return nil, fmt.Errorf("starting agent %s: %w", agent.Name, err)
		}
		result.Agents = append(result.Agents, *info)
	}

	o.logger.Info("all workloads started successfully")
	return result, nil
}

func (o *Orchestrator) startMCPServer(ctx context.Context, stack *config.Stack, server *config.MCPServer, opts UpOptions, hostPort int) (*MCPServerResult, error) {
	containerName := containerName(stack.Name, server.Name)

	// Check if container already exists
	exists, workloadID, err := o.runtime.Exists(ctx, containerName)
	if err != nil {
		return nil, err
	}

	if exists {
		o.logger.Info("MCP server already exists, starting", "name", server.Name)
		// Get actual host port
		actualHostPort, _ := o.runtime.GetHostPort(ctx, workloadID, server.Port)
		return &MCPServerResult{
			Name:       server.Name,
			WorkloadID: workloadID,
			Endpoint:   fmt.Sprintf("localhost:%d", actualHostPort),
			HostPort:   actualHostPort,
		}, nil
	}

	// Determine image
	var imageName string
	if server.Source != nil {
		// Build from source
		o.logger.Info("building MCP server from source", "name", server.Name, "sourceType", server.Source.Type)

		buildOpts := BuildOptions{
			SourceType: server.Source.Type,
			URL:        server.Source.URL,
			Ref:        server.Source.Ref,
			Path:       server.Source.Path,
			Dockerfile: server.Source.Dockerfile,
			Tag:        generateTag(stack.Name, server.Name),
			BuildArgs:  server.BuildArgs,
			NoCache:    opts.NoCache,
		}

		result, err := o.builder.Build(ctx, buildOpts)
		if err != nil {
			return nil, fmt.Errorf("building image: %w", err)
		}
		imageName = result.ImageTag
	} else {
		imageName = server.Image
		o.logger.Info("starting MCP server", "name", server.Name, "image", imageName)

		// Pull image if needed
		if err := o.runtime.EnsureImage(ctx, imageName); err != nil {
			return nil, err
		}
	}

	// Determine network name
	networkName := stack.Network.Name
	if len(stack.Networks) > 0 && server.Network != "" {
		networkName = server.Network
	}

	// Create workload config
	// Note: Name is the logical name, the runtime generates the container name
	cfg := WorkloadConfig{
		Name:        server.Name,
		Stack:       stack.Name,
		Type:        WorkloadTypeMCPServer,
		Image:       imageName,
		Command:     server.Command,
		Env:         server.Env,
		NetworkName: networkName,
		ExposedPort: server.Port,
		HostPort:    hostPort,
		Transport:   server.Transport,
		Labels:      managedLabels(stack.Name, server.Name, true),
	}

	status, err := o.runtime.Start(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Get actual host port (in case it was auto-assigned)
	actualHostPort := status.HostPort
	if actualHostPort == 0 {
		actualHostPort = hostPort
	}

	o.logger.Info("MCP server listening", "name", server.Name, "port", actualHostPort)

	return &MCPServerResult{
		Name:       server.Name,
		WorkloadID: status.ID,
		Endpoint:   fmt.Sprintf("localhost:%d", actualHostPort),
		HostPort:   actualHostPort,
	}, nil
}

func (o *Orchestrator) startResource(ctx context.Context, stack *config.Stack, res *config.Resource) error {
	containerName := containerName(stack.Name, res.Name)

	// Check if container already exists
	exists, workloadID, err := o.runtime.Exists(ctx, containerName)
	if err != nil {
		return err
	}

	if exists {
		o.logger.Info("resource already exists, starting", "name", res.Name)
		// Attempt to start (may already be running)
		status, err := o.runtime.Status(ctx, workloadID)
		if err != nil {
			return err
		}
		if status.State != WorkloadStateRunning {
			// Need to start using the runtime's Start which handles existing containers
			_, err = o.runtime.Start(ctx, WorkloadConfig{Name: res.Name, Stack: stack.Name})
			return err
		}
		return nil
	}

	o.logger.Info("starting resource", "name", res.Name, "image", res.Image)

	// Pull image if needed
	if err := o.runtime.EnsureImage(ctx, res.Image); err != nil {
		return err
	}

	// Determine network name
	networkName := stack.Network.Name
	if len(stack.Networks) > 0 && res.Network != "" {
		networkName = res.Network
	}

	// Create workload config
	// Note: Name is the logical name, the runtime generates the container name
	cfg := WorkloadConfig{
		Name:        res.Name,
		Stack:       stack.Name,
		Type:        WorkloadTypeResource,
		Image:       res.Image,
		Env:         res.Env,
		NetworkName: networkName,
		ExposedPort: 0, // Resources don't expose MCP ports
		Volumes:     res.Volumes,
		Labels:      managedLabels(stack.Name, res.Name, false),
	}

	_, err = o.runtime.Start(ctx, cfg)
	return err
}

func (o *Orchestrator) startAgent(ctx context.Context, stack *config.Stack, agent *config.Agent, opts UpOptions) (*AgentResult, error) {
	containerName := containerName(stack.Name, agent.Name)

	// Check if container already exists
	exists, workloadID, err := o.runtime.Exists(ctx, containerName)
	if err != nil {
		return nil, err
	}

	if exists {
		o.logger.Info("agent already exists, starting", "name", agent.Name)
		return &AgentResult{
			Name:       agent.Name,
			WorkloadID: workloadID,
			Uses:       agent.Uses,
		}, nil
	}

	// Determine image
	var imageName string
	if agent.Source != nil {
		// Build from source
		o.logger.Info("building agent from source", "name", agent.Name, "sourceType", agent.Source.Type)

		buildOpts := BuildOptions{
			SourceType: agent.Source.Type,
			URL:        agent.Source.URL,
			Ref:        agent.Source.Ref,
			Path:       agent.Source.Path,
			Dockerfile: agent.Source.Dockerfile,
			Tag:        generateTag(stack.Name, agent.Name),
			BuildArgs:  agent.BuildArgs,
			NoCache:    opts.NoCache,
		}

		result, err := o.builder.Build(ctx, buildOpts)
		if err != nil {
			return nil, fmt.Errorf("building image: %w", err)
		}
		imageName = result.ImageTag
	} else {
		imageName = agent.Image
		o.logger.Info("starting agent", "name", agent.Name, "image", imageName)

		// Pull image if needed
		if err := o.runtime.EnsureImage(ctx, imageName); err != nil {
			return nil, err
		}
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
	// Inject MCP gateway endpoint for agent to connect to
	if opts.GatewayPort > 0 {
		env["MCP_ENDPOINT"] = fmt.Sprintf("http://host.docker.internal:%d", opts.GatewayPort)
	}

	// Create workload config
	// Note: Name is the logical name, the runtime generates the container name
	cfg := WorkloadConfig{
		Name:        agent.Name,
		Stack:       stack.Name,
		Type:        WorkloadTypeAgent,
		Image:       imageName,
		Command:     agent.Command,
		Env:         env,
		NetworkName: networkName,
		ExposedPort: 0, // Agents don't expose ports
		Labels:      agentLabels(stack.Name, agent.Name),
	}

	status, err := o.runtime.Start(ctx, cfg)
	if err != nil {
		return nil, err
	}

	o.logger.Info("agent started", "name", agent.Name, "uses", agent.Uses)

	return &AgentResult{
		Name:       agent.Name,
		WorkloadID: status.ID,
		Uses:       agent.Uses,
	}, nil
}

// Down stops and removes all managed workloads and networks for a stack.
func (o *Orchestrator) Down(ctx context.Context, stack string) error {
	// Check runtime
	if err := o.runtime.Ping(ctx); err != nil {
		return err
	}

	o.logger.Info("stopping managed workloads")

	workloads, err := o.runtime.List(ctx, WorkloadFilter{Stack: stack})
	if err != nil {
		return err
	}

	if len(workloads) == 0 {
		o.logger.Info("no managed workloads found")
	} else {
		for _, w := range workloads {
			o.logger.Info("stopping workload", "name", w.Name)
			if err := o.runtime.Stop(ctx, w.ID); err != nil {
				o.logger.Warn("failed to stop workload", "name", w.Name, "error", err)
			}

			o.logger.Info("removing workload", "name", w.Name)
			if err := o.runtime.Remove(ctx, w.ID); err != nil {
				o.logger.Warn("failed to remove workload", "name", w.Name, "error", err)
			}
		}
		o.logger.Info("all workloads stopped and removed")
	}

	// Clean up networks
	networks, err := o.runtime.ListNetworks(ctx, stack)
	if err != nil {
		o.logger.Warn("failed to list networks", "error", err)
	} else if len(networks) > 0 {
		o.logger.Info("removing managed networks")
		for _, name := range networks {
			o.logger.Info("removing network", "name", name)
			if err := o.runtime.RemoveNetwork(ctx, name); err != nil {
				o.logger.Warn("failed to remove network", "name", name, "error", err)
			}
		}
	}

	return nil
}

// Status returns information about managed workloads.
func (o *Orchestrator) Status(ctx context.Context, stack string) ([]WorkloadStatus, error) {
	// Check runtime
	if err := o.runtime.Ping(ctx); err != nil {
		return nil, err
	}

	return o.runtime.List(ctx, WorkloadFilter{Stack: stack})
}

// sortAgentsByDependency returns agents sorted in dependency order.
// Agents with no agent dependencies come first, dependent agents come later.
func sortAgentsByDependency(stack *config.Stack) ([]config.Agent, error) {
	if len(stack.Agents) == 0 {
		return nil, nil
	}

	// Build set of A2A-enabled agent names (these are the only valid agent dependencies)
	a2aAgents := make(map[string]bool)
	for _, agent := range stack.Agents {
		if agent.IsA2AEnabled() {
			a2aAgents[agent.Name] = true
		}
	}

	// Build dependency graph
	graph := NewDependencyGraph()
	agentsByName := make(map[string]config.Agent)

	for _, agent := range stack.Agents {
		graph.AddNode(agent.Name)
		agentsByName[agent.Name] = agent

		// Add edges for agent-to-agent dependencies only (not MCP server dependencies)
		for _, selector := range agent.Uses {
			if a2aAgents[selector.Server] {
				graph.AddEdge(agent.Name, selector.Server)
			}
		}
	}

	// Topological sort
	sortedNames, err := graph.Sort()
	if err != nil {
		return nil, err
	}

	// Convert sorted names back to agent configs
	sortedAgents := make([]config.Agent, len(sortedNames))
	for i, name := range sortedNames {
		sortedAgents[i] = agentsByName[name]
	}

	return sortedAgents, nil
}

// Helper functions that don't need Docker-specific code

func containerName(stack, name string) string {
	return "gridctl-" + stack + "-" + name
}

func generateTag(stack, name string) string {
	return fmt.Sprintf("gridctl-%s-%s:latest", stack, name)
}

func managedLabels(stack, name string, isMCPServer bool) map[string]string {
	labels := map[string]string{
		"gridctl.managed": "true",
		"gridctl.stack":   stack,
	}
	if isMCPServer {
		labels["gridctl.mcp-server"] = name
	} else {
		labels["gridctl.resource"] = name
	}
	return labels
}

func agentLabels(stack, name string) map[string]string {
	return map[string]string{
		"gridctl.managed": "true",
		"gridctl.stack":   stack,
		"gridctl.agent":   name,
	}
}
