package skillsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

// writeSkillWithModel writes an active registry skill declaring a
// top-level model preference.
func (f *fixture) writeSkillWithModel(t *testing.T, name, model, body string) {
	t.Helper()
	dir := filepath.Join(f.regDir, "skills", name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: Test skill %s\nstate: active\nmodel: %s\n---\n%s\n", name, name, model, body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho "+name+"\n"), 0o755); err != nil { // #nosec G306 -- test fixture script
		t.Fatal(err)
	}
}

func readProjectedSkillMD(t *testing.T, f *fixture, slug, skill string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dest(t, slug, skill), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func projectedModel(t *testing.T, f *fixture, slug, skill string) string {
	t.Helper()
	sk, err := registry.ParseSkillMD([]byte(readProjectedSkillMD(t, f, slug, skill)))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := sk.Extra["model"].(string)
	return v
}

func TestSkillSync_ModelPolicyForcesCopyWithReason(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})

	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionCopied {
		t.Fatalf("expected copied, got %s", got)
	}
	for _, r := range results {
		if r.Skill == "alpha" && r.Client == "claude-code" {
			if r.Channel != string(ChannelCopy) || r.Reason != ChannelReasonModelPolicy {
				t.Fatalf("expected copy channel with model-policy reason, got %+v", r)
			}
		}
	}
	if got := projectedModel(t, f, "claude-code", "alpha"); got != "sonnet" {
		t.Fatalf("projected model = %q, want sonnet", got)
	}

	// Supporting files still project.
	if _, err := os.Stat(filepath.Join(f.dest(t, "claude-code", "alpha"), "scripts", "run.sh")); err != nil {
		t.Fatalf("supporting files must ride along: %v", err)
	}

	// The registry canonical is byte-unchanged.
	canon, err := os.ReadFile(filepath.Join(f.regDir, "skills", "alpha", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canon), "model:") {
		t.Fatal("registry canonical must never gain a model key from policy")
	}

	// Lock entry carries divergent hashes and the reason.
	lf, err := f.mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := lf.entry("alpha", "claude-code")
	if e == nil || e.ChannelReason != ChannelReasonModelPolicy {
		t.Fatalf("entry should carry model-policy reason: %+v", e)
	}
	if e.InstalledHash == "" || e.InstalledHash == e.TreeHash {
		t.Fatalf("installed hash must diverge from canonical hash under rewrite: %+v", e)
	}

	// Status: in-sync while the policy matches what was written.
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateInSync {
		t.Fatalf("rewritten projection must read in-sync, got %s", got)
	}
}

func TestSkillSync_OverridesRaiseAndLower(t *testing.T) {
	f := newFixture(t)
	f.writeSkillWithModel(t, "cheap", "haiku", "Cheap body.")
	f.writeSkillWithModel(t, "fancy", "opus", "Fancy body.")
	f.reload(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Overrides: map[string]string{
		"cheap": "opus",
		"fancy": "haiku",
	}})

	f.mustSync(t, []string{"cheap", "fancy"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := projectedModel(t, f, "claude-code", "cheap"); got != "opus" {
		t.Fatalf("raise: projected model = %q, want opus", got)
	}
	if got := projectedModel(t, f, "claude-code", "fancy"); got != "haiku" {
		t.Fatalf("lower: projected model = %q, want haiku", got)
	}
}

func TestSkillSync_AuthorDeclarationPassesThrough(t *testing.T) {
	f := newFixture(t)
	f.writeSkillWithModel(t, "declared", "Sonnet", "Body.")
	f.reload(t)
	// Default equals the declaration after normalization: pure symlink
	// pass-through, no copy force, no rewrite.
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})

	results := f.mustSync(t, []string{"declared"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := actionOf(t, results, "declared", "claude-code"); got != ActionLinked {
		t.Fatalf("normalized-equal declaration must stay symlinked, got %s", got)
	}
}

func TestSkillSync_PolicyRemovalRestoresSymlink(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	// Known-absent: a loaded policy that no longer covers the skill
	// reconciles the projection back to pass-through and symlink.
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: false})
	results := f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionUpdated && got != ActionLinked {
		t.Fatalf("expected symlink restore, got %s", got)
	}
	dest := f.dest(t, "claude-code", "alpha")
	if fi, err := os.Lstat(dest); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink restored at %s (err %v)", dest, err)
	}
	lf, err := f.mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("alpha", "claude-code"); e == nil || e.Channel != ChannelSymlink || e.ChannelReason != "" {
		t.Fatalf("entry should return to plain symlink, got %+v", e)
	}
}

func TestSkillSync_StacklessPreservesRewrite(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	f.mgr.SetModelPolicy(nil)
	results := f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionUnchanged {
		t.Fatalf("stackless sync must preserve the rewrite, got %s", got)
	}
	if got := projectedModel(t, f, "claude-code", "alpha"); got != "sonnet" {
		t.Fatalf("rewritten bytes must survive a stackless sync, got model %q", got)
	}
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateInSync {
		t.Fatalf("preserved projection should read in-sync, got %s", got)
	}
	for _, s := range statuses {
		if s.Skill == "alpha" && !strings.Contains(s.Detail, "no stack loaded") {
			t.Fatalf("status should name the unknown-policy condition, got %+v", s)
		}
	}
}

