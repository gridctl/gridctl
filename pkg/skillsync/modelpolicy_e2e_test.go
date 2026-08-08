package skillsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

// TestModelPolicy_EndToEnd exercises the full path a stack file drives:
// parse stack.yaml → compile model policies → sync (rewritten copy,
// forced channel, recorded reason) → policy removal → re-sync (symlink
// restored) → pin store input unchanged throughout. No daemon, no
// Docker: projection is a pure file feature.
func TestModelPolicy_EndToEnd(t *testing.T) {
	f := newFixture(t)

	stackDir := t.TempDir()
	stackPath := filepath.Join(stackDir, "stack.yaml")
	writeStack := func(body string) {
		t.Helper()
		if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeStack(`
version: "1"
name: e2e
network:
  name: net
mcp-servers:
  - name: s1
    url: https://api.example.com/mcp
model_preferences:
  skills:
    rewrite: true
    default: sonnet
    overrides:
      beta: opus
`)

	pinBefore, err := skillpins.CanonicalSkillHash(mustGetSkill(t, f, "alpha"))
	if err != nil {
		t.Fatal(err)
	}

	stack, _, err := config.ValidateStackFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	skillPol, _ := stack.ModelPolicies()
	if skillPol == nil || !skillPol.Rewrite {
		t.Fatalf("compiled policy = %+v", skillPol)
	}
	f.mgr.SetModelPolicy(skillPol)

	results := f.mustSync(t, []string{"alpha", "beta"}, SyncOptions{Clients: []string{"claude-code"}})
	for _, r := range results {
		if r.Channel != string(ChannelCopy) || r.Reason != ChannelReasonModelPolicy {
			t.Fatalf("expected policy-forced copies, got %+v", r)
		}
	}
	if got := projectedModel(t, f, "claude-code", "alpha"); got != "sonnet" {
		t.Fatalf("default should apply to alpha, got %q", got)
	}
	if got := projectedModel(t, f, "claude-code", "beta"); got != "opus" {
		t.Fatalf("override should apply to beta, got %q", got)
	}
	lf, err := readLockFile(f.mgr.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("alpha", "claude-code"); e == nil || e.ChannelReason != ChannelReasonModelPolicy || e.InstalledHash == e.TreeHash {
		t.Fatalf("lock entry should record the rewrite: %+v", e)
	}

	// Delete the block entirely and re-load: a loaded stack compiles to
	// non-nil empty policies with or without the block, so removal is
	// the documented known-absent off switch and reconciles projections
	// back to pass-through (only a missing stack preserves).
	writeStack(`
version: "1"
name: e2e
network:
  name: net
mcp-servers:
  - name: s1
    url: https://api.example.com/mcp
`)
	stack, _, err = config.ValidateStackFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	skillPol, _ = stack.ModelPolicies()
	if skillPol == nil {
		t.Fatal("a loaded stack must compile to a non-nil (known-absent) policy even without the block")
	}
	f.mgr.SetModelPolicy(skillPol)
	f.mustSync(t, nil, SyncOptions{})

	for _, name := range []string{"alpha", "beta"} {
		dest := f.dest(t, "claude-code", name)
		if fi, err := os.Lstat(dest); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s should be a symlink again after rewrite: false (err %v)", name, err)
		}
	}
	lf, err = readLockFile(f.mgr.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("alpha", "claude-code"); e == nil || e.Channel != ChannelSymlink || e.ChannelReason != "" {
		t.Fatalf("entry should return to plain symlink: %+v", e)
	}

	f.reload(t)
	pinAfter, err := skillpins.CanonicalSkillHash(mustGetSkill(t, f, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if pinBefore != pinAfter {
		t.Fatalf("pin hash must be untouched end to end: %s != %s", pinBefore, pinAfter)
	}

	// The canonical files never gained a model key.
	canon, err := os.ReadFile(filepath.Join(f.regDir, "skills", "alpha", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canon), "model:") {
		t.Fatal("registry canonical must stay untouched end to end")
	}
}
