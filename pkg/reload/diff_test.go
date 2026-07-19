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

func TestComputeDiff_AutoscalePolicyOnlyChange(t *testing.T) {
	base := config.MCPServer{
		Name:      "junos",
		Command:   []string{"python", "j.py"},
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 4, TargetInFlight: 3},
	}
	updated := base
	updated.Autoscale = &config.AutoscaleConfig{Min: 1, Max: 8, TargetInFlight: 2}

	diff := ComputeDiff(
		&config.Stack{MCPServers: []config.MCPServer{base}},
		&config.Stack{MCPServers: []config.MCPServer{updated}},
	)

	if len(diff.MCPServers.AutoscalePolicyChanges) != 1 {
		t.Fatalf("AutoscalePolicyChanges = %d, want 1", len(diff.MCPServers.AutoscalePolicyChanges))
	}
	if len(diff.MCPServers.Modified) != 0 {
		t.Errorf("Modified = %d, want 0 (policy-only should not restart)", len(diff.MCPServers.Modified))
	}
}

func TestComputeDiff_StaticToAutoscaleIsRestart(t *testing.T) {
	oldS := config.MCPServer{Name: "junos", Command: []string{"python", "j.py"}, Replicas: 3}
	newS := config.MCPServer{Name: "junos", Command: []string{"python", "j.py"},
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 4, TargetInFlight: 3}}

	diff := ComputeDiff(
		&config.Stack{MCPServers: []config.MCPServer{oldS}},
		&config.Stack{MCPServers: []config.MCPServer{newS}},
	)
	if len(diff.MCPServers.Modified) != 1 {
		t.Errorf("static->autoscale should restart: Modified = %d, want 1", len(diff.MCPServers.Modified))
	}
	if len(diff.MCPServers.AutoscalePolicyChanges) != 0 {
		t.Errorf("static->autoscale must not be a policy update: AutoscalePolicyChanges = %d, want 0",
			len(diff.MCPServers.AutoscalePolicyChanges))
	}
}

func TestComputeDiff_AutoscaleToStaticIsRestart(t *testing.T) {
	oldS := config.MCPServer{Name: "junos", Command: []string{"python", "j.py"},
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 4, TargetInFlight: 3}}
	newS := config.MCPServer{Name: "junos", Command: []string{"python", "j.py"}, Replicas: 2}

	diff := ComputeDiff(
		&config.Stack{MCPServers: []config.MCPServer{oldS}},
		&config.Stack{MCPServers: []config.MCPServer{newS}},
	)
	if len(diff.MCPServers.Modified) != 1 {
		t.Errorf("autoscale->static should restart: Modified = %d, want 1", len(diff.MCPServers.Modified))
	}
}

func TestComputeDiff_ClientsOnlyChange(t *testing.T) {
	servers := []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}}
	old := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: servers,
	}
	new := &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: servers,
		Clients: &config.ClientsConfig{
			Profiles: map[string]config.ClientProfile{
				"grok": {Servers: []string{"github"}},
			},
		},
	}

	diff := ComputeDiff(old, new)
	if !diff.ClientsChanged {
		t.Error("expected ClientsChanged to be true")
	}
	if diff.IsEmpty() {
		t.Error("expected non-empty diff for a clients-only change")
	}
	// A clients-only change must not touch containers, networks, or resources.
	if len(diff.MCPServers.Added) != 0 || len(diff.MCPServers.Removed) != 0 ||
		len(diff.MCPServers.Modified) != 0 || len(diff.MCPServers.AutoscalePolicyChanges) != 0 {
		t.Error("expected no MCP server changes for a clients-only change")
	}
	if len(diff.Resources.Added) != 0 || len(diff.Resources.Removed) != 0 ||
		len(diff.Resources.Modified) != 0 {
		t.Error("expected no resource changes for a clients-only change")
	}
	if diff.NetworkChanged {
		t.Error("expected NetworkChanged to be false for a clients-only change")
	}
}

func TestComputeDiff_ClientsIdentical(t *testing.T) {
	clients := &config.ClientsConfig{
		Default: "deny",
		Profiles: map[string]config.ClientProfile{
			"grok": {Servers: []string{"github"}},
		},
	}
	old := &config.Stack{
		Name:    "test",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		Clients: clients,
	}
	new := &config.Stack{
		Name:    "test",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		Clients: &config.ClientsConfig{
			Default: "deny",
			Profiles: map[string]config.ClientProfile{
				"grok": {Servers: []string{"github"}},
			},
		},
	}

	diff := ComputeDiff(old, new)
	if diff.ClientsChanged {
		t.Error("expected ClientsChanged to be false for identical clients blocks")
	}
	if !diff.IsEmpty() {
		t.Error("expected empty diff for stacks with identical clients blocks")
	}
}

