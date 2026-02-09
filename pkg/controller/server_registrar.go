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
func (r *ServerRegistrar) RegisterOne(ctx context.Context, server config.MCPServer, hostPort int, stackPath string) error {
	cfg := r.buildConfigFromMCPServer(server, hostPort, stackPath)
	return r.gateway.RegisterMCPServer(ctx, cfg)
}

// buildServerConfig constructs an MCPServerConfig from an UpResult server entry
// and its corresponding stack config. This handles all transport types:
// external, local process, SSH, OpenAPI, container stdio, and container HTTP/SSE.
func (r *ServerRegistrar) buildServerConfig(server runtime.MCPServerResult, serverCfg config.MCPServer, stackPath string) mcp.MCPServerConfig {
	transport := resolveTransport(serverCfg.Transport)

	if server.External {
		return mcp.MCPServerConfig{
			Name:      server.Name,
			Transport: transport,
			Endpoint:  server.URL,
			External:  true,
			Tools:     serverCfg.Tools,
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
		}
	}
	if server.SSH {
		return mcp.MCPServerConfig{
			Name:            server.Name,
			SSH:             true,
			Command:         server.Command,
			SSHHost:         server.SSHHost,
			SSHUser:         server.SSHUser,
			SSHPort:         server.SSHPort,
			SSHIdentityFile: server.SSHIdentityFile,
			Env:             serverCfg.Env,
			Tools:           serverCfg.Tools,
		}
	}
	if server.OpenAPI {
		return r.buildOpenAPIConfig(server.Name, server.OpenAPIConfig, serverCfg.Tools)
	}
	if transport == mcp.TransportStdio {
		return mcp.MCPServerConfig{
			Name:        server.Name,
			Transport:   transport,
			ContainerID: string(server.WorkloadID),
			Tools:       serverCfg.Tools,
		}
	}
	// Container HTTP/SSE
	return mcp.MCPServerConfig{
		Name:      server.Name,
		Transport: transport,
		Endpoint:  fmt.Sprintf("http://localhost:%d/mcp", server.HostPort),
		Tools:     serverCfg.Tools,
	}
}

// buildConfigFromMCPServer constructs an MCPServerConfig from a config.MCPServer.
// This is the single-server registration path used by the reload handler.
func (r *ServerRegistrar) buildConfigFromMCPServer(server config.MCPServer, hostPort int, stackPath string) mcp.MCPServerConfig {
	transport := resolveTransport(server.Transport)

	if server.IsExternal() {
		return mcp.MCPServerConfig{
			Name:      server.Name,
			Transport: transport,
			Endpoint:  server.URL,
			External:  true,
			Tools:     server.Tools,
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
		}
	}
	if server.IsSSH() {
		return mcp.MCPServerConfig{
			Name:            server.Name,
			SSH:             true,
			Command:         server.Command,
			SSHHost:         server.SSH.Host,
			SSHUser:         server.SSH.User,
			SSHPort:         server.SSH.Port,
			SSHIdentityFile: server.SSH.IdentityFile,
			Env:             server.Env,
			Tools:           server.Tools,
		}
	}
	if server.IsOpenAPI() {
		return r.buildOpenAPIConfig(server.Name, server.OpenAPI, server.Tools)
	}
	if transport == mcp.TransportStdio {
		// Stdio containers need container ID which requires full reload
		// Return a config that will error on registration with a clear message
		return mcp.MCPServerConfig{
			Name:      server.Name,
			Transport: transport,
			Tools:     server.Tools,
		}
	}
	// Container HTTP/SSE
	return mcp.MCPServerConfig{
		Name:      server.Name,
		Transport: transport,
		Endpoint:  fmt.Sprintf("http://localhost:%d/mcp", hostPort),
		Tools:     server.Tools,
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
		if openAPICfg.Auth.Type == "bearer" && openAPICfg.Auth.TokenEnv != "" {
			cfg.OpenAPIConfig.AuthToken = os.Getenv(openAPICfg.Auth.TokenEnv)
		} else if openAPICfg.Auth.Type == "header" {
			cfg.OpenAPIConfig.AuthHeader = openAPICfg.Auth.Header
			if openAPICfg.Auth.ValueEnv != "" {
				cfg.OpenAPIConfig.AuthValue = os.Getenv(openAPICfg.Auth.ValueEnv)
			}
		}
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
