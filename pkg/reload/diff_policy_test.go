package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func policyTestStack(policy *config.PolicyConfig) *config.Stack {
	return &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "vault", Image: "image1", Port: 3000}},
		Policy:     policy,
	}
}

func TestComputeDiff_PolicyOnlyChange(t *testing.T) {
	old := policyTestStack(nil)
	new := policyTestStack(&config.PolicyConfig{
		Rules: []config.PolicyRule{{Match: "vault__*", Action: "block"}},
	})

	diff := ComputeDiff(old, new)
	if !diff.PolicyChanged {
		t.Error("expected PolicyChanged to be true")
	}
	if diff.IsEmpty() {
		t.Error("expected non-empty diff for a policy-only change")
	}
	if len(diff.MCPServers.Added) != 0 || len(diff.MCPServers.Removed) != 0 ||
		len(diff.MCPServers.Modified) != 0 {
		t.Error("expected no MCP server changes for a policy-only change")
	}
	if diff.NetworkChanged || diff.ClientsChanged || diff.LimitsChanged {
		t.Error("policy-only change flagged unrelated diffs")
	}
}

func TestComputeDiff_PolicyIdentical(t *testing.T) {
	mk := func() *config.PolicyConfig {
		return &config.PolicyConfig{
			Default: "allow",
			Rules:   []config.PolicyRule{{Match: "vault__*", Action: "block"}},
		}
	}
	diff := ComputeDiff(policyTestStack(mk()), policyTestStack(mk()))
	if diff.PolicyChanged {
		t.Error("expected PolicyChanged to be false for identical policy blocks")
	}
	if !diff.IsEmpty() {
		t.Error("expected empty diff for stacks with identical policy blocks")
	}
}

func TestComputeDiff_PolicySetToNil(t *testing.T) {
	old := policyTestStack(&config.PolicyConfig{
		Rules: []config.PolicyRule{{Match: "vault__read_secret", Action: "block"}},
	})
	if !ComputeDiff(old, policyTestStack(nil)).PolicyChanged {
		t.Error("expected removing the policy block to be detected")
	}
}

func TestComputeDiff_PolicyRuleEdit(t *testing.T) {
	old := policyTestStack(&config.PolicyConfig{
		Rules: []config.PolicyRule{{Match: "vault__*", Action: "block"}},
	})
	new := policyTestStack(&config.PolicyConfig{
		Rules: []config.PolicyRule{{Match: "vault__*", Action: "allow"}},
	})
	if !ComputeDiff(old, new).PolicyChanged {
		t.Error("expected an action edit to be detected")
	}
}
