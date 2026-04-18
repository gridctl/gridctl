package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
)

// LoggerSetter is an interface for types that accept a logger.
type LoggerSetter interface {
	SetLogger(logger *slog.Logger)
}

// Orchestrator manages the lifecycle of gridctl workloads.
// It uses a WorkloadRuntime to start/stop workloads and a Builder for image builds.
type Orchestrator struct {
	runtime     WorkloadRuntime
	builder     Builder
	logger      *slog.Logger
	runtimeInfo *RuntimeInfo
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
	Logger     *slog.Logger      // Logger for build operations (optional)
}

// BuildResult from a build operation.
type BuildResult struct {
	ImageTag string // Image tag
	Cached   bool   // Whether build was cached
}

// UpOptions contains options for the Up operation.
type UpOptions struct {
	NoCache  bool // Force rebuild of source-based images
	BasePort int  // Base port for host port allocation (default: 9000)
}

// UpResult contains the result of starting a stack.
type UpResult struct {
	MCPServers []MCPServerResult
}

// MCPServerResult is the runtime-agnostic result for an MCP server.
type MCPServerResult struct {
	Name       string     // Logical name
	WorkloadID WorkloadID // Runtime ID (empty for external/local/SSH/OpenAPI). Mirrors Replicas[0].WorkloadID.
	Endpoint   string     // How to reach it (URL or host:port). Mirrors Replicas[0].Endpoint.
	HostPort   int        // Host port if applicable. Mirrors Replicas[0].HostPort.

	// Replicas carries per-replica runtime handles. Always non-empty after Up()
	// returns: a server with no replicas field (or Replicas=1) gets a single entry
	// whose top-level fields match the mirrored fields above.
	Replicas []MCPServerReplica

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

// MCPServerReplica is one replica's runtime handle. For non-container replicas
// (external, local process, SSH, OpenAPI) only ReplicaID is meaningful —
// WorkloadID, Endpoint, and HostPort are empty/zero.
type MCPServerReplica struct {
	ReplicaID  int        // zero-indexed within the server
	WorkloadID WorkloadID // runtime ID for container replicas
	Endpoint   string     // host:port or URL
	HostPort   int        // host port if applicable
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
// If the underlying runtime supports SetLogger, the logger is propagated.
func (o *Orchestrator) SetLogger(logger *slog.Logger) {
	if logger != nil {
		o.logger = logger
		// Propagate to runtime if it supports logging
		if ls, ok := o.runtime.(LoggerSetter); ok {
			ls.SetLogger(logger)
		}
	}
}

// SetRuntimeInfo stores runtime detection info for runtime-aware behavior.
func (o *Orchestrator) SetRuntimeInfo(info *RuntimeInfo) {
	o.runtimeInfo = info
}

// RuntimeInfo returns the detected runtime info, or nil if not set.
func (o *Orchestrator) RuntimeInfo() *RuntimeInfo {
	return o.runtimeInfo
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
	if opts.BasePort == 0 {
		opts.BasePort = 9000
	}

	o.logger.Info("starting stack", "name", stack.Name)

	// Only check Docker and create networks when container workloads exist
	if stack.NeedsContainerRuntime() {
		if err := o.runtime.Ping(ctx); err != nil {
			return nil, runtimeRequiredError(stack, err)
		}

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
		replicas := replicaCount(&server)

		// Skip container creation for external servers
		if server.IsExternal() {
			o.logger.Info("registering external MCP server", "name", server.Name, "url", server.URL)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:     server.Name,
				External: true,
				URL:      server.URL,
				Replicas: singleReplicaPlaceholder(),
			})
			continue
		}

		// Skip container creation for local process servers
		if server.IsLocalProcess() {
			o.logger.Info("registering local process MCP server", "name", server.Name, "command", server.Command, "replicas", replicas)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:         server.Name,
				LocalProcess: true,
				Command:      server.Command,
				Replicas:     nReplicaPlaceholders(replicas),
			})
			continue
		}

		// Skip container creation for SSH servers
		if server.IsSSH() {
			o.logger.Info("registering SSH MCP server",
				"name", server.Name,
				"host", server.SSH.Host,
				"user", server.SSH.User,
				"command", server.Command,
				"replicas", replicas)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:            server.Name,
				SSH:             true,
				Command:         server.Command,
				SSHHost:         server.SSH.Host,
				SSHUser:         server.SSH.User,
				SSHPort:         server.SSH.Port,
				SSHIdentityFile: server.SSH.IdentityFile,
				Replicas:        nReplicaPlaceholders(replicas),
			})
			continue
		}

		// Skip container creation for OpenAPI servers (already validated
		// to reject replicas > 1; singleReplicaPlaceholder is enough).
		if server.IsOpenAPI() {
			o.logger.Info("registering OpenAPI MCP server",
				"name", server.Name,
				"spec", server.OpenAPI.Spec)
			result.MCPServers = append(result.MCPServers, MCPServerResult{
				Name:          server.Name,
				OpenAPI:       true,
				OpenAPIConfig: server.OpenAPI,
				Replicas:      singleReplicaPlaceholder(),
			})
			continue
		}

		// Container-based server: start one container per replica.
		replicaHandles := make([]MCPServerReplica, 0, replicas)
		for replicaID := 0; replicaID < replicas; replicaID++ {
			hostPort := opts.BasePort + containerIndex
			containerIndex++
			info, err := o.startMCPServer(ctx, stack, &server, opts, hostPort, replicaID, replicas)
			if err != nil {
				return nil, fmt.Errorf("starting MCP server %s replica %d: %w", server.Name, replicaID, err)
			}
			replicaHandles = append(replicaHandles, MCPServerReplica{
				ReplicaID:  replicaID,
				WorkloadID: info.WorkloadID,
				Endpoint:   info.Endpoint,
				HostPort:   info.HostPort,
			})
		}
		result.MCPServers = append(result.MCPServers, MCPServerResult{
			Name:       server.Name,
			WorkloadID: replicaHandles[0].WorkloadID,
			Endpoint:   replicaHandles[0].Endpoint,
			HostPort:   replicaHandles[0].HostPort,
			Replicas:   replicaHandles,
		})
	}

	o.logger.Info("all workloads started successfully")
	return result, nil
}

