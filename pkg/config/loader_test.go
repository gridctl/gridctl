package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStack_Valid(t *testing.T) {
	content := `
version: "1"
name: test-lab
network:
  name: test-net
  driver: bridge
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
  - name: server2
    source:
      type: git
      url: https://github.com/example/repo
    port: 3001
resources:
  - name: postgres
    image: postgres:16
    env:
      POSTGRES_PASSWORD: secret
`
	path := writeTempFile(t, content)

	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if topo.Name != "test-lab" {
		t.Errorf("expected name 'test-lab', got '%s'", topo.Name)
	}
	if len(topo.MCPServers) != 2 {
		t.Errorf("expected 2 mcp-servers, got %d", len(topo.MCPServers))
	}
	if len(topo.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(topo.Resources))
	}
}

func TestLoadStack_Defaults(t *testing.T) {
	content := `
name: test-lab
mcp-servers:
  - name: server1
    source:
      type: git
      url: https://github.com/example/repo
    port: 3000
`
	path := writeTempFile(t, content)

	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults
	if topo.Version != "1" {
		t.Errorf("expected default version '1', got '%s'", topo.Version)
	}
	if topo.Network.Driver != "bridge" {
		t.Errorf("expected default driver 'bridge', got '%s'", topo.Network.Driver)
	}
	if topo.Network.Name != "test-lab-net" {
		t.Errorf("expected default network name 'test-lab-net', got '%s'", topo.Network.Name)
	}
	if topo.MCPServers[0].Source.Dockerfile != "Dockerfile" {
		t.Errorf("expected default dockerfile 'Dockerfile', got '%s'", topo.MCPServers[0].Source.Dockerfile)
	}
	if topo.MCPServers[0].Source.Ref != "main" {
		t.Errorf("expected default ref 'main', got '%s'", topo.MCPServers[0].Source.Ref)
	}
}

func TestLoadStack_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "secret123")
	defer os.Unsetenv("TEST_API_KEY")

	content := `
name: test-lab
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      API_KEY: "${TEST_API_KEY}"
`
	path := writeTempFile(t, content)

	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if topo.MCPServers[0].Env["API_KEY"] != "secret123" {
		t.Errorf("expected env expansion 'secret123', got '%s'", topo.MCPServers[0].Env["API_KEY"])
	}
}

func TestLoadStack_EnvExpansion_CommandAndURL(t *testing.T) {
	os.Setenv("TEST_MCP_URL", "https://actions.zapier.com/mcp/sk-ak-test123/sse")
	os.Setenv("TEST_EXTERNAL_URL", "https://api.example.com/mcp")
	defer os.Unsetenv("TEST_MCP_URL")
	defer os.Unsetenv("TEST_EXTERNAL_URL")

	content := `
name: test-lab
network:
  name: test-net
mcp-servers:
  - name: zapier
    command: ["npx", "mcp-remote", "${TEST_MCP_URL}"]
  - name: external
    url: "${TEST_EXTERNAL_URL}"
    transport: http
`
	path := writeTempFile(t, content)

	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify command env expansion
	if len(topo.MCPServers[0].Command) != 3 {
		t.Fatalf("expected 3 command args, got %d", len(topo.MCPServers[0].Command))
	}
	if topo.MCPServers[0].Command[2] != "https://actions.zapier.com/mcp/sk-ak-test123/sse" {
		t.Errorf("expected command URL expansion, got '%s'", topo.MCPServers[0].Command[2])
	}

	// Verify URL env expansion
	if topo.MCPServers[1].URL != "https://api.example.com/mcp" {
		t.Errorf("expected URL expansion, got '%s'", topo.MCPServers[1].URL)
	}
}


