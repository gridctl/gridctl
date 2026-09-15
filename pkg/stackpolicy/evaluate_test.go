package stackpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pinnedAlpine = "alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"

func TestEvaluate_PinnedImagePass(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: tools
    image: `+pinnedAlpine+`
    port: 8080
    tools: ["read"]
resources:
  - name: db
    image: `+pinnedAlpine+`
`)
	policy := write(t, dir, "policy.yaml", `
version: "1"
enabled:
  - explicit-image-digests
`)
	report := Evaluate(t.Context(), stack, policy)
	if ExitCode(report) != 0 {
		t.Fatalf("exit=%d status=%s diags=%v results=%v", ExitCode(report), report.Status, report.Diagnostics, report.Results)
	}
	if !report.Accepted || !report.EvaluationComplete {
		t.Fatalf("accepted=%v complete=%v", report.Accepted, report.EvaluationComplete)
	}
	if report.CheckerVersion != CheckerVersion || report.Policy.Version != PolicyVersion {
		t.Fatalf("versions %+v", report.Policy)
	}
	if !strings.HasPrefix(report.Policy.Digest, "sha256:") {
		t.Fatalf("digest %q", report.Policy.Digest)
	}
}

func TestEvaluate_MutableImageViolation(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: tools
    image: alpine:latest
    port: 8080
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || report.Status != StatusRejected || ExitCode(report) != 1 {
		t.Fatalf("status=%s accepted=%v exit=%d", report.Status, report.Accepted, ExitCode(report))
	}
	if !hasOutcome(report, RuleExplicitImageDigests, OutcomeViolation, reasonMutableTag) {
		t.Fatalf("results %+v", report.Results)
	}
}

func TestEvaluate_UnresolvedImageUnknown(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: tools
    image: alpine:${TAG}
    port: 8080
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusIndeterminate || report.Accepted {
		t.Fatalf("status=%s", report.Status)
	}
	if !hasOutcome(report, RuleExplicitImageDigests, OutcomeUnknown, reasonUnresolvedImage) {
		t.Fatalf("results %+v", report.Results)
	}
	if strings.Contains(mustJSON(t, report), "${TAG}") || strings.Contains(mustJSON(t, report), "alpine") {
		t.Fatalf("leaked selector: %s", mustJSON(t, report))
	}
}

func TestEvaluate_SourceBuiltExcluded(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: built
    source:
      type: git
      url: https://example.invalid/repo.git
    tools: ["read"]
  - name: img
    image: `+pinnedAlpine+`
    port: 8080
    tools: ["read"]
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s diags=%v results=%v", report.Status, report.Diagnostics, report.Results)
	}
	if !hasOutcome(report, RuleExplicitImageDigests, OutcomeNotApplicable, reasonSourceBuilt) {
		t.Fatalf("missing source-built N/A: %+v", report.Results)
	}
	found := false
	for _, ex := range report.Coverage.Exclusions {
		if ex.ReasonCode == reasonSourceBuilt && ex.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("exclusions %+v", report.Coverage.Exclusions)
	}
	text := mustText(t, report)
	if !strings.Contains(text, "excluded_source_built: 1") {
		t.Fatalf("text %q", text)
	}
}

func TestEvaluate_SourceLocalIsNotLocalProcess(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: built
    source:
      type: local
      path: ./src
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenyLocalCommandServers))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s results=%v diags=%v", report.Status, report.Results, report.Diagnostics)
	}
	if !hasOutcome(report, RuleDenyLocalCommandServers, OutcomePass, reasonNotLocalCommand) {
		t.Fatalf("results %+v", report.Results)
	}
}

func TestEvaluate_CommandOverrideOnContainerIsNotLocal(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: img
    image: `+pinnedAlpine+`
    command: ["sleep", "1"]
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenyLocalCommandServers))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}

func TestEvaluate_LocalCommandViolation(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: local
    command: ["npx", "tool"]
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenyLocalCommandServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusRejected || !hasOutcome(report, RuleDenyLocalCommandServers, OutcomeViolation, reasonLocalCommand) {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
	out := mustJSON(t, report) + mustText(t, report)
	if strings.Contains(out, "npx") || strings.Contains(out, "tool") {
		t.Fatalf("leaked command: %s", out)
	}
}

func TestEvaluate_SSHPassesLocalAndFailsSSH(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: remote
    ssh:
      host: example.invalid
      user: op
    command: ["mcp"]
`)
	localPolicy := write(t, dir, "local.yaml", policyYAML(RuleDenyLocalCommandServers))
	sshPolicy := write(t, dir, "ssh.yaml", policyYAML(RuleDenySSHServers))
	local := Evaluate(t.Context(), stack, localPolicy)
	if !local.Accepted {
		t.Fatalf("local status=%s results=%v", local.Status, local.Results)
	}
	ssh := Evaluate(t.Context(), stack, sshPolicy)
	if ssh.Status != StatusRejected || !hasOutcome(ssh, RuleDenySSHServers, OutcomeViolation, reasonSSHServer) {
		t.Fatalf("ssh status=%s results=%v", ssh.Status, ssh.Results)
	}
	if strings.Contains(mustJSON(t, ssh), "example.invalid") {
		t.Fatalf("leaked host")
	}
}