// replicaCount returns the effective replica count for a server, treating any
// value <= 0 as 1 so callers can use the result directly as a loop bound.
func replicaCount(server *config.MCPServer) int {
	if server.Replicas <= 0 {
		return 1
	}
	return server.Replicas
}

// singleReplicaPlaceholder returns a one-element placeholder slice used by
// non-container server results where the orchestrator has nothing runtime-
// provisioned to carry. The registrar only reads ReplicaID from these.
func singleReplicaPlaceholder() []MCPServerReplica {
	return []MCPServerReplica{{ReplicaID: 0}}
}

// nReplicaPlaceholders returns n placeholders for non-container servers that
// still want N independent replica registrations (local process, SSH).
func nReplicaPlaceholders(n int) []MCPServerReplica {
	if n <= 1 {
		return singleReplicaPlaceholder()
	}
	out := make([]MCPServerReplica, n)
	for i := range out {
		out[i].ReplicaID = i
	}
	return out
}

func (o *Orchestrator) startMCPServer(ctx context.Context, stack *config.Stack, server *config.MCPServer, opts UpOptions, hostPort, replicaID, totalReplicas int) (*MCPServerResult, error) {
	runtimeName := ReplicaContainerName(stack.Name, server.Name, replicaID, totalReplicas)

	// Check if container already exists
	exists, workloadID, err := o.runtime.Exists(ctx, runtimeName)
	if err != nil {
		return nil, err
	}

	if exists {
		o.logger.Info("MCP server already exists, starting", "name", server.Name, "replica", replicaID)
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
			Logger:     o.logger,
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

	// Create workload config. Name drives both container name and DNS alias;
	// for multi-replica servers we suffix it so replicas don't collide. Labels
	// still carry the logical server name so Down/List filters by stack work.
	workloadName := server.Name
	if totalReplicas > 1 {
		workloadName = fmt.Sprintf("%s-replica-%d", server.Name, replicaID)
	}
	cfg := WorkloadConfig{
		Name:        workloadName,
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

	o.logger.Info("MCP server listening", "name", server.Name, "replica", replicaID, "port", actualHostPort)

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

// Helper functions that don't need Docker-specific code

func containerName(stack, name string) string {
	return "gridctl-" + stack + "-" + name
}

// ReplicaContainerName returns the container name for a specific replica.
// For single-replica servers (totalReplicas <= 1) it returns the same name
// containerName produces, so backward-compatible stacks see zero diff.
// For totalReplicas > 1 it appends a "-replica-<id>" suffix so distinct
// replicas of the same logical server never collide in the runtime.
func ReplicaContainerName(stack, name string, replicaID, totalReplicas int) string {
	base := containerName(stack, name)
	if totalReplicas <= 1 {
		return base
	}
	return fmt.Sprintf("%s-replica-%d", base, replicaID)
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

// runtimeRequiredError builds an actionable error message listing which workloads need a container runtime.
func runtimeRequiredError(stack *config.Stack, pingErr error) error {
	msg := "A container runtime is required but not available\n"

	if containerWorkloads := stack.ContainerWorkloads(); len(containerWorkloads) > 0 {
		msg += "\nThese workloads need a container runtime:\n"
		for _, w := range containerWorkloads {
			msg += w + "\n"
		}
	}

	if nonContainerWorkloads := stack.NonContainerWorkloads(); len(nonContainerWorkloads) > 0 {
		msg += "\nThese work without a container runtime:\n"
		for _, w := range nonContainerWorkloads {
			msg += w + "\n"
		}
	}

	msg += "\nInstall Docker: https://docs.docker.com/get-docker/"
	msg += "\nInstall Podman: https://podman.io/getting-started/installation"

	return fmt.Errorf("%s\n\n%w", msg, pingErr)
}
