package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skillsync"
)

func TestRunSkillProjectAdoptExitCodesAndOutput(t *testing.T) {
	mgr, home := newSkillProjectTestManager(t)
	ctx := context.Background()
	var out, errOut bytes.Buffer

	// Not projected: exit 1 with the status-check suffix.
	exit := runSkillProjectAdopt(ctx, &out, &errOut, mgr, "demo", "antigravity", "")
	if exit != ctxExitAttention || !strings.Contains(errOut.String(), "skill project status") {
		t.Fatalf("not-projected exit = %d, stderr = %q", exit, errOut.String())
	}

	// Unknown client: exit 2.
	errOut.Reset()
	if exit := runSkillProjectAdopt(ctx, &out, &errOut, mgr, "demo", "nope", ""); exit != ctxExitInfrastructure {
		t.Fatalf("unknown-client exit = %d", exit)
	}

	// Symlink projection: exit 1 with the source-of-truth wording.
	opts := skillsync.SyncOptions{Clients: []string{"claude-code"}}
	if exit := runSkillProjectSync(ctx, &out, &errOut, mgr, []string{"demo"}, opts, "", true); exit != ctxExitOK {
		t.Fatal("sync failed")
	}
	errOut.Reset()
	exit = runSkillProjectAdopt(ctx, &out, &errOut, mgr, "demo", "claude-code", "")
	if exit != ctxExitAttention || !strings.Contains(errOut.String(), "the registry copy is the source of truth, so there is nothing to adopt") {
		t.Fatalf("symlink exit = %d, stderr = %q", exit, errOut.String())
	}

	// A hand-edited copy adopts cleanly: exit 0, names the files, and
	// reminds about staleness and skill update.
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts = skillsync.SyncOptions{Clients: []string{"antigravity"}}
	if exit := runSkillProjectSync(ctx, &out, &errOut, mgr, []string{"demo"}, opts, "", true); exit != ctxExitOK {
		t.Fatal("copy sync failed")
	}
	copyPath := filepath.Join(home, ".gemini", "config", "skills", "demo", "SKILL.md")
	edited := "---\nname: demo\ndescription: Demo skill\nstate: active\n---\nDemo body, edited.\n"
	if err := os.WriteFile(copyPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if exit := runSkillProjectAdopt(ctx, &out, &errOut, mgr, "demo", "antigravity", ""); exit != ctxExitOK {
		t.Fatalf("adopt exit = %d, stderr = %q", exit, errOut.String())
	}
	for _, want := range []string{"Adopted antigravity's copy of demo", "updated: SKILL.md", "now stale", "local edits"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("adopt output missing %q:\n%s", want, out.String())
		}
	}

	// Re-running with no new edits reports the no-op, and the JSON form
	// carries the structured result.
	out.Reset()
	if exit := runSkillProjectAdopt(ctx, &out, &errOut, mgr, "demo", "antigravity", ""); exit != ctxExitOK {
		t.Fatalf("no-op adopt exit = %d", exit)
	}
	if !strings.Contains(out.String(), "already matches the registry") {
		t.Errorf("no-op output = %q", out.String())
	}

	edited2 := strings.Replace(edited, "edited", "edited twice", 1)
	if err := os.WriteFile(copyPath, []byte(edited2), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if exit := runSkillProjectAdopt(ctx, &out, &errOut, mgr, "demo", "antigravity", "json"); exit != ctxExitOK {
		t.Fatalf("json adopt exit = %d, stderr = %q", exit, errOut.String())
	}
	var doc skillProjectAdoptDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if doc.SchemaVersion != skillProjectJSONSchemaVersion || doc.Result == nil ||
		doc.Result.Skill != "demo" || doc.Result.Client != "antigravity" ||
		len(doc.Result.ChangedFiles) != 1 || doc.Result.ChangedFiles[0] != "SKILL.md" {
		t.Errorf("adopt JSON doc = %+v", doc)
	}
}
