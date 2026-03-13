package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputePlan_NoChanges(t *testing.T) {
	stack := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}

	diff := ComputePlan(stack, stack)
	assert.False(t, diff.HasChanges)
	assert.Equal(t, "No changes detected.", diff.Summary)
	assert.Empty(t, diff.Items)
}

func TestComputePlan_AddServer(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
			{Name: "s2", Image: "nginx", Port: 8080},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Len(t, diff.Items, 1)
	assert.Equal(t, DiffAdd, diff.Items[0].Action)
	assert.Equal(t, "mcp-server", diff.Items[0].Kind)
	assert.Equal(t, "s2", diff.Items[0].Name)
}

func TestComputePlan_RemoveServer(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
			{Name: "s2", Image: "nginx", Port: 8080},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Len(t, diff.Items, 1)
	assert.Equal(t, DiffRemove, diff.Items[0].Action)
	assert.Equal(t, "s2", diff.Items[0].Name)
}

func TestComputePlan_ChangeServerImage(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine:3.18", Port: 3000},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine:3.19", Port: 3000},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Len(t, diff.Items, 1)
	assert.Equal(t, DiffChange, diff.Items[0].Action)
	assert.Contains(t, diff.Items[0].Details[0], "alpine:3.18")
	assert.Contains(t, diff.Items[0].Details[0], "alpine:3.19")
}

func TestComputePlan_ChangeServerPort(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 4000},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, DiffChange, diff.Items[0].Action)
	assert.Contains(t, diff.Items[0].Details[0], "3000")
	assert.Contains(t, diff.Items[0].Details[0], "4000")
}

func TestComputePlan_AddAgent(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Agents: []Agent{
			{Name: "agent1", Image: "agent:latest"},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Len(t, diff.Items, 1)
	assert.Equal(t, DiffAdd, diff.Items[0].Action)
	assert.Equal(t, "agent", diff.Items[0].Kind)
	assert.Equal(t, "agent1", diff.Items[0].Name)
}

func TestComputePlan_AddResource(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Resources: []Resource{
			{Name: "db", Image: "postgres:16"},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, "resource", diff.Items[0].Kind)
}

func TestComputePlan_ChangeAgentUses(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Agents: []Agent{
			{Name: "a1", Image: "agent:1", Uses: []ToolSelector{{Server: "s1"}}},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Agents: []Agent{
			{Name: "a1", Image: "agent:1", Uses: []ToolSelector{{Server: "s1"}, {Server: "s2"}}},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, DiffChange, diff.Items[0].Action)
	assert.Contains(t, diff.Items[0].Details, "uses changed")
}

func TestComputePlan_ChangeEnv(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000, Env: map[string]string{"FOO": "bar"}},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000, Env: map[string]string{"FOO": "baz"}},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Contains(t, diff.Items[0].Details, "env changed")
}

func TestComputePlan_GatewayAdd(t *testing.T) {
	current := &Stack{Name: "test", Network: Network{Name: "test-net"}}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{OutputFormat: "json"},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)

	found := false
	for _, item := range diff.Items {
		if item.Kind == "gateway" && item.Action == DiffAdd {
			found = true
		}
	}
	assert.True(t, found, "expected gateway add item")
}

func TestComputePlan_GatewayChange(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{OutputFormat: "json"},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{OutputFormat: "toon"},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, "gateway", diff.Items[0].Kind)
	assert.Equal(t, DiffChange, diff.Items[0].Action)
}

func TestComputePlan_NetworkChange(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "net1", Driver: "bridge"},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "net1", Driver: "host"},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, "network", diff.Items[0].Kind)
}

func TestComputePlan_AdvancedNetworkAddRemove(t *testing.T) {
	current := &Stack{
		Name:     "test",
		Networks: []Network{{Name: "net1"}, {Name: "net2"}},
	}
	proposed := &Stack{
		Name:     "test",
		Networks: []Network{{Name: "net1"}, {Name: "net3"}},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)

	var added, removed bool
	for _, item := range diff.Items {
		if item.Kind == "network" && item.Action == DiffAdd && item.Name == "net3" {
			added = true
		}
		if item.Kind == "network" && item.Action == DiffRemove && item.Name == "net2" {
			removed = true
		}
	}
	assert.True(t, added, "expected net3 added")
	assert.True(t, removed, "expected net2 removed")
}