func TestComputeDiff_ClientsNilToSet(t *testing.T) {
	old := &config.Stack{Name: "test", Network: config.Network{Name: "test-net", Driver: "bridge"}}
	new := &config.Stack{
		Name:    "test",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		Clients: &config.ClientsConfig{
			Profiles: map[string]config.ClientProfile{"grok": {Servers: []string{"github"}}},
		},
	}

	if !ComputeDiff(old, new).ClientsChanged {
		t.Error("expected adding a clients block to be detected")
	}
}

func TestComputeDiff_ClientsSetToNil(t *testing.T) {
	old := &config.Stack{
		Name:    "test",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		Clients: &config.ClientsConfig{
			Profiles: map[string]config.ClientProfile{"grok": {Servers: []string{"github"}}},
		},
	}
	new := &config.Stack{Name: "test", Network: config.Network{Name: "test-net", Driver: "bridge"}}

	if !ComputeDiff(old, new).ClientsChanged {
		t.Error("expected removing a clients block to be detected")
	}
}

func TestComputeDiff_AutoscaleWithUnrelatedChangeIsRestart(t *testing.T) {
	oldS := config.MCPServer{Name: "junos", Command: []string{"python", "j.py"},
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 4, TargetInFlight: 3}}
	newS := config.MCPServer{Name: "junos", Command: []string{"python", "j_v2.py"},
		Autoscale: &config.AutoscaleConfig{Min: 1, Max: 4, TargetInFlight: 3}}

	diff := ComputeDiff(
		&config.Stack{MCPServers: []config.MCPServer{oldS}},
		&config.Stack{MCPServers: []config.MCPServer{newS}},
	)
	if len(diff.MCPServers.Modified) != 1 {
		t.Errorf("command change mixed with autoscale should restart: Modified = %d, want 1",
			len(diff.MCPServers.Modified))
	}
}

func TestComputeDiff_ModelAttributionOnlyChange(t *testing.T) {
	servers := func(model string) []config.MCPServer {
		return []config.MCPServer{{Name: "github", Image: "image1", Port: 3000, Model: model}}
	}
	old := &config.Stack{Name: "test", MCPServers: servers("")}
	new := &config.Stack{Name: "test", MCPServers: servers("claude-opus-4-7")}

	diff := ComputeDiff(old, new)

	if !diff.ModelAttributionChanged {
		t.Error("expected ModelAttributionChanged for a model-only edit")
	}
	if diff.IsEmpty() {
		t.Error("model-only edit must mark the diff non-empty so onConfigApplied fires")
	}
	if len(diff.MCPServers.Modified) != 0 {
		t.Errorf("Modified = %d, want 0 (pricing metadata must not restart the server)",
			len(diff.MCPServers.Modified))
	}
}

func TestComputeDiff_DefaultModelChange(t *testing.T) {
	servers := []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}}
	old := &config.Stack{Name: "test", MCPServers: servers}
	new := &config.Stack{
		Name:       "test",
		MCPServers: servers,
		Gateway:    &config.GatewayConfig{DefaultModel: "claude-haiku-4-5"},
	}

	diff := ComputeDiff(old, new)

	if !diff.ModelAttributionChanged {
		t.Error("expected ModelAttributionChanged for a default_model edit")
	}
	if diff.IsEmpty() {
		t.Error("default_model edit must mark the diff non-empty")
	}
}

func TestComputeDiff_ModelAttributionUnchanged(t *testing.T) {
	servers := []config.MCPServer{{Name: "github", Image: "image1", Port: 3000, Model: "claude-opus-4-7"}}
	old := &config.Stack{Name: "test", MCPServers: servers}
	new := &config.Stack{Name: "test", MCPServers: servers}

	diff := ComputeDiff(old, new)

	if diff.ModelAttributionChanged {
		t.Error("identical model attribution must not flag a change")
	}
	if !diff.IsEmpty() {
		t.Error("identical stacks must produce an empty diff")
	}
}

