package skillsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

// fixture stages a registry skills dir alongside fake native client
// skill locations under one temp home: the combined harness no other
// package provides.
type fixture struct {
	home   string
	regDir string
	store  *registry.Store
	mgr    *Manager
}

// newFixture builds a temp home with an active "alpha" and "beta", a
// draft "gamma", and initialized ~/.claude and ~/.gemini/config trees.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	regDir := filepath.Join(home, ".gridctl", "registry")
	f := &fixture{home: home, regDir: regDir}
	f.writeSkill(t, "alpha", "active", "Alpha body.")
	f.writeSkill(t, "beta", "active", "Beta body.")
	f.writeSkill(t, "gamma", "draft", "Gamma body.")
	for _, d := range []string{".claude", filepath.Join(".gemini", "config")} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.store = registry.NewStore(regDir)
	f.reload(t)
	f.mgr = NewManagerWithHome(home, f.store)
	return f
}

// writeSkill writes a registry skill dir with a SKILL.md and one script.
func (f *fixture) writeSkill(t *testing.T, name, state, body string) {
	t.Helper()
	dir := filepath.Join(f.regDir, "skills", name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: Test skill %s\nstate: %s\n---\n%s\n", name, name, state, body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho "+name+"\n"), 0o755); err != nil { // #nosec G306 -- test fixture script
		t.Fatal(err)
	}
}

// reload refreshes the store cache from disk.
func (f *fixture) reload(t *testing.T) {
	t.Helper()
	if err := f.store.Load(); err != nil {
		t.Fatal(err)
	}
}

// dest returns the projection destination for (slug, skill).
func (f *fixture) dest(t *testing.T, slug, skill string) string {
	t.Helper()
	tgt, ok := FindTarget(slug)
	if !ok {
		t.Fatalf("unknown slug %s", slug)
	}
	return filepath.Join(tgt.skillsDir(f.home), skill)
}

// mustSync runs Sync and fails the test on error.
func (f *fixture) mustSync(t *testing.T, names []string, opts SyncOptions) []SyncResult {
	t.Helper()
	results, err := f.mgr.Sync(context.Background(), names, opts)
	if err != nil {
		t.Fatalf("Sync(%v): %v", names, err)
	}
	return results
}

// actionOf returns the action for one (skill, client) result row.
func actionOf(t *testing.T, results []SyncResult, skill, client string) string {
	t.Helper()
	for _, r := range results {
		if r.Skill == skill && r.Client == client {
			return r.Action
		}
	}
	t.Fatalf("no result for (%s, %s) in %+v", skill, client, results)
	return ""
}

// stateOf returns the state for one (skill, client) status row.
func stateOf(t *testing.T, statuses []ProjectionStatus, skill, client string) string {
	t.Helper()
	for _, s := range statuses {
		if s.Skill == skill && s.Client == client {
			return s.State
		}
	}
	t.Fatalf("no status for (%s, %s) in %+v", skill, client, statuses)
	return ""
}

func TestNewManagerUsesUserHome(t *testing.T) {
	f := newFixture(t)
	mgr, err := NewManager(f.store)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got := mgr.LockPath(); got != filepath.Join(home, ".gridctl", lockFileName) {
		t.Errorf("LockPath() = %s", got)
	}
}

func TestTargetsTable(t *testing.T) {
	targets := Targets()
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}
	byslug := map[string]Target{}
	for _, tgt := range targets {
		byslug[tgt.Slug] = tgt
	}
	if !byslug["agents"].AlwaysAvailable {
		t.Error("agents target must be always available")
	}
	if byslug["antigravity"].ForcedChannel != ChannelCopy {
		t.Error("antigravity must be copy-forced")
	}
	if byslug["claude-code"].DefaultChannel != ChannelSymlink {
		t.Error("claude-code must default to symlink")
	}
	if _, ok := FindTarget("nope"); ok {
		t.Error("FindTarget should miss unknown slugs")
	}
	slugs := SupportedSlugs()
	if len(slugs) != 3 || slugs[0] != "agents" {
		t.Errorf("SupportedSlugs() = %v", slugs)
	}
}

