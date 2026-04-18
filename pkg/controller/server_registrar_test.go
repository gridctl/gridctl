package controller

import (
	"context"
	"log/slog"
	"testing"
	"time"

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

	cfg := r.buildConfigFromMCPServer(server, 0, "", "/path/stack.yaml")

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

	cfg := r.buildConfigFromMCPServer(server, 0, "", "/home/user/stacks/stack.yaml")

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

	cfg := r.buildConfigFromMCPServer(server, 0, "", "/path/stack.yaml")

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

	cfg := r.buildConfigFromMCPServer(server, 9005, "", "/path/stack.yaml")

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

func TestServerRegistrar_SetLogger(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)
	// Should not panic with nil
	r.SetLogger(nil)
	// Should not panic with valid logger
	r.SetLogger(slog.Default())
}

func TestServerRegistrar_BuildConfigFromMCPServer_Stdio(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:      "stdio-server",
		Image:     "my-image:latest",
		Transport: "stdio",
		Tools:     []string{"exec"},
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "container-abc", "/path/stack.yaml")

	if cfg.Transport != mcp.TransportStdio {
		t.Errorf("expected transport stdio, got '%s'", cfg.Transport)
	}
	if cfg.ContainerID != "container-abc" {
		t.Errorf("expected container ID 'container-abc', got '%s'", cfg.ContainerID)
	}
	if len(cfg.Tools) != 1 || cfg.Tools[0] != "exec" {
		t.Errorf("unexpected tools: %v", cfg.Tools)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_StdioWithoutContainerID(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:      "stdio-server",
		Image:     "my-image:latest",
		Transport: "stdio",
	}

	// Passing an empty container ID builds the config but the resulting
	// registration will fail downstream in gateway.RegisterMCPServer — this is
	// an intentional fail-fast rather than a silent success.
	cfg := r.buildConfigFromMCPServer(server, 0, "", "/path/stack.yaml")

	if cfg.Transport != mcp.TransportStdio {
		t.Errorf("expected transport stdio, got '%s'", cfg.Transport)
	}
	if cfg.ContainerID != "" {
		t.Errorf("expected empty container ID, got '%s'", cfg.ContainerID)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_OpenAPI(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), true) // noExpand=true

	server := config.MCPServer{
		Name: "api-server",
		OpenAPI: &config.OpenAPIConfig{
			Spec:    "https://api.example.com/openapi.json",
			BaseURL: "https://api.example.com",
		},
		Tools: []string{"getUser"},
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "", "/path/stack.yaml")

	if !cfg.OpenAPI {
		t.Error("expected OpenAPI to be true")
	}
	if cfg.OpenAPIConfig == nil {
		t.Fatal("expected non-nil OpenAPIConfig")
	}
	if cfg.OpenAPIConfig.Spec != "https://api.example.com/openapi.json" {
		t.Errorf("unexpected spec: %s", cfg.OpenAPIConfig.Spec)
	}
	if !cfg.OpenAPIConfig.NoExpand {
		t.Error("expected NoExpand to be true")
	}
}

func TestServerRegistrar_BuildOpenAPIConfig_NoAuth(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	openAPICfg := &config.OpenAPIConfig{
		Spec: "/path/to/spec.json",
	}

	cfg := r.buildOpenAPIConfig("simple-api", openAPICfg, nil)

	if !cfg.OpenAPI {
		t.Error("expected OpenAPI to be true")
	}
	if cfg.OpenAPIConfig.AuthType != "" {
		t.Errorf("expected empty auth type, got '%s'", cfg.OpenAPIConfig.AuthType)
	}
}

func TestServerRegistrar_BuildOpenAPIConfig_NoOperations(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	openAPICfg := &config.OpenAPIConfig{
		Spec: "/spec.json",
	}

	cfg := r.buildOpenAPIConfig("api", openAPICfg, nil)

	if cfg.OpenAPIConfig.Include != nil {
		t.Error("expected nil Include")
	}
	if cfg.OpenAPIConfig.Exclude != nil {
		t.Error("expected nil Exclude")
	}
}

func TestServerRegistrar_BuildServerConfig_SSE(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:       "sse-server",
		WorkloadID: runtime.WorkloadID("container789"),
		HostPort:   9002,
	}
	serverCfg := config.MCPServer{
		Name:      "sse-server",
		Transport: "sse",
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if cfg.Transport != mcp.TransportSSE {
		t.Errorf("expected transport SSE, got '%s'", cfg.Transport)
	}
	if cfg.Endpoint != "http://localhost:9002/mcp" {
		t.Errorf("expected endpoint 'http://localhost:9002/mcp', got '%s'", cfg.Endpoint)
	}
}