func TestEvaluate_URLIsNotSSH(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: ext
    url: https://example.invalid/mcp
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}

func TestEvaluate_PinningDefaultsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	def := write(t, dir, "default.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
`)
	disabled := write(t, dir, "disabled.yaml", `
name: demo
gateway:
  security:
    schema_pinning:
      enabled: false
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    pin_schemas: true
`)
	serverOff := write(t, dir, "off.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    pin_schemas: false
`)
	blocked := write(t, dir, "block.yaml", `
name: demo
gateway:
  security:
    schema_pinning:
      action: block
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
`)
	enabledPolicy := write(t, dir, "en.yaml", policyYAML(RuleSchemaPinningEnabled))
	blockPolicy := write(t, dir, "bl.yaml", policyYAML(RuleSchemaPinningBlock))

	if r := Evaluate(t.Context(), def, enabledPolicy); !r.Accepted {
		t.Fatalf("default enabled: %s %+v", r.Status, r.Results)
	}
	if r := Evaluate(t.Context(), def, blockPolicy); r.Status != StatusRejected {
		t.Fatalf("default warn is not block: %s %+v", r.Status, r.Results)
	}
	if r := Evaluate(t.Context(), disabled, enabledPolicy); r.Status != StatusRejected {
		t.Fatalf("global disable wins: %s %+v", r.Status, r.Results)
	}
	if r := Evaluate(t.Context(), serverOff, enabledPolicy); r.Status != StatusRejected {
		t.Fatalf("server false: %s %+v", r.Status, r.Results)
	}
	if r := Evaluate(t.Context(), blocked, blockPolicy); !r.Accepted {
		t.Fatalf("block: %s %+v %+v", r.Status, r.Results, r.Diagnostics)
	}
}

func TestEvaluate_ToolLists(t *testing.T) {
	dir := t.TempDir()
	empty := write(t, dir, "empty.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
`)
	ok := write(t, dir, "ok.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    tools: ["read", "search_*", "*"]
`)
	dyn := write(t, dir, "dyn.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    tools: ["${TOOL}"]
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleNonemptyServerToolLists))
	if r := Evaluate(t.Context(), empty, policy); r.Status != StatusRejected {
		t.Fatalf("empty: %s %+v", r.Status, r.Results)
	}
	pass := Evaluate(t.Context(), ok, policy)
	if !pass.Accepted || ExitCode(pass) != 2 {
		t.Fatalf("wildcard literals: accepted=%v exit=%d warnings=%v results=%v diags=%v", pass.Accepted, ExitCode(pass), pass.Warnings, pass.Results, pass.Diagnostics)
	}
	if len(pass.Warnings) != 2 {
		t.Fatalf("want 2 wildcard warnings, got %+v", pass.Warnings)
	}
	text := mustText(t, pass)
	js := mustJSON(t, pass)
	if strings.Contains(text, "search_") || strings.Contains(js, "search_") || strings.Contains(js, `"*"`) {
		t.Fatalf("echoed tool value: text=%s json=%s", text, js)
	}
	if !strings.Contains(text, WarningLiteralToolNameNotPattern) || !strings.Contains(js, WarningLiteralToolNameNotPattern) {
		t.Fatalf("missing warning code")
	}
	unk := Evaluate(t.Context(), dyn, policy)
	if unk.Status != StatusIndeterminate {
		t.Fatalf("dynamic tools: %s %+v", unk.Status, unk.Results)
	}
}

