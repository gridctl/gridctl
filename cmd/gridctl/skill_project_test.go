package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillsync"
)

// newSkillProjectTestManager returns a projection Manager rooted at a
// temp home with one active skill "demo" in the registry and ~/.claude
// pre-created.
func newSkillProjectTestManager(t *testing.T) (*skillsync.Manager, string) {
	t.Helper()
	home := t.TempDir()
	regDir := filepath.Join(home, ".gridctl", "registry")
	skillDir := filepath.Join(regDir, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo\ndescription: Demo skill\nstate: active\n---\nDemo body.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return skillsync.NewManagerWithHome(home, store), home
}

func TestRunSkillProjectSyncTableAndJSON(t *testing.T) {
	mgr, home := newSkillProjectTestManager(t)
	var out, errOut bytes.Buffer

	opts := skillsync.SyncOptions{Clients: []string{"claude-code"}}
	exit := runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"demo"}, opts, "", true)
	if exit != ctxExitOK {
		t.Fatalf("exit = %d, stderr: %s", exit, errOut.String())
	}
	if !strings.Contains(out.String(), "linked") {
		t.Errorf("table output missing linked action: %q", out.String())
	}
	if _, err := os.Readlink(filepath.Join(home, ".claude", "skills", "demo")); err != nil {
		t.Fatalf("projection not created: %v", err)
	}

	out.Reset()
	exit = runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"demo"}, opts, "json", false)
	if exit != ctxExitOK {
		t.Fatalf("json exit = %d", exit)
	}
	var doc skillProjectSyncDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if doc.SchemaVersion != skillProjectJSONSchemaVersion {
		t.Errorf("schema_version = %d", doc.SchemaVersion)
	}
	if len(doc.Results) != 1 || doc.Results[0].Action != skillsync.ActionUnchanged {
		t.Errorf("results = %+v", doc.Results)
	}
}

func TestRunSkillProjectSyncErrorsAreInfrastructure(t *testing.T) {
	mgr, _ := newSkillProjectTestManager(t)
	var out, errOut bytes.Buffer
	exit := runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"nope"}, skillsync.SyncOptions{}, "", true)
	if exit != ctxExitInfrastructure {
		t.Errorf("unknown skill exit = %d, want %d", exit, ctxExitInfrastructure)
	}
	if !strings.Contains(errOut.String(), "nope (not found)") {
		t.Errorf("stderr missing offender: %q", errOut.String())
	}
}

func TestRunSkillProjectSyncFailureExitCode(t *testing.T) {
	mgr, home := newSkillProjectTestManager(t)
	dest := filepath.Join(home, ".claude", "skills", "demo")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	opts := skillsync.SyncOptions{Clients: []string{"claude-code"}}
	exit := runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"demo"}, opts, "", true)
	if exit != ctxExitAttention {
		t.Errorf("unmanaged-path exit = %d, want %d", exit, ctxExitAttention)
	}
	if !strings.Contains(out.String(), "not managed by gridctl") {
		t.Errorf("output missing guidance: %q", out.String())
	}
}

func TestRunSkillProjectSyncCoScanWarning(t *testing.T) {
	mgr, home := newSkillProjectTestManager(t)
	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	opts := skillsync.SyncOptions{Clients: []string{"claude-code", "agents"}}
	exit := runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"demo"}, opts, "", true)
	if exit != ctxExitOK {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(errOut.String(), "discover these skills twice") {
		t.Errorf("stderr missing co-scan warning: %q", errOut.String())
	}
}

