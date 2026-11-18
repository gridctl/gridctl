package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/state"
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

	when := time.Date(2026, 10, 2, 15, 4, 5, 0, time.UTC)
	doc.LastApproval = &pinapprovals.Entry{
		ApprovedAt:       when,
		ApprovedBy:       "wcollins",
		ApprovedBySource: pinapprovals.SourceDeclared,
		Reason:           "Adds runbook step 4",
		Evidence:         []string{"https://example.test/pr/1"},
	}
	b.Reset()
	renderSkillPinsDiffText(&b, doc)
	out = b.String()
	for _, want := range []string{
		"last approved 2026-10-02T15:04:05Z by wcollins (declared)",
		"Adds runbook step 4",
		"https://example.test/pr/1",
		`--reason "<why>"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff text missing %q:\n%s", want, out)
		}
	}
}

// seedSkillPinsListHome writes skill alpha into a temp home's registry, pins
// it for stack "lab", and returns the registry directory.
func seedSkillPinsListHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	prev := skillPinsStack
	skillPinsStack = "lab"
	t.Cleanup(func() { skillPinsStack = prev })

	regDir := filepath.Join(home, ".gridctl", "registry")
	writeAlphaSkill(t, regDir, "Nothing suspicious here.")
	reg := registry.NewStore(regDir)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := skillpins.New("lab").Sync(reg); err != nil {
		t.Fatal(err)
	}
	return regDir
}

func writeAlphaSkill(t *testing.T, regDir, body string) {
	t.Helper()
	skillDir := filepath.Join(regDir, "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "---\nname: alpha\ndescription: A quiet helper.\nstate: active\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshotPinsDir returns every file under ~/.gridctl/pins with its bytes.
func snapshotPinsDir(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(os.Getenv(state.HomeEnv), ".gridctl", "pins")
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRunSkillPinsList_ReportsDriftWithoutSync(t *testing.T) {
	regDir := seedSkillPinsListHome(t)
	writeAlphaSkill(t, regDir, "Something changed on disk.")
	before := snapshotPinsDir(t)
	if len(before) == 0 {
		t.Fatal("no pin files written by the seed sync")
	}

	var stdout bytes.Buffer
	if err := runSkillPinsList(&stdout, "json"); err != nil {
		t.Fatal(err)
	}
	var doc skillPinsListDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", stdout.String(), err)
	}
	if doc.Stack != "lab" || doc.SchemaVersion != 1 {
		t.Fatalf("doc header = %d %q", doc.SchemaVersion, doc.Stack)
	}
	if got := doc.Skills["alpha"]; got == nil || got.Status != skillpins.StatusDrift {
		t.Fatalf("alpha = %+v, want drift", got)
	}

	var vout, verr strings.Builder
	if code := runSkillPinsVerify(&vout, &verr, "", "json", ""); code != pinsExitDrift {
		t.Fatalf("verify exit = %d, want %d\n%s%s", code, pinsExitDrift, vout.String(), verr.String())
	}

	var table bytes.Buffer
	if err := runSkillPinsList(&table, "table"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), pinStatusLabel(skillpins.StatusDrift)) {
		t.Fatalf("table lacks drift label:\n%s", table.String())
	}

	if after := snapshotPinsDir(t); !maps.Equal(before, after) {
		t.Fatal("list changed files under the pins directory")
	}
	fresh := skillpins.New("lab")
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	if pin, ok := fresh.Get("alpha"); !ok || pin.Status != skillpins.StatusPinned {
		t.Fatalf("stored alpha = %+v, want pinned", pin)
	}
}

func TestRunSkillPinsList_JSONShape(t *testing.T) {
	seedSkillPinsListHome(t)

	var stdout bytes.Buffer
	if err := runSkillPinsList(&stdout, "json"); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(raw)); !slices.Equal(got, []string{"schema_version", "skills", "stack"}) {
		t.Fatalf("document keys = %v", got)
	}
	var skills map[string]json.RawMessage
	if err := json.Unmarshal(raw["skills"], &skills); err != nil {
		t.Fatal(err)
	}
	// The record encodes exactly as a stored SkillPin does.
	stored := skillpins.New("lab")
	if err := stored.Load(); err != nil {
		t.Fatal(err)
	}
	pin, _ := stored.Get("alpha")
	want, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	var gotPin, wantPin map[string]any
	if err := json.Unmarshal(skills["alpha"], &gotPin); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantPin); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPin, wantPin) {
		t.Fatalf("listed pin = %v\nstored pin = %v", gotPin, wantPin)
	}
}

func TestRunSkillPinsList_MissingSkillKeepsStoredStatus(t *testing.T) {
	regDir := seedSkillPinsListHome(t)
	if err := os.RemoveAll(filepath.Join(regDir, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runSkillPinsList(&stdout, "json"); err != nil {
		t.Fatal(err)
	}
	var doc skillPinsListDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if got := doc.Skills["alpha"]; got == nil || got.Status != skillpins.StatusPinned {
		t.Fatalf("alpha = %+v, want stored pinned", got)
	}

	var vout, verr strings.Builder
	runSkillPinsVerify(&vout, &verr, "", "json", "")
	var vdoc skillPinsVerifyDoc
	if err := json.Unmarshal([]byte(vout.String()), &vdoc); err != nil {
		t.Fatalf("decode %s: %v", vout.String(), err)
	}
	if !slices.Equal(vdoc.Missing, []string{"alpha"}) {
		t.Fatalf("verify missing = %v", vdoc.Missing)
	}
}

func TestRunSkillPinsList_EmptyWritesToWriter(t *testing.T) {
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	prev := skillPinsStack
	skillPinsStack = "lab"
	t.Cleanup(func() { skillPinsStack = prev })

	var stdout bytes.Buffer
	if err := runSkillPinsList(&stdout, "table"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "No skill pins found for stack 'lab'") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