func TestServerRegistrar_BuildServerConfig_OutputFormat(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:     "ext-server",
		External: true,
		URL:      "https://api.example.com/mcp",
	}
	serverCfg := config.MCPServer{
		Name:         "ext-server",
		Transport:    "http",
		OutputFormat: "toon",
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/to/stack.yaml")

	if cfg.OutputFormat != "toon" {
		t.Errorf("expected OutputFormat 'toon', got '%s'", cfg.OutputFormat)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_OutputFormat(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:         "ext",
		URL:          "https://example.com/mcp",
		OutputFormat: "csv",
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "", "/path/stack.yaml")

	if cfg.OutputFormat != "csv" {
		t.Errorf("expected OutputFormat 'csv', got '%s'", cfg.OutputFormat)
	}
}

func TestServerRegistrar_BuildServerConfig_OutputFormat_AllTransports(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	tests := []struct {
		name   string
		server runtime.MCPServerResult
		cfg    config.MCPServer
	}{
		{
			"local-process",
			runtime.MCPServerResult{Name: "local", LocalProcess: true, Command: []string{"./server"}},
			config.MCPServer{Name: "local", Command: []string{"./server"}, OutputFormat: "toon"},
		},
		{
			"ssh",
			runtime.MCPServerResult{Name: "ssh", SSH: true, Command: []string{"/opt/server"}, SSHHost: "host", SSHUser: "user"},
			config.MCPServer{Name: "ssh", OutputFormat: "csv"},
		},
		{
			"stdio",
			runtime.MCPServerResult{Name: "stdio", WorkloadID: runtime.WorkloadID("c1")},
			config.MCPServer{Name: "stdio", Transport: "stdio", OutputFormat: "toon"},
		},
		{
			"http",
			runtime.MCPServerResult{Name: "http", HostPort: 9001},
			config.MCPServer{Name: "http", Transport: "http", OutputFormat: "csv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := r.buildServerConfig(tt.server, tt.cfg, "/path/stack.yaml")
			if cfg.OutputFormat != tt.cfg.OutputFormat {
				t.Errorf("expected OutputFormat '%s', got '%s'", tt.cfg.OutputFormat, cfg.OutputFormat)
			}
		})
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_SSE(t *testing.T) {
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := config.MCPServer{
		Name:      "sse-ext",
		URL:       "https://example.com/events",
		Transport: "sse",
	}

	cfg := r.buildConfigFromMCPServer(server, 0, "", "/path/stack.yaml")

	if !cfg.External {
		t.Error("expected External to be true")
	}
	if cfg.Transport != mcp.TransportSSE {
		t.Errorf("expected transport SSE, got '%s'", cfg.Transport)
	}
}

func TestServerRegistrar_RegisterAll_WithExternalServer(t *testing.T) {
	gw := mcp.NewGateway()
	r := NewServerRegistrar(gw, false)
	r.SetLogger(slog.Default())

	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{
				Name: "ext-server",
				URL:  "http://127.0.0.1:1/nonexistent",
			},
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{
				Name:     "ext-server",
				External: true,
				URL:      "http://127.0.0.1:1/nonexistent",
			},
		},
	}

	// Use a cancelled context so the RegisterMCPServer call fails fast
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should not panic — logs warning on failure
	r.RegisterAll(ctx, result, stack, "/path/stack.yaml")
}

func TestServerRegistrar_RegisterOne_External(t *testing.T) {
	gw := mcp.NewGateway()
	r := NewServerRegistrar(gw, false)
	r.SetLogger(slog.Default())

	server := config.MCPServer{
		Name: "ext",
		URL:  "http://127.0.0.1:1/nonexistent",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Will fail to connect but exercises the code path
	err := r.RegisterOne(ctx, server, []ReplicaRuntime{{}}, "/path/stack.yaml")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// recordingRuntime records Stop/Remove calls so tests can assert cleanup behavior
// without spinning up a real Docker daemon.
type recordingRuntime struct {
	runtime.WorkloadRuntime // embed for unused methods; nil-panic signals unexpected calls
	stopCalls   []runtime.WorkloadID
	removeCalls []runtime.WorkloadID
}

func (r *recordingRuntime) Stop(_ context.Context, id runtime.WorkloadID) error {
	r.stopCalls = append(r.stopCalls, id)
	return nil
}

func (r *recordingRuntime) Remove(_ context.Context, id runtime.WorkloadID) error {
	r.removeCalls = append(r.removeCalls, id)
	return nil
}

func TestServerRegistrar_BuildServerConfig_ContainerHTTP_PopulatesReadyTimeoutAndCleanup(t *testing.T) {
	rec := &recordingRuntime{}
	r := NewServerRegistrar(mcp.NewGateway(), false)
	r.SetRuntime(rec)

	server := runtime.MCPServerResult{
		Name:       "slow-http",
		WorkloadID: runtime.WorkloadID("container-slow"),
		HostPort:   9100,
	}
	serverCfg := config.MCPServer{
		Name:         "slow-http",
		Transport:    "http",
		ReadyTimeout: "90s",
	}

	cfg := r.buildServerConfig(server, serverCfg, "/path/stack.yaml")

	if cfg.ReadyTimeout != 90*time.Second {
		t.Errorf("expected ReadyTimeout 90s, got %v", cfg.ReadyTimeout)
	}
	if cfg.CleanupOnReadyFailure == nil {
		t.Fatal("expected CleanupOnReadyFailure to be populated for container HTTP")
	}
	if err := cfg.CleanupOnReadyFailure(context.Background()); err != nil {
		t.Fatalf("cleanup closure returned error: %v", err)
	}
	if len(rec.stopCalls) != 1 || rec.stopCalls[0] != "container-slow" {
		t.Errorf("expected one Stop(container-slow), got %v", rec.stopCalls)
	}
	if len(rec.removeCalls) != 1 || rec.removeCalls[0] != "container-slow" {
		t.Errorf("expected one Remove(container-slow), got %v", rec.removeCalls)
	}
}

func TestServerRegistrar_BuildServerConfig_ContainerStdio_NoCleanup(t *testing.T) {
	// Stdio containers attach immediately and never call waitForHTTPServer, so
	// populating CleanupOnReadyFailure would be dead code — verify we skip it.
	r := NewServerRegistrar(mcp.NewGateway(), false)
	r.SetRuntime(&recordingRuntime{})

	server := runtime.MCPServerResult{
		Name:       "stdio",
		WorkloadID: runtime.WorkloadID("c"),
	}
	serverCfg := config.MCPServer{Name: "stdio", Transport: "stdio"}

	cfg := r.buildServerConfig(server, serverCfg, "/path/stack.yaml")
	if cfg.CleanupOnReadyFailure != nil {
		t.Error("stdio transport should not carry a cleanup callback")
	}
	if cfg.ReadyTimeout != 0 {
		t.Errorf("stdio transport should not carry a ready timeout, got %v", cfg.ReadyTimeout)
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_ContainerHTTP_PopulatesCleanup(t *testing.T) {
	rec := &recordingRuntime{}
	r := NewServerRegistrar(mcp.NewGateway(), false)
	r.SetRuntime(rec)

	server := config.MCPServer{
		Name:         "reload-http",
		Image:        "example:latest",
		Port:         3000,
		Transport:    "http",
		ReadyTimeout: "2m",
	}

	cfg := r.buildConfigFromMCPServer(server, 9200, "container-reload", "/path/stack.yaml")

	if cfg.ReadyTimeout != 2*time.Minute {
		t.Errorf("expected ReadyTimeout 2m, got %v", cfg.ReadyTimeout)
	}
	if cfg.CleanupOnReadyFailure == nil {
		t.Fatal("expected CleanupOnReadyFailure to be populated for reload path")
	}
	_ = cfg.CleanupOnReadyFailure(context.Background())
	if len(rec.removeCalls) != 1 || rec.removeCalls[0] != "container-reload" {
		t.Errorf("expected Remove(container-reload), got %v", rec.removeCalls)
	}
}

func TestServerRegistrar_BuildServerConfig_ContainerHTTP_NoCleanupWithoutRuntime(t *testing.T) {
	// Tests and CI paths often construct a registrar without a runtime.
	// The gateway treats a nil callback as "leave the container alone," so
	// the closure must be nil when no runtime was wired.
	r := NewServerRegistrar(mcp.NewGateway(), false)

	server := runtime.MCPServerResult{
		Name:       "http",
		WorkloadID: runtime.WorkloadID("c1"),
		HostPort:   9101,
	}
	cfg := r.buildServerConfig(server, config.MCPServer{Name: "http"}, "/path/stack.yaml")

	if cfg.CleanupOnReadyFailure != nil {
		t.Error("cleanup closure must be nil when no runtime is wired")
	}
}

func TestServerRegistrar_BuildConfigFromMCPServer_ContainerHTTP_NoCleanupWithoutWorkloadID(t *testing.T) {
	// The reload path passes an empty containerID for non-container transports.
	// Ensure we don't construct a closure that would call Stop("") and panic.
	rec := &recordingRuntime{}
	r := NewServerRegistrar(mcp.NewGateway(), false)
	r.SetRuntime(rec)

	server := config.MCPServer{
		Name:      "no-id",
		Image:     "example:latest",
		Port:      3000,
		Transport: "http",
	}

	cfg := r.buildConfigFromMCPServer(server, 9201, "", "/path/stack.yaml")
	if cfg.CleanupOnReadyFailure != nil {
		t.Error("cleanup closure must be nil without a workload id")
	}
}
