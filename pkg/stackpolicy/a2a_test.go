package stackpolicy

import "testing"

func TestEvaluate_A2ASourceClassification(t *testing.T) {
	dir := t.TempDir()
	for _, rule := range []string{RuleDenyLocalCommandServers, RuleDenySSHServers} {
		policy := write(t, dir, "policy.yaml", policyYAML(rule, RuleExplicitImageDigests))
		stack := write(t, dir, "stack.yaml", "name: demo\nmcp-servers:\n  - name: agent\n    a2a: {card: 'https://example.com/card'}\n")
		r := Evaluate(t.Context(), stack, policy)
		if !r.Accepted {
			t.Fatalf("A2A treated as prohibited source: %+v", r)
		}
		if !hasOutcome(r, RuleExplicitImageDigests, OutcomeNotApplicable, reasonNonImage) {
			t.Fatalf("A2A missing non-image exclusion: %+v", r)
		}
		if len(r.Coverage.Exclusions) != 1 || r.Coverage.Exclusions[0].Rule != RuleExplicitImageDigests || r.Coverage.Exclusions[0].ReasonCode != reasonNonImage || r.Coverage.Exclusions[0].Count != 1 {
			t.Fatalf("unexpected exclusions: %+v", r.Coverage.Exclusions)
		}
	}
}

func TestEvaluate_A2AImageDigestsOnly(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	stack := write(t, dir, "stack.yaml", "name: demo\nmcp-servers:\n  - name: agent\n    a2a: {card: 'https://example.com/card'}\n")
	r := Evaluate(t.Context(), stack, policy)
	if !hasOutcome(r, RuleExplicitImageDigests, OutcomeNotApplicable, reasonNonImage) {
		t.Fatalf("A2A missing non-image exclusion: %+v", r)
	}
	if r.Accepted || r.Status != StatusError || !hasDiag(r, "no-applicable-checks") {
		t.Fatalf("all-N/A evaluation must remain an error: %+v", r)
	}
}