func TestSkillSync_PolicyChangeReadsStale(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "opus"})
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateStale {
		t.Fatalf("policy change should read stale, got %s", got)
	}
	f.mustSync(t, nil, SyncOptions{})
	if got := projectedModel(t, f, "claude-code", "alpha"); got != "opus" {
		t.Fatalf("re-sync should apply the new policy, got %q", got)
	}
}

func TestSkillSync_SymlinkGoesStaleWhenPolicyApplies(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateStale {
		t.Fatalf("symlinked projection under an applying policy should read stale, got %s", got)
	}
	// Re-sync converts to a rewritten copy.
	f.mustSync(t, nil, SyncOptions{})
	if got := projectedModel(t, f, "claude-code", "alpha"); got != "sonnet" {
		t.Fatalf("reconcile should convert to a rewritten copy, got %q", got)
	}
}

func TestSkillSync_UserEditOfRewrittenCopyIsDrift(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	body := filepath.Join(f.dest(t, "claude-code", "alpha"), "SKILL.md")
	if err := os.WriteFile(body, []byte(readProjectedSkillMD(t, f, "claude-code", "alpha")+"\nEdited.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateDrifted {
		t.Fatalf("hand edit must read drifted, got %s", got)
	}
}

func TestSkillSync_RegistryEditReadsStale(t *testing.T) {
	f := newFixture(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	f.writeSkill(t, "alpha", "active", "Alpha body v2.")
	f.reload(t)
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateStale {
		t.Fatalf("registry edit must read stale, got %s", got)
	}
}

func TestSkillSync_DryRunCountsChannelFlips(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha", "beta"}, SyncOptions{Clients: []string{"claude-code"}})

	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	results := f.mustSync(t, nil, SyncOptions{DryRun: true})
	flips := 0
	for _, r := range results {
		if r.Reason == ChannelReasonModelPolicy && r.Channel == string(ChannelCopy) {
			flips++
		}
	}
	if flips != 2 {
		t.Fatalf("dry run should report 2 symlink-to-copy flips, got %d: %+v", flips, results)
	}
	// Dry run wrote nothing: destinations are still symlinks.
	if fi, err := os.Lstat(f.dest(t, "claude-code", "alpha")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dry run must not materialize the copy")
	}
}

func TestSkillAdopt_PolicyKeysNeverReachCanonical(t *testing.T) {
	f := newFixture(t)
	f.writeSkillWithModel(t, "declared", "opus", "Declared body.")
	f.reload(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Overrides: map[string]string{"declared": "haiku"}})
	f.mustSync(t, []string{"declared"}, SyncOptions{Clients: []string{"claude-code"}})

	// Adopt with no user edits: the only SKILL.md delta is the policy
	// rewrite, so nothing flows back.
	res, err := f.mgr.Adopt(context.Background(), "declared", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ChangedFiles) != 0 || !res.PolicyKeysRestored {
		t.Fatalf("policy-only delta must adopt nothing, got %+v", res)
	}
	canon, _ := os.ReadFile(filepath.Join(f.regDir, "skills", "declared", "SKILL.md"))
	if !strings.Contains(string(canon), "model: opus") {
		t.Fatalf("canonical declaration must be untouched:\n%s", string(canon))
	}

	// A real body edit adopts the edit but restores the author's model.
	f.mustSync(t, nil, SyncOptions{})
	projPath := filepath.Join(f.dest(t, "claude-code", "declared"), "SKILL.md")
	proj := readProjectedSkillMD(t, f, "claude-code", "declared")
	if !strings.Contains(proj, "model: haiku") {
		t.Fatalf("precondition: projection should carry the rewrite:\n%s", proj)
	}
	if err := os.WriteFile(projPath, []byte(strings.Replace(proj, "Declared body.", "Edited body.", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = f.mgr.Adopt(context.Background(), "declared", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !res.PolicyKeysRestored || !slicesContains(res.ChangedFiles, "SKILL.md") {
		t.Fatalf("expected SKILL.md adopted with policy keys restored, got %+v", res)
	}
	canon, _ = os.ReadFile(filepath.Join(f.regDir, "skills", "declared", "SKILL.md"))
	if !strings.Contains(string(canon), "Edited body.") {
		t.Fatalf("the body edit should have been adopted:\n%s", string(canon))
	}
	if !strings.Contains(string(canon), "model: opus") || strings.Contains(string(canon), "haiku") {
		t.Fatalf("the policy value must never reach the canonical:\n%s", string(canon))
	}
}

func TestSkillSync_UserCopyStaysStickyThroughPolicy(t *testing.T) {
	f := newFixture(t)
	// The user explicitly asks for a copy: no policy involved.
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}, Copy: true})

	// A policy rewrite touches the copy: bytes are rewritten, but the
	// channel was the user's choice, so no forced-channel reason.
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, nil, SyncOptions{})
	lf, err := f.mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := lf.entry("alpha", "claude-code")
	if e == nil || e.ModelValue != "sonnet" || e.ChannelReason != "" {
		t.Fatalf("user copy rewritten by policy must keep an empty channel reason, got %+v", e)
	}

	// Removing the policy restores the bytes but never the user's
	// channel choice: the copy stays a copy.
	f.mgr.SetModelPolicy(&registry.ModelPolicy{})
	f.mustSync(t, nil, SyncOptions{})
	if fi, err := os.Lstat(f.dest(t, "claude-code", "alpha")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the user's --copy must survive policy removal (err %v)", err)
	}
	lf, err = f.mgr.loadView(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := lf.entry("alpha", "claude-code"); e == nil || e.Channel != ChannelCopy || e.ModelValue != "" {
		t.Fatalf("expected a plain user copy after policy removal, got %+v", e)
	}
}

func TestSkillAdopt_UserModelEditIsAdopted(t *testing.T) {
	f := newFixture(t)
	f.writeSkillWithModel(t, "declared", "opus", "Body.")
	f.reload(t)
	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Overrides: map[string]string{"declared": "haiku"}})
	f.mustSync(t, []string{"declared"}, SyncOptions{Clients: []string{"claude-code"}})

	// The user deliberately edits the projected model to a THIRD value:
	// that is author intent, not the policy's write, and it adopts.
	projPath := filepath.Join(f.dest(t, "claude-code", "declared"), "SKILL.md")
	proj := readProjectedSkillMD(t, f, "claude-code", "declared")
	if err := os.WriteFile(projPath, []byte(strings.Replace(proj, "model: haiku", "model: sonnet", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := f.mgr.Adopt(context.Background(), "declared", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if res.PolicyKeysRestored {
		t.Fatalf("a deliberate user model edit is not policy-owned, got %+v", res)
	}
	canon, _ := os.ReadFile(filepath.Join(f.regDir, "skills", "declared", "SKILL.md"))
	if !strings.Contains(string(canon), "model: sonnet") {
		t.Fatalf("the user's model edit should have been adopted:\n%s", string(canon))
	}
}

func TestSkillSync_MigrateOnRead_LegacySingleHash(t *testing.T) {
	f := newFixture(t)
	// Project a plain copy (no policy): the lockfile stores the legacy
	// single-hash shape (InstalledHash empty on disk).
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}, Copy: true})
	lf, err := readLockFile(f.mgr.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	e := lf.entry("alpha", "claude-code")
	if e == nil || e.InstalledHash != e.TreeHash || e.InstalledHash == "" {
		t.Fatalf("migrate-on-read should equate installed and canonical for legacy entries, got %+v", e)
	}
	// And the projection reads in-sync through the dual-hash status path.
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateInSync {
		t.Fatalf("legacy copy should read in-sync, got %s", got)
	}
}

func TestSkillSync_PolicyRewriteTripsNoPinDrift(t *testing.T) {
	// Pins hash the registry canonical, which rewrite never touches: a
	// policy rewrite (and its removal) must leave the canonical skill
	// hash byte-identical. This is the invariant that makes projection
	// rewrite safe to compose with skill governance pins.
	f := newFixture(t)
	before, err := skillpins.CanonicalSkillHash(mustGetSkill(t, f, "alpha"))
	if err != nil {
		t.Fatal(err)
	}

	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: true, Default: "sonnet"})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	f.reload(t)
	after, err := skillpins.CanonicalSkillHash(mustGetSkill(t, f, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("policy rewrite must not move the canonical pin hash: %s != %s", before, after)
	}

	f.mgr.SetModelPolicy(&registry.ModelPolicy{Rewrite: false})
	f.mustSync(t, nil, SyncOptions{})
	f.reload(t)
	restored, err := skillpins.CanonicalSkillHash(mustGetSkill(t, f, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if before != restored {
		t.Fatalf("policy removal must not move the canonical pin hash: %s != %s", before, restored)
	}
}

func mustGetSkill(t *testing.T, f *fixture, name string) *registry.AgentSkill {
	t.Helper()
	sk, err := f.store.GetSkill(name)
	if err != nil {
		t.Fatal(err)
	}
	return sk
}

func TestSkillHonorMatrix_CoversEveryTarget(t *testing.T) {
	matrix := registry.SkillHonorMatrix()
	for _, tgt := range Targets() {
		if _, ok := matrix[tgt.Slug]; !ok {
			t.Errorf("skill projection target %q has no honor matrix row; add it to pkg/registry/honor.go", tgt.Slug)
		}
	}
	for slug := range matrix {
		if _, ok := FindTarget(slug); !ok {
			t.Errorf("honor matrix row %q names no existing skill target", slug)
		}
	}
}