func TestTargetAvailabilityAndChannel(t *testing.T) {
	home := t.TempDir()
	agents, _ := FindTarget("agents")
	claude, _ := FindTarget("claude-code")
	anti, _ := FindTarget("antigravity")
	if !agents.available(home) {
		t.Error("agents must be available on a bare home")
	}
	if claude.available(home) {
		t.Error("claude-code must be unavailable without ~/.claude")
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !claude.available(home) {
		t.Error("claude-code must be available once ~/.claude exists")
	}
	if got := anti.channel(false); got != ChannelCopy {
		t.Errorf("antigravity channel(false) = %s, want copy (forced)", got)
	}
	if got := claude.channel(false); got != ChannelSymlink {
		t.Errorf("claude channel(false) = %s, want symlink", got)
	}
	if got := claude.channel(true); got != ChannelCopy {
		t.Errorf("claude channel(true) = %s, want copy", got)
	}
}

func TestSyncSymlinkToClaudeCode(t *testing.T) {
	f := newFixture(t)
	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionLinked {
		t.Fatalf("action = %s, want linked", got)
	}
	dest := f.dest(t, "claude-code", "alpha")
	link, err := os.Readlink(dest)
	if err != nil {
		t.Fatalf("dest is not a symlink: %v", err)
	}
	if want := filepath.Join(f.regDir, "skills", "alpha"); link != want {
		t.Errorf("link = %s, want %s", link, want)
	}
	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "Alpha body.") {
		t.Errorf("SKILL.md unreadable through link: %v", err)
	}
	// Second sync is unchanged.
	results = f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionUnchanged {
		t.Errorf("re-sync action = %s, want unchanged", got)
	}
}

func TestSyncCreatesAgentsDirOnBareHome(t *testing.T) {
	f := newFixture(t)
	// Simulate a pure Grok Build machine: no ~/.agents at all.
	if _, err := os.Stat(filepath.Join(f.home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("fixture should start without ~/.agents")
	}
	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"agents"}})
	if got := actionOf(t, results, "alpha", "agents"); got != ActionLinked {
		t.Fatalf("action = %s, want linked", got)
	}
	if _, err := os.Readlink(f.dest(t, "agents", "alpha")); err != nil {
		t.Errorf("expected symlink under created ~/.agents/skills: %v", err)
	}
}

func TestSyncCopyForcedForAntigravity(t *testing.T) {
	f := newFixture(t)
	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}})
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionCopied {
		t.Fatalf("action = %s, want copied", got)
	}
	dest := f.dest(t, "antigravity", "alpha")
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("antigravity projection must be a real directory, not a symlink")
	}
	if _, err := os.Stat(filepath.Join(dest, "scripts", "run.sh")); err != nil {
		t.Errorf("supporting files must be copied: %v", err)
	}
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "antigravity"); got != StateInSync {
		t.Errorf("state = %s, want in-sync", got)
	}
}

func TestSyncRejectsInactiveAndUnknownSkills(t *testing.T) {
	f := newFixture(t)
	_, err := f.mgr.Sync(context.Background(), []string{"gamma", "nope"}, SyncOptions{})
	if err == nil {
		t.Fatal("expected error for draft and unknown skills")
	}
	for _, want := range []string{"gamma (draft)", "nope (not found)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if _, serr := os.Lstat(f.dest(t, "claude-code", "gamma")); !os.IsNotExist(serr) {
		t.Error("nothing may be written when validation fails")
	}
}

func TestSyncSkipsUnmanagedPathWithoutForce(t *testing.T) {
	f := newFixture(t)
	dest := f.dest(t, "claude-code", "alpha")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dest, "SKILL.md")
	if err := os.WriteFile(foreign, []byte("user-installed skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionSkippedUnmanaged {
		t.Fatalf("action = %s, want skipped-unmanaged", got)
	}
	data, err := os.ReadFile(foreign)
	if err != nil || string(data) != "user-installed skill" {
		t.Fatal("unmanaged path must be untouched")
	}
	if !HasFailures(results) {
		t.Error("skipped-unmanaged must count as a failure for exit codes")
	}

	// --force backs up, then replaces.
	results = f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}, Force: true})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionUpdated {
		t.Fatalf("forced action = %s, want updated", got)
	}
	var backup string
	for _, r := range results {
		if r.Skill == "alpha" {
			backup = r.BackupPath
		}
	}
	if backup == "" {
		t.Fatal("forced replace must record a backup path")
	}
	if strings.HasPrefix(backup, filepath.Dir(dest)) {
		t.Errorf("backup %s must not live inside the scanned skills dir", backup)
	}
	data, err = os.ReadFile(filepath.Join(backup, "SKILL.md"))
	if err != nil || string(data) != "user-installed skill" {
		t.Errorf("backup must preserve the foreign content: %v", err)
	}
	if _, err := os.Readlink(dest); err != nil {
		t.Errorf("dest must now be the managed symlink: %v", err)
	}
}

