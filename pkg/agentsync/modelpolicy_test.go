package agentsync

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

func agentContentWithModel(name, model string) string {
	return "---\nname: " + name + "\ndescription: Reviews things\nmodel: " + model + "\n---\n\nReview.\n"
}

func readProjected(t *testing.T, home, name string) string {
	t.Helper()
	data, err := os.ReadFile(projectedPath(home, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRewriteAgentModel(t *testing.T) {
	raw := []byte("---\nname: a\ndescription: d\nmodel: opus\ntools: Read, Bash\n---\n\nBody.\n")
	out, ok := rewriteAgentModel(raw, "sonnet")
	if !ok {
		t.Fatal("rewrite refused")
	}
	s := string(out)
	if !strings.Contains(s, "model: sonnet\n") || strings.Contains(s, "opus") {
		t.Fatalf("model line not replaced:\n%s", s)
	}
	if !strings.Contains(s, "tools: Read, Bash\n") || !strings.Contains(s, "Body.") {
		t.Fatalf("unrelated bytes must ride through verbatim:\n%s", s)
	}

	// No declared model: the key is inserted before the closing delimiter.
	out, ok = rewriteAgentModel([]byte("---\nname: a\ndescription: d\n---\n\nBody.\n"), "haiku")
	if !ok || !strings.Contains(string(out), "description: d\nmodel: haiku\n---\n") {
		t.Fatalf("insert failed: %q", string(out))
	}

	// Empty value removes the key (adopt's restore path).
	out, ok = rewriteAgentModel([]byte("---\nname: a\ndescription: d\nmodel: opus\n---\n\nBody.\n"), "")
	if !ok || strings.Contains(string(out), "model") {
		t.Fatalf("removal failed: %q", string(out))
	}

	// An indented model key belongs to a nested mapping, not the agent.
	nested := []byte("---\nname: a\ndescription: d\nvendor:\n  model: keep\n---\n\nBody.\n")
	out, ok = rewriteAgentModel(nested, "sonnet")
	if !ok || !strings.Contains(string(out), "  model: keep\n") || !strings.Contains(string(out), "\nmodel: sonnet\n") {
		t.Fatalf("nested key must be untouched, top-level inserted: %q", string(out))
	}

	// Multi-line values are refused rather than corrupted.
	if _, ok := rewriteAgentModel([]byte("---\nname: a\ndescription: d\nmodel: |\n  opus\n---\n\nB.\n"), "sonnet"); ok {
		t.Fatal("block scalar model must refuse rewrite")
	}
	// No frontmatter at all is refused.
	if _, ok := rewriteAgentModel([]byte("just a body\n"), "sonnet"); ok {
		t.Fatal("missing frontmatter must refuse rewrite")
	}
}

func TestAgentSync_ModelPolicyRewrite(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "declared", agentContentWithModel("declared", "opus"))
	writeAgent(t, registryDir, "plain", agentContent("plain", "Do things."))
	mgr.SetModelPolicy(&registry.ModelPolicy{
		Rewrite:   true,
		Default:   "sonnet",
		Overrides: map[string]string{"declared": "haiku"},
	})

	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	if got := readProjected(t, home, "declared"); !strings.Contains(got, "model: haiku\n") {
		t.Fatalf("override must beat author declaration:\n%s", got)
	}
	if got := readProjected(t, home, "plain"); !strings.Contains(got, "model: sonnet\n") {
		t.Fatalf("default must apply where nothing was declared:\n%s", got)
	}

	// The canonical store is byte-unchanged.
	canon, err := os.ReadFile(registryDir + "/agents/declared/AGENT.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canon), "model: opus") {
		t.Fatal("registry canonical must never be mutated by policy rewrite")
	}

	// Lock entries carry the rewrite marker.
	lf, err := mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("declared", "claude-code"); e == nil || e.ModelValue != "haiku" {
		t.Fatalf("entry should carry the applied model value, got %+v", e)
	}

	// Status is in-sync while the policy matches what was written.
	statuses, err := mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		if s.State != StateInSync {
			t.Fatalf("expected in-sync, got %+v", s)
		}
	}
}

func TestAgentSync_AuthorEqualsResolvedNoRewrite(t *testing.T) {
	mgr, _, registryDir := newTestManager(t)
	// Case differs but normalizes equal: no rewrite, no marker.
	writeAgent(t, registryDir, "cased", agentContentWithModel("cased", "Sonnet"))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	lf, err := mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("cased", "claude-code"); e == nil || e.ModelValue != "" {
		t.Fatalf("normalized-equal declaration must not be rewritten, got %+v", e)
	}
}

func TestAgentSync_PolicyChangeGoesStaleThenResyncs(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "a", agentContent("a", "Do."))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "opus"})
	statuses, err := mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].State != StateStale {
		t.Fatalf("policy change should read stale, got %+v", statuses)
	}

	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readProjected(t, home, "a"); !strings.Contains(got, "model: opus\n") {
		t.Fatalf("re-sync should apply the new policy:\n%s", got)
	}
}

func TestAgentSync_PolicyRemovalReconcilesBack(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "a", agentContent("a", "Do."))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// A loaded policy that no longer covers the agent (rewrite off) is
	// known-absent: the projection reconciles back to pass-through.
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: false})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readProjected(t, home, "a"); strings.Contains(got, "model:") {
		t.Fatalf("known-absent policy should restore pass-through bytes:\n%s", got)
	}
	lf, err := mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("a", "claude-code"); e == nil || e.ModelValue != "" {
		t.Fatalf("the rewrite marker should clear on reconcile-back, got %+v", e)
	}
}