func TestEvaluate_ResourcesNotInPinOrTools(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    tools: ["read"]
resources:
  - name: db
    image: `+pinnedAlpine+`
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleSchemaPinningEnabled, RuleNonemptyServerToolLists))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s %+v %+v", report.Status, report.Results, report.Diagnostics)
	}
	na := 0
	for _, r := range report.Results {
		if r.Outcome == OutcomeNotApplicable && r.ReasonCode == reasonNotMCPServer {
			na++
		}
	}
	if na != 2 {
		t.Fatalf("resource N/A count %d results=%+v", na, report.Results)
	}
}

func TestEvaluate_AllNAError(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: built
    source:
      type: git
      url: https://example.invalid/repo.git
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || report.Status != StatusError || ExitCode(report) != 1 {
		t.Fatalf("status=%s accepted=%v", report.Status, report.Accepted)
	}
	found := false
	for _, d := range report.Diagnostics {
		if d.Code == "no-applicable-checks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_EmptyPolicy(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", "name: demo\nmcp-servers: []\n")
	cases := []struct {
		name string
		body string
		code string
	}{
		{"empty-enabled", "version: \"1\"\nenabled: []\n", "policy-enabled"},
		{"missing-enabled", "version: \"1\"\n", "policy-enabled"},
		{"unknown-rule", "version: \"1\"\nenabled: [\"nope\"]\n", "policy-unknown-rule"},
		{"unknown-field", "version: \"1\"\nenabled: [\"deny-ssh-servers\"]\nfoo: 1\n", "policy-unknown-field"},
		{"version-int", "version: 1\nenabled: [\"deny-ssh-servers\"]\n", "policy-version"},
		{"duplicate-rule", "version: \"1\"\nenabled: [\"deny-ssh-servers\", \"deny-ssh-servers\"]\n", "policy-duplicate-rule"},
		{"interpolation", "version: \"1\"\nenabled: [\"${RULE}\"]\n", "policy-interpolation"},
		{"multi-doc", "version: \"1\"\nenabled: [\"deny-ssh-servers\"]\n---\nversion: \"1\"\n", "multiple-documents"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := write(t, dir, tc.name+".yaml", tc.body)
			report := Evaluate(t.Context(), stack, policy)
			if report.Accepted || report.Status != StatusError {
				t.Fatalf("status=%s", report.Status)
			}
			if !hasDiag(report, tc.code) {
				t.Fatalf("want %s in %+v", tc.code, report.Diagnostics)
			}
			js := mustJSON(t, report)
			var decoded Report
			if err := json.Unmarshal([]byte(js), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Accepted {
				t.Fatal("json accepted")
			}
		})
	}
}

func TestEvaluate_MissingPolicy(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", "name: demo\nmcp-servers: []\n")
	report := Evaluate(t.Context(), stack, filepath.Join(dir, "missing.yaml"))
	if report.Accepted || !hasDiag(report, "policy-missing") {
		t.Fatalf("%+v", report.Diagnostics)
	}
}

func TestEvaluate_InheritanceChildWins(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "parent.yaml", `
name: parent
mcp-servers:
  - name: img
    image: alpine:latest
    port: 1
  - name: kept
    image: `+pinnedAlpine+`
    port: 2
`)
	stack := write(t, dir, "stack.yaml", `
name: child
extends: ./parent.yaml
mcp-servers:
  - name: img
    image: `+pinnedAlpine+`
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if !report.Accepted {
		t.Fatalf("status=%s results=%v diags=%v", report.Status, report.Results, report.Diagnostics)
	}
	passes := 0
	for _, r := range report.Results {
		if r.Outcome == OutcomePass {
			passes++
		}
		if r.Outcome == OutcomeViolation {
			t.Fatalf("parent tag should not win: %+v", r)
		}
	}
	if passes != 2 {
		t.Fatalf("want 2 passes, got %+v", report.Results)
	}
}

func TestEvaluate_DynamicNameUnknown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "parent.yaml", `
name: parent
mcp-servers:
  - name: img
    image: alpine:latest
    port: 1
`)
	stack := write(t, dir, "stack.yaml", `
name: child
extends: ./parent.yaml
mcp-servers:
  - name: ${NAME}
    image: `+pinnedAlpine+`
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	if report.Status != StatusIndeterminate {
		t.Fatalf("status=%s results=%v", report.Status, report.Results)
	}
}

