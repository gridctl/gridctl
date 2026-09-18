package controller

import (
	"slices"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func buildA2AConfig(server config.MCPServer) mcp.MCPServerConfig {
	cfg := mcp.MCPServerConfig{
		Name: server.Name, A2A: true, Tools: slices.Clone(server.Tools),
		OutputFormat: server.OutputFormat, PinSchemas: server.PinSchemas,
		PingTimeout: server.ResolvedPingTimeout(),
	}
	if a := server.A2A; a != nil {
		timeout := 5 * time.Minute
		if a.Timeout != "" {
			timeout, _ = time.ParseDuration(a.Timeout)
		}
		dialect := a.Dialect
		if dialect == "" {
			dialect = "auto"
		}
		cfg.A2AConfig = &mcp.A2AClientConfig{
			Card: a.Card, Endpoint: a.Endpoint, Dialect: dialect, Profile: a.Profile,
			Include: slices.Clone(a.Include), Timeout: timeout,
		}
		if a.Auth != nil {
			cfg.A2AConfig.Token = a.Auth.Token
		}
	}
	return cfg
}
