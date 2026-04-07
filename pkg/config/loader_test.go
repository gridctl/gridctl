package config

import (
	"fmt"
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

// writeFile writes content to a specific path (for multi-file extends tests).
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func TestLoadStack_Extends_BasicInheritance(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: base-net
mcp-servers:
  - name: auth-server
    url: https://auth.internal/mcp
  - name: logging
    image: myorg/mcp-logging:latest
    port: 8080
`)
	writeFile(t, filepath.Join(dir, "dev.yaml"), `
version: "1"
name: dev
extends: ./base.yaml
mcp-servers:
  - name: logging
    image: myorg/mcp-logging:dev
    port: 8080
  - name: github
    image: ghcr.io/github/mcp:latest
    port: 9000
`)

	stack, err := LoadStack(filepath.Join(dir, "dev.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Name != "dev" {
		t.Errorf("expected name 'dev', got %q", stack.Name)
	}
	// Child servers first, then parent-only servers appended
	if len(stack.MCPServers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(stack.MCPServers))
	}
	if stack.MCPServers[0].Name != "logging" || stack.MCPServers[0].Image != "myorg/mcp-logging:dev" {
		t.Errorf("expected child override of logging, got %+v", stack.MCPServers[0])
	}
	if stack.MCPServers[1].Name != "github" {
		t.Errorf("expected github at index 1, got %q", stack.MCPServers[1].Name)
	}
	if stack.MCPServers[2].Name != "auth-server" {
		t.Errorf("expected auth-server (inherited) at index 2, got %q", stack.MCPServers[2].Name)
	}
	// Extends directive consumed
	if stack.Extends != "" {
		t.Errorf("expected extends cleared, got %q", stack.Extends)
	}
}

func TestLoadStack_Extends_GatewayInheritance(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: base-net
gateway:
  auth:
    type: bearer
    token: secret-token
mcp-servers:
  - name: server1
    url: https://api.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./base.yaml
mcp-servers:
  - name: server2
    url: https://api2.example.com/mcp
`)

	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Child should inherit parent gateway
	if stack.Gateway == nil || stack.Gateway.Auth == nil {
		t.Fatal("expected inherited gateway with auth")
	}
	if stack.Gateway.Auth.Token != "secret-token" {
		t.Errorf("expected inherited token, got %q", stack.Gateway.Auth.Token)
	}
	// Child's own servers come first, parent-only appended
	if len(stack.MCPServers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(stack.MCPServers))
	}
	if stack.MCPServers[0].Name != "server2" {
		t.Errorf("expected child server first, got %q", stack.MCPServers[0].Name)
	}
}

func TestLoadStack_Extends_GatewayOverride(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: base-net
gateway:
  auth:
    type: bearer
    token: parent-token
mcp-servers:
  - name: server1
    url: https://api.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./base.yaml
gateway:
  auth:
    type: bearer
    token: child-token
mcp-servers:
  - name: server2
    url: https://api2.example.com/mcp
`)

	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Child's gateway wins
	if stack.Gateway.Auth.Token != "child-token" {
		t.Errorf("expected child token, got %q", stack.Gateway.Auth.Token)
	}
}

func TestLoadStack_Extends_NetworkInheritance(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: shared-net
  driver: bridge
mcp-servers:
  - name: server1
    url: https://api.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./base.yaml
mcp-servers:
  - name: server2
    url: https://api2.example.com/mcp
`)

	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Network.Name != "shared-net" {
		t.Errorf("expected inherited network 'shared-net', got %q", stack.Network.Name)
	}
}

func TestLoadStack_Extends_MultiLevel(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "grandparent.yaml"), `
version: "1"
name: grandparent
network:
  name: gp-net
mcp-servers:
  - name: gp-server
    url: https://gp.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "parent.yaml"), `
version: "1"
name: parent
extends: ./grandparent.yaml
mcp-servers:
  - name: parent-server
    url: https://parent.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./parent.yaml
mcp-servers:
  - name: child-server
    url: https://child.example.com/mcp
`)

	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stack.MCPServers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(stack.MCPServers))
	}
	names := map[string]bool{}
	for _, s := range stack.MCPServers {
		names[s.Name] = true
	}
	for _, want := range []string{"child-server", "parent-server", "gp-server"} {
		if !names[want] {
			t.Errorf("expected server %q in merged stack", want)
		}
	}
}

func TestLoadStack_Extends_CircularDependency(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "a.yaml"), `
version: "1"
name: a
extends: ./b.yaml
mcp-servers:
  - name: server-a
    url: https://a.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "b.yaml"), `
version: "1"
name: b
extends: ./a.yaml
mcp-servers:
  - name: server-b
    url: https://b.example.com/mcp
`)

	_, err := LoadStack(filepath.Join(dir, "a.yaml"))
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
	if !contains(err.Error(), "circular dependency") {
		t.Errorf("expected 'circular dependency' in error, got: %v", err)
	}
}

func TestLoadStack_Extends_MissingParent(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./nonexistent.yaml
mcp-servers:
  - name: server1
    url: https://api.example.com/mcp
`)

	_, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
	if !contains(err.Error(), "nonexistent.yaml") {
		t.Errorf("expected missing file path in error, got: %v", err)
	}
}

