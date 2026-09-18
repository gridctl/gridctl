package stackpolicy

import "testing"

func TestEvaluate_A2ASourceClassification(t *testing.T) {
	dir := t.TempDir()
	for _, rule := range []string{RuleDenyLocalCommandServers, RuleDenySSHServers} {
		policy := write(t, dir, "policy.yaml", policyYAML(rule))
		stack := write(t, dir, "stack.yaml", "name: demo\nmcp-servers:\n  - name: agent\n    a2a: {card: 'https://example.com/card'}\n")
		r := Evaluate(t.Context(), stack, policy)
		if !r.Accepted {
			t.Fatalf("A2A treated as prohibited source: %+v", r)
		}
	}
}