func TestLoadStack_InvalidYAML(t *testing.T) {
	content := `
name: test-lab
mcp-servers:
  - invalid yaml content
    missing: colon
`
	path := writeTempFile(t, content)

	_, err := LoadStack(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		topo    *Stack
		wantErr bool
	}{
		{
			name: "valid stack",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			topo: &Stack{
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: true,
		},
		{
			name: "both image and source",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{
						Name:  "server1",
						Image: "alpine",
						Source: &Source{
							Type: "git",
							URL:  "https://github.com/example/repo",
						},
						Port: 3000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "neither image nor source",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{Name: "server1", Port: 3000},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid source type",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{
						Name: "server1",
						Source: &Source{
							Type: "invalid",
							URL:  "https://github.com/example/repo",
						},
						Port: 3000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate MCP server names",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
					{Name: "server1", Image: "alpine", Port: 3001},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port (zero)",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 0},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.topo)
			if tc.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// Multi-network tests

func TestLoadStack_MultiNetwork(t *testing.T) {
	content := `
version: "1"
name: multi-net-test

networks:
  - name: public-net
    driver: bridge
  - name: private-net
    driver: bridge

mcp-servers:
  - name: public-server
    image: alpine:latest
    port: 3000
    network: public-net
  - name: private-server
    image: alpine:latest
    port: 3001
    network: private-net

resources:
  - name: postgres
    image: postgres:16
    network: private-net
    env:
      POSTGRES_PASSWORD: secret
`
	path := writeTempFile(t, content)

	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(topo.Networks) != 2 {
		t.Errorf("expected 2 networks, got %d", len(topo.Networks))
	}
	if topo.MCPServers[0].Network != "public-net" {
		t.Errorf("expected server network 'public-net', got '%s'", topo.MCPServers[0].Network)
	}
	if topo.Resources[0].Network != "private-net" {
		t.Errorf("expected resource network 'private-net', got '%s'", topo.Resources[0].Network)
	}
}

func TestLoadStack_MultiNetwork_DefaultDriver(t *testing.T) {
	content := `
version: "1"
name: multi-net-test

networks:
  - name: net1
  - name: net2

mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    network: net1
`
	path := writeTempFile(t, content)

	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check default driver is applied to networks
	for i, net := range topo.Networks {
		if net.Driver != "bridge" {
			t.Errorf("network %d: expected default driver 'bridge', got '%s'", i, net.Driver)
		}
	}
}

func TestValidate_MultiNetwork(t *testing.T) {
	tests := []struct {
		name    string
		topo    *Stack
		wantErr bool
	}{
		{
			name: "valid multi-network",
			topo: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "net1", Driver: "bridge"},
					{Name: "net2", Driver: "bridge"},
				},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000, Network: "net1"},
				},
			},
			wantErr: false,
		},
		{
			name: "both network and networks",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "single-net"},
				Networks: []Network{
					{Name: "net1"},
					{Name: "net2"},
				},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000, Network: "net1"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing server network in multi-network mode",
			topo: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "net1", Driver: "bridge"},
				},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000}, // Missing network
				},
			},
			wantErr: true,
		},
		{
			name: "invalid network reference",
			topo: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "net1", Driver: "bridge"},
				},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000, Network: "nonexistent"},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate network names",
			topo: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "net1", Driver: "bridge"},
					{Name: "net1", Driver: "bridge"},
				},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000, Network: "net1"},
				},
			},
			wantErr: true,
		},
		{
			name: "simple mode ignores server network",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "single-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000, Network: "some-network"},
				},
			},
			wantErr: false, // server.Network should be ignored in simple mode
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.topo)
			if tc.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}


func TestMCPServerToolsFilter(t *testing.T) {
	content := `
version: "1"
name: test
network:
  name: test-net
mcp-servers:
  - name: filtered-server
    image: alpine
    port: 3000
    tools: ["allowed-tool1", "allowed-tool2"]
  - name: unfiltered-server
    image: alpine
    port: 3001
`
	path := writeTempFile(t, content)
	topo, err := LoadStack(path)
	if err != nil {
		t.Fatalf("LoadTopology failed: %v", err)
	}

	if len(topo.MCPServers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(topo.MCPServers))
	}

	// First server should have tools filter
	if len(topo.MCPServers[0].Tools) != 2 {
		t.Errorf("expected 2 tools in filter, got %d", len(topo.MCPServers[0].Tools))
	}
	if topo.MCPServers[0].Tools[0] != "allowed-tool1" {
		t.Errorf("expected first tool to be 'allowed-tool1', got %q", topo.MCPServers[0].Tools[0])
	}

	// Second server should have no tools filter
	if len(topo.MCPServers[1].Tools) != 0 {
		t.Errorf("expected no tools in filter, got %d", len(topo.MCPServers[1].Tools))
	}
}


