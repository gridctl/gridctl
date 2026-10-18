package reload

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestComputeDiff_EnableTransitions(t *testing.T) {
	off := false
	on := &config.Stack{MCPServers: []config.MCPServer{{Name: "s", Image: "alpine"}}}
	disabled := &config.Stack{MCPServers: []config.MCPServer{{Name: "s", Image: "alpine", Enabled: &off}}}

	flipOff := ComputeDiff(on, disabled)
	if flipOff.IsEmpty() {
		t.Fatal("true to false must not be empty")
	}
	if len(flipOff.MCPServers.Modified) != 0 || len(flipOff.MCPServers.Disabled) != 1 {
		t.Fatalf("disable classification = %+v", flipOff.MCPServers)
	}

	flipOn := ComputeDiff(disabled, on)
	if flipOn.IsEmpty() {
		t.Fatal("false to true must not be empty")
	}
	if len(flipOn.MCPServers.Added) != 0 || len(flipOn.MCPServers.Enabled) != 1 {
		t.Fatalf("enable classification = %+v", flipOn.MCPServers)
	}

	edited := &config.Stack{MCPServers: []config.MCPServer{{
		Name: "s", Image: "alpine", Enabled: &off, Env: map[string]string{"A": "1"},
	}}}
	dormant := ComputeDiff(disabled, edited)
	if dormant.IsEmpty() {
		t.Fatal("dormant env edit must not be empty")
	}
	if len(dormant.MCPServers.Modified) != 0 || len(dormant.MCPServers.Dormant) != 1 {
		t.Fatalf("dormant classification = %+v", dormant.MCPServers)
	}
}

func TestReload_DormantSwapsCurrentConfig(t *testing.T) {
	off := false
	path := writeStackFile(t, `name: parked
network:
  name: parked-net
mcp-servers:
  - name: s
    command: ["/bin/true"]
    enabled: false
    env:
      A: "2"
`)
	current := &config.Stack{
		Name:       "parked",
		Network:    config.Network{Name: "parked-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "s", Command: []string{"/bin/true"}, Enabled: &off, Env: map[string]string{"A": "1"}}},
	}
	h, _ := setupHandler(t, path, current)
	result, err := h.Reload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("reload failed: %+v", result)
	}
	if len(result.Added)+len(result.Removed)+len(result.Modified)+len(result.Enabled)+len(result.Disabled) != 0 {
		t.Fatalf("dormant reload did runtime work: %+v", result)
	}
	got := h.CurrentConfig()
	if got == nil || got.MCPServers[0].Env["A"] != "2" {
		t.Fatalf("currentCfg was not swapped: %+v", got)
	}
}

func TestReload_InitialDisabledLandsInNoList(t *testing.T) {
	path := writeStackFile(t, `name: parked
network:
  name: parked-net
mcp-servers:
  - name: off
    command: ["/bin/true"]
    enabled: false
`)
	h, _ := setupHandler(t, path, nil)
	registered := 0
	h.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []ReplicaRuntime, stackPath string) error {
		registered++
		return nil
	})
	result, err := h.Initialize(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("initialize failed: %+v", result)
	}
	if registered != 0 {
		t.Fatalf("disabled server was registered %d times", registered)
	}
	if len(result.Added)+len(result.Enabled)+len(result.Disabled) != 0 {
		t.Fatalf("initial disabled server landed in a result list: %+v", result)
	}
	cfg := h.CurrentConfig()
	if cfg == nil || len(cfg.MCPServers) != 1 || cfg.MCPServers[0].IsEnabled() {
		t.Fatalf("currentCfg = %+v", cfg)
	}
}

func TestSecurityPreflight_OverrideCannotChangeBind(t *testing.T) {
	startup := &config.Stack{
		Name: "secure",
		Gateway: &config.GatewayConfig{
			Bind:           "127.0.0.1",
			AllowedOrigins: []string{"http://localhost"},
			Auth:           &config.AuthConfig{Type: "bearer", Token: "secret"},
		},
	}
	candidate := *startup
	candidate.Gateway = &config.GatewayConfig{
		Bind:           startup.Gateway.Bind,
		AllowedOrigins: append([]string(nil), startup.Gateway.AllowedOrigins...),
		Auth:           &config.AuthConfig{Type: "bearer", Token: "secret"},
	}
	candidate.MCPServers = []config.MCPServer{{Name: "s", Command: []string{"/bin/true"}, Enabled: boolPtr(false)}}
	ov, err := config.DecodeOverride([]byte("mcp-servers:\n  s:\n    enabled: true\n"), "override.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ApplyOverride(&candidate, ov, "override.yaml"); err != nil {
		t.Fatal(err)
	}
	changed := NewSecurityPreflight(startup, "", false)(&candidate)
	if len(changed) != 0 {
		t.Fatalf("override changed security fields: %v", changed)
	}
}

func boolPtr(v bool) *bool { return &v }
