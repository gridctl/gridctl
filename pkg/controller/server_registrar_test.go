package controller

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestResolveTransport(t *testing.T) {
	tests := []struct {
		input string
		want  mcp.Transport
	}{
		{"sse", mcp.TransportSSE},
		{"stdio", mcp.TransportStdio},
		{"http", mcp.TransportHTTP},
		{"", mcp.TransportHTTP},
		{"unknown", mcp.TransportHTTP},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveTransport(tt.input)
			if got != tt.want {
				t.Errorf("resolveTransport(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestServerRegistrar_BuildServerConfig_External(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:     "ext-server",
		External: true,
		URL:      "https://api.example.com/mcp",
	}
	serverCfg := config.MCPServer{
		Name:      "ext-server",
		Transport: "http",
		Tools:     []string{"tool1", "tool2"},
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if cfg.Name != "ext-server" {
		t.Errorf("expected name 'ext-server', got '%s'", cfg.Name)
	}
	if !cfg.External {
		t.Error("expected External to be true")
	}
	if cfg.Endpoint != "https://api.example.com/mcp" {
		t.Errorf("expected endpoint 'https://api.example.com/mcp', got '%s'", cfg.Endpoint)
	}
	if cfg.Transport != mcp.TransportHTTP {
		t.Errorf("expected transport HTTP, got '%s'", cfg.Transport)
	}
	if len(cfg.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(cfg.Tools))
	}
}

func TestServerRegistrar_BuildServerConfig_LocalProcess(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:         "local-server",
		LocalProcess: true,
		Command:      []string{"./my-server"},
	}
	serverCfg := config.MCPServer{
		Name:    "local-server",
		Command: []string{"./my-server"},
		Env:     map[string]string{"DEBUG": "true"},
		Tools:   []string{"read"},
	}

	cfg := r.buildServerConfig(server, serverCfg, "/home/user/stacks/stack.yaml")

	if !cfg.LocalProcess {
		t.Error("expected LocalProcess to be true")
	}
	if cfg.WorkDir != "/home/user/stacks" {
		t.Errorf("expected workdir '/home/user/stacks', got '%s'", cfg.WorkDir)
	}
	if len(cfg.Command) != 1 || cfg.Command[0] != "./my-server" {
		t.Errorf("unexpected command: %v", cfg.Command)
	}
	if cfg.Env["DEBUG"] != "true" {
		t.Errorf("expected env DEBUG=true, got '%s'", cfg.Env["DEBUG"])
	}
}

func TestServerRegistrar_BuildServerConfig_SSH(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:            "ssh-server",
		SSH:             true,
		Command:         []string{"/opt/mcp/server"},
		SSHHost:         "192.168.1.50",
		SSHUser:         "mcp",
		SSHPort:         2222,
		SSHIdentityFile: "~/.ssh/id_ed25519",
	}
	serverCfg := config.MCPServer{
		Name:  "ssh-server",
		Tools: []string{"remote-tool"},
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if !cfg.SSH {
		t.Error("expected SSH to be true")
	}
	if cfg.SSHHost != "192.168.1.50" {
		t.Errorf("expected SSHHost '192.168.1.50', got '%s'", cfg.SSHHost)
	}
	if cfg.SSHUser != "mcp" {
		t.Errorf("expected SSHUser 'mcp', got '%s'", cfg.SSHUser)
	}
	if cfg.SSHPort != 2222 {
		t.Errorf("expected SSHPort 2222, got %d", cfg.SSHPort)
	}
	if cfg.SSHIdentityFile != "~/.ssh/id_ed25519" {
		t.Errorf("expected identity file '~/.ssh/id_ed25519', got '%s'", cfg.SSHIdentityFile)
	}
}

func TestServerRegistrar_BuildServerConfig_OpenAPI(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), true) // noExpand=true

	server := runtime.MCPServerResult{
		Name:    "api-server",
		OpenAPI: true,
		OpenAPIConfig: &config.OpenAPIConfig{
			Spec:    "https://api.example.com/openapi.json",
			BaseURL: "https://api.example.com",
			Operations: &config.OperationsFilter{
				Include: []string{"getUser", "listItems"},
			},
		},
	}
	serverCfg := config.MCPServer{
		Name: "api-server",
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if !cfg.OpenAPI {
		t.Error("expected OpenAPI to be true")
	}
	if cfg.OpenAPIConfig == nil {
		t.Fatal("expected OpenAPIConfig to be non-nil")
	}
	if cfg.OpenAPIConfig.Spec != "https://api.example.com/openapi.json" {
		t.Errorf("unexpected spec: %s", cfg.OpenAPIConfig.Spec)
	}
	if cfg.OpenAPIConfig.BaseURL != "https://api.example.com" {
		t.Errorf("unexpected baseURL: %s", cfg.OpenAPIConfig.BaseURL)
	}
	if !cfg.OpenAPIConfig.NoExpand {
		t.Error("expected NoExpand to be true")
	}
	if len(cfg.OpenAPIConfig.Include) != 2 {
		t.Errorf("expected 2 include operations, got %d", len(cfg.OpenAPIConfig.Include))
	}
}

func TestServerRegistrar_BuildServerConfig_ContainerStdio(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:       "stdio-server",
		WorkloadID: runtime.WorkloadID("container123"),
	}
	serverCfg := config.MCPServer{
		Name:      "stdio-server",
		Transport: "stdio",
		Tools:     []string{"exec"},
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if cfg.Transport != mcp.TransportStdio {
		t.Errorf("expected transport stdio, got '%s'", cfg.Transport)
	}
	if cfg.ContainerID != "container123" {
		t.Errorf("expected container ID 'container123', got '%s'", cfg.ContainerID)
	}
}

