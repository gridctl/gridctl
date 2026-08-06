package limits

import (
	"context"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func githubCall(client string) mcp.GateCall {
	return mcp.GateCall{
		PrefixedTool:   "github__search_code",
		ServerName:     "github",
		ClientAccessID: client,
	}
}

func newTestPolicy(t *testing.T, cfg *config.LimitsConfig) *Policy {
	t.Helper()
	p := NewPolicy(cfg, nil)
	if p == nil {
		t.Fatal("expected non-nil policy")
	}
	return p
}

func TestNewPolicy_NilAndEmpty(t *testing.T) {
	if p := NewPolicy(nil, nil); p != nil {
		t.Error("nil config should compile to nil policy")
	}
	if p := NewPolicy(&config.LimitsConfig{}, nil); p != nil {
		t.Error("empty config should compile to nil policy")
	}
	// Nil policy methods are all safe and permissive.
	var p *Policy
	if gates := p.Gates(); gates != nil {
		t.Error("nil policy should return nil gates")
	}
	if st := p.Status(); st.Configured || st.Entries == nil || len(st.Entries) != 0 {
		t.Errorf("nil policy status = %+v", st)
	}
}

func TestGates_SingleRateGate(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 30}},
	})
	gates := p.Gates()
	if len(gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(gates))
	}
	if gates[0].Name() != "rate-limits" {
		t.Errorf("gate name = %s", gates[0].Name())
	}
}

func TestRateGate_BurstThenDeny(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 6, Burst: 3}},
	})
	gate := p.Gates()[0]
	ctx := context.Background()

	for i := range 3 {
		if d := gate.CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
			t.Fatalf("burst call %d denied: %s", i, d.Message)
		}
	}
	d := gate.CheckToolCall(ctx, githubCall("cursor"))
	if d.Allow {
		t.Fatal("call past burst should be denied")
	}
	for _, want := range []string{`server "github"`, "6 calls/min", "Retry after"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("denial message missing %q: %s", want, d.Message)
		}
	}

	// A call that matches no entry is unaffected.
	other := mcp.GateCall{PrefixedTool: "gitlab__list", ServerName: "gitlab", ClientAccessID: "cursor"}
	if d := gate.CheckToolCall(ctx, other); !d.Allow {
		t.Errorf("unmatched call denied: %s", d.Message)
	}
}

func TestRateGate_ClientScopeNormalizes(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Client: "claude-code", CallsPerMinute: 6, Burst: 1}},
	})
	gate := p.Gates()[0]
	ctx := context.Background()

	// "Claude Code" normalizes to "claude-code" and must share the bucket.
	if d := gate.CheckToolCall(ctx, githubCall("Claude Code")); !d.Allow {
		t.Fatalf("first call denied: %s", d.Message)
	}
	if d := gate.CheckToolCall(ctx, githubCall("claude-code")); d.Allow {
		t.Fatal("alias variant should hit the same bucket and be denied")
	}
	// A different client is unaffected.
	if d := gate.CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
		t.Errorf("other client denied: %s", d.Message)
	}
}

func TestStatus_States(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 60, Burst: 1}},
	})
	ctx := context.Background()

	st := p.Status()
	if !st.Configured || len(st.Entries) != 1 {
		t.Fatalf("status = %+v", st)
	}
	entry := st.Entries[0]
	if entry.Kind != "rate" || entry.State != "ok" || entry.Rate == nil {
		t.Errorf("initial entry = %+v", entry)
	}
	if entry.Rate.CallsPerMinute != 60 || entry.Rate.Burst != 1 {
		t.Errorf("rate snapshot = %+v", entry.Rate)
	}

	// Drain the rate bucket; status flips to exceeded.
	p.Gates()[0].CheckToolCall(ctx, githubCall("cursor"))
	if got := p.Status().Entries[0]; got.State != "exceeded" {
		t.Errorf("drained bucket state = %s", got.State)
	}
}

func TestCarryOver_RateBucketsSurviveReload(t *testing.T) {
	mk := func() *Policy {
		return newTestPolicy(t, &config.LimitsConfig{
			RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 6, Burst: 2}},
		})
	}
	ctx := context.Background()
	oldP := mk()
	gate := oldP.Gates()[0]
	gate.CheckToolCall(ctx, githubCall("cursor"))
	gate.CheckToolCall(ctx, githubCall("cursor")) // bucket drained

	// Unrelated reload rebuilds the policy with an identical rate entry:
	// the drained bucket must carry, not refill.
	newP := mk()
	newP.CarryOver(oldP)
	if d := newP.Gates()[0].CheckToolCall(ctx, githubCall("cursor")); d.Allow {
		t.Fatal("reload refilled a drained rate bucket")
	}

	// A changed rate gets a fresh bucket by design.
	changed := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{{Server: "github", CallsPerMinute: 12, Burst: 2}},
	})
	changed.CarryOver(newP)
	if d := changed.Gates()[0].CheckToolCall(ctx, githubCall("cursor")); !d.Allow {
		t.Fatalf("changed rate should start fresh: %s", d.Message)
	}
}

func TestNewPolicy_DuplicateClientAfterNormalization(t *testing.T) {
	p := newTestPolicy(t, &config.LimitsConfig{
		RateLimits: []config.RateLimit{
			{Client: "Claude Code", CallsPerMinute: 6, Burst: 1},
			{Client: "claude-code", CallsPerMinute: 9, Burst: 5},
		},
	})
	if len(p.rates) != 1 {
		t.Fatalf("compiled %d rate entries, want 1 (duplicates fold)", len(p.rates))
	}
	// The first declaration wins: one call drains the burst-1 bucket.
	ctx := context.Background()
	if d := p.Gates()[0].CheckToolCall(ctx, githubCall("claude-code")); !d.Allow {
		t.Fatalf("first call denied: %s", d.Message)
	}
	if d := p.Gates()[0].CheckToolCall(ctx, githubCall("claude-code")); d.Allow {
		t.Fatal("second call should hit the folded burst-1 bucket")
	}
}

func TestDefaultBurst(t *testing.T) {
	tests := []struct{ rate, want int }{
		{1, 5}, {6, 5}, {30, 5}, {60, 10}, {600, 100},
	}
	for _, tc := range tests {
		if got := DefaultBurst(tc.rate); got != tc.want {
			t.Errorf("DefaultBurst(%d) = %d, want %d", tc.rate, got, tc.want)
		}
	}
}