func TestAgentSync_StacklessPreservesRewrite(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "a", agentContent("a", "Do."))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// The preserve rule: nil policy (no stack context) must not revert
	// the daemon's rewrite.
	mgr.SetModelPolicy(nil)
	results, err := mgr.Sync(context.Background(), nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range availableResults(results) {
		if r.Action != ActionUnchanged {
			t.Fatalf("stackless sync must preserve, got %+v", r)
		}
	}
	if got := readProjected(t, home, "a"); !strings.Contains(got, "model: sonnet\n") {
		t.Fatalf("rewritten bytes must survive a stackless sync:\n%s", got)
	}

	// Status names the unknown-policy condition without flagging state.
	statuses, err := mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].State != StateInSync || !strings.Contains(statuses[0].Detail, "no stack loaded") {
		t.Fatalf("expected in-sync with unknown-policy detail, got %+v", statuses[0])
	}
}

func TestAgentAdopt_PolicyKeysNeverReachCanonical(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "a", agentContentWithModel("a", "opus"))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Overrides: map[string]string{"a": "haiku"}})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Adopt with no user edits: the only delta is the policy key, so
	// nothing flows back.
	res, err := mgr.Adopt(context.Background(), "a", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || !res.PolicyKeysRestored {
		t.Fatalf("policy-only delta must adopt nothing and report the restore, got %+v", res)
	}
	canon, _ := os.ReadFile(registryDir + "/agents/a/AGENT.md")
	if !strings.Contains(string(canon), "model: opus") {
		t.Fatal("canonical model declaration must be untouched")
	}

	// Adopt refreshed the lock without touching the rewritten projection
	// (rewritten pairs are never re-materialized by adopt); a plain sync
	// confirms it still carries the rewrite. Then make a real body edit
	// and adopt: the edit lands, the model key does not.
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	projected := readProjected(t, home, "a")
	if !strings.Contains(projected, "model: haiku\n") {
		t.Fatalf("precondition: projection should be rewritten:\n%s", projected)
	}
	edited := strings.Replace(projected, "Review.", "Review carefully.", 1)
	if err := os.WriteFile(projectedPath(home, "a"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = mgr.Adopt(context.Background(), "a", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.PolicyKeysRestored {
		t.Fatalf("expected changed adopt with policy keys restored, got %+v", res)
	}
	canon, _ = os.ReadFile(registryDir + "/agents/a/AGENT.md")
	if !strings.Contains(string(canon), "Review carefully.") {
		t.Fatal("the body edit should have been adopted")
	}
	if !strings.Contains(string(canon), "model: opus") || strings.Contains(string(canon), "haiku") {
		t.Fatalf("the policy value must never reach the canonical:\n%s", string(canon))
	}
}

func TestAgentAdopt_RestoreFailureRefusesRatherThanPoisons(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "a", agentContentWithModel("a", "opus"))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Overrides: map[string]string{"a": "haiku"}})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Hand-edit the projected file into a form the restore surgery
	// refuses (a block-scalar model whose value still normalizes to the
	// policy's write). The adopt must refuse outright: falling through
	// would write the policy value into the canonical store.
	projected := readProjected(t, home, "a")
	mangled := strings.Replace(projected, "model: haiku\n", "model: |\n  haiku\n", 1)
	if mangled == projected {
		t.Fatal("precondition: projected model line not found")
	}
	if err := os.WriteFile(projectedPath(home, "a"), []byte(mangled), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := mgr.Adopt(context.Background(), "a", "claude-code")
	var refusal *AdoptRefusal
	if err == nil || !errors.As(err, &refusal) {
		t.Fatalf("expected an adopt refusal, got %v", err)
	}
	canon, _ := os.ReadFile(registryDir + "/agents/a/AGENT.md")
	if !strings.Contains(string(canon), "model: opus") || strings.Contains(string(canon), "haiku") {
		t.Fatalf("a failed restore must never write the policy value into the canonical:\n%s", string(canon))
	}
}

func TestAgentAdopt_UserModelEditIsAdopted(t *testing.T) {
	mgr, home, registryDir := newTestManager(t)
	writeAgent(t, registryDir, "a", agentContentWithModel("a", "sonnet"))
	mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Overrides: map[string]string{"a": "opus"}})
	if _, err := mgr.Sync(context.Background(), nil, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// The user deliberately edits the projected model to a THIRD value:
	// that is author intent, not the policy's write, and it adopts.
	projected := readProjected(t, home, "a")
	if err := os.WriteFile(projectedPath(home, "a"), []byte(strings.Replace(projected, "model: opus", "model: haiku", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Adopt(context.Background(), "a", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.PolicyKeysRestored {
		t.Fatalf("a deliberate user model edit must adopt as a real change, got %+v", res)
	}
	canon, _ := os.ReadFile(registryDir + "/agents/a/AGENT.md")
	if !strings.Contains(string(canon), "model: haiku") {
		t.Fatalf("the user's model edit should have been adopted:\n%s", string(canon))
	}
	// The pair equalized, so the rewrite marker clears.
	lf, err := mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("a", "claude-code"); e == nil || e.ModelValue != "" {
		t.Fatalf("equalized pair should carry no rewrite marker, got %+v", e)
	}
}

func TestAgentHonorMatrix_CoversEveryTarget(t *testing.T) {
	matrix := registry.AgentHonorMatrix()
	for _, tgt := range Targets() {
		if _, ok := matrix[tgt.Slug]; !ok {
			t.Errorf("agent projection target %q has no honor matrix row; add it to pkg/registry/honor.go", tgt.Slug)
		}
	}
	for slug := range matrix {
		if _, ok := FindTarget(slug); !ok {
			t.Errorf("honor matrix row %q names no existing agent target", slug)
		}
	}
}
