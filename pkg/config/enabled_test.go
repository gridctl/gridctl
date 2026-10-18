package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsEnabled_DefaultTrue(t *testing.T) {
	var server *MCPServer
	if !server.IsEnabled() {
		t.Fatal("nil server is enabled")
	}
	if !(&MCPServer{}).IsEnabled() {
		t.Fatal("omitted enabled is enabled")
	}
	if !(&MCPServer{Enabled: boolPtr(true)}).IsEnabled() {
		t.Fatal("explicit true is enabled")
	}
	if (&MCPServer{Enabled: boolPtr(false)}).IsEnabled() {
		t.Fatal("explicit false is disabled")
	}
	if !(&Resource{}).IsEnabled() {
		t.Fatal("omitted resource enabled is enabled")
	}
}

func TestActiveMCPServers_SkipsDisabled(t *testing.T) {
	stack := &Stack{MCPServers: []MCPServer{
		{Name: "on"},
		{Name: "off", Enabled: boolPtr(false)},
		{Name: "also", Enabled: boolPtr(true)},
	}}
	active := stack.ActiveMCPServers()
	if len(active) != 2 || active[0].Name != "on" || active[1].Name != "also" {
		t.Fatalf("active = %+v", active)
	}
}

func TestNeedsContainerRuntime_IgnoresDisabled(t *testing.T) {
	stack := &Stack{
		MCPServers: []MCPServer{{Name: "parked", Image: "alpine:3", Enabled: boolPtr(false)}},
		Resources:  []Resource{{Name: "db", Image: "postgres", Enabled: boolPtr(false)}},
	}
	if stack.NeedsContainerRuntime() {
		t.Fatal("disabled container workloads must not demand a runtime")
	}
	if got := stack.ContainerWorkloads(); len(got) != 0 {
		t.Fatalf("workloads = %v", got)
	}
	stack.MCPServers[0].Enabled = nil
	if !stack.NeedsContainerRuntime() {
		t.Fatal("enabled container server demands a runtime")
	}
}

func TestMCPServerEqual_ExplicitEnabledTrueIsNoChange(t *testing.T) {
	base := MCPServer{Name: "s", Image: "alpine", Port: 3000}
	withTrue := base
	withTrue.Enabled = boolPtr(true)
	if !MCPServerEqual(base, withTrue) {
		t.Fatal("enabled: true must compare equal to an omitted key")
	}
	disabled := base
	disabled.Enabled = boolPtr(false)
	if MCPServerEqual(base, disabled) {
		t.Fatal("enabled: false must not compare equal")
	}
}

func TestComputePlan_EnabledFlipIsChange(t *testing.T) {
	current := &Stack{Name: "t", MCPServers: []MCPServer{{Name: "s", Image: "alpine"}}}
	proposed := &Stack{Name: "t", MCPServers: []MCPServer{{Name: "s", Image: "alpine", Enabled: boolPtr(false)}}}
	diff := ComputePlan(proposed, current)
	if !diff.HasChanges || len(diff.Items) != 1 {
		t.Fatalf("diff = %+v", diff)
	}
	item := diff.Items[0]
	if item.Action != DiffChange {
		t.Fatalf("action = %s", item.Action)
	}
	if len(item.Details) == 0 || item.Details[0] != "enabled: true → false" {
		t.Fatalf("details = %v", item.Details)
	}
}

func TestLoadStack_DisabledOnlyVarDoesNotRefuse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `name: parked
network:
  name: parked-net
mcp-servers:
  - name: off
    image: alpine:3
    port: 3000
    enabled: false
    env:
      TOKEN: "${var:MISSING}"
  - name: on
    command: ["/bin/true"]
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stack, err := LoadStack(path, WithVault(stubVault{}))
	if err != nil {
		t.Fatalf("disabled-only variable refused deploy: %v", err)
	}
	if stack.MCPServers[0].IsEnabled() {
		t.Fatal("server should stay disabled")
	}

	content = strings.Replace(content, `command: ["/bin/true"]`, "command: [\"/bin/true\"]\n    env:\n      TOKEN: \"${var:MISSING}\"", 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStack(path, WithVault(stubVault{})); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("enabled consumer must still refuse, got %v", err)
	}
}

func TestInjectSetSecrets_SkipsDisabled(t *testing.T) {
	stack := &Stack{
		Secrets: &Secrets{Sets: []SecretSetRef{{Name: "dev"}}},
		MCPServers: []MCPServer{
			{Name: "off", Enabled: boolPtr(false)},
			{Name: "on"},
		},
	}
	injectSetSecrets(stack, &stubSetVault{members: map[string]string{"TOKEN": "secret"}})
	if stack.MCPServers[0].Env != nil {
		t.Fatalf("disabled server received secrets: %v", stack.MCPServers[0].Env)
	}
	if stack.MCPServers[1].Env["TOKEN"] != "secret" {
		t.Fatalf("enabled server env = %v", stack.MCPServers[1].Env)
	}
}

type stubVault struct{}

func (stubVault) Get(string) (string, bool) { return "", false }

type stubSetVault struct{ members map[string]string }

func (s *stubSetVault) Get(string) (string, bool) { return "", false }

func (s *stubSetVault) GetSetSecrets(string) []VaultSecret {
	out := make([]VaultSecret, 0, len(s.members))
	for k, v := range s.members {
		out = append(out, VaultSecret{Key: k, Value: v})
	}
	return out
}