func TestSyncExplicitUnavailableClientErrors(t *testing.T) {
	f := newFixture(t)
	if err := os.RemoveAll(filepath.Join(f.home, ".claude")); err != nil {
		t.Fatal(err)
	}
	_, err := f.mgr.Sync(context.Background(), []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected ErrNotAvailable, got %v", err)
	}
	_, err = f.mgr.Sync(context.Background(), []string{"alpha"}, SyncOptions{Clients: []string{"cursor"}})
	if err == nil || !strings.Contains(err.Error(), "unknown client") {
		t.Fatalf("expected ErrUnknownClient, got %v", err)
	}
}

func TestSyncDefaultTargetsSkipUnavailable(t *testing.T) {
	f := newFixture(t)
	if err := os.RemoveAll(filepath.Join(f.home, ".claude")); err != nil {
		t.Fatal(err)
	}
	results := f.mustSync(t, []string{"alpha"}, SyncOptions{})
	var skippedClaude bool
	for _, r := range results {
		if r.Client == "claude-code" && r.Action == ActionSkippedUnavailable {
			skippedClaude = true
		}
	}
	if !skippedClaude {
		t.Errorf("claude-code must be reported skipped-unavailable: %+v", results)
	}
	if got := actionOf(t, results, "alpha", "agents"); got != ActionLinked {
		t.Errorf("agents action = %s, want linked", got)
	}
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionCopied {
		t.Errorf("antigravity action = %s, want copied", got)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	f := newFixture(t)
	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}, DryRun: true})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionWouldLink {
		t.Fatalf("action = %s, want would-link", got)
	}
	if _, err := os.Lstat(f.dest(t, "claude-code", "alpha")); !os.IsNotExist(err) {
		t.Error("dry-run must not write the projection")
	}
	if _, err := os.Stat(f.mgr.LockPath()); !os.IsNotExist(err) {
		t.Error("dry-run must not write the lockfile")
	}
	// Copy channel dry-run.
	results = f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}, DryRun: true})
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionWouldCopy {
		t.Errorf("action = %s, want would-copy", got)
	}
}

func TestCopyDriftSkipsThenForceOverwrites(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}})
	dest := f.dest(t, "antigravity", "alpha")
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "antigravity"); got != StateDrifted {
		t.Fatalf("state = %s, want drifted", got)
	}
	if !NeedsAttention(statuses) {
		t.Error("drift must need attention")
	}

	// Reconcile (no names) never forces over drift.
	results := f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionSkippedDrift {
		t.Fatalf("reconcile action = %s, want skipped-drift", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "SKILL.md")); string(data) != "hand edit" {
		t.Fatal("drifted copy must be untouched without --force")
	}

	results = f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}, Force: true})
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionUpdated {
		t.Fatalf("forced action = %s, want updated", got)
	}
	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "Alpha body.") {
		t.Error("forced sync must restore registry content")
	}
}

func TestCopyStaleAfterRegistryEditThenRefresh(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}})
	f.writeSkill(t, "alpha", "active", "Alpha body, revised.")
	f.reload(t)

	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "antigravity"); got != StateStale {
		t.Fatalf("state = %s, want stale", got)
	}

	results := f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "antigravity"); got != ActionUpdated {
		t.Fatalf("reconcile action = %s, want updated", got)
	}
	data, err := os.ReadFile(filepath.Join(f.dest(t, "antigravity", "alpha"), "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "revised") {
		t.Error("stale copy must be refreshed")
	}
	// A clean managed refresh backs nothing up (the copy was
	// registry-derived) and leaves no phantom siblings in the scanned
	// skills dir.
	for _, r := range results {
		if r.BackupPath != "" {
			t.Errorf("clean refresh must not create a backup, got %s", r.BackupPath)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(f.dest(t, "antigravity", "alpha")))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "alpha" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("scanned skills dir must hold only the skill, found %v", names)
	}
}

