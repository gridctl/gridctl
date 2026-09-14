package stackpolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluate_DoesNotResolveEnvironment(t *testing.T) {
	t.Setenv("IMG", pinnedAlpine)
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: tools
    image: ${IMG}
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusIndeterminate {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}

func TestEvaluate_FallbackExpressionUnknown(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: tools
    image: alpine:${TAG:-latest}
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if !hasOutcome(report, RuleExplicitImageDigests, OutcomeUnknown, reasonUnresolvedImage) {
		t.Fatalf("results %+v", report.Results)
	}
	if hasOutcome(report, RuleExplicitImageDigests, OutcomeViolation, reasonMutableTag) {
		t.Fatal("fallback became a tag violation")
	}
}

func TestEvaluate_InvalidDigestIsInputError(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: tools
    image: alpine@sha256:zzzz
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusError || report.Accepted {
		t.Fatalf("status=%s diags=%v results=%v", report.Status, report.Diagnostics, report.Results)
	}
}

func TestEvaluate_OpenAPIIsNotSSHOrLocal(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: api
    openapi:
      spec: ./spec.yaml
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers, RuleDenyLocalCommandServers))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s results=%v diags=%v", report.Status, report.Results, report.Diagnostics)
	}
}

func TestEvaluate_ScanDoesNotSatisfyBlock(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
gateway:
  security:
    schema_pinning:
      enabled: true
      scan: true
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleSchemaPinningBlock))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusRejected {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}

func TestEvaluate_UnknownPinningField(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
gateway:
  security:
    schema_pinning:
      enabled: true
      action: block
      exotic: true
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleSchemaPinningBlock))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusIndeterminate {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}

func TestEvaluate_DuplicateYAMLKey(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", "name: demo\nname: other\nmcp-servers: []\n")
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "duplicate-key") {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_CandidateCannotReadPolicy(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	stack := write(t, dir, "stack.yaml", "name: child\nextends: ./policy.yaml\nmcp-servers: []\n")
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "extends-policy") {
		t.Fatalf("diags %+v status=%s", report.Diagnostics, report.Status)
	}
}

func TestEvaluate_ExtendsDepth(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "level-10.yaml", "name: n\nmcp-servers: []\n")
	for i := 9; i >= 0; i-- {
		parent := "level-10.yaml"
		if i < 9 {
			parent = "level-" + itoa(i+1) + ".yaml"
		}
		name := "level-" + itoa(i) + ".yaml"
		if i == 0 {
			name = "stack.yaml"
		}
		write(t, dir, name, "name: n\nextends: ./"+parent+"\nmcp-servers: []\n")
	}
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	ok := Evaluate(t.Context(), filepath.Join(dir, "stack.yaml"), policy)
	if !ok.Accepted && ok.Status != StatusError {
		if hasDiag(ok, "extends-depth") {
			t.Fatalf("depth 10 should be allowed: %+v", ok.Diagnostics)
		}
	}
	write(t, dir, "level-11.yaml", "name: n\nmcp-servers: []\n")
	write(t, dir, "level-10.yaml", "name: n\nextends: ./level-11.yaml\nmcp-servers: []\n")
	over := Evaluate(t.Context(), filepath.Join(dir, "stack.yaml"), policy)
	if !hasDiag(over, "extends-depth") {
		t.Fatalf("expected depth error, diags=%+v status=%s", over.Diagnostics, over.Status)
	}
}

func TestEvaluate_JSONDeterministic(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    tools: ["read"]
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests, RuleNonemptyServerToolLists))
	a := mustJSON(t, Evaluate(t.Context(), stack, policy))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SECRET", "nope")
	b := mustJSON(t, Evaluate(t.Context(), stack, policy))
	if a != b {
		t.Fatalf("json drifted\n%s\n%s", a, b)
	}
}

func TestParsePolicy_Symlink(t *testing.T) {
	dir := t.TempDir()
	real := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	stack := write(t, dir, "stack.yaml", "name: demo\nmcp-servers: []\n")
	report := Evaluate(t.Context(), stack, link)
	if !hasDiag(report, "policy-symlink") {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}
