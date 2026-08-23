package modelsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// e2eFixture is a temp home with a parent LiteLLM config, an OpenCode
// config, and a policy pointing at both.
type e2eFixture struct {
	m      *Manager
	home   string
	parent string
	occfg  string
}

func newE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()
	home := t.TempDir()
	m := NewManagerWithHome(home)

	parent := filepath.Join(home, ".litellm", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(parent), 0755); err != nil {
		t.Fatal(err)
	}
	parentContent := `# personal proxy config
model_list:
  - model_name: qwen-local
    litellm_params:
      model: openai/qwen3
      api_base: http://127.0.0.1:8000/v1
      api_key: os.environ/DUMMY_KEY
  - model_name: fable
    litellm_params:
      model: openai/fable
      api_key: os.environ/FABLE_API_KEY

router_settings:
  num_retries: 2
`
	if err := os.WriteFile(parent, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	occfg := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(occfg), 0755); err != nil {
		t.Fatal(err)
	}
	ocContent := `{
  // keep my settings
  "theme": "dark",
  "model": "anthropic/claude-sonnet"
}
`
	if err := os.WriteFile(occfg, []byte(ocContent), 0644); err != nil {
		t.Fatal(err)
	}

	policy := `name: default
kind: models
router:
  entry_model: smart-router
  default_tier: MEDIUM
backends:
  - qwen-local
  - fable
tiers:
  SIMPLE: qwen-local
  MEDIUM: qwen-local
  COMPLEX: fable
  REASONING: fable
clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY
    schema: detect
targets:
  litellm:
    config_path: ~/.litellm/config.yaml
`
	if err := m.SavePolicy([]byte(policy)); err != nil {
		t.Fatal(err)
	}
	return &e2eFixture{m: m, home: home, parent: parent, occfg: occfg}
}

