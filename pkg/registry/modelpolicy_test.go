package registry

import (
	"strings"
	"testing"
)

func TestResolveClient(t *testing.T) {
	p := &ModelPolicy{
		Rewrite: true,
		Default: "sonnet",
		Clients: map[string]ClientModelPolicy{
			"opencode": {
				Default:   "anthropic/claude-sonnet-5-5",
				Overrides: map[string]string{"reviewer": "anthropic/claude-opus-5-5#high", "blank": ""},
			},
			"gemini": {Overrides: map[string]string{"reviewer": "inherit"}},
		},
	}
	tests := []struct {
		slug, name, wantValue, wantRes string
	}{
		{"opencode", "reviewer", "anthropic/claude-opus-5-5#high", ResolutionOverride},
		{"opencode", "other", "anthropic/claude-sonnet-5-5", ResolutionDefault},
		{"opencode", "blank", "anthropic/claude-sonnet-5-5", ResolutionDefault},
		{"gemini", "reviewer", "inherit", ResolutionOverride},
		{"gemini", "other", "", ""},
		{"copilot", "reviewer", "", ""},
		{"claude-code", "reviewer", "", ""},
	}
	for _, tt := range tests {
		v, res := p.ResolveClient(tt.slug, tt.name)
		if v != tt.wantValue || res != tt.wantRes {
			t.Errorf("ResolveClient(%q, %q) = (%q, %q), want (%q, %q)", tt.slug, tt.name, v, res, tt.wantValue, tt.wantRes)
		}
	}

	// Nil policy and nil client map answer nothing without panicking.
	var nilPolicy *ModelPolicy
	if v, res := nilPolicy.ResolveClient("opencode", "reviewer"); v != "" || res != "" {
		t.Errorf("nil policy resolved (%q, %q)", v, res)
	}
	if v, _ := (&ModelPolicy{Rewrite: true}).ResolveClient("opencode", "reviewer"); v != "" {
		t.Errorf("nil Clients resolved %q", v)
	}
}

func TestResolveClient_DoesNotAffectTopLevelResolution(t *testing.T) {
	p := &ModelPolicy{Rewrite: true, Clients: map[string]ClientModelPolicy{"opencode": {Default: "a/b"}}}
	if v, need := p.NeedsRewrite("reviewer", ""); need || v != "" {
		t.Errorf("client values must not leak into the identity rewrite, got (%q, %v)", v, need)
	}
}

func TestRenderedAgentTargets(t *testing.T) {
	got := RenderedAgentTargets()
	if strings.Join(got, ",") != "copilot,gemini,opencode" {
		t.Fatalf("RenderedAgentTargets = %v", got)
	}
	for _, slug := range got {
		if !IsRenderedAgentTarget(slug) {
			t.Errorf("IsRenderedAgentTarget(%q) = false", slug)
		}
		if AgentHonor(slug) != HonorDropped {
			t.Errorf("rendered target %q must be dropped-on-render in the honor matrix", slug)
		}
	}
	for _, slug := range []string{"claude-code", "agents", "", "OpenCode"} {
		if IsRenderedAgentTarget(slug) {
			t.Errorf("IsRenderedAgentTarget(%q) = true", slug)
		}
	}
	got[0] = "mutated"
	if RenderedAgentTargets()[0] != "copilot" {
		t.Error("RenderedAgentTargets must return a copy")
	}
}