func TestComputePlan_A2AAgentChange(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		A2AAgents: []A2AAgent{
			{Name: "remote1", URL: "http://old.example.com"},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		A2AAgents: []A2AAgent{
			{Name: "remote1", URL: "http://new.example.com"},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, "a2a-agent", diff.Items[0].Kind)
	assert.Equal(t, DiffChange, diff.Items[0].Action)
}

func TestComputePlan_MultipleChanges(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
			{Name: "s2", Image: "nginx", Port: 8080},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine:3.19", Port: 3000}, // changed
			// s2 removed
			{Name: "s3", Image: "redis", Port: 6379}, // added
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Len(t, diff.Items, 3)
	assert.Contains(t, diff.Summary, "1 to add")
	assert.Contains(t, diff.Summary, "1 to change")
	assert.Contains(t, diff.Summary, "1 to remove")
}

func TestComputePlan_SourceChange(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Source: &Source{Type: "git", URL: "https://github.com/foo/bar", Ref: "main"}, Port: 3000},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", Source: &Source{Type: "git", URL: "https://github.com/foo/bar", Ref: "v2.0"}, Port: 3000},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Contains(t, diff.Items[0].Details, "source changed")
}

func TestComputePlan_SSHChange(t *testing.T) {
	current := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", SSH: &SSHConfig{Host: "old.host", User: "root", Port: 22}, Command: []string{"cmd"}},
		},
	}
	proposed := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "s1", SSH: &SSHConfig{Host: "new.host", User: "root", Port: 22}, Command: []string{"cmd"}},
		},
	}

	diff := ComputePlan(proposed, current)
	assert.True(t, diff.HasChanges)
	assert.Contains(t, diff.Items[0].Details, "ssh config changed")
}

func TestBuildSummary(t *testing.T) {
	tests := []struct {
		name     string
		items    []DiffItem
		expected string
	}{
		{"empty", nil, "No changes detected."},
		{"add only", []DiffItem{{Action: DiffAdd}}, "1 to add"},
		{"mixed", []DiffItem{{Action: DiffAdd}, {Action: DiffRemove}, {Action: DiffChange}}, "1 to add, 1 to change, 1 to remove"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, buildSummary(tc.items))
		})
	}
}

func TestEnvEqual(t *testing.T) {
	assert.True(t, envEqual(nil, nil))
	assert.True(t, envEqual(map[string]string{"a": "1"}, map[string]string{"a": "1"}))
	assert.False(t, envEqual(map[string]string{"a": "1"}, map[string]string{"a": "2"}))
	assert.False(t, envEqual(map[string]string{"a": "1"}, map[string]string{"b": "1"}))
	assert.False(t, envEqual(map[string]string{"a": "1"}, nil))
}

func TestStringSliceEqual(t *testing.T) {
	assert.True(t, stringSliceEqual(nil, nil))
	assert.True(t, stringSliceEqual([]string{"a"}, []string{"a"}))
	assert.False(t, stringSliceEqual([]string{"a"}, []string{"b"}))
	assert.False(t, stringSliceEqual([]string{"a"}, []string{"a", "b"}))
}

func TestUsesEqual(t *testing.T) {
	a := []ToolSelector{{Server: "s1", Tools: []string{"t1"}}}
	b := []ToolSelector{{Server: "s1", Tools: []string{"t1"}}}
	assert.True(t, usesEqual(a, b))

	c := []ToolSelector{{Server: "s1", Tools: []string{"t2"}}}
	assert.False(t, usesEqual(a, c))

	d := []ToolSelector{{Server: "s1"}, {Server: "s2"}}
	assert.False(t, usesEqual(a, d))
}