func TestReconcileRemovesDeactivatedSkill(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	f.writeSkill(t, "alpha", "draft", "Alpha body.")
	f.reload(t)

	results := f.mustSync(t, nil, SyncOptions{})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionRemoved {
		t.Fatalf("action = %s, want removed", got)
	}
	if _, err := os.Lstat(f.dest(t, "claude-code", "alpha")); !os.IsNotExist(err) {
		t.Error("deactivated skill's projection must be removed")
	}
	has, err := f.mgr.HasProjections()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("projection set must be empty after removal")
	}
}

func TestReconcileRepairsMissingSymlink(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	dest := f.dest(t, "claude-code", "alpha")
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}

	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateTargetMissing {
		t.Fatalf("state = %s, want target-missing", got)
	}

	results, err := f.mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionLinked {
		t.Fatalf("reconcile action = %s, want linked", got)
	}
	if _, err := os.Readlink(dest); err != nil {
		t.Error("symlink must be repaired")
	}
}

func TestRepointedSymlinkIsDriftNotAutoRepair(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	dest := f.dest(t, "claude-code", "alpha")
	elsewhere := filepath.Join(f.regDir, "skills", "beta")
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, dest); err != nil {
		t.Fatal(err)
	}

	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, statuses, "alpha", "claude-code"); got != StateDrifted {
		t.Fatalf("state = %s, want drifted", got)
	}

	results := f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionSkippedDrift {
		t.Fatalf("action = %s, want skipped-drift", got)
	}
	if link, _ := os.Readlink(dest); link != elsewhere {
		t.Fatal("re-pointed symlink must survive a plain sync")
	}

	results = f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}, Force: true})
	if got := actionOf(t, results, "alpha", "claude-code"); got != ActionUpdated {
		t.Fatalf("forced action = %s, want updated", got)
	}
	if link, _ := os.Readlink(dest); link != filepath.Join(f.regDir, "skills", "alpha") {
		t.Fatal("forced sync must restore the registry link")
	}
}

func TestReconcileNoOpWhenNothingProjected(t *testing.T) {
	f := newFixture(t)
	results, err := f.mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %+v", results)
	}
}

func TestUnsyncPreservesUserFiles(t *testing.T) {
	f := newFixture(t)
	userFile := filepath.Join(f.home, ".claude", "skills", "my-own", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.mustSync(t, []string{"alpha", "beta"}, SyncOptions{Clients: []string{"claude-code"}})
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"antigravity"}})

	results, err := f.mgr.Unsync(context.Background(), nil, UnsyncOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 removals, got %d", len(results))
	}
	var copyBackup string
	for _, r := range results {
		if r.Action != ActionRemoved {
			t.Errorf("(%s, %s) action = %s, want removed", r.Skill, r.Client, r.Action)
		}
		if r.Client == "antigravity" {
			copyBackup = r.BackupPath
		}
	}
	if copyBackup == "" {
		t.Error("removed copy must leave a timestamped backup")
	}
	// The backup must live outside the client's scanned skills dir: a
	// sibling backup containing SKILL.md would surface as a phantom
	// skill and keep the removed skill alive in the client.
	antiSkillsDir := filepath.Dir(f.dest(t, "antigravity", "alpha"))
	if strings.HasPrefix(copyBackup, antiSkillsDir) {
		t.Errorf("backup %s must not live inside the scanned skills dir %s", copyBackup, antiSkillsDir)
	}
	if entries, err := os.ReadDir(antiSkillsDir); err == nil && len(entries) != 0 {
		t.Errorf("scanned skills dir must be empty after unsync, found %v", entries)
	}
	for _, skill := range []string{"alpha", "beta"} {
		if _, err := os.Lstat(f.dest(t, "claude-code", skill)); !os.IsNotExist(err) {
			t.Errorf("%s projection must be gone", skill)
		}
	}
	data, err := os.ReadFile(userFile)
	if err != nil || string(data) != "mine" {
		t.Error("user-created files must be untouched byte-for-byte")
	}
	has, err := f.mgr.HasProjections()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("lockfile must be empty after unsync --all")
	}
}

