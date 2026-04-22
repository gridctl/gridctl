package controller

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

// ServerRegistrar handles MCP server registration with the gateway.
// It provides a unified registration path for both bulk and single-server
// registration, eliminating the duplication between the former
// registerMCPServers() and registerSingleMCPServer() functions.
type ServerRegistrar struct {
	gateway  *mcp.Gateway
	noExpand bool
	logger   *slog.Logger
	// runtime is optional. When set, container-based HTTP/SSE registrations
	// receive a CleanupOnReadyFailure closure that removes the workload if the
	// gateway's readiness wait times out. nil disables cleanup (e.g. in tests).
	runtime runtime.WorkloadRuntime

	// basePort is the starting host port for new container replicas spawned
	// at runtime by the autoscaler. Defaults to runtime.DefaultBasePort when
	// the caller omits it; collisions with ports assigned during static
	// bring-up are avoided by the allocator's monotonic counter.
	basePort int
}

// NewServerRegistrar creates a ServerRegistrar.
func NewServerRegistrar(gateway *mcp.Gateway, noExpand bool) *ServerRegistrar {
	return &ServerRegistrar{
		gateway:  gateway,
		noExpand: noExpand,
		logger:   logging.NewDiscardLogger(),
	}
}

// SetLogger sets the logger.
func (r *ServerRegistrar) SetLogger(logger *slog.Logger) {
	if logger != nil {
		r.logger = logger
	}
}

// SetRuntime wires the workload runtime used to clean up orphan containers
// when an HTTP/SSE MCP server fails its readiness check. Without a runtime,
// readiness failures still surface as errors but the container is left running.
func (r *ServerRegistrar) SetRuntime(rt runtime.WorkloadRuntime) {
	r.runtime = rt
}

// SetBasePort sets the starting host port for dynamic container spawns by the
// autoscaler. Picks up where the initial bring-up left off.
func (r *ServerRegistrar) SetBasePort(p int) {
	r.basePort = p
}

// RegisterAll registers all MCP servers from the UpResult with the gateway.
// For each server it builds one MCPServerConfig per replica and registers
// them as a single ReplicaSet under the server's logical name. Autoscaled
// servers are routed through registerAutoscaled instead so the Spawner owns
// replica provisioning.
func (r *ServerRegistrar) RegisterAll(ctx context.Context, result *runtime.UpResult, stack *config.Stack, stackPath string) {
	serverConfigs := make(map[string]config.MCPServer)
	for _, s := range stack.MCPServers {
		serverConfigs[s.Name] = s
	}

	// Container-based autoscaled servers need a post-bring-up port allocator
	// that starts where the static containers left off.
	nextHostPort := r.basePort
	for _, s := range result.MCPServers {
		for _, rep := range s.Replicas {
			if rep.HostPort > nextHostPort {
				nextHostPort = rep.HostPort
			}
		}
	}

	for _, server := range result.MCPServers {
		serverCfg := serverConfigs[server.Name]

		if serverCfg.Autoscale != nil {
			if err := r.registerAutoscaled(ctx, serverCfg, stack, stackPath, nextHostPort); err != nil {
				r.logger.Warn("failed to register autoscaled MCP server", "name", server.Name, "error", err)
			}
			continue
		}

		cfgs := r.buildReplicaConfigs(server, serverCfg, stackPath)
		if len(cfgs) == 0 {
			r.logger.Warn("no replica configs built", "name", server.Name)
			continue
		}
		policy := serverCfg.ReplicaPolicy
		if policy == "" {
			policy = mcp.ReplicaPolicyRoundRobin
		}
		if err := r.gateway.RegisterMCPReplicaSet(ctx, server.Name, policy, cfgs); err != nil {
			r.logger.Warn("failed to register MCP server", "name", server.Name, "error", err)
		}
	}
}

// ReplicaRuntime carries the runtime handles for one replica that the reload
// handler has already provisioned. For non-container replicas, ContainerID is
// "" and HostPort may be 0.
type ReplicaRuntime struct {
	HostPort    int
	ContainerID string
}

