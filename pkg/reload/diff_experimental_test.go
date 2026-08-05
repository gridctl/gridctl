package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func experimentalTestStack(experimental map[string]bool) *config.Stack {
	return &config.Stack{
		Name:         "test",
		Network:      config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers:   []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}},
		Experimental: experimental,
	}
}

func TestComputeDiff_ExperimentalOnlyChange(t *testing.T) {
	old := experimentalTestStack(nil)
	new := experimentalTestStack(map[string]bool{"test_flag": true})

	diff := ComputeDiff(old, new)
	if !diff.ExperimentalChanged {
		t.Error("expected ExperimentalChanged to be true")
	}
	if diff.IsEmpty() {
		t.Error("expected non-empty diff for an experimental-only change so onConfigApplied fires")
	}
	if len(diff.MCPServers.Added) != 0 || len(diff.MCPServers.Modified) != 0 ||
		diff.NetworkChanged || diff.ClientsChanged || diff.LimitsChanged || diff.GroupsChanged {
		t.Error("experimental-only change flagged unrelated diffs")
	}
}

func TestComputeDiff_ExperimentalIdentical(t *testing.T) {
	mk := func() map[string]bool {
		return map[string]bool{"test_flag": true, "other": false}
	}
	diff := ComputeDiff(experimentalTestStack(mk()), experimentalTestStack(mk()))
	if diff.ExperimentalChanged {
		t.Error("expected ExperimentalChanged to be false for identical maps")
	}
	if !diff.IsEmpty() {
		t.Error("expected empty diff for stacks with identical experimental maps")
	}
}

func TestComputeDiff_ExperimentalValueFlip(t *testing.T) {
	old := experimentalTestStack(map[string]bool{"test_flag": true})
	new := experimentalTestStack(map[string]bool{"test_flag": false})
	if diff := ComputeDiff(old, new); !diff.ExperimentalChanged {
		t.Error("expected a true-to-false flip to mark ExperimentalChanged")
	}
}

func TestComputeDiff_ExperimentalSetToNil(t *testing.T) {
	old := experimentalTestStack(map[string]bool{"test_flag": true})
	new := experimentalTestStack(nil)
	if diff := ComputeDiff(old, new); !diff.ExperimentalChanged {
		t.Error("expected removing the block to mark ExperimentalChanged")
	}
}