func TestComputeDiff_RedundantPerServerModelIsNoOp(t *testing.T) {
	// Adding a per-server model identical to the gateway default does not
	// change the effective mapping, so the diff stays empty.
	old := &config.Stack{
		Name:       "test",
		MCPServers: []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}},
		Gateway:    &config.GatewayConfig{DefaultModel: "claude-opus-4-7"},
	}
	new := &config.Stack{
		Name:       "test",
		MCPServers: []config.MCPServer{{Name: "github", Image: "image1", Port: 3000, Model: "claude-opus-4-7"}},
		Gateway:    &config.GatewayConfig{DefaultModel: "claude-opus-4-7"},
	}

	diff := ComputeDiff(old, new)

	if diff.ModelAttributionChanged {
		t.Error("redundant per-server model must not flag a change")
	}
}

func TestComputeDiff_ClientModelsOnlyChange(t *testing.T) {
	servers := []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}}
	old := &config.Stack{Name: "test", MCPServers: servers}
	new := &config.Stack{
		Name:         "test",
		MCPServers:   servers,
		ClientModels: map[string]string{"claude-code": "claude-opus-4-7"},
	}

	diff := ComputeDiff(old, new)

	if !diff.ModelAttributionChanged {
		t.Error("expected ModelAttributionChanged for a client_models-only edit")
	}
	if diff.IsEmpty() {
		t.Error("client_models-only edit must mark the diff non-empty so onConfigApplied fires")
	}
	if len(diff.MCPServers.Modified) != 0 {
		t.Errorf("Modified = %d, want 0 (pricing metadata must not restart servers)",
			len(diff.MCPServers.Modified))
	}
	if diff.ClientsChanged {
		t.Error("client_models must not flag ClientsChanged (access stays separate)")
	}
}

func TestComputeDiff_ClientModelsUnchanged(t *testing.T) {
	servers := []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}}
	models := map[string]string{"claude-code": "claude-opus-4-7"}
	old := &config.Stack{Name: "test", MCPServers: servers, ClientModels: models}
	new := &config.Stack{Name: "test", MCPServers: servers, ClientModels: map[string]string{"claude-code": "claude-opus-4-7"}}

	diff := ComputeDiff(old, new)

	if diff.ModelAttributionChanged {
		t.Error("identical client_models must not flag a change")
	}
	if !diff.IsEmpty() {
		t.Error("identical stacks must produce an empty diff")
	}
}

func TestComputeDiff_ClientModelValueChange(t *testing.T) {
	servers := []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}}
	old := &config.Stack{Name: "test", MCPServers: servers,
		ClientModels: map[string]string{"claude-code": "claude-opus-4-7"}}
	new := &config.Stack{Name: "test", MCPServers: servers,
		ClientModels: map[string]string{"claude-code": "claude-haiku-4-5"}}

	diff := ComputeDiff(old, new)

	if !diff.ModelAttributionChanged {
		t.Error("expected ModelAttributionChanged when a client's model changes")
	}
}

func TestMCPServerEqual_AuthChanges(t *testing.T) {
	base := func(auth *config.ServerAuth) config.MCPServer {
		return config.MCPServer{
			Name: "remote",
			URL:  "https://mcp.example.com/mcp",
			Auth: auth,
		}
	}

	tests := []struct {
		name string
		a, b *config.ServerAuth
		want bool
	}{
		{"both nil", nil, nil, true},
		{"added", nil, &config.ServerAuth{Type: "bearer", Token: "t"}, false},
		{"removed", &config.ServerAuth{Type: "bearer", Token: "t"}, nil, false},
		{"same bearer", &config.ServerAuth{Type: "bearer", Token: "t"}, &config.ServerAuth{Type: "bearer", Token: "t"}, true},
		{"rotated token", &config.ServerAuth{Type: "bearer", Token: "old"}, &config.ServerAuth{Type: "bearer", Token: "new"}, false},
		{"type switch", &config.ServerAuth{Type: "bearer", Token: "t"}, &config.ServerAuth{Type: "oauth"}, false},
		{"scope change", &config.ServerAuth{Type: "oauth", Scopes: []string{"a"}}, &config.ServerAuth{Type: "oauth", Scopes: []string{"b"}}, false},
		{"client change", &config.ServerAuth{Type: "oauth", ClientID: "x"}, &config.ServerAuth{Type: "oauth", ClientID: "y"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mcpServerEqual(base(tt.a), base(tt.b)); got != tt.want {
				t.Errorf("mcpServerEqual = %v, want %v", got, tt.want)
			}
		})
	}
}
