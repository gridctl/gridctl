package stackpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluate_YAMLMergeKeyRejected(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleSchemaPinningEnabled))
	mergedPin := write(t, dir, "merge-pin.yaml", `
name: demo
mcp-servers:
  - name: server
    image: alpine:latest
    <<: {pin_schemas: false}
`)
	if r := Evaluate(t.Context(), mergedPin, policy); r.Accepted || !hasDiag(r, "yaml-merge") {
		t.Fatalf("merged pin: status=%s diags=%+v", r.Status, r.Diagnostics)
	}
	write(t, dir, "parent.yaml", "name: parent\nmcp-servers:\n  - name: a\n    command: [\"npx\"]\n")
	mergedExtends := write(t, dir, "merge-extends.yaml", `
name: child
<<:
  extends: parent.yaml
mcp-servers:
  - name: a
    image: alpine:latest
    port: 1
`)
	if r := Evaluate(t.Context(), mergedExtends, write(t, dir, "local.yaml", policyYAML(RuleDenyLocalCommandServers))); r.Accepted || !hasDiag(r, "yaml-merge") {
		t.Fatalf("merged extends: status=%s diags=%+v", r.Status, r.Diagnostics)
	}
}

func TestEvaluate_DynamicKindIsUnknownForProhibitions(t *testing.T) {
	dir := t.TempDir()
	localPolicy := write(t, dir, "local.yaml", policyYAML(RuleDenyLocalCommandServers))
	sshPolicy := write(t, dir, "ssh.yaml", policyYAML(RuleDenySSHServers))
	imagePolicy := write(t, dir, "img.yaml", policyYAML(RuleExplicitImageDigests))
	cases := []struct {
		name string
		body string
	}{
		{"bare-image", "name: demo\nmcp-servers:\n  - name: server\n    image: $IMAGE\n    command: [\"server\"]\n"},
		{"braced-image", "name: demo\nmcp-servers:\n  - name: server\n    image: \"${IMAGE}\"\n    command: [\"server\"]\n"},
		{"default-image", "name: demo\nmcp-servers:\n  - name: server\n    image: \"${IMAGE:-alpine:latest}\"\n    command: [\"server\"]\n"},
		{"replace-image", "name: demo\nmcp-servers:\n  - name: server\n    image: \"${IMAGE:+alpine:latest}\"\n    command: [\"server\"]\n"},
		{"bare-url", "name: demo\nmcp-servers:\n  - name: server\n    url: $URL\n    command: [\"server\"]\n"},
		{"braced-url", "name: demo\nmcp-servers:\n  - name: server\n    url: \"${URL}\"\n    command: [\"server\"]\n"},
		{"default-url", "name: demo\nmcp-servers:\n  - name: server\n    url: \"${URL:-https://example.invalid/mcp}\"\n    command: [\"server\"]\n"},
		{"replace-url", "name: demo\nmcp-servers:\n  - name: server\n    url: \"${URL:+https://example.invalid/mcp}\"\n    command: [\"server\"]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stack := write(t, dir, tc.name+".yaml", tc.body)
			local := Evaluate(t.Context(), stack, localPolicy)
			if local.Accepted || !hasOutcome(local, RuleDenyLocalCommandServers, OutcomeUnknown, reasonUnknownApplicability) {
				t.Fatalf("local status=%s results=%+v", local.Status, local.Results)
			}
			ssh := Evaluate(t.Context(), stack, sshPolicy)
			if ssh.Accepted || !hasOutcome(ssh, RuleDenySSHServers, OutcomeUnknown, reasonUnknownApplicability) {
				t.Fatalf("ssh status=%s results=%+v", ssh.Status, ssh.Results)
			}
			if strings.Contains(tc.name, "image") {
				img := Evaluate(t.Context(), stack, imagePolicy)
				if img.Accepted || !hasOutcome(img, RuleExplicitImageDigests, OutcomeUnknown, reasonUnresolvedImage) {
					t.Fatalf("image status=%s results=%+v", img.Status, img.Results)
				}
			}
		})
	}
}

