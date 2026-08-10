package pack

import (
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