func TestLoadStack_Extends_DepthLimit(t *testing.T) {
	dir := t.TempDir()

	// Create a chain of 12 files (exceeds maxExtendsDepth=10)
	for i := 11; i >= 0; i-- {
		var content string
		if i == 11 {
			// Leaf: no extends
			content = `
version: "1"
name: leaf
network:
  name: leaf-net
mcp-servers:
  - name: leaf-server
    url: https://leaf.example.com/mcp
`
		} else {
			content = fmt.Sprintf(`
version: "1"
name: level-%d
extends: ./level-%d.yaml
mcp-servers:
  - name: server-%d
    url: https://level%d.example.com/mcp
`, i, i+1, i, i)
		}
		name := fmt.Sprintf("level-%d.yaml", i)
		if i == 11 {
			name = "level-11.yaml"
		}
		writeFile(t, filepath.Join(dir, name), content)
	}

	_, err := LoadStack(filepath.Join(dir, "level-0.yaml"))
	if err == nil {
		t.Fatal("expected error for depth limit exceeded")
	}
	if !contains(err.Error(), "maximum inheritance depth") {
		t.Errorf("expected 'maximum inheritance depth' in error, got: %v", err)
	}
}

func TestLoadStack_Extends_ExtendsFieldCleared(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: base-net
mcp-servers:
  - name: base-server
    url: https://base.example.com/mcp
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./base.yaml
mcp-servers:
  - name: child-server
    url: https://child.example.com/mcp
`)

	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.Extends != "" {
		t.Errorf("expected extends field cleared after load, got %q", stack.Extends)
	}
}

func TestLoadStack_Extends_ResourceInheritance(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "base.yaml"), `
version: "1"
name: base
network:
  name: base-net
mcp-servers:
  - name: server1
    url: https://api.example.com/mcp
resources:
  - name: postgres
    image: postgres:16
    env:
      POSTGRES_PASSWORD: secret
`)
	writeFile(t, filepath.Join(dir, "child.yaml"), `
version: "1"
name: child
extends: ./base.yaml
mcp-servers:
  - name: server2
    url: https://api2.example.com/mcp
`)

	stack, err := LoadStack(filepath.Join(dir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(stack.Resources) != 1 || stack.Resources[0].Name != "postgres" {
		t.Errorf("expected inherited postgres resource, got %v", stack.Resources)
	}
}

func TestLoadStack_Extends_CrossDirectory_LocalSource(t *testing.T) {
	tmpRoot := t.TempDir()
	parentsDir := filepath.Join(tmpRoot, "parents")
	childrenDir := filepath.Join(tmpRoot, "children")

	// Create directories (writeFile does not create intermediate dirs)
	if err := os.MkdirAll(filepath.Join(parentsDir, "src", "server"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(childrenDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(parentsDir, "base.yaml"), `
version: "1"
name: base
mcp-servers:
  - name: my-server
    source:
      type: local
      path: ./src/server
    port: 3000
`)
	writeFile(t, filepath.Join(childrenDir, "child.yaml"), `
version: "1"
name: child
extends: ../parents/base.yaml
`)

	stack, err := LoadStack(filepath.Join(childrenDir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inherited *MCPServer
	for i := range stack.MCPServers {
		if stack.MCPServers[i].Name == "my-server" {
			inherited = &stack.MCPServers[i]
		}
	}
	if inherited == nil {
		t.Fatal("inherited server 'my-server' not found")
	}

	want := filepath.Join(parentsDir, "src", "server")
	if inherited.Source.Path != want {
		t.Errorf("inherited source.path resolved against wrong directory:\n  got  %q\n  want %q", inherited.Source.Path, want)
	}
}

func TestLoadStack_Extends_CrossDirectory_SSHIdentityFile(t *testing.T) {
	tmpRoot := t.TempDir()
	parentsDir := filepath.Join(tmpRoot, "parents")
	childrenDir := filepath.Join(tmpRoot, "children")

	if err := os.MkdirAll(parentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(childrenDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(parentsDir, "base.yaml"), `
version: "1"
name: base
mcp-servers:
  - name: remote-server
    ssh:
      host: example.com
      user: git
      identityFile: ./keys/id_rsa
    command: [mcp-server]
`)
	writeFile(t, filepath.Join(childrenDir, "child.yaml"), `
version: "1"
name: child
extends: ../parents/base.yaml
`)

	stack, err := LoadStack(filepath.Join(childrenDir, "child.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var inherited *MCPServer
	for i := range stack.MCPServers {
		if stack.MCPServers[i].Name == "remote-server" {
			inherited = &stack.MCPServers[i]
		}
	}
	if inherited == nil {
		t.Fatal("inherited server 'remote-server' not found")
	}

	want := filepath.Join(parentsDir, "keys", "id_rsa")
	if inherited.SSH.IdentityFile != want {
		t.Errorf("inherited ssh.identityFile resolved against wrong directory:\n  got  %q\n  want %q", inherited.SSH.IdentityFile, want)
	}
}
