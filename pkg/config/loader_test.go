package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTopology_Valid(t *testing.T) {
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

	topo, err := LoadTopology(path)
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

func TestLoadTopology_Defaults(t *testing.T) {
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

	topo, err := LoadTopology(path)
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

func TestLoadTopology_EnvExpansion(t *testing.T) {
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

	topo, err := LoadTopology(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if topo.MCPServers[0].Env["API_KEY"] != "secret123" {
		t.Errorf("expected env expansion 'secret123', got '%s'", topo.MCPServers[0].Env["API_KEY"])
	}
}

func TestLoadTopology_InvalidYAML(t *testing.T) {
	content := `
name: test-lab
mcp-servers:
  - invalid yaml content
    missing: colon
`
	path := writeTempFile(t, content)

	_, err := LoadTopology(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		topo    *Topology
		wantErr bool
	}{
		{
			name: "valid topology",
			topo: &Topology{
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
			topo: &Topology{
				Network: Network{Name: "test-net"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: true,
		},
		{
			name: "both image and source",
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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

func TestLoadTopology_MultiNetwork(t *testing.T) {
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

	topo, err := LoadTopology(path)
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

func TestLoadTopology_MultiNetwork_DefaultDriver(t *testing.T) {
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

	topo, err := LoadTopology(path)
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
		topo    *Topology
		wantErr bool
	}{
		{
			name: "valid multi-network",
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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
			topo: &Topology{
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

func TestValidate_HeadlessAgent(t *testing.T) {
	tests := []struct {
		name    string
		topo    *Topology
		wantErr bool
	}{
		{
			name: "valid headless agent",
			topo: &Topology{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:    "headless-agent",
						Runtime: "claude-code",
						Prompt:  "You are a helpful assistant",
						Uses:    []string{"server1"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "headless agent missing prompt",
			topo: &Topology{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:    "headless-agent",
						Runtime: "claude-code",
						Uses:    []string{"server1"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "headless agent with image",
			topo: &Topology{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:    "headless-agent",
						Runtime: "claude-code",
						Prompt:  "You are a helpful assistant",
						Image:   "some-image",
						Uses:    []string{"server1"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "headless agent with source",
			topo: &Topology{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:    "headless-agent",
						Runtime: "claude-code",
						Prompt:  "You are a helpful assistant",
						Source:  &Source{Type: "git", URL: "https://github.com/example/repo"},
						Uses:    []string{"server1"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "container agent still valid",
			topo: &Topology{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:  "container-agent",
						Image: "my-agent:latest",
						Uses:  []string{"server1"},
					},
				},
			},
			wantErr: false,
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

func TestAgent_IsHeadless(t *testing.T) {
	tests := []struct {
		name  string
		agent Agent
		want  bool
	}{
		{
			name:  "headless agent",
			agent: Agent{Name: "test", Runtime: "claude-code", Prompt: "test"},
			want:  true,
		},
		{
			name:  "container agent",
			agent: Agent{Name: "test", Image: "alpine"},
			want:  false,
		},
		{
			name:  "empty runtime",
			agent: Agent{Name: "test", Runtime: ""},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.IsHeadless(); got != tc.want {
				t.Errorf("IsHeadless() = %v, want %v", got, tc.want)
			}
		})
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}