func TestServerRegistrar_BuildServerConfig_ContainerHTTP(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:       "http-server",
		WorkloadID: runtime.WorkloadID("container456"),
		HostPort:   9001,
	}
	serverCfg := config.MCPServer{
		Name:      "http-server",
		Transport: "http",
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if cfg.Transport != mcp.TransportHTTP {
		t.Errorf("expected transport HTTP, got '%s'", cfg.Transport)
	}
	if cfg.Endpoint != "http://localhost:9001/mcp" {
		t.Errorf("expected endpoint 'http://localhost:9001/mcp', got '%s'", cfg.Endpoint)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_External(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:      "ext",
		URL:       "https://example.com/mcp",
		Transport: "sse",
		Tools:     []string{"search"},
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "/path/stack.yaml")

	if !cfg.External {
		t.Error("expected External to be true")
	}
	if cfg.Endpoint != "https://example.com/mcp" {
		t.Errorf("unexpected endpoint: %s", cfg.Endpoint)
	}
	if cfg.Transport != mcp.TransportSSE {
		t.Errorf("expected transport SSE, got '%s'", cfg.Transport)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_LocalProcess(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:    "local",
		Command: []string{"./server", "--port", "3000"},
		Env:     map[string]string{"MODE": "dev"},
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "/home/user/stacks/stack.yaml")

	if !cfg.LocalProcess {
		t.Error("expected LocalProcess to be true")
	}
	if cfg.WorkDir != "/home/user/stacks" {
		t.Errorf("expected workdir '/home/user/stacks', got '%s'", cfg.WorkDir)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_SSH(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:    "remote",
		Command: []string{"/opt/server"},
		SSH: &config.SSHConfig{
			Host:         "10.0.0.1",
			User:         "admin",
			Port:         22,
			IdentityFile: "/path/to/key",
		},
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "/path/stack.yaml")

	if !cfg.SSH {
		t.Error("expected SSH to be true")
	}
	if cfg.SSHHost != "10.0.0.1" {
		t.Errorf("unexpected SSHHost: %s", cfg.SSHHost)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_ContainerHTTP(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:      "web-server",
		Image:     "my-server:latest",
		Port:      3000,
		Transport: "",
		Tools:     []string{"get", "post"},
	}

	cfg := r.buildConfigFromMCPServer(server, 9005, "/path/stack.yaml")

	if cfg.Transport != mcp.TransportHTTP {
		t.Errorf("expected transport HTTP, got '%s'", cfg.Transport)
	}
	if cfg.Endpoint != "http://localhost:9005/mcp" {
		t.Errorf("expected endpoint 'http://localhost:9005/mcp', got '%s'", cfg.Endpoint)
	}
	if len(cfg.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(cfg.Tools))
	}
}

func TestServerRegistrar_BuildOpenAPIConfig_WithBearerAuth(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	t.Setenv("MY_TOKEN", "secret-token-123")

	openAPICfg := &config.OpenAPIConfig{
		Spec:    "/path/to/spec.json",
		BaseURL: "https://api.example.com",
		Auth: &config.OpenAPIAuth{
			Type:     "bearer",
			TokenEnv: "MY_TOKEN",
		},
		Operations: &config.OperationsFilter{
			Exclude: []string{"deleteUser"},
		},
	}

	cfg := r.buildOpenAPIConfig("api-server", openAPICfg, []string{"getUser"})

	if !cfg.OpenAPI {
		t.Error("expected OpenAPI to be true")
	}
	if cfg.OpenAPIConfig.AuthType != "bearer" {
		t.Errorf("expected auth type 'bearer', got '%s'", cfg.OpenAPIConfig.AuthType)
	}
	if cfg.OpenAPIConfig.AuthToken != "secret-token-123" {
		t.Errorf("expected auth token 'secret-token-123', got '%s'", cfg.OpenAPIConfig.AuthToken)
	}
	if len(cfg.OpenAPIConfig.Exclude) != 1 || cfg.OpenAPIConfig.Exclude[0] != "deleteUser" {
		t.Errorf("unexpected exclude: %v", cfg.OpenAPIConfig.Exclude)
	}
	if len(cfg.Tools) != 1 || cfg.Tools[0] != "getUser" {
		t.Errorf("unexpected tools: %v", cfg.Tools)
	}
}

func TestServerRegistrar_BuildOpenAPIConfig_WithHeaderAuth(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	t.Setenv("API_KEY", "my-api-key")

	openAPICfg := &config.OpenAPIConfig{
		Spec: "https://api.example.com/spec.yaml",
		Auth: &config.OpenAPIAuth{
			Type:     "header",
			Header:   "X-API-Key",
			ValueEnv: "API_KEY",
		},
	}

	cfg := r.buildOpenAPIConfig("header-server", openAPICfg, nil)

	if cfg.OpenAPIConfig.AuthType != "header" {
		t.Errorf("expected auth type 'header', got '%s'", cfg.OpenAPIConfig.AuthType)
	}
	if cfg.OpenAPIConfig.AuthHeader != "X-API-Key" {
		t.Errorf("expected auth header 'X-API-Key', got '%s'", cfg.OpenAPIConfig.AuthHeader)
	}
	if cfg.OpenAPIConfig.AuthValue != "my-api-key" {
		t.Errorf("expected auth value 'my-api-key', got '%s'", cfg.OpenAPIConfig.AuthValue)
	}
}
