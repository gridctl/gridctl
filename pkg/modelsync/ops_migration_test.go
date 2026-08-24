package modelsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: a fragment_path change must not strand the previously
// rendered file at the old location.
func TestSyncFragmentPathChangeCleansUpOldFragment(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	oldFrag := filepath.Join(filepath.Dir(f.parent), "gridctl-models.yaml")
	if _, err := os.Stat(oldFrag); err != nil {
		t.Fatalf("old fragment missing before move: %v", err)
	}

	// Point the policy at an explicit new fragment location.
	data, _ := os.ReadFile(f.m.PolicyPath())
	moved := strings.Replace(string(data),
		"    config_path: ~/.litellm/config.yaml",
		"    config_path: ~/.litellm/config.yaml\n    fragment_path: ~/.litellm/router-fragment.yaml", 1)
	if err := os.WriteFile(f.m.PolicyPath(), []byte(moved), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := f.m.Sync(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(results, srcFragment); r == nil || r.Action != ActionUpdated {
		t.Fatalf("fragment move: %+v", results)
	}
	if _, err := os.Stat(oldFrag); !os.IsNotExist(err) {
		t.Error("old fragment must be removed after the path change")
	}
	newFrag := filepath.Join(f.home, ".litellm", "router-fragment.yaml")
	if _, err := os.Stat(newFrag); err != nil {
		t.Errorf("new fragment missing: %v", err)
	}
	// The include line followed the move without --force: a declared
	// path change is not user drift.
	parent, _ := os.ReadFile(f.parent)
	if !strings.Contains(string(parent), "- router-fragment.yaml") {
		t.Errorf("include line did not follow the fragment move:\n%s", parent)
	}
	if strings.Contains(string(parent), "- gridctl-models.yaml") {
		t.Errorf("stale include line left behind:\n%s", parent)
	}
}

// Regression: a hand-edited old fragment survives a path change and is
// reported, never silently deleted.
func TestSyncFragmentPathChangeKeepsEditedOldFragment(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	oldFrag := filepath.Join(filepath.Dir(f.parent), "gridctl-models.yaml")
	fragData, _ := os.ReadFile(oldFrag)
	if err := os.WriteFile(oldFrag, append(fragData, []byte("# tuned\n")...), 0644); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(f.m.PolicyPath())
	moved := strings.Replace(string(data),
		"    config_path: ~/.litellm/config.yaml",
		"    config_path: ~/.litellm/config.yaml\n    fragment_path: ~/.litellm/router-fragment.yaml", 1)
	if err := os.WriteFile(f.m.PolicyPath(), []byte(moved), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := f.m.Sync(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := resultFor(results, srcFragment)
	if r == nil || r.Action != ActionUpdated {
		t.Fatalf("fragment move: %+v", results)
	}
	if !strings.Contains(r.Detail, "hand-edited") {
		t.Errorf("edited old fragment must be reported: %q", r.Detail)
	}
	if _, err := os.Stat(oldFrag); err != nil {
		t.Error("edited old fragment must be left in place")
	}
}

// Regression: an OpenCode schema flip migrates the owned subtree to the
// new container instead of orphaning a duplicate.
func TestSyncOpenCodeSchemaFlipMigrates(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()
	if _, err := f.m.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	// The fixture's config had neither container, so detect chose v1.
	oc, _ := os.ReadFile(f.occfg)
	if !strings.Contains(string(oc), `"provider"`) {
		t.Fatalf("expected v1 provider container:\n%s", oc)
	}

	// Pin the policy to v2 (an explicit generation change).
	data, _ := os.ReadFile(f.m.PolicyPath())
	flipped := strings.Replace(string(data), "schema: detect", "schema: v2", 1)
	if err := os.WriteFile(f.m.PolicyPath(), []byte(flipped), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := f.m.Sync(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := resultFor(results, srcOpenCode); r == nil || r.Action != ActionUpdated {
		t.Fatalf("schema flip: %+v", results)
	}
	oc, _ = os.ReadFile(f.occfg)
	got := string(oc)
	if !strings.Contains(got, `"providers"`) || !strings.Contains(got, `"package"`) {
		t.Errorf("v2 stanza missing after flip:\n%s", got)
	}
	if strings.Contains(got, `"npm"`) {
		t.Errorf("old v1 stanza must be migrated out, not orphaned:\n%s", got)
	}
	if !strings.Contains(got, `"model": "anthropic/claude-sonnet"`) {
		t.Errorf("user's model pick must survive the migration:\n%s", got)
	}

	// Unsync still cleans up completely after the flip.
	unResults, err := f.m.Unsync(ctx, UnsyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range unResults {
		if r.Target == srcOpenCode && r.Action != ActionRemoved {
			t.Errorf("post-flip unsync: %+v", r)
		}
	}
	oc, _ = os.ReadFile(f.occfg)
	if strings.Contains(string(oc), "LiteLLM") {
		t.Errorf("provider not removed after flip + unsync:\n%s", oc)
	}
}
