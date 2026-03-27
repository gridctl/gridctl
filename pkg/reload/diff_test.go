package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestComputeDiff_Empty(t *testing.T) {
	old := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
	}
	new := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
	}

	diff := ComputeDiff(old, new)
	if !diff.IsEmpty() {
		t.Error("expected empty diff for identical stacks")
	}
}

func TestComputeDiff_AddedMCPServer(t *testing.T) {
	old := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "image1", Port: 3000},
		},
	}
	new := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "image1", Port: 3000},
			{Name: "server2", Image: "image2", Port: 3001},
		},
	}

	diff := ComputeDiff(old, new)
	if diff.IsEmpty() {
		t.Fatal("expected non-empty diff")
	}
	if len(diff.MCPServers.Added) != 1 {
		t.Fatalf("expected 1 added server, got %d", len(diff.MCPServers.Added))
	}
	if diff.MCPServers.Added[0].Name != "server2" {
		t.Errorf("expected added server 'server2', got '%s'", diff.MCPServers.Added[0].Name)
	}
}

func TestComputeDiff_RemovedMCPServer(t *testing.T) {
	old := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "image1", Port: 3000},
			{Name: "server2", Image: "image2", Port: 3001},
		},
	}
	new := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "image1", Port: 3000},
		},
	}

	diff := ComputeDiff(old, new)
	if diff.IsEmpty() {
		t.Fatal("expected non-empty diff")
	}
	if len(diff.MCPServers.Removed) != 1 {
		t.Fatalf("expected 1 removed server, got %d", len(diff.MCPServers.Removed))
	}
	if diff.MCPServers.Removed[0].Name != "server2" {
		t.Errorf("expected removed server 'server2', got '%s'", diff.MCPServers.Removed[0].Name)
	}
}

func TestComputeDiff_ModifiedMCPServer(t *testing.T) {
	old := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "image1", Port: 3000},
		},
	}
	new := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		MCPServers: []config.MCPServer{
			{Name: "server1", Image: "image2", Port: 3000}, // Changed image
		},
	}

	diff := ComputeDiff(old, new)
	if diff.IsEmpty() {
		t.Fatal("expected non-empty diff")
	}
	if len(diff.MCPServers.Modified) != 1 {
		t.Fatalf("expected 1 modified server, got %d", len(diff.MCPServers.Modified))
	}
	if diff.MCPServers.Modified[0].Name != "server1" {
		t.Errorf("expected modified server 'server1', got '%s'", diff.MCPServers.Modified[0].Name)
	}
	if diff.MCPServers.Modified[0].Old.Image != "image1" {
		t.Errorf("expected old image 'image1', got '%s'", diff.MCPServers.Modified[0].Old.Image)
	}
	if diff.MCPServers.Modified[0].New.Image != "image2" {
		t.Errorf("expected new image 'image2', got '%s'", diff.MCPServers.Modified[0].New.Image)
	}
}

func TestComputeDiff_NetworkChanged(t *testing.T) {
	old := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
	}
	new := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net-new", // Changed network name
			Driver: "bridge",
		},
	}

	diff := ComputeDiff(old, new)
	if !diff.NetworkChanged {
		t.Error("expected NetworkChanged to be true")
	}
}


func TestComputeDiff_ResourceChanges(t *testing.T) {
	old := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		Resources: []config.Resource{
			{Name: "db", Image: "postgres:15"},
		},
	}
	new := &config.Stack{
		Name: "test",
		Network: config.Network{
			Name:   "test-net",
			Driver: "bridge",
		},
		Resources: []config.Resource{
			{Name: "db", Image: "postgres:16"}, // Changed version
		},
	}

	diff := ComputeDiff(old, new)
	if len(diff.Resources.Modified) != 1 {
		t.Fatalf("expected 1 modified resource, got %d", len(diff.Resources.Modified))
	}
	if diff.Resources.Modified[0].Name != "db" {
		t.Errorf("expected modified resource 'db', got '%s'", diff.Resources.Modified[0].Name)
	}
}

func TestMCPServerEqual_EnvChanges(t *testing.T) {
	a := config.MCPServer{
		Name:  "server1",
		Image: "image1",
		Port:  3000,
		Env:   map[string]string{"KEY": "value1"},
	}
	b := config.MCPServer{
		Name:  "server1",
		Image: "image1",
		Port:  3000,
		Env:   map[string]string{"KEY": "value2"},
	}

	if mcpServerEqual(a, b) {
		t.Error("expected servers with different env to be not equal")
	}
}

func TestMCPServerEqual_ToolsChanges(t *testing.T) {
	a := config.MCPServer{
		Name:  "server1",
		Image: "image1",
		Port:  3000,
		Tools: []string{"tool1", "tool2"},
	}
	b := config.MCPServer{
		Name:  "server1",
		Image: "image1",
		Port:  3000,
		Tools: []string{"tool1"},
	}

	if mcpServerEqual(a, b) {
		t.Error("expected servers with different tools to be not equal")
	}
}