// RegisterOne registers a single MCP server (with one or more replicas) with
// the gateway. Used by the reload handler to register newly added servers.
// replicas carries the runtime handles the caller has already provisioned —
// one entry per replica, in replica-id order. For External / LocalProcess /
// SSH / OpenAPI servers that were not container-provisioned, pass a slice of
// zero-valued ReplicaRuntime entries of the desired length (or a single-entry
// slice for the single-replica case).
func (r *ServerRegistrar) RegisterOne(ctx context.Context, server config.MCPServer, replicas []ReplicaRuntime, stackPath string) error {
	// Autoscaled servers ignore the caller's replica slice — the Spawner
	// owns provisioning. This path runs for hot-reload adds of autoscaled
	// servers; the reload handler passes placeholder runtimes it wouldn't
	// normally provision for us.
	if server.Autoscale != nil {
		return r.registerAutoscaled(ctx, server, nil, stackPath, r.basePort)
	}

	if len(replicas) == 0 {
		replicas = []ReplicaRuntime{{}}
	}
	cfgs := make([]mcp.MCPServerConfig, 0, len(replicas))
	for _, rep := range replicas {
		cfgs = append(cfgs, r.buildConfigFromMCPServer(server, rep.HostPort, rep.ContainerID, stackPath))
	}
	policy := server.ReplicaPolicy
	if policy == "" {
		policy = mcp.ReplicaPolicyRoundRobin
	}
	return r.gateway.RegisterMCPReplicaSet(ctx, server.Name, policy, cfgs)
}

// registerAutoscaled wires up a Spawner for the server transport and calls
// Gateway.RegisterAutoscaler. stack may be nil when called from the hot-reload
// path; it's only needed for container-backed servers to derive the network
// name and resolved image.
func (r *ServerRegistrar) registerAutoscaled(ctx context.Context, server config.MCPServer, stack *config.Stack, stackPath string, hostPortBase int) error {
	if server.Autoscale == nil {
		return fmt.Errorf("registerAutoscaled: %s has no autoscale block", server.Name)
	}

	template := r.buildConfigFromMCPServer(server, 0, "", stackPath)
	template.CleanupOnReadyFailure = nil // spawner's own cleanup takes over

	var spawner mcp.Spawner
	switch {
	case server.IsLocalProcess():
		spawner = NewProcessSpawner(r.gateway, template)
	case server.IsSSH():
		spawner = NewSSHSpawner(r.gateway, template)
	case server.IsContainerBased():
		var networkName, imageName string
		if stack != nil {
			networkName = stack.Network.Name
			if len(stack.Networks) > 0 && server.Network != "" {
				networkName = server.Network
			}
		}
		imageName = server.Image
		if server.Source != nil && stack != nil {
			imageName = fmt.Sprintf("gridctl-%s-%s:latest", stack.Name, server.Name)
		}
		transport := server.Transport
		if transport == "" {
			transport = "http"
		}
		spawner = NewContainerSpawner(ContainerSpawnerOptions{
			Builder:   r.gateway,
			Runtime:   r.runtime,
			Stack:     stackNameOrEmpty(stack),
			Server:    server,
			Network:   networkName,
			Image:     imageName,
			Transport: transport,
			Ports:     NewAtomicPortAllocator(hostPortBase),
			Logger:    r.logger,
			InitialID: 0,
		})
	default:
		return fmt.Errorf("autoscale not supported for %s transport", server.Name)
	}

	policy := server.ReplicaPolicy
	if policy == "" {
		policy = mcp.ReplicaPolicyRoundRobin
	}
	return r.gateway.RegisterAutoscaler(ctx, template, policy, spawner, toAutoscalePolicy(server.Autoscale))
}

func stackNameOrEmpty(s *config.Stack) string {
	if s == nil {
		return ""
	}
	return s.Name
}

// toAutoscalePolicy converts the YAML AutoscaleConfig into the runtime
// AutoscalePolicy used by the scaler.
func toAutoscalePolicy(a *config.AutoscaleConfig) mcp.AutoscalePolicy {
	return mcp.AutoscalePolicy{
		Min:            a.Min,
		Max:            a.Max,
		TargetInFlight: a.TargetInFlight,
		ScaleUpAfter:   a.ResolvedScaleUpAfter(),
		ScaleDownAfter: a.ResolvedScaleDownAfter(),
		WarmPool:       a.WarmPool,
		IdleToZero:     a.IdleToZero,
	}
}

