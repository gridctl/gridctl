package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validManifest = `apiVersion: gridctl.dev/v1alpha1
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

func TestParse_Envelope(t *testing.T) {
	cases := map[string]string{
		"bad apiVersion": strings.Replace(validManifest, "gridctl.dev/v1alpha1", "gridctl.dev/v2", 1),
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

func TestParse_RulesReservedWarns(t *testing.T) {
	src := validManifest + "rules: [style-guide]\n"
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("reserved rules must parse: %v", err)
	}
	w := m.Warnings()
	if len(w) != 1 || !strings.Contains(w[0], "reserved") {
		t.Errorf("warnings = %v", w)
	}
}

func TestParseFile_MissingIsNotExist(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), ManifestFileName))
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want IsNotExist", err)
	}
}
