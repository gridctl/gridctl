package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestComputeDiff_ToolDiscovery(t *testing.T) {
	old := &config.Stack{Name: "demo", Gateway: &config.GatewayConfig{}}
	same := &config.Stack{Name: "demo", Gateway: &config.GatewayConfig{}}
	if diff := ComputeDiff(old, same); diff.ToolDiscoveryChanged || !diff.IsEmpty() {
		t.Fatalf("identical stacks: %+v", diff)
	}
	changed := &config.Stack{Name: "demo", Gateway: &config.GatewayConfig{ToolDiscovery: &config.ToolDiscoveryConfig{
		Mode:         "deferred",
		AlwaysExpose: []string{"alpha__echo", "beta__add"},
	}}}
	diff := ComputeDiff(old, changed)
	if !diff.ToolDiscoveryChanged || diff.IsEmpty() {
		t.Fatal("discovery-only edit was empty")
	}
	reordered := &config.Stack{Name: "demo", Gateway: &config.GatewayConfig{ToolDiscovery: &config.ToolDiscoveryConfig{
		Mode:         "deferred",
		AlwaysExpose: []string{"beta__add", "alpha__echo"},
	}}}
	if !ComputeDiff(changed, reordered).ToolDiscoveryChanged {
		t.Fatal("order-sensitive always_expose change was ignored")
	}
}
