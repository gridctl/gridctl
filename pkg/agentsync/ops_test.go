package agentsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestManager stages a temp home with a detected ~/.claude tree and
// a canonical agent store holding the named agents.
func newTestManager(t *testing.T, agents ...string) (*Manager, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	registryDir := filepath.Join(home, ".gridctl", "registry")
	for _, name := range agents {
		writeAgent(t, registryDir, name, agentContent(name, "Review the code."))
	}
	return NewManagerWithHome(home, registryDir), home, registryDir
}

func agentContent(name, body string) string {
	return "---\nname: " + name + "\ndescription: Reviews things\n---\n\n" + body + "\n"
}

func writeAgent(t *testing.T, registryDir, name, content string) {
	t.Helper()
	dir := filepath.Join(registryDir, "agents", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func projectedPath(home, name string) string {
	return filepath.Join(home, ".claude", "agents", name+".md")
}

// availableResults drops skipped-unavailable rows: most ops tests stage
// only ~/.claude, so the rendered targets (opencode, copilot, gemini)
// report unavailable and are not what those tests assert on.
func availableResults(results []SyncResult) []SyncResult {
	var out []SyncResult
	for _, r := range results {
		if r.Action != ActionSkippedUnavailable {
			out = append(out, r)
		}
	}
	return out
}

func TestSync_ProjectsAllImportedAgents(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha", "beta")
	ctx := context.Background()

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	results = availableResults(results)
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	for _, name := range []string{"alpha", "beta"} {
		data, err := os.ReadFile(projectedPath(home, name))
		if err != nil {
			t.Fatalf("projected file missing for %s: %v", name, err)
		}
		if string(data) != agentContent(name, "Review the code.") {
			t.Errorf("%s projected content differs from canon (must be verbatim)", name)
		}
	}

	// Second sync is a no-op.
	results, err = mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range availableResults(results) {
		if r.Action != ActionUnchanged {
			t.Errorf("re-sync action for %s = %q, want unchanged", r.Agent, r.Action)
		}
	}
}

func TestSync_NamedUnknownAgentFails(t *testing.T) {
	mgr, _, _ := newTestManager(t, "alpha")
	if _, err := mgr.Sync(context.Background(), []string{"nope"}, SyncOptions{}); err == nil {
		t.Fatal("expected error for unknown agent name")
	}
}

func TestSync_DryRunWritesNothing(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	rawResults, err := mgr.Sync(context.Background(), nil, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	results := availableResults(rawResults)
	if len(results) != 1 || results[0].Action != ActionWouldCopy {
		t.Fatalf("results = %+v", results)
	}
	if _, err := os.Stat(projectedPath(home, "alpha")); !os.IsNotExist(err) {
		t.Error("dry run created the projected file")
	}
	if _, err := os.Stat(mgr.LockPath()); !os.IsNotExist(err) {
		t.Error("dry run wrote the lockfile")
	}
}

func TestSync_UnmanagedFileRefusedThenForced(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	ctx := context.Background()

	// Pre-existing hand-authored definition at the destination.
	dest := projectedPath(home, "alpha")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	handAuthored := "---\nname: alpha\ndescription: mine\n---\n\nHand-authored.\n"
	if err := os.WriteFile(dest, []byte(handAuthored), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	results = availableResults(results)
	if results[0].Action != ActionSkippedUnmanaged {
		t.Fatalf("action = %q, want skipped-unmanaged", results[0].Action)
	}
	if !strings.Contains(results[0].Error, "hand-authored subagent definition") {
		t.Errorf("refusal must name what --force would destroy: %q", results[0].Error)
	}
	if data, _ := os.ReadFile(dest); string(data) != handAuthored {
		t.Fatal("refused sync still modified the file")
	}

	results, err = mgr.Sync(ctx, nil, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	results = availableResults(results)
	if results[0].Action != ActionUpdated {
		t.Fatalf("forced action = %q, want updated", results[0].Action)
	}
	if results[0].BackupPath == "" {
		t.Fatal("forced replace of unmanaged file must back it up")
	}
	backup, err := os.ReadFile(results[0].BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != handAuthored {
		t.Error("backup content differs from the replaced file")
	}
	if !strings.HasPrefix(results[0].BackupPath, filepath.Join(home, ".gridctl", "project-backups", "agent")) {
		t.Errorf("backup landed in-tree: %s", results[0].BackupPath)
	}
}

func TestSync_DriftRefusedThenForced(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	dest := projectedPath(home, "alpha")
	if err := os.WriteFile(dest, []byte("hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	results = availableResults(results)
	if results[0].Action != ActionSkippedDrift {
		t.Fatalf("action = %q, want skipped-drift", results[0].Action)
	}

	results, err = mgr.Sync(ctx, nil, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	results = availableResults(results)
	if results[0].Action != ActionUpdated || results[0].BackupPath == "" {
		t.Fatalf("forced result = %+v, want updated with backup", results[0])
	}
	data, _ := os.ReadFile(dest)
	if string(data) != agentContent("alpha", "Review the code.") {
		t.Error("forced sync did not restore canonical content")
	}
}

func TestSync_RemovesProjectionWhenAgentGone(t *testing.T) {
	mgr, home, registryDir := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(registryDir, "agents", "alpha")); err != nil {
		t.Fatal(err)
	}

	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := availableResults(results); len(r) != 1 || r[0].Action != ActionRemoved {
		t.Fatalf("results = %+v, want one removed", results)
	}
	if _, err := os.Stat(projectedPath(home, "alpha")); !os.IsNotExist(err) {
		t.Error("projected file still present after removal")
	}
}

func TestStatuses_FourStates(t *testing.T) {
	mgr, home, registryDir := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].State != StateInSync {
		t.Fatalf("statuses = %+v, want in-sync", statuses)
	}
	if statuses[0].Render != "identity" {
		t.Errorf("render = %q, want claude-code to receive canonical bytes", statuses[0].Render)
	}

	// Hand edit at the destination: drifted.
	dest := projectedPath(home, "alpha")
	if err := os.WriteFile(dest, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, _ = mgr.Statuses(ctx)
	if statuses[0].State != StateDrifted {
		t.Errorf("state = %q, want drifted", statuses[0].State)
	}

	// Restore, then advance the canon: stale.
	if _, err := mgr.Sync(ctx, nil, SyncOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	writeAgent(t, registryDir, "alpha", agentContent("alpha", "Revised body."))
	statuses, _ = mgr.Statuses(ctx)
	if statuses[0].State != StateStale {
		t.Errorf("state = %q, want stale", statuses[0].State)
	}

	// Remove the destination: target-missing.
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	statuses, _ = mgr.Statuses(ctx)
	if statuses[0].State != StateTargetMissing {
		t.Errorf("state = %q, want target-missing", statuses[0].State)
	}
}

func TestUnsync_RemovesOwnedFilesOnly(t *testing.T) {
	mgr, home, _ := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// An unmanaged neighbor must survive the unsync.
	neighbor := projectedPath(home, "hand-authored")
	if err := os.WriteFile(neighbor, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := mgr.Unsync(ctx, nil, UnsyncOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionRemoved {
		t.Fatalf("results = %+v", results)
	}
	if _, err := os.Stat(projectedPath(home, "alpha")); !os.IsNotExist(err) {
		t.Error("owned file still present")
	}
	if _, err := os.Stat(neighbor); err != nil {
		t.Error("unmanaged neighbor was removed")
	}

	has, err := mgr.HasProjections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("lock entries remain after unsync --all")
	}

	if _, err := mgr.Unsync(ctx, []string{"alpha"}, UnsyncOptions{}); err == nil {
		t.Error("unsync of unprojected agent must fail with ErrNotProjected")
	}
}

func TestReconcile_GuardsAndRecordedSetOnly(t *testing.T) {
	mgr, _, registryDir := newTestManager(t, "alpha")
	ctx := context.Background()

	// Nothing projected: no-op.
	results, err := mgr.Reconcile(ctx)
	if err != nil || results != nil {
		t.Fatalf("results = %+v, err = %v", results, err)
	}

	if _, err := mgr.Sync(ctx, []string{"alpha"}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// A new import must not be auto-projected by reconcile.
	writeAgent(t, registryDir, "beta", agentContent("beta", "New."))
	results, err = mgr.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Agent == "beta" {
			t.Error("reconcile projected an agent that was never synced")
		}
	}

	// Empty store with recorded projections: refused, not mass-removed.
	if err := os.RemoveAll(filepath.Join(registryDir, "agents")); err != nil {
		t.Fatal(err)
	}
	results, err = mgr.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedEmptyStore {
		t.Fatalf("results = %+v, want skipped-empty-store", results)
	}
}

func TestSync_UnavailableClientSkippedOrErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	registryDir := filepath.Join(home, ".gridctl", "registry")
	writeAgent(t, registryDir, "alpha", agentContent("alpha", "Body."))
	mgr := NewManagerWithHome(home, registryDir)
	ctx := context.Background()

	// No client trees at all: every target reports skipped-unavailable.
	results, err := mgr.Sync(ctx, nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(Targets()) {
		t.Fatalf("results = %+v, want one unavailable row per target", results)
	}
	for _, r := range results {
		if r.Action != ActionSkippedUnavailable {
			t.Fatalf("results = %+v, want all skipped-unavailable", results)
		}
	}

	// Explicitly named unavailable client is an error.
	if _, err := mgr.Sync(ctx, nil, SyncOptions{Clients: []string{"claude-code"}}); err == nil {
		t.Error("named unavailable client must error")
	}
	if _, err := mgr.Sync(ctx, nil, SyncOptions{Clients: []string{"nope"}}); err == nil {
		t.Error("unknown client must error")
	}
}

// TestStatuses_CarryPackTag pins the provenance ride-along: a projection
// applied by a pack reports the tag on its status row; untagged rows
// omit it (json omitempty keeps the wire shape unchanged for them).
func TestStatuses_CarryPackTag(t *testing.T) {
	mgr, _, _ := newTestManager(t, "alpha")
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, nil, SyncOptions{Pack: "team-pack"}); err != nil {
		t.Fatal(err)
	}
	statuses, err := mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Pack != "team-pack" {
		t.Fatalf("statuses = %+v, want pack tag team-pack", statuses)
	}

	// A plain re-sync never strips the tag; an untagged fresh projection
	// carries none.
	if _, err := mgr.Sync(ctx, nil, SyncOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	statuses, _ = mgr.Statuses(ctx)
	if statuses[0].Pack != "team-pack" {
		t.Errorf("re-sync stripped the pack tag: %+v", statuses[0])
	}
}
