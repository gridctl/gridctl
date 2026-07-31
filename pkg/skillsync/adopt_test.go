package skillsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
)

// seedOrigin marks a fixture skill as git-imported with a clean
// installed hash, so drift against it models what `skill update`
// checks.
func seedOrigin(t *testing.T, f *fixture, skill string) string {
	t.Helper()
	dir := filepath.Join(f.regDir, "skills", skill)
	hash, err := skills.ContentHashFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.WriteOrigin(dir, &skills.Origin{
		Repo:          "https://example.com/skills.git",
		Ref:           "main",
		CommitSHA:     "0123456789abcdef0123456789abcdef01234567",
		InstalledHash: hash,
	}); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestAdoptPullsCopyEditBackIntoRegistry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cleanHash := seedOrigin(t, f, "alpha")
	// Two copy projections of the same skill: adopt one, the other must
	// go stale.
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}, Copy: true})

	// Hand-edit the antigravity copy: change SKILL.md, add a new file.
	copyDir := f.dest(t, "antigravity", "alpha")
	edited := "---\nname: alpha\ndescription: Test skill alpha\nstate: active\n---\nAlpha body, hand-tuned.\n"
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, "scripts", "extra.sh"), []byte("#!/bin/sh\necho extra\n"), 0o755); err != nil { // #nosec G306 -- test fixture script
		t.Fatal(err)
	}

	res, err := f.mgr.Adopt(ctx, "alpha", "antigravity")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if len(res.ChangedFiles) != 2 || res.ChangedFiles[0] != "SKILL.md" || res.ChangedFiles[1] != filepath.Join("scripts", "extra.sh") {
		t.Fatalf("ChangedFiles = %v", res.ChangedFiles)
	}

	// The registry holds the adopted content.
	regDir := filepath.Join(f.regDir, "skills", "alpha")
	data, err := os.ReadFile(filepath.Join(regDir, "SKILL.md"))
	if err != nil || string(data) != edited {
		t.Errorf("registry SKILL.md not adopted: %v\n%s", err, data)
	}
	if _, err := os.Stat(filepath.Join(regDir, "scripts", "extra.sh")); err != nil {
		t.Errorf("new supporting file not adopted: %v", err)
	}

	// The prior registry SKILL.md is backed up with the origin's short
	// SHA, the pkg/skills convention.
	if res.BackupFile != "SKILL.md.pre-01234567" {
		t.Errorf("BackupFile = %q", res.BackupFile)
	}
	backup, err := os.ReadFile(filepath.Join(regDir, res.BackupFile))
	if err != nil || !strings.Contains(string(backup), "Alpha body.") {
		t.Errorf("backup must hold the pre-adopt content: %v", err)
	}

	// The origin is untouched, so `skill update` sees local edits: the
	// on-disk hash no longer matches origin.InstalledHash (the exact
	// predicate pkg/skills.DetectDrift uses).
	origin, err := skills.ReadOrigin(regDir)
	if err != nil {
		t.Fatal(err)
	}
	if origin.InstalledHash != cleanHash {
		t.Error("adopt must not advance the import origin")
	}
	nowHash, err := skills.ContentHashFile(filepath.Join(regDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if nowHash == origin.InstalledHash {
		t.Error("adopted SKILL.md must read as locally edited against the origin")
	}

	// The adopted pair is in-sync; the other copy client went stale.
	statuses, err := f.mgr.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "antigravity"); got != StateInSync {
		t.Errorf("adopted pair state = %s, want in-sync", got)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateStale {
		t.Errorf("other copy client state = %s, want stale", got)
	}
}

func TestAdoptRefusals(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.mgr.Adopt(ctx, "alpha", "cursor"); !errors.Is(err, ErrUnknownClient) {
		t.Errorf("unknown client error = %v", err)
	}
	if _, err := f.mgr.Adopt(ctx, "alpha", "antigravity"); !errors.Is(err, ErrNotProjected) {
		t.Errorf("not-projected error = %v", err)
	}

	// Symlink projections have nothing to adopt.
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	var refusal *AdoptRefusal
	_, err := f.mgr.Adopt(ctx, "alpha", "claude-code")
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "symlinked") {
		t.Errorf("symlink refusal = %v", err)
	}

	// Empty projected content is refused, mirroring context adopt.
	f.mustSync(t, []string{"beta"}, SyncOptions{Clients: []string{"antigravity"}})
	copyDir := f.dest(t, "antigravity", "beta")
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = f.mgr.Adopt(ctx, "beta", "antigravity")
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty refusal = %v", err)
	}

	// A copy whose SKILL.md renames the skill is refused.
	renamed := "---\nname: other\ndescription: Renamed\nstate: active\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = f.mgr.Adopt(ctx, "beta", "antigravity")
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "rename") {
		t.Errorf("rename refusal = %v", err)
	}

	// Refusals leave the registry untouched.
	data, err := os.ReadFile(filepath.Join(f.regDir, "skills", "beta", "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "Beta body.") {
		t.Error("refused adopt must not touch the registry")
	}
}

// TestAdoptSurvivesWatcherReconcileRoundTrip models the full loop:
// adopt writes into the watched registry tree (deliberately: it is a
// user action), the watcher-triggered refresh reloads the store, and
// the daemon reconcile must then see in-sync hashes rather than fight
// the adopt.
func TestAdoptSurvivesWatcherReconcileRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}})

	copyDir := f.dest(t, "antigravity", "alpha")
	edited := "---\nname: alpha\ndescription: Test skill alpha\nstate: active\n---\nAlpha body, adopted.\n"
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mgr.Adopt(ctx, "alpha", "antigravity"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	// The watcher fires on the registry write and the daemon reconciles
	// with a freshly loaded store (a second manager models the daemon
	// process).
	f.reload(t)
	daemon := NewManagerWithHome(f.home, f.store)
	results, err := daemon.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionUnchanged {
		t.Errorf("post-adopt reconcile action = %s, want unchanged (reconcile must not fight the adopt)", got)
	}
	data, err := os.ReadFile(filepath.Join(copyDir, "SKILL.md"))
	if err != nil || string(data) != edited {
		t.Error("adopted content must survive the reconcile")
	}
	statuses, err := daemon.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "antigravity"); got != StateInSync {
		t.Errorf("state after round-trip = %s, want in-sync", got)
	}
}
