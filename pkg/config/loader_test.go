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

func TestValidate_HeadlessAgent(t *testing.T) {
	tests := []struct {
		name    string
		topo    *Stack
		wantErr bool
	}{
		{
			name: "valid headless agent",
			topo: &Stack{
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
						Uses:    []ToolSelector{{Server: "server1"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "headless agent missing prompt",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:    "headless-agent",
						Runtime: "claude-code",
						Uses:    []ToolSelector{{Server: "server1"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "headless agent with image",
			topo: &Stack{
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
						Uses:    []ToolSelector{{Server: "server1"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "headless agent with source",
			topo: &Stack{
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
						Uses:    []ToolSelector{{Server: "server1"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "container agent still valid",
			topo: &Stack{
				Name:    "test",
				Network: Network{Name: "test-net", Driver: "bridge"},
				MCPServers: []MCPServer{
					{Name: "server1", Image: "alpine", Port: 3000},
				},
				Agents: []Agent{
					{
						Name:  "container-agent",
						Image: "my-agent:latest",
						Uses:  []ToolSelector{{Server: "server1"}},
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

func TestToolSelectorUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []ToolSelector
	}{
		{
			name: "string format",
			content: `
version: "1"
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine
    port: 3000
agents:
  - name: agent1
    image: alpine
    uses:
      - server1
`,
			want: []ToolSelector{{Server: "server1"}},
		},
		{
			name: "object format without tools",
			content: `
version: "1"
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine
    port: 3000
agents:
  - name: agent1
    image: alpine
    uses:
      - server: server1
`,
			want: []ToolSelector{{Server: "server1"}},
		},
		{
			name: "object format with tools",
			content: `
version: "1"
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine
    port: 3000
agents:
  - name: agent1
    image: alpine
    uses:
      - server: server1
        tools: ["tool1", "tool2"]
`,
			want: []ToolSelector{{Server: "server1", Tools: []string{"tool1", "tool2"}}},
		},
		{
			name: "mixed formats",
			content: `
version: "1"
name: test
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine
    port: 3000
  - name: server2
    image: alpine
    port: 3001
agents:
  - name: agent1
    image: alpine
    uses:
      - server1
      - server: server2
        tools: ["restricted-tool"]
`,
			want: []ToolSelector{
				{Server: "server1"},
				{Server: "server2", Tools: []string{"restricted-tool"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempFile(t, tc.content)
			topo, err := LoadStack(path)
			if err != nil {
				t.Fatalf("LoadTopology failed: %v", err)
			}

			if len(topo.Agents) == 0 {
				t.Fatal("expected at least one agent")
			}

			got := topo.Agents[0].Uses
			if len(got) != len(tc.want) {
				t.Fatalf("got %d selectors, want %d", len(got), len(tc.want))
			}

			for i := range got {
				if got[i].Server != tc.want[i].Server {
					t.Errorf("selector[%d].Server = %q, want %q", i, got[i].Server, tc.want[i].Server)
				}
				if len(got[i].Tools) != len(tc.want[i].Tools) {
					t.Errorf("selector[%d] has %d tools, want %d", i, len(got[i].Tools), len(tc.want[i].Tools))
				}
				for j := range got[i].Tools {
					if got[i].Tools[j] != tc.want[i].Tools[j] {
						t.Errorf("selector[%d].Tools[%d] = %q, want %q", i, j, got[i].Tools[j], tc.want[i].Tools[j])
					}
				}
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

func TestServerNames(t *testing.T) {
	selectors := []ToolSelector{
		{Server: "server1"},
		{Server: "server2", Tools: []string{"tool1"}},
		{Server: "server3"},
	}

	names := ServerNames(selectors)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "server1" || names[1] != "server2" || names[2] != "server3" {
		t.Errorf("unexpected names: %v", names)
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