func TestUnsyncNamedAndErrors(t *testing.T) {
	f := newFixture(t)
	if _, err := f.mgr.Unsync(context.Background(), nil, UnsyncOptions{}); err == nil {
		t.Error("unsync without names or --all must error")
	}
	if _, err := f.mgr.Unsync(context.Background(), []string{"alpha"}, UnsyncOptions{}); err == nil || !strings.Contains(err.Error(), "not projected") {
		t.Errorf("unsync of unprojected skill must error, got %v", err)
	}
	f.mustSync(t, []string{"alpha", "beta"}, SyncOptions{Clients: []string{"claude-code"}})
	results, err := f.mgr.Unsync(context.Background(), []string{"alpha"}, UnsyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Skill != "alpha" {
		t.Fatalf("expected one alpha removal, got %+v", results)
	}
	if _, err := os.Readlink(f.dest(t, "claude-code", "beta")); err != nil {
		t.Error("beta projection must survive alpha's unsync")
	}
	// Dry-run removal reports without touching disk.
	dry, err := f.mgr.Unsync(context.Background(), []string{"beta"}, UnsyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(dry) != 1 || dry[0].Action != ActionWouldRemove {
		t.Fatalf("dry-run = %+v, want would-remove", dry)
	}
	if _, err := os.Readlink(f.dest(t, "claude-code", "beta")); err != nil {
		t.Error("dry-run unsync must not remove anything")
	}
}

func TestLockfileVersionGuard(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.mgr.LockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.mgr.LockPath(), []byte("version: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mgr.Sync(context.Background(), []string{"alpha"}, SyncOptions{}); err == nil || !strings.Contains(err.Error(), "newer gridctl") {
		t.Errorf("Sync must reject a newer lockfile, got %v", err)
	}
	if _, err := f.mgr.Statuses(context.Background()); err == nil {
		t.Error("Statuses must reject a newer lockfile")
	}
}

func TestTreeHash(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.regDir, "skills", "alpha")
	h1, err := treeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h1, hashScheme) {
		t.Errorf("hash %q missing scheme prefix", h1)
	}
	h2, err := treeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error("tree hash must be deterministic")
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "extra.md"), []byte("x"), 0o644); err != nil {
		if merr := os.MkdirAll(filepath.Join(dir, "references"), 0o755); merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(filepath.Join(dir, "references", "extra.md"), []byte("x"), 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	h3, err := treeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Error("adding a file must change the tree hash")
	}
}

func TestConcurrentSyncAndReconcileDoNotCorruptLockfile(t *testing.T) {
	f := newFixture(t)
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code"}})

	// A second Manager over the same home models the daemon process; the
	// in-process mutexes are distinct, so serialization rides on the
	// flock alone.
	daemonMgr := NewManagerWithHome(f.home, f.store)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := f.mgr.Sync(context.Background(), []string{"beta"}, SyncOptions{Clients: []string{"claude-code"}}); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := daemonMgr.Reconcile(context.Background()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent operation failed: %v", err)
	}
	lf, err := readLockFile(f.mgr.LockPath())
	if err != nil {
		t.Fatalf("lockfile corrupt after concurrent access: %v", err)
	}
	if lf.entry("alpha", "claude-code") == nil || lf.entry("beta", "claude-code") == nil {
		t.Errorf("lockfile lost entries: %+v", lf.Projections)
	}
}

func TestStatusesEmptyAndHelpers(t *testing.T) {
	f := newFixture(t)
	statuses, err := f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected no statuses, got %+v", statuses)
	}
	if NeedsAttention(statuses) {
		t.Error("empty set needs no attention")
	}
	if HasFailures(nil) {
		t.Error("no results, no failures")
	}
	f.mustSync(t, []string{"alpha"}, SyncOptions{Clients: []string{"claude-code", "antigravity"}})
	statuses, err = f.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	for _, s := range statuses {
		if s.State != StateInSync {
			t.Errorf("(%s, %s) state = %s, want in-sync", s.Skill, s.Client, s.State)
		}
		if s.SyncedAt == nil {
			t.Error("synced_at must be recorded")
		}
	}
	// Statuses are sorted skill, then client.
	if statuses[0].Client != "antigravity" || statuses[1].Client != "claude-code" {
		t.Errorf("statuses must sort by client within a skill: %+v", statuses)
	}
}
