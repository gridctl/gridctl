package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"agentlab/pkg/builder"
	"agentlab/pkg/config"
	"agentlab/pkg/dockerclient"
	"agentlab/pkg/logging"
)

// Runtime manages the lifecycle of agentlab containers.
type Runtime struct {
	cli     dockerclient.DockerClient
	builder *builder.Builder
	logger  *slog.Logger
}

// MCPServerInfo contains runtime information about a started MCP server.
type MCPServerInfo struct {
	Name          string
	ContainerID   string   // Empty for external/local process/SSH servers
	ContainerName string   // Empty for external/local process/SSH servers
	ContainerPort int      // 0 for external/local process/SSH servers
	HostPort      int      // 0 for external/local process/SSH servers
	External      bool     // True if external server (no container)
	LocalProcess  bool     // True if local process server (no container)
	SSH           bool     // True if SSH server (remote process over SSH)
	URL           string   // Full URL for external servers
	Command       []string // Command for local process or SSH servers
	SSHHost       string   // SSH hostname (for SSH servers)
	SSHUser       string   // SSH username (for SSH servers)
	SSHPort       int      // SSH port (for SSH servers, 0 = default 22)
	SSHIdentityFile string // SSH identity file path (for SSH servers)
}

// AgentInfo contains runtime information about a started agent.
type AgentInfo struct {
	Name          string
	ContainerID   string
	ContainerName string
	Uses          []string // MCP servers this agent depends on
}

// New creates a new Runtime instance.
func New() (*Runtime, error) {
	cli, err := NewDockerClient()
	if err != nil {
		return nil, err
	}
	return &Runtime{
		cli:     cli,
		builder: builder.New(cli),
		logger:  logging.NewDiscardLogger(),
	}, nil
}

// SetLogger sets the logger for runtime operations.
// If nil is passed, logging is disabled (default).
func (r *Runtime) SetLogger(logger *slog.Logger) {
	if logger != nil {
		r.logger = logger
	}
}

// Close closes the Docker client.
func (r *Runtime) Close() error {
	return r.cli.Close()
}

// DockerClient returns the Docker client for use by other components.
func (r *Runtime) DockerClient() dockerclient.DockerClient {
	return r.cli
}

// UpOptions contains options for the Up operation.
type UpOptions struct {
	NoCache     bool // Force rebuild of source-based images
	BasePort    int  // Base port for host port allocation (default: 9000)
	GatewayPort int  // Port for MCP gateway (for agent MCP_ENDPOINT injection)
}

// UpResult contains the result of starting a topology.
type UpResult struct {
	MCPServers []MCPServerInfo
	Agents     []AgentInfo
}

