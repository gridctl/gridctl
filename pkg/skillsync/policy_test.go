package skillsync

import (
	"context"
	"os"
	"strings"
	"testing"
)

func denyPolicy(rules map[string]string) func(string) (bool, string) {
	return func(name string) (bool, string) {
		if rule, denied := rules[name]; denied {
			return false, rule
		}
		return true, ""
	}
}

func policyResults(results []SyncResult, skill string) []SyncResult {
	var out []SyncResult
	for _, r := range results {
		if r.Skill == skill && r.Action == ActionSkippedPolicy {
			out = append(out, r)
		}
	}
	return out
}

// TestSync_NamedPolicyDenySkipsWithoutFailingBatch: a denied name yields
// visible skipped-policy rows while the rest of the batch still projects.
func TestSync_NamedPolicyDenySkipsWithoutFailingBatch(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetPolicy(denyPolicy(map[string]string{"alpha": "alpha*"}))

	results := f.mustSync(t, []string{"alpha", "beta"}, SyncOptions{Clients: []string{"claude-code"}})

	skipped := policyResults(results, "alpha")
	if len(skipped) != 1 {
		t.Fatalf("alpha results = %+v, want one skipped-policy row", results)
	}
	if !strings.Contains(skipped[0].Error, "alpha*") {
		t.Fatalf("skip row does not name the rule: %+v", skipped[0])
	}
	if _, err := os.Lstat(f.dest(t, "claude-code", "alpha")); !os.IsNotExist(err) {
		t.Fatal("denied skill was projected anyway")
	}
	if _, err := os.Lstat(f.dest(t, "claude-code", "beta")); err != nil {
		t.Fatalf("allowed skill in the same batch was not projected: %v", err)
	}
	if !HasFailures(results) {
		t.Fatal("skipped-policy must count as needing attention")
	}
}

// TestReconcile_PolicyDenyKeepsProjection: a recorded projection a new
// policy denies is reported, never silently removed.
func TestReconcile_PolicyDenyKeepsProjection(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	f.mgr.SetPolicy(denyPolicy(map[string]string{"alpha": "default: deny"}))
	results, err := f.mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	skipped := policyResults(results, "alpha")
	if len(skipped) != 1 {
		t.Fatalf("results = %+v, want one skipped-policy row", results)
	}
	if _, err := os.Lstat(f.dest(t, "claude-code", "alpha")); err != nil {
		t.Fatalf("denied projection was removed from disk: %v", err)
	}
}

// TestSync_NilPolicyUnchanged: without a policy nothing is skipped.
func TestSync_NilPolicyUnchanged(t *testing.T) {
	f := newFixture(t)
	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	for _, r := range results {
		if r.Action == ActionSkippedPolicy {
			t.Fatalf("nil policy produced a policy skip: %+v", r)
		}
	}
}