func TestRunSkillProjectStatusJSONAndExitCodes(t *testing.T) {
	mgr, home := newSkillProjectTestManager(t)
	var out, errOut bytes.Buffer

	// Empty set: clean exit, guidance message.
	exit := runSkillProjectStatus(context.Background(), &out, &errOut, mgr, "", true)
	if exit != ctxExitOK {
		t.Fatalf("empty status exit = %d", exit)
	}
	if !strings.Contains(out.String(), "Nothing projected yet") {
		t.Errorf("output missing guidance: %q", out.String())
	}

	opts := skillsync.SyncOptions{Clients: []string{"claude-code"}}
	if exit := runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"demo"}, opts, "", true); exit != ctxExitOK {
		t.Fatal("sync failed")
	}

	out.Reset()
	exit = runSkillProjectStatus(context.Background(), &out, &errOut, mgr, "json", false)
	if exit != ctxExitOK {
		t.Fatalf("status exit = %d", exit)
	}
	var doc skillProjectStatusDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.NeedsAttention || len(doc.Projections) != 1 || doc.Projections[0].State != skillsync.StateInSync {
		t.Errorf("doc = %+v", doc)
	}

	// Break the projection: exit 1.
	if err := os.Remove(filepath.Join(home, ".claude", "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	exit = runSkillProjectStatus(context.Background(), &out, &errOut, mgr, "", true)
	if exit != ctxExitAttention {
		t.Errorf("missing-target exit = %d, want %d", exit, ctxExitAttention)
	}
	if !strings.Contains(out.String(), "target-missing") {
		t.Errorf("output missing state: %q", out.String())
	}
}

func TestRunSkillProjectUnsync(t *testing.T) {
	mgr, home := newSkillProjectTestManager(t)
	var out, errOut bytes.Buffer
	opts := skillsync.SyncOptions{Clients: []string{"claude-code"}}
	if exit := runSkillProjectSync(context.Background(), &out, &errOut, mgr, []string{"demo"}, opts, "", true); exit != ctxExitOK {
		t.Fatal("sync failed")
	}

	out.Reset()
	if err := runSkillProjectUnsync(context.Background(), &out, mgr, nil, skillsync.UnsyncOptions{All: true}, "json"); err != nil {
		t.Fatal(err)
	}
	var doc skillProjectUnsyncDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(doc.Results) != 1 || doc.Results[0].Action != skillsync.ActionRemoved {
		t.Errorf("results = %+v", doc.Results)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "demo")); !os.IsNotExist(err) {
		t.Error("projection must be removed")
	}

	// Unsyncing an unprojected skill surfaces guidance.
	err := runSkillProjectUnsync(context.Background(), &out, mgr, []string{"demo"}, skillsync.UnsyncOptions{}, "")
	if err == nil || !strings.Contains(err.Error(), "skill project status") {
		t.Errorf("expected not-projected guidance, got %v", err)
	}
}

func TestWarnCoScannedDuplicates(t *testing.T) {
	var buf bytes.Buffer
	warnCoScannedDuplicates(&buf, []string{"demo"}, []string{"claude-code"})
	if buf.Len() != 0 {
		t.Errorf("single target must not warn: %q", buf.String())
	}
	warnCoScannedDuplicates(&buf, nil, []string{"claude-code", "agents"})
	if buf.Len() != 0 {
		t.Errorf("reconcile (no names) must not warn: %q", buf.String())
	}
	warnCoScannedDuplicates(&buf, []string{"demo"}, []string{"agents", "claude-code"})
	if buf.Len() == 0 {
		t.Error("dual-root projection must warn")
	}
}

func TestSkillProjectLabels(t *testing.T) {
	for action, prefix := range map[string]string{
		skillsync.ActionLinked:           "✓",
		skillsync.ActionSkippedDrift:     "✗",
		skillsync.ActionWouldCopy:        "—",
		skillsync.ActionSkippedUnmanaged: "✗",
	} {
		if got := skillProjectActionLabel(action); !strings.HasPrefix(got, prefix) {
			t.Errorf("label(%s) = %q, want prefix %q", action, got, prefix)
		}
	}
	for state, prefix := range map[string]string{
		skillsync.StateInSync:        "✓",
		skillsync.StateStale:         "~",
		skillsync.StateDrifted:       "✗",
		skillsync.StateTargetMissing: "✗",
	} {
		if got := skillProjectStateLabel(skillsync.ProjectionStatus{State: state}); !strings.HasPrefix(got, prefix) {
			t.Errorf("label(%s) = %q, want prefix %q", state, got, prefix)
		}
	}
	got := skillProjectStateLabel(skillsync.ProjectionStatus{State: skillsync.StateInSync, Experimental: true})
	if !strings.Contains(got, "(experimental)") {
		t.Errorf("experimental marker missing: %q", got)
	}
}
