package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func limitsTestStack(limits *config.LimitsConfig) *config.Stack {
	return &config.Stack{
		Name:       "test",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "github", Image: "image1", Port: 3000}},
		Limits:     limits,
	}
}

func TestComputeDiff_LimitsOnlyChange(t *testing.T) {
	old := limitsTestStack(nil)
	new := limitsTestStack(&config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Client: "claude-code", MaxUSD: 5, Period: "daily"}},
	})

	diff := ComputeDiff(old, new)
	if !diff.LimitsChanged {
		t.Error("expected LimitsChanged to be true")
	}
	if diff.IsEmpty() {
		t.Error("expected non-empty diff for a limits-only change")
	}
	// A limits-only change must not touch containers, networks, or resources.
	if len(diff.MCPServers.Added) != 0 || len(diff.MCPServers.Removed) != 0 ||
		len(diff.MCPServers.Modified) != 0 {
		t.Error("expected no MCP server changes for a limits-only change")
	}
	if diff.NetworkChanged || diff.ClientsChanged {
		t.Error("limits-only change flagged unrelated diffs")
	}
}

func TestComputeDiff_LimitsIdentical(t *testing.T) {
	mk := func() *config.LimitsConfig {
		return &config.LimitsConfig{
			Budgets:    []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
			RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 30}},
		}
	}
	diff := ComputeDiff(limitsTestStack(mk()), limitsTestStack(mk()))
	if diff.LimitsChanged {
		t.Error("expected LimitsChanged to be false for identical limits blocks")
	}
	if !diff.IsEmpty() {
		t.Error("expected empty diff for stacks with identical limits blocks")
	}
}

func TestComputeDiff_LimitsSetToNil(t *testing.T) {
	old := limitsTestStack(&config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 30}},
	})
	if !ComputeDiff(old, limitsTestStack(nil)).LimitsChanged {
		t.Error("expected removing the limits block to be detected")
	}
}

func TestComputeDiff_LimitsCapEdit(t *testing.T) {
	old := limitsTestStack(&config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 5, Period: "daily"}},
	})
	new := limitsTestStack(&config.LimitsConfig{
		Budgets: []config.BudgetLimit{{Server: "github", MaxUSD: 10, Period: "daily"}},
	})
	if !ComputeDiff(old, new).LimitsChanged {
		t.Error("expected a max_usd edit to be detected")
	}
}
