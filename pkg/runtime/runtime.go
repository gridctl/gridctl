package runtime

import (
	"context"
	"fmt"

	"agentlab/pkg/builder"
	"agentlab/pkg/config"
	"agentlab/pkg/dockerclient"
)

// Runtime manages the lifecycle of agentlab containers.
type Runtime struct {
	cli     dockerclient.DockerClient
	builder *builder.Builder
}

// MCPServerInfo contains runtime information about a started MCP server.
type MCPServerInfo struct {
	Name          string
	ContainerID   string
	ContainerName string
	ContainerPort int
	HostPort      int
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
	}, nil
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

	fmt.Printf("Starting topology '%s'\n", topo.Name)

	// Create network(s)
	if len(topo.Networks) > 0 {
		// Advanced mode: create multiple networks
		for _, net := range topo.Networks {
			fmt.Printf("Creating network '%s'...\n", net.Name)
			_, err := EnsureNetwork(ctx, r.cli, net.Name, net.Driver, topo.Name)
			if err != nil {
				return nil, fmt.Errorf("ensuring network %s: %w", net.Name, err)
			}
		}
	} else {
		// Simple mode: single network
		fmt.Printf("Creating network '%s'...\n", topo.Network.Name)
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
	for i, server := range topo.MCPServers {
		hostPort := opts.BasePort + i
		info, err := r.startMCPServer(ctx, topo, &server, opts, hostPort)
		if err != nil {
			return nil, fmt.Errorf("starting MCP server %s: %w", server.Name, err)
		}
		result.MCPServers = append(result.MCPServers, *info)
	}

	// Start agents (after MCP servers so dependencies are ready)
	for _, agent := range topo.Agents {
		info, err := r.startAgent(ctx, topo, &agent, opts)
		if err != nil {
			return nil, fmt.Errorf("starting agent %s: %w", agent.Name, err)
		}
		result.Agents = append(result.Agents, *info)
	}

	fmt.Println("\nAll containers started successfully!")
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
		fmt.Printf("  MCP server '%s' already exists, starting...\n", server.Name)
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
		fmt.Printf("  Building MCP server '%s' from %s source...\n", server.Name, server.Source.Type)

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
		fmt.Printf("  Starting MCP server '%s' (%s)...\n", server.Name, imageName)

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

	fmt.Printf("  MCP server '%s' listening on localhost:%d\n", server.Name, actualHostPort)

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
		fmt.Printf("  Resource '%s' already exists, starting...\n", res.Name)
		return StartContainer(ctx, r.cli, containerID)
	}

	fmt.Printf("  Starting resource '%s' (%s)...\n", res.Name, res.Image)

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
		fmt.Printf("  Agent '%s' already exists, starting...\n", agent.Name)
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
		fmt.Printf("  Building agent '%s' from %s source...\n", agent.Name, agent.Source.Type)

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
		fmt.Printf("  Starting agent '%s' (%s)...\n", agent.Name, imageName)

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

	fmt.Printf("  Agent '%s' started (uses: %v)\n", agent.Name, agent.Uses)

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

	fmt.Println("Stopping managed containers...")

	containers, err := ListManagedContainers(ctx, r.cli, topology)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		fmt.Println("No managed containers found.")
	} else {
		for _, c := range containers {
			name := c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}

			fmt.Printf("  Stopping %s...\n", name)
			if err := StopContainer(ctx, r.cli, c.ID, 10); err != nil {
				fmt.Printf("    Warning: %v\n", err)
			}

			fmt.Printf("  Removing %s...\n", name)
			if err := RemoveContainer(ctx, r.cli, c.ID, true); err != nil {
				fmt.Printf("    Warning: %v\n", err)
			}
		}
		fmt.Println("All containers stopped and removed.")
	}

	// Clean up networks
	networks, err := ListManagedNetworks(ctx, r.cli, topology)
	if err != nil {
		fmt.Printf("Warning: failed to list networks: %v\n", err)
	} else if len(networks) > 0 {
		fmt.Println("Removing managed networks...")
		for _, name := range networks {
			fmt.Printf("  Removing network %s...\n", name)
			if err := RemoveNetwork(ctx, r.cli, name); err != nil {
				fmt.Printf("    Warning: %v\n", err)
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
