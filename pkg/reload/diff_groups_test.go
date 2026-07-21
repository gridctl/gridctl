package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func groupsTestStack(groups map[string]config.GroupConfig) *config.Stack {
	return &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}},
		Groups:     groups,
	}
}

func TestComputeDiff_GroupsOnlyChange(t *testing.T) {
	old := groupsTestStack(nil)
	new := groupsTestStack(map[string]config.GroupConfig{
		"release": {Servers: []string{"github"}},
	})

	diff := ComputeDiff(old, new)
	if !diff.GroupsChanged {
		t.Error("expected GroupsChanged to be true")
	}
	if diff.IsEmpty() {
		t.Error("expected non-empty diff for a groups-only change")
	}
	if len(diff.MCPServers.Added) != 0 || len(diff.MCPServers.Modified) != 0 ||
		diff.NetworkChanged || diff.ClientsChanged || diff.LimitsChanged {
		t.Error("groups-only change flagged unrelated diffs")
	}
}

func TestComputeDiff_GroupsIdentical(t *testing.T) {
	mk := func() map[string]config.GroupConfig {
		return map[string]config.GroupConfig{
			"release": {
				Servers: []string{"github"},
				Overrides: map[string]config.GroupOverride{
					"github__create_issue": {Name: "create_issue"},
				},
			},
		}
	}
	diff := ComputeDiff(groupsTestStack(mk()), groupsTestStack(mk()))
	if diff.GroupsChanged {
		t.Error("expected GroupsChanged to be false for identical groups blocks")
	}
	if !diff.IsEmpty() {
		t.Error("expected empty diff for stacks with identical groups blocks")
	}
}

func TestComputeDiff_GroupsOverrideEdit(t *testing.T) {
	old := groupsTestStack(map[string]config.GroupConfig{
		"release": {Servers: []string{"github"}},
	})
	new := groupsTestStack(map[string]config.GroupConfig{
		"release": {
			Servers: []string{"github"},
			Overrides: map[string]config.GroupOverride{
				"github__create_issue": {Description: "rewritten"},
			},
		},
	})
	if !ComputeDiff(old, new).GroupsChanged {
		t.Error("expected an override edit to be detected")
	}
	if !ComputeDiff(new, groupsTestStack(nil)).GroupsChanged {
		t.Error("expected removing the groups block to be detected")
	}
}
