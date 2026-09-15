package stackpolicy

import "testing"

func TestEvaluate_ClientAndGroupListsAreNotSubstitutes(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
clients:
  default: deny
  profiles:
    ci:
      tools: ["read"]
groups:
  ops:
    tools: ["read"]
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleNonemptyServerToolLists))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusRejected || !hasOutcome(report, RuleNonemptyServerToolLists, OutcomeViolation, reasonEmptyTools) {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}