// Up starts all MCP servers and resources defined in the topology.
func (r *Runtime) Up(ctx context.Context, topo *config.Topology, opts UpOptions) (*UpResult, error) {
	// Check Docker daemon
	if err := Ping(ctx, r.cli); err != nil {
		return nil, err
	}

	if opts.BasePort == 0 {
		opts.BasePort = 9000
	}

	r.logger.Info("starting topology", "name", topo.Name)

	// Create network(s)
	if len(topo.Networks) > 0 {
		// Advanced mode: create multiple networks
		for _, net := range topo.Networks {
			r.logger.Info("creating network", "name", net.Name)
			_, err := EnsureNetwork(ctx, r.cli, net.Name, net.Driver, topo.Name)
			if err != nil {
				return nil, fmt.Errorf("ensuring network %s: %w", net.Name, err)
			}
		}
	} else {
		// Simple mode: single network
		r.logger.Info("creating network", "name", topo.Network.Name)
		_, err := EnsureNetwork(ctx, r.cli, topo.Network.Name, topo.Network.Driver, topo.Name)
		if err != nil {
			return nil, fmt.Errorf("ensuring network: %w", err)
		}
	}

	// Start resources first (databases, etc.)
	for _, res := range topo.Resources {
		if err := r.startResource(ctx, topo, &res); err != nil {
			return nil, fmt.Errorf("starting resource %s: %w", res.Name, err)
		}
	}

	// Start MCP servers and collect info
	result := &UpResult{}
	containerIndex := 0 // Track container-based servers for port allocation
	for _, server := range topo.MCPServers {
		// Skip container creation for external servers
		if server.IsExternal() {
			r.logger.Info("registering external MCP server", "name", server.Name, "url", server.URL)
			result.MCPServers = append(result.MCPServers, MCPServerInfo{
				Name:     server.Name,
				External: true,
				URL:      server.URL,
			})
			continue
		}

		// Skip container creation for local process servers
		if server.IsLocalProcess() {
			r.logger.Info("registering local process MCP server", "name", server.Name, "command", server.Command)
			result.MCPServers = append(result.MCPServers, MCPServerInfo{
				Name:         server.Name,
				LocalProcess: true,
				Command:      server.Command,
			})
			continue
		}

		// Skip container creation for SSH servers
		if server.IsSSH() {
			r.logger.Info("registering SSH MCP server",
				"name", server.Name,
				"host", server.SSH.Host,
				"user", server.SSH.User,
				"command", server.Command)
			result.MCPServers = append(result.MCPServers, MCPServerInfo{
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

		hostPort := opts.BasePort + containerIndex
		containerIndex++
		info, err := r.startMCPServer(ctx, topo, &server, opts, hostPort)
		if err != nil {
			return nil, fmt.Errorf("starting MCP server %s: %w", server.Name, err)
		}
		result.MCPServers = append(result.MCPServers, *info)
	}

	// Start agents in dependency order (topologically sorted)
	sortedAgents, err := sortAgentsByDependency(topo)
	if err != nil {
		return nil, fmt.Errorf("resolving agent dependencies: %w", err)
	}

	for _, agent := range sortedAgents {
		info, err := r.startAgent(ctx, topo, &agent, opts)
		if err != nil {
			return nil, fmt.Errorf("starting agent %s: %w", agent.Name, err)
		}
		result.Agents = append(result.Agents, *info)
	}

	r.logger.Info("all containers started successfully")
	return result, nil
}

func (r *Runtime) startMCPServer(ctx context.Context, topo *config.Topology, server *config.MCPServer, opts UpOptions, hostPort int) (*MCPServerInfo, error) {
	containerName := ContainerName(topo.Name, server.Name)

	// Check if container already exists
	exists, containerID, err := ContainerExists(ctx, r.cli, containerName)
	if err != nil {
		return nil, err
	}

	if exists {
		r.logger.Info("MCP server already exists, starting", "name", server.Name)
		if err := StartContainer(ctx, r.cli, containerID); err != nil {
			return nil, err
		}
		// Get actual host port
		actualHostPort, _ := GetContainerHostPort(ctx, r.cli, containerID, server.Port)
		return &MCPServerInfo{
			Name:          server.Name,
			ContainerID:   containerID,
			ContainerName: containerName,
			ContainerPort: server.Port,
			HostPort:      actualHostPort,
		}, nil
	}

	// Determine image
	var imageName string
	if server.Source != nil {
		// Build from source
		r.logger.Info("building MCP server from source", "name", server.Name, "sourceType", server.Source.Type)

		buildOpts := builder.BuildOptions{
			SourceType: server.Source.Type,
			URL:        server.Source.URL,
			Ref:        server.Source.Ref,
			Path:       server.Source.Path,
			Dockerfile: server.Source.Dockerfile,
			Tag:        builder.GenerateTag(topo.Name, server.Name),
			BuildArgs:  server.BuildArgs,
			NoCache:    opts.NoCache,
		}

		result, err := r.builder.Build(ctx, buildOpts)
		if err != nil {
			return nil, fmt.Errorf("building image: %w", err)
		}
		imageName = result.ImageTag
	} else {
		imageName = server.Image
		r.logger.Info("starting MCP server", "name", server.Name, "image", imageName)

		// Pull image if needed
		if err := EnsureImage(ctx, r.cli, imageName); err != nil {
			return nil, err
		}
	}

	// Determine network name
	// In advanced mode (networks[] defined), use server.Network
	// In simple mode, use topo.Network.Name (server.Network is ignored)
	networkName := topo.Network.Name
	if len(topo.Networks) > 0 && server.Network != "" {
		networkName = server.Network
	}

	// Create container with host port binding
	cfg := ContainerConfig{
		Name:        containerName,
		Image:       imageName,
		Command:     server.Command,
		Env:         server.Env,
		Port:        server.Port,
		HostPort:    hostPort,
		NetworkName: networkName,
		Labels:      ManagedLabels(topo.Name, server.Name, true),
		Transport:   server.Transport,
	}

	containerID, err = CreateContainer(ctx, r.cli, cfg)
	if err != nil {
		return nil, err
	}

	// Start container
	if err := StartContainer(ctx, r.cli, containerID); err != nil {
		return nil, err
	}

	// Get actual host port (in case it was auto-assigned)
	actualHostPort, _ := GetContainerHostPort(ctx, r.cli, containerID, server.Port)
	if actualHostPort == 0 {
		actualHostPort = hostPort
	}

	r.logger.Info("MCP server listening", "name", server.Name, "port", actualHostPort)

	return &MCPServerInfo{
		Name:          server.Name,
		ContainerID:   containerID,
		ContainerName: containerName,
		ContainerPort: server.Port,
		HostPort:      actualHostPort,
	}, nil
}

func (r *Runtime) startResource(ctx context.Context, topo *config.Topology, res *config.Resource) error {
	containerName := ContainerName(topo.Name, res.Name)

	// Check if container already exists
	exists, containerID, err := ContainerExists(ctx, r.cli, containerName)
	if err != nil {
		return err
	}

	if exists {
		r.logger.Info("resource already exists, starting", "name", res.Name)
		return StartContainer(ctx, r.cli, containerID)
	}

	r.logger.Info("starting resource", "name", res.Name, "image", res.Image)

	// Pull image if needed
	if err := EnsureImage(ctx, r.cli, res.Image); err != nil {
		return err
	}

	// Determine network name
	// In advanced mode (networks[] defined), use res.Network
	// In simple mode, use topo.Network.Name (res.Network is ignored)
	networkName := topo.Network.Name
	if len(topo.Networks) > 0 && res.Network != "" {
		networkName = res.Network
	}

	// Create container
	cfg := ContainerConfig{
		Name:        containerName,
		Image:       res.Image,
		Env:         res.Env,
		Port:        0, // Resources don't expose MCP ports
		NetworkName: networkName,
		Labels:      ManagedLabels(topo.Name, res.Name, false),
		Volumes:     res.Volumes,
	}

	containerID, err = CreateContainer(ctx, r.cli, cfg)
	if err != nil {
		return err
	}

	return StartContainer(ctx, r.cli, containerID)
}

func (r *Runtime) startAgent(ctx context.Context, topo *config.Topology, agent *config.Agent, opts UpOptions) (*AgentInfo, error) {
	containerName := ContainerName(topo.Name, agent.Name)

	// Check if container already exists
	exists, containerID, err := ContainerExists(ctx, r.cli, containerName)
	if err != nil {
		return nil, err
	}

	if exists {
		r.logger.Info("agent already exists, starting", "name", agent.Name)
		if err := StartContainer(ctx, r.cli, containerID); err != nil {
			return nil, err
		}
		return &AgentInfo{
			Name:          agent.Name,
			ContainerID:   containerID,
			ContainerName: containerName,
			Uses:          agent.Uses,
		}, nil
	}

	// Determine image
	var imageName string
	if agent.Source != nil {
		// Build from source
		r.logger.Info("building agent from source", "name", agent.Name, "sourceType", agent.Source.Type)

		buildOpts := builder.BuildOptions{
			SourceType: agent.Source.Type,
			URL:        agent.Source.URL,
			Ref:        agent.Source.Ref,
			Path:       agent.Source.Path,
			Dockerfile: agent.Source.Dockerfile,
			Tag:        builder.GenerateTag(topo.Name, agent.Name),
			BuildArgs:  agent.BuildArgs,
			NoCache:    opts.NoCache,
		}

		result, err := r.builder.Build(ctx, buildOpts)
		if err != nil {
			return nil, fmt.Errorf("building image: %w", err)
		}
		imageName = result.ImageTag
	} else {
		imageName = agent.Image
		r.logger.Info("starting agent", "name", agent.Name, "image", imageName)

		// Pull image if needed
		if err := EnsureImage(ctx, r.cli, imageName); err != nil {
			return nil, err
		}
	}

	// Determine network name
	// In advanced mode (networks[] defined), use agent.Network
	// In simple mode, use topo.Network.Name (agent.Network is ignored)
	networkName := topo.Network.Name
	if len(topo.Networks) > 0 && agent.Network != "" {
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

	// Create container (agents don't expose ports like MCP servers)
	cfg := ContainerConfig{
		Name:        containerName,
		Image:       imageName,
		Command:     agent.Command,
		Env:         env,
		Port:        0, // Agents don't need to expose ports
		NetworkName: networkName,
		Labels:      AgentLabels(topo.Name, agent.Name),
	}

	containerID, err = CreateContainer(ctx, r.cli, cfg)
	if err != nil {
		return nil, err
	}

	// Start container
	if err := StartContainer(ctx, r.cli, containerID); err != nil {
		return nil, err
	}

	r.logger.Info("agent started", "name", agent.Name, "uses", agent.Uses)

	return &AgentInfo{
		Name:          agent.Name,
		ContainerID:   containerID,
		ContainerName: containerName,
		Uses:          agent.Uses,
	}, nil
}

// Down stops and removes all managed containers and networks for a topology.
func (r *Runtime) Down(ctx context.Context, topology string) error {
	// Check Docker daemon
	if err := Ping(ctx, r.cli); err != nil {
		return err
	}

	r.logger.Info("stopping managed containers")

	containers, err := ListManagedContainers(ctx, r.cli, topology)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		r.logger.Info("no managed containers found")
	} else {
		for _, c := range containers {
			name := c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}

			r.logger.Info("stopping container", "name", name)
			if err := StopContainer(ctx, r.cli, c.ID, 10); err != nil {
				r.logger.Warn("failed to stop container", "name", name, "error", err)
			}

			r.logger.Info("removing container", "name", name)
			if err := RemoveContainer(ctx, r.cli, c.ID, true); err != nil {
				r.logger.Warn("failed to remove container", "name", name, "error", err)
			}
		}
		r.logger.Info("all containers stopped and removed")
	}

	// Clean up networks
	networks, err := ListManagedNetworks(ctx, r.cli, topology)
	if err != nil {
		r.logger.Warn("failed to list networks", "error", err)
	} else if len(networks) > 0 {
		r.logger.Info("removing managed networks")
		for _, name := range networks {
			r.logger.Info("removing network", "name", name)
			if err := RemoveNetwork(ctx, r.cli, name); err != nil {
				r.logger.Warn("failed to remove network", "name", name, "error", err)
			}
		}
	}

	return nil
}

// Status returns information about managed containers.
func (r *Runtime) Status(ctx context.Context, topology string) ([]ContainerStatus, error) {
	// Check Docker daemon
	if err := Ping(ctx, r.cli); err != nil {
		return nil, err
	}

	containers, err := ListManagedContainers(ctx, r.cli, topology)
	if err != nil {
		return nil, err
	}

	var statuses []ContainerStatus
	for _, c := range containers {
		name := c.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}

		status := ContainerStatus{
			ID:       c.ID[:12],
			Name:     name,
			Image:    c.Image,
			State:    c.State,
			Status:   c.Status,
			Topology: c.Labels[LabelTopology],
		}

		if server, ok := c.Labels[LabelMCPServer]; ok {
			status.Type = "mcp-server"
			status.MCPServerName = server
		} else if res, ok := c.Labels[LabelResource]; ok {
			status.Type = "resource"
			status.MCPServerName = res
		} else if agentName, ok := c.Labels[LabelAgent]; ok {
			status.Type = "agent"
			status.MCPServerName = agentName
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// ContainerStatus holds status information for a container.
type ContainerStatus struct {
	ID            string
	Name          string
	Image         string
	State         string
	Status        string
	Type          string // "mcp-server", "resource", or "agent"
	MCPServerName string // Name of the MCP server, resource, or agent
	Topology      string
}

// sortAgentsByDependency returns agents sorted in dependency order.
// Agents with no agent dependencies come first, dependent agents come later.
// This ensures that when Agent A uses Agent B (as a skill), Agent B starts first.
func sortAgentsByDependency(topo *config.Topology) ([]config.Agent, error) {
	if len(topo.Agents) == 0 {
		return nil, nil
	}

	// Build set of A2A-enabled agent names (these are the only valid agent dependencies)
	a2aAgents := make(map[string]bool)
	for _, agent := range topo.Agents {
		if agent.IsA2AEnabled() {
			a2aAgents[agent.Name] = true
		}
	}

	// Build dependency graph
	graph := NewDependencyGraph()
	agentsByName := make(map[string]config.Agent)

	for _, agent := range topo.Agents {
		graph.AddNode(agent.Name)
		agentsByName[agent.Name] = agent

		// Add edges for agent-to-agent dependencies only (not MCP server dependencies)
		for _, dep := range agent.Uses {
			if a2aAgents[dep] {
				graph.AddEdge(agent.Name, dep)
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