func TestSyncStatusUnsyncRoundTrip(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	parentBefore, _ := os.ReadFile(f.parent)

	results, err := f.m.Sync(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || HasFailures(results) {
		t.Fatalf("sync results: %+v", results)
	}
	for _, r := range results {
		if r.Action != ActionUpdated {
			t.Errorf("%s: action %s, want updated (%s)", r.Target, r.Action, r.Error)
		}
	}

	// Fragment landed next to the parent with a relative include.
	frag := filepath.Join(filepath.Dir(f.parent), "gridctl-models.yaml")
	if _, err := os.Stat(frag); err != nil {
		t.Fatalf("fragment missing: %v", err)
	}
	parentAfter, _ := os.ReadFile(f.parent)
	if !strings.Contains(string(parentAfter), "include:\n  - gridctl-models.yaml") {
		t.Errorf("include line missing:\n%s", parentAfter)
	}
	if !strings.HasPrefix(string(parentAfter), string(parentBefore)) {
		t.Errorf("parent content before the include must be untouched")
	}

	// Status: everything in-sync, fragment restart-pending until acked.
	statuses, err := f.m.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 || NeedsAttention(statuses) {
		t.Fatalf("statuses: %+v", statuses)
	}
	for _, s := range statuses {
		if s.Target == srcFragment && !s.RestartPending {
			t.Error("fragment must be restart-pending after first sync")
		}
	}

	// A second sync without an ack keeps the latch (the latch never
	// clears on sync).
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	statuses, _ = f.m.Statuses(ctx)
	for _, s := range statuses {
		if s.Target == srcFragment && !s.RestartPending {
			t.Error("restart-pending must survive a second sync")
		}
	}

	if err := f.m.AckRestart(ctx); err != nil {
		t.Fatal(err)
	}
	statuses, _ = f.m.Statuses(ctx)
	for _, s := range statuses {
		if s.Target == srcFragment && s.RestartPending {
			t.Error("ack-restart must clear the latch")
		}
	}

	// OpenCode config: owned subtree present, model key untouched.
	oc, _ := os.ReadFile(f.occfg)
	if !strings.Contains(string(oc), `"model": "anthropic/claude-sonnet"`) {
		t.Errorf("user's model pick must survive:\n%s", oc)
	}
	if !strings.Contains(string(oc), "// keep my settings") {
		t.Errorf("comments must survive:\n%s", oc)
	}

	// Unsync restores the parent byte-identically and removes the rest.
	unResults, err := f.m.Unsync(ctx, UnsyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range unResults {
		if r.Action != ActionRemoved {
			t.Errorf("%s: unsync action %s (%s)", r.Target, r.Action, r.Error)
		}
	}
	parentRestored, _ := os.ReadFile(f.parent)
	if string(parentRestored) != string(parentBefore) {
		t.Errorf("parent not byte-identical after unsync:\n%q\nwant\n%q", parentRestored, parentBefore)
	}
	if _, err := os.Stat(frag); !os.IsNotExist(err) {
		t.Error("fragment must be removed")
	}
	ocAfter, _ := os.ReadFile(f.occfg)
	if strings.Contains(string(ocAfter), "LiteLLM") {
		t.Errorf("provider stanza must be removed:\n%s", ocAfter)
	}
	if !strings.Contains(string(ocAfter), `"model": "anthropic/claude-sonnet"`) {
		t.Errorf("user's model pick must survive unsync:\n%s", ocAfter)
	}
}

func TestSyncStates(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Policy edit -> fragment stale.
	data, _ := os.ReadFile(f.m.PolicyPath())
	edited := strings.Replace(string(data), "COMPLEX: fable", "COMPLEX: qwen-local", 1)
	if err := os.WriteFile(f.m.PolicyPath(), []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}
	statuses, _ := f.m.Statuses(ctx)
	if s := statusFor(statuses, srcFragment); s == nil || s.State != StateStale {
		t.Errorf("expected stale fragment after policy edit, got %+v", statuses)
	}

	// Re-sync, then hand-edit the fragment -> drifted, and sync skips.
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	frag := filepath.Join(filepath.Dir(f.parent), "gridctl-models.yaml")
	fragData, _ := os.ReadFile(frag)
	if err := os.WriteFile(frag, append(fragData, []byte("# hand edit\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	statuses, _ = f.m.Statuses(ctx)
	if s := statusFor(statuses, srcFragment); s == nil || s.State != StateDrifted {
		t.Errorf("expected drifted fragment, got %+v", statuses)
	}
	// The drifted fragment blocks a plain re-sync... (the policy is
	// unchanged, so rendering equals the recorded install, but the hand
	// edit differs from both).
	results, err := f.m.Sync(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(results, srcFragment); r == nil || r.Action != ActionSkippedDrift {
		t.Errorf("expected skipped-drift, got %+v", results)
	}
	// ...and --force overwrites it.
	results, err = f.m.Sync(ctx, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(results, srcFragment); r == nil || r.Action != ActionUpdated {
		t.Errorf("expected forced update, got %+v", results)
	}

	// Include line removed by hand -> drifted, kept on plain sync.
	parentData, _ := os.ReadFile(f.parent)
	stripped := strings.Replace(string(parentData), "include:\n  - gridctl-models.yaml\n", "", 1)
	if err := os.WriteFile(f.parent, []byte(stripped), 0644); err != nil {
		t.Fatal(err)
	}
	statuses, _ = f.m.Statuses(ctx)
	if s := statusFor(statuses, srcInclude); s == nil || s.State != StateDrifted {
		t.Errorf("expected drifted include, got %+v", statuses)
	}
	results, _ = f.m.Sync(ctx, SyncOptions{})
	if r := resultFor(results, srcInclude); r == nil || r.Action != ActionSkippedDrift {
		t.Errorf("expected include skipped-drift, got %+v", results)
	}

	// Deleted fragment -> target-missing.
	if err := os.Remove(frag); err != nil {
		t.Fatal(err)
	}
	statuses, _ = f.m.Statuses(ctx)
	if s := statusFor(statuses, srcFragment); s == nil || s.State != StateTargetMissing {
		t.Errorf("expected target-missing fragment, got %+v", statuses)
	}
}

func TestSyncDryRunIsWriteFree(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()

	before := snapshotTree(t, f.home)
	results, err := f.m.Sync(ctx, SyncOptions{DryRun: true, Diff: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionWouldUpdate {
			t.Errorf("%s: dry-run action %s", r.Target, r.Action)
		}
	}
	after := snapshotTree(t, f.home)
	if len(before) != len(after) {
		t.Fatalf("dry-run changed the file tree: %d -> %d files", len(before), len(after))
	}
	for path, hash := range before {
		if after[path] != hash {
			t.Errorf("dry-run modified %s", path)
		}
	}
	// Specifically: no lockfile was created.
	if _, err := os.Stat(f.m.LockPath()); !os.IsNotExist(err) {
		t.Error("dry-run must not create the lockfile")
	}
}

func TestSyncForeignRefusals(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()

	// A pre-existing file at the fragment path is never overwritten
	// without force.
	frag := filepath.Join(filepath.Dir(f.parent), "gridctl-models.yaml")
	if err := os.WriteFile(frag, []byte("# someone else's file\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// A pre-existing provider entry gridctl did not write is foreign.
	oc := `{"provider": {"litellm": {"npm": "their-own"}}}`
	if err := os.WriteFile(f.occfg, []byte(oc), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := f.m.Sync(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(results, srcFragment); r == nil || r.Action != ActionSkippedForeign {
		t.Errorf("fragment: expected skipped-foreign, got %+v", results)
	}
	if r := resultFor(results, srcOpenCode); r == nil || r.Action != ActionSkippedForeign {
		t.Errorf("opencode: expected skipped-foreign, got %+v", results)
	}
}

func TestAdoptClearsDrift(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	frag := filepath.Join(filepath.Dir(f.parent), "gridctl-models.yaml")
	fragData, _ := os.ReadFile(frag)
	if err := os.WriteFile(frag, append(fragData, []byte("# tuned by hand\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Adopt(ctx); err != nil {
		t.Fatal(err)
	}
	statuses, _ := f.m.Statuses(ctx)
	if s := statusFor(statuses, srcFragment); s == nil || s.State != StateInSync {
		t.Errorf("adopt must clear drift, got %+v", statuses)
	}
}

func statusFor(statuses []Status, target string) *Status {
	for i := range statuses {
		if statuses[i].Target == target {
			return &statuses[i]
		}
	}
	return nil
}

func resultFor(results []SyncResult, target string) *SyncResult {
	for i := range results {
		if results[i].Target == target {
			return &results[i]
		}
	}
	return nil
}

// snapshotTree hashes every file under root.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[path] = contentHash(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