// buildReplicaConfigs fans out an UpResult's per-replica handles into one
// MCPServerConfig per replica, reusing the existing single-server config
// builder for each.
func (r *ServerRegistrar) buildReplicaConfigs(server runtime.MCPServerResult, serverCfg config.MCPServer, stackPath string) []mcp.MCPServerConfig {
	replicas := server.Replicas
	if len(replicas) == 0 {
		// Defensive: treat as single-replica if the orchestrator didn't set it.
		replicas = []runtime.MCPServerReplica{{ReplicaID: 0, WorkloadID: server.WorkloadID, Endpoint: server.Endpoint, HostPort: server.HostPort}}
	}
	cfgs := make([]mcp.MCPServerConfig, 0, len(replicas))
	for _, rep := range replicas {
		perReplica := runtime.MCPServerResult{
			Name:            server.Name,
			WorkloadID:      rep.WorkloadID,
			Endpoint:        rep.Endpoint,
			HostPort:        rep.HostPort,
			External:        server.External,
			LocalProcess:    server.LocalProcess,
			SSH:             server.SSH,
			OpenAPI:         server.OpenAPI,
			URL:             server.URL,
			Command:         server.Command,
			SSHHost:         server.SSHHost,
			SSHUser:         server.SSHUser,
			SSHPort:         server.SSHPort,
			SSHIdentityFile: server.SSHIdentityFile,
			OpenAPIConfig:   server.OpenAPIConfig,
		}
		cfgs = append(cfgs, r.buildServerConfig(perReplica, serverCfg, stackPath))
	}
	return cfgs
}

// buildServerConfig constructs an MCPServerConfig from an UpResult server entry
// and its corresponding stack config. This handles all transport types:
// external, local process, SSH, OpenAPI, container stdio, and container HTTP/SSE.
func (r *ServerRegistrar) buildServerConfig(server runtime.MCPServerResult, serverCfg config.MCPServer, stackPath string) mcp.MCPServerConfig {
	transport := resolveTransport(serverCfg.Transport)

	if server.External {
		return mcp.MCPServerConfig{
			Name:         server.Name,
			Transport:    transport,
			Endpoint:     server.URL,
			External:     true,
			Tools:        serverCfg.Tools,
			OutputFormat: serverCfg.OutputFormat,
			PinSchemas:   serverCfg.PinSchemas,
		}
	}
	if server.LocalProcess {
		return mcp.MCPServerConfig{
			Name:         server.Name,
			LocalProcess: true,
			Command:      server.Command,
			WorkDir:      filepath.Dir(stackPath),
			Env:          serverCfg.Env,
			Tools:        serverCfg.Tools,
			OutputFormat: serverCfg.OutputFormat,
			PinSchemas:   serverCfg.PinSchemas,
		}
	}
	if server.SSH {
		cfg := mcp.MCPServerConfig{
			Name:            server.Name,
			SSH:             true,
			Command:         server.Command,
			SSHHost:         server.SSHHost,
			SSHUser:         server.SSHUser,
			SSHPort:         server.SSHPort,
			SSHIdentityFile: server.SSHIdentityFile,
			Env:             serverCfg.Env,
			Tools:           serverCfg.Tools,
			OutputFormat:    serverCfg.OutputFormat,
			PinSchemas:      serverCfg.PinSchemas,
		}
		if serverCfg.SSH != nil {
			cfg.SSHKnownHostsFile = serverCfg.SSH.KnownHostsFile
			cfg.SSHJumpHost = serverCfg.SSH.JumpHost
		}
		return cfg
	}
	if server.OpenAPI {
		cfg := r.buildOpenAPIConfig(server.Name, server.OpenAPIConfig, serverCfg.Tools)
		cfg.OutputFormat = serverCfg.OutputFormat
		cfg.PinSchemas = serverCfg.PinSchemas
		return cfg
	}
	if transport == mcp.TransportStdio {
		return mcp.MCPServerConfig{
			Name:         server.Name,
			Transport:    transport,
			ContainerID:  string(server.WorkloadID),
			Tools:        serverCfg.Tools,
			OutputFormat: serverCfg.OutputFormat,
			PinSchemas:   serverCfg.PinSchemas,
		}
	}
	// Container HTTP/SSE
	return r.buildContainerHTTPConfig(server.Name, transport, server.HostPort, serverCfg, server.WorkloadID)
}