func TestEvaluate_SameFileDuplicateName(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenyLocalCommandServers))
	stack := write(t, dir, "stack.yaml", `
name: demo
mcp-servers:
  - name: server
    image: alpine:latest
    port: 1
  - name: server
    command: ["server"]
`)
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "duplicate-name") {
		t.Fatalf("status=%s diags=%+v results=%+v", report.Status, report.Diagnostics, report.Results)
	}
	resources := write(t, dir, "res.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
resources:
  - name: db
    image: alpine:latest
  - name: db
    image: alpine:latest
`)
	resReport := Evaluate(t.Context(), resources, write(t, dir, "img.yaml", policyYAML(RuleExplicitImageDigests)))
	if resReport.Accepted || !hasDiag(resReport, "duplicate-name") {
		t.Fatalf("resource status=%s diags=%+v", resReport.Status, resReport.Diagnostics)
	}
	write(t, dir, "parent.yaml", `
name: parent
mcp-servers:
  - name: other
    command: ["server"]
  - name: other
    image: alpine:latest
    port: 1
`)
	child := write(t, dir, "child.yaml", `
name: child
extends: ./parent.yaml
mcp-servers:
  - name: kept
    image: `+pinnedAlpine+`
    port: 1
`)
	parentDup := Evaluate(t.Context(), child, policy)
	if parentDup.Accepted || !hasDiag(parentDup, "duplicate-name") {
		t.Fatalf("parent dup status=%s diags=%+v", parentDup.Status, parentDup.Diagnostics)
	}
}

func TestEvaluate_NullToolEntries(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleNonemptyServerToolLists))
	nullOnly := write(t, dir, "null.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
    tools: [null]
`)
	if r := Evaluate(t.Context(), nullOnly, policy); r.Accepted || r.Status != StatusRejected {
		t.Fatalf("null-only status=%s results=%+v", r.Status, r.Results)
	}
	if r := Evaluate(t.Context(), nullOnly, policy); !hasOutcome(r, RuleNonemptyServerToolLists, OutcomeViolation, reasonEmptyToolName) && !hasOutcome(r, RuleNonemptyServerToolLists, OutcomeViolation, reasonEmptyTools) {
		t.Fatalf("null-only results=%+v", r.Results)
	}
	tilde := write(t, dir, "tilde.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
    tools: [~]
`)
	if r := Evaluate(t.Context(), tilde, policy); r.Accepted {
		t.Fatalf("tilde accepted: %+v", r.Results)
	}
	quoted := write(t, dir, "quoted.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
    tools: ["null"]
`)
	if r := Evaluate(t.Context(), quoted, policy); !r.Accepted {
		t.Fatalf("quoted null status=%s results=%+v diags=%+v", r.Status, r.Results, r.Diagnostics)
	}
	empty := write(t, dir, "empty-name.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
    tools: [""]
`)
	if r := Evaluate(t.Context(), empty, policy); !hasOutcome(r, RuleNonemptyServerToolLists, OutcomeViolation, reasonEmptyToolName) {
		t.Fatalf("empty name results=%+v", r.Results)
	}
	mixed := write(t, dir, "mixed.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
    tools: [null, "read"]
`)
	if r := Evaluate(t.Context(), mixed, policy); r.Accepted || !hasOutcome(r, RuleNonemptyServerToolLists, OutcomeViolation, reasonEmptyToolName) {
		t.Fatalf("mixed results=%+v", r.Results)
	}
	invalid := write(t, dir, "invalid.yaml", `
name: demo
mcp-servers:
  - name: a
    command: ["server"]
    tools:
      - {name: read}
`)
	if r := Evaluate(t.Context(), invalid, policy); r.Accepted || r.Status != StatusError {
		t.Fatalf("invalid entry status=%s diags=%+v", r.Status, r.Diagnostics)
	}
}

