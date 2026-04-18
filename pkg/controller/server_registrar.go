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

// RegisterAll registers all MCP servers from the UpResult with the gateway.
func (r *ServerRegistrar) RegisterAll(ctx context.Context, result *runtime.UpResult, stack *config.Stack, stackPath string) {
	serverConfigs := make(map[string]config.MCPServer)
	for _, s := range stack.MCPServers {
		serverConfigs[s.Name] = s
	}

	for _, server := range result.MCPServers {
		serverCfg := serverConfigs[server.Name]
		cfg := r.buildServerConfig(server, serverCfg, stackPath)
		if err := r.gateway.RegisterMCPServer(ctx, cfg); err != nil {
			r.logger.Warn("failed to register MCP server", "name", server.Name, "error", err)
		}
	}
}

// RegisterOne registers a single MCP server with the gateway.
// Used by the reload handler to register newly added servers.
// containerID is the runtime container ID for any container-backed server
// (stdio or HTTP/SSE). Pass "" for External, LocalProcess, SSH, and OpenAPI —
// the container HTTP/SSE branch uses it to wire up the readiness-failure cleanup.
func (r *ServerRegistrar) RegisterOne(ctx context.Context, server config.MCPServer, hostPort int, containerID, stackPath string) error {
	cfg := r.buildConfigFromMCPServer(server, hostPort, containerID, stackPath)
	return r.gateway.RegisterMCPServer(ctx, cfg)
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
