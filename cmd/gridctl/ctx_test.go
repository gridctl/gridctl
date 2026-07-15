package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/contexts"
)

// newCtxTestManager returns a Manager rooted at a temp home with the
// given client detect dirs pre-created.
func newCtxTestManager(t *testing.T, detectDirs ...string) *contexts.Manager {
	t.Helper()
	home := t.TempDir()
	for _, d := range detectDirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return contexts.NewManagerWithHome(home)
}

// ctxHome recovers the temp home from the manager's canonical path
// (<home>/.gridctl/context/AGENTS.md).
func ctxHome(mgr *contexts.Manager) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(mgr.CanonicalPath())))
}

func TestRunCtxInitScaffoldsWhenNothingExists(t *testing.T) {
	mgr := newCtxTestManager(t)
	var out bytes.Buffer

	if err := runCtxInit(&out, mgr, "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if !mgr.HasCanonical() {
		t.Fatal("canonical file not scaffolded")
	}
	if !strings.Contains(out.String(), "starter template") {
		t.Errorf("output missing template note: %q", out.String())
	}

	// Second run refuses to overwrite.
	if err := runCtxInit(&out, mgr, "", "", true, false); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}

func TestRunCtxInitReportsExistingAndDoesNotWrite(t *testing.T) {
	mgr := newCtxTestManager(t, ".gemini")
	path := filepath.Join(ctxHome(mgr), ".gemini", "GEMINI.md")
	if err := os.WriteFile(path, []byte("# existing\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCtxInit(&out, mgr, "", "", false, false); err != nil {
		t.Fatal(err)
	}
	if mgr.HasCanonical() {
		t.Error("init wrote the canonical file despite existing client files")
	}
	if !strings.Contains(out.String(), "--import <client>") {
		t.Errorf("output missing adoption guidance: %q", out.String())
	}
}

func TestRunCtxInitImportsClientFile(t *testing.T) {
	mgr := newCtxTestManager(t, ".gemini")
	if err := os.WriteFile(filepath.Join(ctxHome(mgr), ".gemini", "GEMINI.md"), []byte("# my rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCtxInit(&out, mgr, "gemini", "", false, false); err != nil {
		t.Fatal(err)
	}
	content, err := mgr.CanonicalContent()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "# my rules") {
		t.Errorf("import missed content: %q", content)
	}
}

func TestRunCtxStatusJSONAndExitCodes(t *testing.T) {
	mgr := newCtxTestManager(t, ".claude")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	if exit := runCtxStatus(ctx, &stdout, &stderr, mgr, "json", false); exit != ctxExitOK {
		t.Fatalf("clean status exit = %d, want 0 (stderr: %s)", exit, stderr.String())
	}
	var doc ctxStatusDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if doc.SchemaVersion != ctxJSONSchemaVersion || !doc.Canonical.Exists {
		t.Errorf("unexpected doc: %+v", doc)
	}
	if len(doc.Clients) == 0 {
		t.Fatal("no clients in status doc")
	}

	// Sync then delete the target: status must exit 1.
	res, err := mgr.SyncClient(ctx, "claude-code", contexts.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(res.TargetPath); err != nil {
		t.Fatal(err)
	}
	if exit := runCtxStatus(ctx, &bytes.Buffer{}, &stderr, mgr, "json", false); exit != ctxExitAttention {
		t.Errorf("missing-target status exit = %d, want 1", exit)
	}
}

func TestRunCtxSyncTableAndJSON(t *testing.T) {
	mgr := newCtxTestManager(t, ".claude", ".gemini")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	exit := runCtxSync(ctx, &stdout, &stderr, mgr, nil, contexts.SyncOptions{}, "json", false)
	if exit != ctxExitOK {
		t.Fatalf("sync exit = %d, want 0 (stderr: %s)", exit, stderr.String())
	}
	var doc ctxSyncDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if doc.HasFailures {
		t.Errorf("unexpected failures: %+v", doc.Results)
	}
	actions := map[string]string{}
	for _, r := range doc.Results {
		actions[r.Slug] = r.Action
	}
	if actions["claude-code"] != contexts.ActionCreated || actions["gemini"] != contexts.ActionCreated {
		t.Errorf("unexpected actions: %v", actions)
	}
	if actions["opencode"] != contexts.ActionSkippedUnavailable {
		t.Errorf("opencode action = %q, want skipped-unavailable", actions["opencode"])
	}

	// Table output for a named client.
	stdout.Reset()
	exit = runCtxSync(ctx, &stdout, &stderr, mgr, []string{"claude-code"}, contexts.SyncOptions{}, "", true)
	if exit != ctxExitOK {
		t.Fatalf("named sync exit = %d", exit)
	}
	if !strings.Contains(stdout.String(), "claude-code") || !strings.Contains(stdout.String(), "unchanged") {
		t.Errorf("table output unexpected: %q", stdout.String())
	}
}

func TestRunCtxSyncDriftExitsOne(t *testing.T) {
	mgr := newCtxTestManager(t, ".config/opencode")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := mgr.SyncClient(ctx, "opencode", contexts.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(res.TargetPath)
	if err := os.WriteFile(res.TargetPath, []byte(strings.Replace(string(data), "# Rules", "# EDITED", 1)), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := runCtxSync(ctx, &stdout, &stderr, mgr, []string{"opencode"}, contexts.SyncOptions{}, "", true)
	if exit != ctxExitAttention {
		t.Fatalf("drift sync exit = %d, want 1", exit)
	}
	if !strings.Contains(stdout.String(), "gridctl ctx adopt opencode") {
		t.Errorf("drift guidance missing: %q", stdout.String())
	}
}

func TestRunCtxCheckExitCodes(t *testing.T) {
	mgr := newCtxTestManager(t, ".claude")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	if exit := runCtxCheck(ctx, &stdout, &stderr, mgr, ""); exit != ctxExitOK {
		t.Fatalf("clean check exit = %d, want 0", exit)
	}

	if _, err := mgr.SyncClient(ctx, "claude-code", contexts.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	// Canonical changes -> the synced client is stale -> check fails.
	if err := mgr.SaveCanonical("# Rules v2\n"); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if exit := runCtxCheck(ctx, &stdout, &stderr, mgr, ""); exit != ctxExitAttention {
		t.Errorf("stale check exit = %d, want 1", exit)
	}
	if !strings.Contains(stdout.String(), "stale") {
		t.Errorf("check output missing stale line: %q", stdout.String())
	}
}

func TestRunCtxDiffExitCodes(t *testing.T) {
	mgr := newCtxTestManager(t, ".config/opencode")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := mgr.SyncClient(ctx, "opencode", contexts.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if exit := runCtxDiff(ctx, &stdout, &stderr, mgr, "opencode"); exit != ctxExitOK {
		t.Fatalf("clean diff exit = %d, want 0", exit)
	}

	data, _ := os.ReadFile(res.TargetPath)
	if err := os.WriteFile(res.TargetPath, []byte(strings.Replace(string(data), "# Rules", "# EDITED", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if exit := runCtxDiff(ctx, &stdout, &stderr, mgr, "opencode"); exit != ctxExitAttention {
		t.Errorf("differing diff exit = %d, want 1", exit)
	}
	if !strings.Contains(stdout.String(), "+# EDITED") {
		t.Errorf("diff output missing hunk: %q", stdout.String())
	}

	if exit := runCtxDiff(ctx, &stdout, &stderr, mgr, "unknown-client"); exit != ctxExitInfrastructure {
		t.Error("unknown client should exit 2")
	}
}

func TestRunCtxUnsyncRequiresTargets(t *testing.T) {
	mgr := newCtxTestManager(t)
	var out bytes.Buffer
	err := runCtxUnsync(context.Background(), &out, mgr, nil, false, "")
	if err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunCtxUnsyncAllRemovesArtifacts(t *testing.T) {
	mgr := newCtxTestManager(t, ".claude")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := mgr.SyncClient(ctx, "claude-code", contexts.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCtxUnsync(ctx, &out, mgr, nil, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(res.TargetPath); !os.IsNotExist(statErr) {
		t.Error("dedicated file survived unsync")
	}
	if !strings.Contains(out.String(), "removed-file") {
		t.Errorf("output missing action: %q", out.String())
	}
}

func TestRunCtxUnsyncJSON(t *testing.T) {
	mgr := newCtxTestManager(t, ".claude")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := mgr.SyncClient(ctx, "claude-code", contexts.SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCtxUnsync(ctx, &out, mgr, []string{"claude-code"}, false, "json"); err != nil {
		t.Fatal(err)
	}
	var doc ctxUnsyncDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(doc.Results) != 1 || doc.Results[0].Action != "removed-file" {
		t.Errorf("unexpected doc: %+v", doc)
	}
}

func TestRunCtxSyncNamedErrorBecomesResultRow(t *testing.T) {
	// gemini's detect dir is absent, so an explicit sync fails at runtime;
	// the failure must land in the results document, not abort the run.
	mgr := newCtxTestManager(t, ".claude")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := runCtxSync(context.Background(), &stdout, &stderr, mgr, []string{"claude-code", "gemini"}, contexts.SyncOptions{}, "json", false)
	if exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", exit, stderr.String())
	}
	var doc ctxSyncDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(doc.Results) != 2 {
		t.Fatalf("results = %d, want 2 (claude-code write must be reported)", len(doc.Results))
	}
	if doc.Results[0].Action != contexts.ActionCreated {
		t.Errorf("claude-code action = %q, want created", doc.Results[0].Action)
	}
	if doc.Results[1].Action != contexts.ActionError || doc.Results[1].Error == "" {
		t.Errorf("gemini result = %+v, want error row", doc.Results[1])
	}

	// A typo still aborts with exit 2 so it cannot pass a CI gate.
	stdout.Reset()
	exit = runCtxSync(context.Background(), &stdout, &stderr, mgr, []string{"bogus"}, contexts.SyncOptions{}, "json", false)
	if exit != ctxExitInfrastructure {
		t.Errorf("unknown client exit = %d, want 2", exit)
	}
}

func TestRunCtxAdopt(t *testing.T) {
	mgr := newCtxTestManager(t, ".config/opencode")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := mgr.SyncClient(ctx, "opencode", contexts.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(res.TargetPath)
	if err := os.WriteFile(res.TargetPath, []byte(strings.Replace(string(data), "# Rules", "# ADOPTED", 1)), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runCtxAdopt(ctx, &out, mgr, "opencode"); err != nil {
		t.Fatal(err)
	}
	content, _ := mgr.CanonicalContent()
	if !strings.Contains(content, "# ADOPTED") {
		t.Errorf("canonical missing adopted content: %q", content)
	}
}
