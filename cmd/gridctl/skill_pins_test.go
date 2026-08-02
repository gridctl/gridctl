package main

import (
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

func TestValidateFailOnFindings(t *testing.T) {
	for _, ok := range []string{"", "warn", "critical"} {
		if err := validateFailOnFindings(ok); err != nil {
			t.Fatalf("validateFailOnFindings(%q) = %v", ok, err)
		}
	}
	if err := validateFailOnFindings("info"); err == nil {
		t.Fatal("'info' accepted; only warn/critical gate")
	}
}

func TestSkillFindingsAtOrAbove(t *testing.T) {
	pinned := map[string]*skillpins.SkillPin{
		"quiet": {Status: skillpins.StatusPinned},
		"noisy": {Status: skillpins.StatusPinned, Findings: []pins.Finding{
			{Code: "P004", Severity: pins.SeverityInfo},
			{Code: "P001", Severity: pins.SeverityWarn},
		}},
	}
	if skillFindingsAtOrAbove(pinned, []string{"quiet"}, pins.SeverityWarn) {
		t.Fatal("finding-free skill tripped the gate")
	}
	if !skillFindingsAtOrAbove(pinned, []string{"noisy"}, pins.SeverityWarn) {
		t.Fatal("warn finding did not trip the warn gate")
	}
	if skillFindingsAtOrAbove(pinned, []string{"noisy"}, pins.SeverityCritical) {
		t.Fatal("warn finding tripped the critical gate")
	}
}

func TestRenderSkillPinsDiffText(t *testing.T) {
	doc := skillPinsDiffDoc{
		Skill:           "alpha",
		Status:          skillpins.StatusDrift,
		CompositeHash:   "abc",
		DocumentChanged: true,
		OldDocument:     "old",
		NewDocument:     "new-longer",
		AddedFiles:      []string{"scripts/new.sh"},
		RemovedFiles:    []string{"references/old.md"},
		ModifiedFiles:   []string{"assets/logo.svg"},
		Findings: []pins.Finding{
			{Code: "P001", Severity: pins.SeverityWarn, Field: "body", Message: "hidden-instruction phrasing"},
		},
	}
	var b strings.Builder
	renderSkillPinsDiffText(&b, doc)
	out := b.String()
	for _, want := range []string{
		"pin drift", "SKILL.md changed",
		"+ scripts/new.sh", "- references/old.md", "~ assets/logo.svg",
		"P001", "--expect abc",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff text missing %q:\n%s", want, out)
		}
	}

	clean := skillPinsDiffDoc{Skill: "alpha", Status: skillpins.StatusPinned}
	b.Reset()
	renderSkillPinsDiffText(&b, clean)
	if !strings.Contains(b.String(), "verified") {
		t.Fatalf("clean diff text = %q", b.String())
	}
}