// buildConfigFromMCPServer constructs an MCPServerConfig from a config.MCPServer.
// This is the single-server registration path used by the reload handler.
// containerID is consumed only by the stdio container branch; other branches
// ignore it.
func (r *ServerRegistrar) buildConfigFromMCPServer(server config.MCPServer, hostPort int, containerID, stackPath string) mcp.MCPServerConfig {
	transport := resolveTransport(server.Transport)

	if server.IsExternal() {
		return mcp.MCPServerConfig{
			Name:         server.Name,
			Transport:    transport,
			Endpoint:     server.URL,
			External:     true,
			Tools:        server.Tools,
			OutputFormat: server.OutputFormat,
			PinSchemas:   server.PinSchemas,
		}
	}
	if server.IsLocalProcess() {
		return mcp.MCPServerConfig{
			Name:         server.Name,
			LocalProcess: true,
			Command:      server.Command,
			WorkDir:      filepath.Dir(stackPath),
			Env:          server.Env,
			Tools:        server.Tools,
			OutputFormat: server.OutputFormat,
			PinSchemas:   server.PinSchemas,
		}
	}
	if server.IsSSH() {
		return mcp.MCPServerConfig{
			Name:               server.Name,
			SSH:                true,
			Command:            server.Command,
			SSHHost:            server.SSH.Host,
			SSHUser:            server.SSH.User,
			SSHPort:            server.SSH.Port,
			SSHIdentityFile:    server.SSH.IdentityFile,
			SSHKnownHostsFile:  server.SSH.KnownHostsFile,
			SSHJumpHost:        server.SSH.JumpHost,
			Env:                server.Env,
			Tools:              server.Tools,
			OutputFormat:       server.OutputFormat,
			PinSchemas:         server.PinSchemas,
		}
	}
	if server.IsOpenAPI() {
		cfg := r.buildOpenAPIConfig(server.Name, server.OpenAPI, server.Tools)
		cfg.OutputFormat = server.OutputFormat
		cfg.PinSchemas = server.PinSchemas
		return cfg
	}
	if transport == mcp.TransportStdio {
		return mcp.MCPServerConfig{
			Name:         server.Name,
			Transport:    transport,
			ContainerID:  containerID,
			Tools:        server.Tools,
			OutputFormat: server.OutputFormat,
			PinSchemas:   server.PinSchemas,
		}
	}
	// Container HTTP/SSE
	return r.buildContainerHTTPConfig(server.Name, transport, hostPort, server, runtime.WorkloadID(containerID))
}

// buildContainerHTTPConfig is the shared factory for container HTTP/SSE servers
// used by both the bulk-apply path and the single-server reload path. Keeps
// ReadyTimeout / CleanupOnReadyFailure propagation in one place so future fields
// only need to be added once.
func (r *ServerRegistrar) buildContainerHTTPConfig(name string, transport mcp.Transport, hostPort int, serverCfg config.MCPServer, id runtime.WorkloadID) mcp.MCPServerConfig {
	return mcp.MCPServerConfig{
		Name:                  name,
		Transport:             transport,
		Endpoint:              fmt.Sprintf("http://localhost:%d/mcp", hostPort),
		Tools:                 serverCfg.Tools,
		OutputFormat:          serverCfg.OutputFormat,
		PinSchemas:            serverCfg.PinSchemas,
		ReadyTimeout:          serverCfg.ResolvedReadyTimeout(),
		CleanupOnReadyFailure: r.cleanupClosure(name, id),
	}
}

