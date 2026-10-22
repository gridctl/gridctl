package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateToolDiscovery(t *testing.T) {
	ok := &Stack{
		Name:    "demo",
		Network: Network{Name: "net"},
		Gateway: &GatewayConfig{ToolDiscovery: &ToolDiscoveryConfig{
			Mode:         "deferred",
			AlwaysExpose: []string{"alpha__echo"},
		}},
	}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	badMode := &Stack{Name: "demo", Network: Network{Name: "net"}, Gateway: &GatewayConfig{ToolDiscovery: &ToolDiscoveryConfig{Mode: "lazy"}}}
	if err := Validate(badMode); err == nil || !strings.Contains(err.Error(), "gateway.tool_discovery.mode") {
		t.Fatalf("mode: %v", err)
	}
	badName := &Stack{Name: "demo", Network: Network{Name: "net"}, Gateway: &GatewayConfig{ToolDiscovery: &ToolDiscoveryConfig{AlwaysExpose: []string{"echo"}}}}
	if err := Validate(badName); err == nil || !strings.Contains(err.Error(), "gateway.tool_discovery.always_expose[0]") {
		t.Fatalf("prefix: %v", err)
	}
	dup := &Stack{Name: "demo", Network: Network{Name: "net"}, Gateway: &GatewayConfig{ToolDiscovery: &ToolDiscoveryConfig{AlwaysExpose: []string{"alpha__echo", "alpha__echo"}}}}
	if err := Validate(dup); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestValidateLinkDiscovery(t *testing.T) {
	var s Stack
	if err := yaml.Unmarshal([]byte("version: \"1\"\nname: demo\nnetwork:\n  name: net\nlink:\n  - client: cursor\n    discovery: deferred\n"), &s); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&s); err != nil {
		t.Fatal(err)
	}
	var bad Stack
	if err := yaml.Unmarshal([]byte("version: \"1\"\nname: demo\nnetwork:\n  name: net\nlink:\n  - client: cursor\n    discovery: lazy\n"), &bad); err != nil {
		t.Fatal(err)
	}
	err := Validate(&bad)
	if err == nil || !strings.Contains(err.Error(), "link[0].discovery") {
		t.Fatalf("link discovery: %v", err)
	}
}

func TestValidateGroupOverrideReservedDiscoveryNames(t *testing.T) {
	s := &Stack{
		Name:    "demo",
		Network: Network{Name: "net"},
		MCPServers: []MCPServer{
			{Name: "alpha", Image: "alpine", Port: 3000},
		},
		Groups: map[string]GroupConfig{
			"dev": {
				Tools: []string{"alpha__echo"},
				Overrides: map[string]GroupOverride{
					"alpha__echo": {Name: "search_tools"},
				},
			},
		},
	}
	err := Validate(s)
	if err == nil || !strings.Contains(err.Error(), "search_tools") || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("override: %v", err)
	}
	s.Groups["dev"].Overrides["alpha__echo"] = GroupOverride{Name: "call_tool"}
	err = Validate(s)
	if err == nil || !strings.Contains(err.Error(), "call_tool") {
		t.Fatalf("call_tool override: %v", err)
	}
}

func TestPlanToolDiscovery(t *testing.T) {
	current := &Stack{Name: "demo", Gateway: &GatewayConfig{}}
	proposed := &Stack{Name: "demo", Gateway: &GatewayConfig{ToolDiscovery: &ToolDiscoveryConfig{
		Mode:         "deferred",
		AlwaysExpose: []string{"alpha__echo"},
	}}}
	diff := ComputePlan(proposed, current)
	var details []string
	for _, item := range diff.Items {
		if item.Kind == "gateway" {
			details = append(details, item.Details...)
		}
	}
	joined := strings.Join(details, "\n")
	if !strings.Contains(joined, "tool_discovery.mode: full → deferred") || !strings.Contains(joined, "tool_discovery.always_expose changed") {
		t.Fatalf("plan details: %v", details)
	}
	if strings.Contains(joined, "alpha__echo") {
		t.Fatalf("plan leaked pinned names: %s", joined)
	}
}