func TestLoadStack_AuthConfig(t *testing.T) {
	content := `
version: "1"
name: auth-test
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    type: bearer
    token: my-secret-token
`
	path := writeTempFile(t, content)

	stack, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Gateway == nil {
		t.Fatal("expected gateway config")
	}
	if stack.Gateway.Auth == nil {
		t.Fatal("expected auth config")
	}
	if stack.Gateway.Auth.Type != "bearer" {
		t.Errorf("expected type 'bearer', got '%s'", stack.Gateway.Auth.Type)
	}
	if stack.Gateway.Auth.Token != "my-secret-token" {
		t.Errorf("expected token 'my-secret-token', got '%s'", stack.Gateway.Auth.Token)
	}
}

func TestLoadStack_AuthConfigEnvExpansion(t *testing.T) {
	t.Setenv("TEST_AUTH_TOKEN", "expanded-token")

	content := `
version: "1"
name: auth-env-test
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    type: api_key
    token: $TEST_AUTH_TOKEN
    header: X-API-Key
`
	path := writeTempFile(t, content)

	stack, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Gateway.Auth.Token != "expanded-token" {
		t.Errorf("expected expanded token, got '%s'", stack.Gateway.Auth.Token)
	}
	if stack.Gateway.Auth.Header != "X-API-Key" {
		t.Errorf("expected header 'X-API-Key', got '%s'", stack.Gateway.Auth.Header)
	}
}

func TestLoadStack_AuthValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "missing type",
			content: `
version: "1"
name: test
mcp-servers:
  - name: s1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    token: secret
`,
			wantErr: true,
		},
		{
			name: "invalid type",
			content: `
version: "1"
name: test
mcp-servers:
  - name: s1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    type: oauth
    token: secret
`,
			wantErr: true,
		},
		{
			name: "missing token",
			content: `
version: "1"
name: test
mcp-servers:
  - name: s1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    type: bearer
`,
			wantErr: true,
		},
		{
			name: "header with bearer type",
			content: `
version: "1"
name: test
mcp-servers:
  - name: s1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    type: bearer
    token: secret
    header: X-Custom
`,
			wantErr: true,
		},
		{
			name: "valid api_key with header",
			content: `
version: "1"
name: test
mcp-servers:
  - name: s1
    image: alpine:latest
    port: 3000
gateway:
  auth:
    type: api_key
    token: secret
    header: X-API-Key
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempFile(t, tt.content)
			_, err := LoadStack(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadStack() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_CodeMode(t *testing.T) {
	tests := []struct {
		name    string
		topo    *Stack
		wantErr bool
	}{
		{
			name: "valid code_mode on",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				Gateway: &GatewayConfig{CodeMode: "on"},
				MCPServers: []MCPServer{
					{Name: "s1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: false,
		},
		{
			name: "valid code_mode off",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				Gateway: &GatewayConfig{CodeMode: "off"},
				MCPServers: []MCPServer{
					{Name: "s1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid code_mode value",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net"},
				Gateway: &GatewayConfig{CodeMode: "auto"},
				MCPServers: []MCPServer{
					{Name: "s1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.topo)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadStack_NoVault_VaultRefsIgnored(t *testing.T) {
	content := `
name: test-vault
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      SECRET: "${vault:ANY_KEY}"
`
	path := writeTempFile(t, content)

	// Without WithVault, unresolved vault refs should be silently ignored
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatalf("expected no error without vault, got: %v", err)
	}
	if stack.Name != "test-vault" {
		t.Errorf("expected name 'test-vault', got %q", stack.Name)
	}
	// The raw vault reference should remain as-is
	if stack.MCPServers[0].Env["SECRET"] != "${vault:ANY_KEY}" {
		t.Errorf("expected raw vault ref preserved, got %q", stack.MCPServers[0].Env["SECRET"])
	}
}

func TestLoadStack_WithVault_MissingKeyErrors(t *testing.T) {
	vault := &mockVault{secrets: map[string]string{}}

	content := `
name: test-vault
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      SECRET: "${vault:MISSING_KEY}"
`
	path := writeTempFile(t, content)

	_, err := LoadStack(path, WithVault(vault))
	if err == nil {
		t.Fatal("expected error for missing vault key with vault provided")
	}
	if !contains(err.Error(), "missing vault secret") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}