// buildOpenAPIConfig constructs an MCPServerConfig for OpenAPI-backed servers.
func (r *ServerRegistrar) buildOpenAPIConfig(name string, openAPICfg *config.OpenAPIConfig, tools []string) mcp.MCPServerConfig {
	cfg := mcp.MCPServerConfig{
		Name:    name,
		OpenAPI: true,
		OpenAPIConfig: &mcp.OpenAPIClientConfig{
			Spec:     openAPICfg.Spec,
			BaseURL:  openAPICfg.BaseURL,
			NoExpand: r.noExpand,
		},
		Tools: tools,
	}

	if openAPICfg.Auth != nil {
		cfg.OpenAPIConfig.AuthType = openAPICfg.Auth.Type
		switch openAPICfg.Auth.Type {
		case "bearer":
			if openAPICfg.Auth.TokenEnv != "" {
				cfg.OpenAPIConfig.AuthToken = os.Getenv(openAPICfg.Auth.TokenEnv)
			}
		case "header":
			cfg.OpenAPIConfig.AuthHeader = openAPICfg.Auth.Header
			if openAPICfg.Auth.ValueEnv != "" {
				cfg.OpenAPIConfig.AuthValue = os.Getenv(openAPICfg.Auth.ValueEnv)
			}
		case "query":
			cfg.OpenAPIConfig.AuthQueryParam = openAPICfg.Auth.ParamName
			if openAPICfg.Auth.ValueEnv != "" {
				cfg.OpenAPIConfig.AuthQueryValue = os.Getenv(openAPICfg.Auth.ValueEnv)
			}
		case "oauth2":
			if openAPICfg.Auth.ClientIdEnv != "" {
				cfg.OpenAPIConfig.OAuth2ClientID = os.Getenv(openAPICfg.Auth.ClientIdEnv)
			}
			if openAPICfg.Auth.ClientSecretEnv != "" {
				cfg.OpenAPIConfig.OAuth2ClientSecret = os.Getenv(openAPICfg.Auth.ClientSecretEnv)
			}
			cfg.OpenAPIConfig.OAuth2TokenURL = openAPICfg.Auth.TokenUrl
			cfg.OpenAPIConfig.OAuth2Scopes = openAPICfg.Auth.Scopes
		case "basic":
			if openAPICfg.Auth.UsernameEnv != "" {
				cfg.OpenAPIConfig.BasicUsername = os.Getenv(openAPICfg.Auth.UsernameEnv)
			}
			if openAPICfg.Auth.PasswordEnv != "" {
				cfg.OpenAPIConfig.BasicPassword = os.Getenv(openAPICfg.Auth.PasswordEnv)
			}
		}
	}

	if openAPICfg.TLS != nil {
		cfg.OpenAPIConfig.TLSCertFile = openAPICfg.TLS.CertFile
		cfg.OpenAPIConfig.TLSKeyFile = openAPICfg.TLS.KeyFile
		cfg.OpenAPIConfig.TLSCAFile = openAPICfg.TLS.CaFile
		cfg.OpenAPIConfig.TLSInsecureSkipVerify = openAPICfg.TLS.InsecureSkipVerify
	}

	if openAPICfg.Operations != nil {
		cfg.OpenAPIConfig.Include = openAPICfg.Operations.Include
		cfg.OpenAPIConfig.Exclude = openAPICfg.Operations.Exclude
	}

	return cfg
}

// resolveTransport converts a string transport name to a typed Transport constant.
func resolveTransport(transport string) mcp.Transport {
	switch transport {
	case "sse":
		return mcp.TransportSSE
	case "stdio":
		return mcp.TransportStdio
	default:
		return mcp.TransportHTTP
	}
}

// cleanupClosure returns a function the gateway can call after a readiness
// timeout to stop and remove the container backing an HTTP/SSE MCP server.
// Returns nil when the registrar has no runtime or no workload ID — the
// gateway treats a nil callback as "leave the workload alone."
func (r *ServerRegistrar) cleanupClosure(name string, id runtime.WorkloadID) func(context.Context) error {
	if r.runtime == nil || id == "" {
		return nil
	}
	return func(ctx context.Context) error {
		if err := r.runtime.Stop(ctx, id); err != nil {
			r.logger.Warn("stop after ready-timeout failed", "name", name, "id", id, "error", err)
		}
		return r.runtime.Remove(ctx, id)
	}
}
