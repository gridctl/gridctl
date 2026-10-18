package toolpolicy

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

func TestNewPolicy_NilIsPermissive(t *testing.T) {
	var p *Policy
	if p.Gates() != nil {
		t.Fatal("nil policy must not install a gate")
	}
	v := p.Evaluate("vault__read_secret", &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)})
	if v.Action != actionAllow {
		t.Fatalf("nil evaluate = %+v", v)
	}
	st := p.Status()
	if st.Configured || st.Rules == nil || st.Tools == nil {
		t.Fatalf("nil status = %+v", st)
	}
}

func TestNewPolicy_EmptyBlockInstallsGate(t *testing.T) {
	p := NewPolicy(&config.PolicyConfig{}, slog.Default())
	if p == nil || len(p.Gates()) != 1 {
		t.Fatal("empty policy must compile to a gate")
	}
	if p.Gates()[0].Name() != "policy" {
		t.Fatalf("gate name = %q", p.Gates()[0].Name())
	}
	st := p.Status()
	if !st.Configured || st.Default != "allow" || st.BlockDestructive == nil || *st.BlockDestructive {
		t.Fatalf("empty status = %+v", st)
	}
	if st.Rules == nil || st.Tools == nil {
		t.Fatal("empty collections must be non-nil")
	}
}

func TestEvaluate_LastMatchWins(t *testing.T) {
	p := NewPolicy(&config.PolicyConfig{
		Rules: []config.PolicyRule{
			{Match: "vault__*", Action: "block"},
			{Match: "vault__list_paths", Action: "allow"},
		},
	}, nil)
	blocked := p.Evaluate("vault__read_secret", nil)
	if blocked.Action != actionBlock || blocked.Source != sourceRule || blocked.RuleIndex != 0 || blocked.Rule != "vault__*" {
		t.Fatalf("blocked = %+v", blocked)
	}
	allowed := p.Evaluate("vault__list_paths", nil)
	if allowed.Action != actionAllow || allowed.RuleIndex != 1 {
		t.Fatalf("allowed = %+v", allowed)
	}
	other := p.Evaluate("pager__page", nil)
	if other.Action != actionAllow || other.Source != sourceDefault {
		t.Fatalf("default = %+v", other)
	}
}

func TestEvaluate_BlockDestructivePrecedence(t *testing.T) {
	destr := &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}
	p := NewPolicy(&config.PolicyConfig{BlockDestructive: true}, nil)
	if v := p.Evaluate("vault__purge", destr); v.Action != actionBlock || v.Source != sourceBlockDestructive {
		t.Fatalf("destructive = %+v", v)
	}
	if v := p.Evaluate("vault__list", nil); v.Action != actionAllow || v.Source != sourceDefault {
		t.Fatalf("no annotations = %+v", v)
	}
	if v := p.Evaluate("vault__safe", &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)}); v.Action != actionAllow {
		t.Fatalf("destructive false = %+v", v)
	}

	explicit := NewPolicy(&config.PolicyConfig{
		BlockDestructive: true,
		Rules:            []config.PolicyRule{{Match: "vault__purge", Action: "allow"}},
	}, nil)
	if v := explicit.Evaluate("vault__purge", destr); v.Action != actionAllow || v.Source != sourceRule {
		t.Fatalf("explicit allow must beat block_destructive: %+v", v)
	}
}

func TestEvaluate_UnparseableNeverMatches(t *testing.T) {
	p := NewPolicy(&config.PolicyConfig{
		Default: "block",
		Rules:   []config.PolicyRule{{Match: "[", Action: "allow"}},
	}, slog.New(slog.DiscardHandler))
	v := p.Evaluate("vault__read", nil)
	if v.Action != actionBlock || v.Source != sourceDefault {
		t.Fatalf("unparseable pattern failed open: %+v", v)
	}
}

func TestEvaluate_RequireApprovalDenies(t *testing.T) {
	p := NewPolicy(&config.PolicyConfig{
		Rules: []config.PolicyRule{{Match: "pager__manage", Action: "require_approval"}},
	}, nil)
	v := p.Evaluate("pager__manage", nil)
	if v.Action != actionRequireApproval || v.Source != sourceRule {
		t.Fatalf("verdict = %+v", v)
	}
	decision := p.Gates()[0].CheckToolCall(context.Background(), mcp.GateCall{PrefixedTool: "pager__manage"})
	if decision.Allow || decision.Reason != runs.ReasonPolicyBlocked {
		t.Fatalf("decision = %+v", decision)
	}
	want := "Error: tool blocked by policy (rule 1 requires approval, which this gateway does not yet support); do not retry"
	if decision.Message != want {
		t.Fatalf("message = %q", decision.Message)
	}
	if strings.Contains(decision.Message, "{") {
		t.Fatal("message included arguments")
	}
}

func TestGate_Messages(t *testing.T) {
	p := NewPolicy(&config.PolicyConfig{
		Default:          "block",
		BlockDestructive: true,
		Rules: []config.PolicyRule{
			{Match: "vault__*", Action: "block"},
			{Match: "vault__list_paths", Action: "allow"},
		},
	}, nil)
	gate := p.Gates()[0]
	destr := &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}

	cases := []struct {
		name string
		call mcp.GateCall
		want string
	}{
		{
			name: "rule",
			call: mcp.GateCall{PrefixedTool: "vault__read_secret"},
			want: "Error: tool blocked by policy (rule 1: vault__*); do not retry",
		},
		{
			name: "allow exception",
			call: mcp.GateCall{PrefixedTool: "vault__list_paths"},
			want: "",
		},
		{
			name: "block_destructive",
			call: mcp.GateCall{PrefixedTool: "pager__purge", Annotations: destr},
			want: "Error: tool blocked by policy (block_destructive: destructiveHint is true); do not retry",
		},
		{
			name: "default",
			call: mcp.GateCall{PrefixedTool: "pager__list"},
			want: "Error: tool blocked by policy (default: block); do not retry",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := gate.CheckToolCall(context.Background(), tc.call)
			if tc.want == "" {
				if !d.Allow {
					t.Fatalf("expected allow, got %+v", d)
				}
				return
			}
			if d.Allow || d.Message != tc.want || d.Reason != runs.ReasonPolicyBlocked {
				t.Fatalf("decision = %+v", d)
			}
		})
	}
}

func TestStatusWithCatalog_HiddenDestructive(t *testing.T) {
	p := NewPolicy(&config.PolicyConfig{BlockDestructive: true}, nil)
	destr := &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)}
	st := p.StatusWithCatalog([]CatalogEntry{
		{Name: "vault__purge", Annotations: destr, Exposed: false},
		{Name: "vault__list", Exposed: true},
	})
	hidden := st.Tools["vault__purge"]
	if hidden.Action != actionBlock || hidden.Source != sourceBlockDestructive || hidden.Exposed {
		t.Fatalf("hidden destructive = %+v", hidden)
	}
	if st.Tools["vault__list"].Action != actionAllow || !st.Tools["vault__list"].Exposed {
		t.Fatalf("listed = %+v", st.Tools["vault__list"])
	}
}