func TestEvaluate_MalformedGatewayAndUnknownPinField(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleSchemaPinningEnabled))
	invalid := write(t, dir, "gw.yaml", `
name: demo
gateway: invalid
mcp-servers:
  - name: a
    command: ["server"]
`)
	if r := Evaluate(t.Context(), invalid, policy); r.Accepted || r.Status != StatusError {
		t.Fatalf("gateway scalar status=%s diags=%+v results=%+v", r.Status, r.Diagnostics, r.Results)
	}
	typo := write(t, dir, "typo.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    pin_schema: false
`)
	if r := Evaluate(t.Context(), typo, policy); r.Accepted || r.Status != StatusIndeterminate {
		t.Fatalf("pin_schema typo status=%s results=%+v", r.Status, r.Results)
	}
	transport := write(t, dir, "transport.yaml", `
name: demo
mcp-servers:
  - name: a
    image: `+pinnedAlpine+`
    port: 1
    transport: exotic
`)
	if r := Evaluate(t.Context(), transport, policy); r.Accepted || r.Status != StatusIndeterminate {
		t.Fatalf("unknown transport status=%s results=%+v", r.Status, r.Results)
	}
}

func TestEvaluate_AncestorSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, realDir, "parent.yaml", "name: parent\nmcp-servers:\n  - name: a\n    image: "+pinnedAlpine+"\n    port: 1\n")
	if err := os.Symlink("real", filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	stack := write(t, dir, "stack.yaml", "name: child\nextends: alias/parent.yaml\nmcp-servers:\n  - name: a\n    image: "+pinnedAlpine+"\n    port: 1\n")
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "extends-symlink") {
		t.Fatalf("in-root ancestor: status=%s diags=%+v", report.Status, report.Diagnostics)
	}
	outside := t.TempDir()
	write(t, outside, "escaped.yaml", "name: escaped\nmcp-servers: []\n")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	esc := write(t, dir, "esc.yaml", "name: child\nextends: escape/escaped.yaml\nmcp-servers: []\n")
	escReport := Evaluate(t.Context(), esc, policy)
	if escReport.Accepted || (!hasDiag(escReport, "extends-symlink") && !hasDiag(escReport, "extends-escape")) {
		t.Fatalf("escaping ancestor: status=%s diags=%+v", escReport.Status, escReport.Diagnostics)
	}
}

func TestEvaluate_PolicyHardLinkRejected(t *testing.T) {
	dir := t.TempDir()
	policy := write(t, dir, "policy.yaml", policyYAML(RuleDenySSHServers))
	link := filepath.Join(dir, "policy-link.yaml")
	if err := os.Link(policy, link); err != nil {
		t.Fatal(err)
	}
	stack := write(t, dir, "stack.yaml", "name: child\nextends: ./policy-link.yaml\nmcp-servers:\n  - name: a\n    image: "+pinnedAlpine+"\n    port: 1\n")
	report := Evaluate(t.Context(), stack, policy)
	if report.Accepted || !hasDiag(report, "extends-policy") {
		t.Fatalf("hard link: status=%s diags=%+v", report.Status, report.Diagnostics)
	}
}

func TestEvaluate_SourceIndexAndTextProvenance(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "parent.yaml", `
name: parent
mcp-servers:
  - name: kept
    image: `+pinnedAlpine+`
    port: 2
`)
	stack := write(t, dir, "stack.yaml", `
name: child
extends: ./parent.yaml
mcp-servers:
  - name: child
    image: alpine:latest
    port: 1
`)
	policy := write(t, dir, "policy.yaml", policyYAML(RuleExplicitImageDigests))
	report := Evaluate(t.Context(), stack, policy)
	var childLoc, parentLoc Location
	for _, res := range report.Results {
		if res.Outcome == OutcomeViolation {
			childLoc = res.Location
		}
		if res.Outcome == OutcomePass {
			parentLoc = res.Location
		}
	}
	if childLoc.Path != "mcp-servers[0].image" || childLoc.Source != "input-0" {
		t.Fatalf("child loc %+v", childLoc)
	}
	if childLoc.Line == 0 || childLoc.Column == 0 {
		t.Fatalf("child field coords %+v", childLoc)
	}
	if parentLoc.Path != "mcp-servers[0].image" || parentLoc.Source != "input-1" {
		t.Fatalf("parent loc %+v (merged index must not replace source index)", parentLoc)
	}
	text := mustText(t, report)
	if !strings.Contains(text, "enabled: explicit-image-digests") {
		t.Fatalf("missing enabled rules: %s", text)
	}
	if !strings.Contains(text, "reason: mutable-tag") {
		t.Fatalf("missing reason code: %s", text)
	}
	if !strings.Contains(text, "mcp-servers[0].image:") {
		t.Fatalf("missing line/column: %s", text)
	}
}

func TestRejectPolicyReuse(t *testing.T) {
	id := fileID{}
	if err := rejectPolicyReuse(false, false, id, id); err != nil {
		t.Fatal(err)
	}
	if err := rejectPolicyReuse(true, false, id, id); err == nil || errorCode(err) != "extends-policy" {
		t.Fatalf("want extends-policy, got %v", err)
	}
}
