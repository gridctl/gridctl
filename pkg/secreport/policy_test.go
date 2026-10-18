package secreport

import (
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestAssemble_PolicyDeclaredAndUndeclared(t *testing.T) {
	undeclared := Assemble(time.Now(), Inputs{Stack: &StackView{Name: "demo", Policy: &PolicyDeclView{}}})
	var found bool
	for _, c := range undeclared.Checks {
		if c.ID != "policy.declared.gateway" {
			continue
		}
		found = true
		if c.Outcome != OutcomePass || c.ReasonCode != "policy_undeclared" || c.Predicate != PredPolicyDeclared {
			t.Fatalf("undeclared = %+v", c)
		}
		if len(c.Limitations) != 1 || !strings.Contains(c.Limitations[0], "name-based") {
			t.Fatalf("limitations = %+v", c.Limitations)
		}
	}
	if !found {
		t.Fatal("missing policy.declared.gateway on an undeclared stack")
	}

	declared := Assemble(time.Now(), Inputs{Stack: &StackView{Name: "demo", Policy: &PolicyDeclView{
		Declared: true, Default: "block", RuleCount: 2, BlockDestructive: true, ApprovalRuleCount: 1,
	}}})
	found = false
	for _, c := range declared.Checks {
		if c.ID != "policy.declared.gateway" {
			continue
		}
		found = true
		if c.ReasonCode != "policy_declared" || c.Facts.DefaultAction != "block" || c.Facts.RuleCount == nil || *c.Facts.RuleCount != 2 {
			t.Fatalf("declared = %+v", c)
		}
		if c.Facts.BlockDestructive == nil || !*c.Facts.BlockDestructive {
			t.Fatalf("block_destructive fact = %+v", c.Facts)
		}
		if !strings.Contains(c.Explanation, "default block") || !strings.Contains(c.Explanation, "2 rules") {
			t.Fatalf("explanation = %q", c.Explanation)
		}
	}
	if !found {
		t.Fatal("missing declared policy check")
	}
}

func TestStackView_PolicyFromConfig(t *testing.T) {
	view := stackViewFromConfig(&config.Stack{
		Name: "demo",
		Policy: &config.PolicyConfig{
			BlockDestructive: true,
			Rules: []config.PolicyRule{
				{Match: "vault__*", Action: "block"},
				{Match: "pager__page", Action: "require_approval"},
			},
		},
	})
	if view.Policy == nil || !view.Policy.Declared || view.Policy.Default != "allow" || view.Policy.RuleCount != 2 || view.Policy.ApprovalRuleCount != 1 || !view.Policy.BlockDestructive {
		t.Fatalf("policy view = %+v", view.Policy)
	}
	absent := stackViewFromConfig(&config.Stack{Name: "demo"})
	if absent.Policy == nil || absent.Policy.Declared {
		t.Fatalf("omitted policy = %+v", absent.Policy)
	}
}