func TestEvaluate_ExtendsEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-"+filepath.Base(root)+".yaml")
	if err := os.WriteFile(outside, []byte("name: outside\nmcp-servers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	stack := write(t, root, "stack.yaml", "name: child\nextends: ../"+filepath.Base(outside)+"\nmcp-servers: []\n")
	policy := write(t, root, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || (!hasDiag(report, "extends-escape") && !hasDiag(report, "extends-absolute")) {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_ExtendsSymlink(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "parent.yaml", "name: parent\nmcp-servers: []\n")
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink("parent.yaml", link); err != nil {
		t.Fatal(err)
	}
	stack := write(t, dir, "stack.yaml", "name: child\nextends: ./link.yaml\nmcp-servers: []\n")
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "extends-symlink") {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_ExtendsCycle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", "name: a\nextends: ./b.yaml\nmcp-servers: []\n")
	stack := write(t, dir, "b.yaml", "name: b\nextends: ./a.yaml\nmcp-servers: []\n")
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "extends-cycle") {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_MissingParent(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", "name: child\nextends: ./missing.yaml\nmcp-servers: []\n")
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "extends-missing") {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_AbsoluteAndDynamicExtends(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	abs := write(t, dir, "abs.yaml", "name: child\nextends: /tmp/nope.yaml\nmcp-servers: []\n")
	dyn := write(t, dir, "dyn.yaml", "name: child\nextends: ./${PARENT}.yaml\nmcp-servers: []\n")
	remote := write(t, dir, "remote.yaml", "name: child\nextends: https://example.invalid/base.yaml\nmcp-servers: []\n")
	if r := Evaluate(t.Context(), abs, policy); !hasDiag(r, "extends-absolute") {
		t.Fatalf("abs %+v", r.Diagnostics)
	}
	if r := Evaluate(t.Context(), dyn, policy); !hasDiag(r, "extends-dynamic") {
		t.Fatalf("dyn %+v", r.Diagnostics)
	}
	if r := Evaluate(t.Context(), remote, policy); !hasDiag(r, "extends-remote") {
		t.Fatalf("remote %+v", r.Diagnostics)
	}
}

func TestEvaluate_MalformedServer(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: both
    image: `+pinnedAlpine+`
    url: https://example.invalid/mcp
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || report.Status != StatusError {
		t.Fatalf("status=%s", report.Status)
	}
	if !hasDiag(report, reasonMalformedServer) {
		t.Fatalf("diags %+v", report.Diagnostics)
	}
}

func TestEvaluate_CredentialCanaries(t *testing.T) {
	dir := t.TempDir()
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: local
    command: ["sh", "-c", "echo CANARY_COMMAND"]
    env:
      TOKEN: CANARY_ENV
  - name: ext
    url: https://CANARY_URL.example.invalid/mcp
    auth:
      type: bearer
      token: CANARY_TOKEN
  - name: img
    image: alpine:${CANARY_DEFAULT:-s3cret}
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenyLocalCommandServers, RuleExplicitImageDigests, RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	blob := mustJSON(t, report) + mustText(t, report)
	for _, canary := range []string{"CANARY_COMMAND", "CANARY_ENV", "CANARY_URL", "CANARY_TOKEN", "s3cret", "CANARY_DEFAULT"} {
		if strings.Contains(blob, canary) {
			t.Fatalf("leaked %s in %s", canary, blob)
		}
	}
}

func TestEvaluate_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report := Evaluate(ctx, "stack.yaml", "policy.yaml")
	if report.Accepted || ExitCode(report) != 1 {
		t.Fatalf("status=%s", report.Status)
	}
}

func TestFormatJSON_Nil(t *testing.T) {
	if err := FormatJSON(&bytes.Buffer{}, nil); err == nil {
		t.Fatal("expected error")
	}
	if ExitCode(nil) != 1 {
		t.Fatal("nil exit")
	}
}

func policyYAML(rules ...string) string {
	var b strings.Builder
	b.WriteString("version: \"1\"\nenabled:\n")
	for _, r := range rules {
		b.WriteString("  - ")
		b.WriteString(r)
		b.WriteByte('\n')
	}
	return b.String()
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasOutcome(report *Report, rule string, outcome Outcome, reason string) bool {
	for _, r := range report.Results {
		if r.Rule == rule && r.Outcome == outcome && r.ReasonCode == reason {
			return true
		}
	}
	return false
}

func hasDiag(report *Report, code string) bool {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func mustJSON(t *testing.T, report *Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := FormatJSON(&buf, report); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func mustText(t *testing.T, report *Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := FormatText(&buf, report); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
