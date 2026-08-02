package mcp

import "testing"

func TestSkillPolicy_NilAllowsEverything(t *testing.T) {
	var p *SkillPolicy
	if !p.Allows("anything") {
		t.Fatal("nil policy denied a skill")
	}
	if allowed, rule := p.Evaluate("anything"); !allowed || rule != "" {
		t.Fatalf("nil policy Evaluate = (%v, %q)", allowed, rule)
	}
}

func TestSkillPolicy_DenyWinsOverAllow(t *testing.T) {
	p := NewSkillPolicy(&SkillPolicySpec{
		Allow: []string{"refund-*"},
		Deny:  []string{"*refund*"},
	})
	allowed, rule := p.Evaluate("refund-processor")
	if allowed {
		t.Fatal("deny did not win over allow")
	}
	if rule != "*refund*" {
		t.Fatalf("rule = %q, want the matching deny glob", rule)
	}
}

func TestSkillPolicy_DefaultDenyWithAllowList(t *testing.T) {
	p := NewSkillPolicy(&SkillPolicySpec{
		Default: "deny",
		Allow:   []string{"incident-*"},
	})
	if !p.Allows("incident-triage") {
		t.Fatal("allow-listed skill denied")
	}
	allowed, rule := p.Evaluate("other")
	if allowed {
		t.Fatal("unlisted skill allowed under default: deny")
	}
	if rule != DefaultDenyRule {
		t.Fatalf("rule = %q, want %q", rule, DefaultDenyRule)
	}
}

func TestSkillPolicy_DefaultAllow(t *testing.T) {
	p := NewSkillPolicy(&SkillPolicySpec{Deny: []string{"secret-*"}})
	if !p.Allows("ordinary") {
		t.Fatal("unlisted skill denied under default allow")
	}
	if p.Allows("secret-plans") {
		t.Fatal("deny glob did not match")
	}
}

func TestSkillPolicy_ExactNameGlob(t *testing.T) {
	p := NewSkillPolicy(&SkillPolicySpec{Deny: []string{"exact-name"}})
	if p.Allows("exact-name") {
		t.Fatal("exact deny did not match")
	}
	if !p.Allows("exact-name-2") {
		t.Fatal("exact deny over-matched")
	}
}

func TestNewSkillPolicy_NilSpec(t *testing.T) {
	if NewSkillPolicy(nil) != nil {
		t.Fatal("nil spec must compile to nil policy")
	}
}
