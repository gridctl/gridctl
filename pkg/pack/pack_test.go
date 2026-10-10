package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
version: 1.0.0
description: Example pack
author:
  name: Acme
skills: [incident-triage]
agents: [reviewer]
wiring: true
clients: [claude-code]
`

func TestParse_Valid(t *testing.T) {
	m, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "team-pack" || !m.Wiring || len(m.Skills) != 1 || len(m.Agents) != 1 {
		t.Errorf("manifest = %+v", m)
	}
	if len(m.Warnings()) != 0 {
		t.Errorf("unexpected warnings: %v", m.Warnings())
	}
}

// Packs authored before the schema graduated to gridctl.dev/v1 must keep
// importing unchanged: the two versions are structurally identical and
// v1alpha1 stays accepted indefinitely (Article IX).
func TestParse_LegacyAPIVersionStillAccepted(t *testing.T) {
	src := strings.Replace(validManifest, APIVersion, LegacyAPIVersion, 1)
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("%s must still parse: %v", LegacyAPIVersion, err)
	}
	if m.APIVersion != LegacyAPIVersion {
		t.Errorf("apiVersion = %q, want the manifest's own value preserved", m.APIVersion)
	}
	if m.Name != "team-pack" || !m.Wiring {
		t.Errorf("legacy manifest decoded differently: %+v", m)
	}
}

// The rejection message names every accepted version so an author on a
// wrong apiVersion learns both spellings, not just the current one.
func TestParse_UnsupportedAPIVersionNamesBoth(t *testing.T) {
	src := strings.Replace(validManifest, APIVersion, "gridctl.dev/v2", 1)
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected an error for an unsupported apiVersion")
	}
	for _, want := range []string{APIVersion, LegacyAPIVersion} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %q", err, want)
		}
	}
}

func TestParse_Envelope(t *testing.T) {
	cases := map[string]string{
		"bad apiVersion": strings.Replace(validManifest, "gridctl.dev/v1", "gridctl.dev/v2", 1),
		"bad kind":       strings.Replace(validManifest, "kind: Pack", "kind: Bundle", 1),
		"missing name":   strings.Replace(validManifest, "name: team-pack\n", "", 1),
		"bad name":       strings.Replace(validManifest, "name: team-pack", "name: Team_Pack", 1),
	}
	for label, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected error", label)
		}
	}
}

func TestParse_RulesActiveNoWarning(t *testing.T) {
	src := validManifest + "rules: [style-guide]\n"
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("rules must parse: %v", err)
	}
	if len(m.Rules) != 1 || m.Rules[0] != "style-guide" {
		t.Fatalf("rules = %v", m.Rules)
	}
	if w := m.Warnings(); len(w) != 0 {
		t.Errorf("rules are active; warnings = %v", w)
	}
}

func TestParseFile_MissingIsNotExist(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), ManifestFileName))
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want IsNotExist", err)
	}
}

func TestValidate_RejectsBadRuleName(t *testing.T) {
	src := validManifest + "rules: [Bad_Name]\n"
	if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "rule name") {
		t.Fatalf("want rule-name validation error, got %v", err)
	}
}

func TestParse_StringSelectionsKeepLocalNames(t *testing.T) {
	m, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.SkillNames(); len(got) != 1 || got[0] != "incident-triage" {
		t.Fatalf("SkillNames = %v", got)
	}
	if got := m.AgentNames(); len(got) != 1 || got[0] != "reviewer" {
		t.Fatalf("AgentNames = %v", got)
	}
	if m.Skills[0].Source != "" || m.Agents[0].Source != "" {
		t.Fatalf("string entries must have an empty source: %+v %+v", m.Skills, m.Agents)
	}
}

func TestParse_SourceMappingAndJSONRoundTrip(t *testing.T) {
	src := `apiVersion: gridctl.dev/v1
kind: Pack
name: network-eng
sources:
  netops:
    repo: git@gitlab.example.com:network/netops-copilot.git
    ref: v2.3.0
    auth:
      method: ssh-key
      ssh_user: git
  skillsbench:
    repo: https://github.com/benchflow-ai/skillsbench
    ref: 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b
    path: tasks/dapt/skills
skills:
  - incident-triage
  - { name: noc-l2-agent, source: netops }
agents:
  - { name: neteng-reviewer, source: netops }
`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if m.Skills[0].Name != "incident-triage" || m.Skills[0].Source != "" {
		t.Fatalf("scalar skill = %+v", m.Skills[0])
	}
	if m.Skills[1].Name != "noc-l2-agent" || m.Skills[1].Source != "netops" {
		t.Fatalf("mapped skill = %+v", m.Skills[1])
	}
	if m.Sources["skillsbench"].Path != "tasks/dapt/skills" {
		t.Fatalf("path = %q", m.Sources["skillsbench"].Path)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back Manifest
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Skills[1].Name != "noc-l2-agent" || back.Skills[1].Source != "netops" {
		t.Fatalf("json round-trip skill = %+v", back.Skills[1])
	}
	if back.Sources["netops"].Auth == nil || back.Sources["netops"].Auth.Method != "ssh-key" {
		t.Fatalf("json round-trip auth = %+v", back.Sources["netops"].Auth)
	}
}

func TestParse_SelectionMappingRequiresNameAndSource(t *testing.T) {
	cases := []string{
		"skills:\n  - { source: netops }\n",
		"agents:\n  - { name: reviewer }\n",
	}
	base := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
sources:
  netops:
    repo: https://github.com/acme/netops
`
	for _, extra := range cases {
		_, err := Parse([]byte(base + extra))
		if err == nil || !strings.Contains(err.Error(), "line ") {
			t.Fatalf("src %q: want a line-numbered parse error, got %v", extra, err)
		}
	}
}

func TestParse_SourceValidationNamesKey(t *testing.T) {
	base := "apiVersion: gridctl.dev/v1\nkind: Pack\nname: team-pack\n"
	cases := []struct {
		label string
		src   string
		want  string
	}{
		{"bad source name", base + "sources:\n  NetOps:\n    repo: https://github.com/acme/netops\n", "NetOps"},
		{"file repo", base + "sources:\n  netops:\n    repo: file:///tmp/netops\n", "netops"},
		{"git scheme", base + "sources:\n  netops:\n    repo: git://github.com/acme/netops\n", "netops"},
		{"absolute repo", base + "sources:\n  netops:\n    repo: /tmp/netops\n", "netops"},
		{"relative repo", base + "sources:\n  netops:\n    repo: ./netops\n", "netops"},
		{"home repo", base + "sources:\n  netops:\n    repo: ~/netops\n", "netops"},
		{"duplicate repo", base + "sources:\n  alpha:\n    repo: https://github.com/acme/netops\n  beta:\n    repo: https://github.com/acme/netops\n", "beta"},
		{"undeclared source", base + "skills:\n  - { name: noc, source: missing }\n", "missing"},
		{"bad path", base + "sources:\n  netops:\n    repo: https://github.com/acme/netops\n    path: ../outside\n", "netops"},
		{"bad method", base + "sources:\n  netops:\n    repo: https://github.com/acme/netops\n    auth:\n      method: password\n", "netops"},
		{"ssh key path", base + "sources:\n  netops:\n    repo: ssh://git@github.com/acme/netops.git\n    auth:\n      method: ssh-key\n      ssh_key_path: /tmp/key\n", "ssh_key_path"},
	}
	for _, tc := range cases {
		_, err := Parse([]byte(tc.src))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v should name %q", tc.label, err, tc.want)
		}
	}
}
