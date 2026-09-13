package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestExecution_MixedAutoscaleChangeRecreatesAndKeepsPins(t *testing.T) {
	before := config.MCPServer{Name: "fixture", Command: []string{"/bin/cat"}, Autoscale: &config.AutoscaleConfig{Min: 1, Max: 2, TargetInFlight: 1}}
	after := before
	after.Autoscale = &config.AutoscaleConfig{Min: 1, Max: 3, TargetInFlight: 1}
	after.Execution = &config.ExecutionConfig{Mode: "local"}
	diff := ComputeDiff(&config.Stack{MCPServers: []config.MCPServer{before}}, &config.Stack{MCPServers: []config.MCPServer{after}})
	if len(diff.MCPServers.Modified) != 1 || len(diff.MCPServers.AutoscalePolicyChanges) != 0 {
		t.Fatal("mixed execution change bypassed recreation")
	}
	if !config.ExecutionOnlyChange(before, after) {
		t.Fatal("boundary and pool changes should not reset tool trust")
	}
	after.Command = []string{"/bin/echo"}
	if config.ExecutionOnlyChange(before, after) {
		t.Fatal("changed executable incorrectly retained execution-only classification")
	}
}
