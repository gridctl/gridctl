package resetops

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// TestMain would normally sandbox HOME; resetops tests sandbox via
// GRIDCTL_HOME (the feature's own mechanism) per test instead.

func sandboxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	// Never probe the machine's real gateway port from unit tests.
	origFind, origHome := findOrphanFn, daemonHomeFn
	findOrphanFn = func(int) (int, bool, error) { return 0, false, nil }
	daemonHomeFn = func(int) (string, bool) { return "", false }
	t.Cleanup(func() { findOrphanFn, daemonHomeFn = origFind, origHome })
	return home
}

// --- fakes -----------------------------------------------------------------

type fakeSkills struct {
	statuses []skillsync.ProjectionStatus
	unsynced [][]string
	err      error
}

func (f *fakeSkills) Statuses(context.Context) ([]skillsync.ProjectionStatus, error) {
	return f.statuses, f.err
}

func (f *fakeSkills) Unsync(_ context.Context, names []string, _ skillsync.UnsyncOptions) ([]skillsync.UnsyncResult, error) {
	f.unsynced = append(f.unsynced, names)
	var out []skillsync.UnsyncResult
	for _, n := range names {
		out = append(out, skillsync.UnsyncResult{Skill: n, Client: "claude-code", Action: "removed"})
	}
	return out, nil
}

type fakeAgents struct {
	statuses []agentsync.ProjectionStatus
	unsynced [][]string
}

func (f *fakeAgents) Statuses(context.Context) ([]agentsync.ProjectionStatus, error) {
	return f.statuses, nil
}

func (f *fakeAgents) Unsync(_ context.Context, names []string, _ agentsync.UnsyncOptions) ([]agentsync.UnsyncResult, error) {
	f.unsynced = append(f.unsynced, names)
	var out []agentsync.UnsyncResult
	for _, n := range names {
		out = append(out, agentsync.UnsyncResult{Agent: n, Client: "claude-code", Action: "removed"})
	}
	return out, nil
}

type fakeContexts struct {
	statuses []contexts.ClientStatus
	unsynced []string
}

func (f *fakeContexts) Statuses(context.Context) ([]contexts.ClientStatus, error) {
	return f.statuses, nil
}

func (f *fakeContexts) Unsync(_ context.Context, slug string) ([]contexts.UnsyncResult, error) {
	f.unsynced = append(f.unsynced, slug)
	return []contexts.UnsyncResult{{Slug: slug, Action: "removed-file"}}, nil
}

type fakeRuntime struct {
	downed []string
	err    error
}

func (f *fakeRuntime) Down(_ context.Context, stack string) error {
	if f.err != nil {
		return f.err
	}
	f.downed = append(f.downed, stack)
	return nil
}

// --- tests -----------------------------------------------------------------

func TestPreview_EmptyHome(t *testing.T) {
	home := sandboxHome(t)
	m := &Managers{Home: home}

	doc, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(doc.Rows) != 0 || doc.Failed != 0 {
		t.Errorf("empty home should preview zero rows, got %+v", doc.Rows)
	}
	if !doc.DryRun {
		t.Error("preview doc must be marked dry_run")
	}
}

func TestPreview_DriftKept(t *testing.T) {
	home := sandboxHome(t)
	m := &Managers{
		Home: home,
		Skills: &fakeSkills{statuses: []skillsync.ProjectionStatus{
			{Skill: "clean", Client: "claude-code", State: skillsync.StateInSync, Target: filepath.Join(home, ".claude", "skills", "clean")},
			{Skill: "edited", Client: "claude-code", State: skillsync.StateDrifted, Target: filepath.Join(home, ".claude", "skills", "edited")},
		}},
	}

	doc, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	actions := map[string]string{}
	for _, r := range doc.Rows {
		actions[r.Name] = r.Action
	}
	if actions["clean"] != ActionWouldRemove {
		t.Errorf("clean skill: action = %q, want %q", actions["clean"], ActionWouldRemove)
	}
	if actions["edited"] != ActionKeptDrift {
		t.Errorf("edited skill: action = %q, want %q (drift must be kept without --force)", actions["edited"], ActionKeptDrift)
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "skill/edited" {
		t.Errorf("Kept = %v, want [skill/edited]", doc.Kept)
	}

	// --force removes the drift-kept entry too.
	forced, err := m.Preview(context.Background(), Options{Force: true})
	if err != nil {
		t.Fatalf("Preview force: %v", err)
	}
	for _, r := range forced.Rows {
		if r.Action == ActionKeptDrift {
			t.Errorf("forced preview still keeps %s", r.Name)
		}
	}
}

func TestExecute_DriftPreFilteredFromUnsync(t *testing.T) {
	home := sandboxHome(t)
	skills := &fakeSkills{statuses: []skillsync.ProjectionStatus{
		{Skill: "clean", Client: "claude-code", State: skillsync.StateInSync},
		{Skill: "edited", Client: "claude-code", State: skillsync.StateDrifted},
	}}
	m := &Managers{Home: home, Skills: skills}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(skills.unsynced) != 1 {
		t.Fatalf("expected one Unsync call, got %d", len(skills.unsynced))
	}
	if got := skills.unsynced[0]; len(got) != 1 || got[0] != "clean" {
		t.Errorf("Unsync received %v; the drifted skill must be pre-filtered (Article XVI)", got)
	}
	if doc.Failed != 0 {
		t.Errorf("Failed = %d, want 0", doc.Failed)
	}
}

func TestExecute_BackupFailClosed(t *testing.T) {
	home := sandboxHome(t)
	gridctlDir := filepath.Join(home, ".gridctl")
	// Make the backups path impossible: a FILE where the directory goes.
	if err := os.MkdirAll(gridctlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gridctlDir, "backups"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "skills", "s")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := &fakeSkills{statuses: []skillsync.ProjectionStatus{
		{Skill: "s", Client: "claude-code", State: skillsync.StateInSync, Target: target},
	}}
	m := &Managers{Home: home, Skills: skills}

	_, err := m.Execute(context.Background(), Options{}, nil)
	if err == nil {
		t.Fatal("Execute must fail when the backup cannot be written")
	}
	if len(skills.unsynced) != 0 {
		t.Error("nothing may be removed after a failed backup (fail closed)")
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Error("projection target must be untouched after a failed backup")
	}
}

func TestExecute_PurgeRemovesVersionsStore(t *testing.T) {
	home := sandboxHome(t)
	gridctlDir := filepath.Join(home, ".gridctl")
	binDir := filepath.Join(gridctlDir, "versions", "v1.0.0")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gridctlDir, "versions", "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "gridctl"), []byte("retained"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := &Managers{Home: home}
	if _, err := m.Execute(context.Background(), Options{Purge: true}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "versions")); !os.IsNotExist(err) {
		t.Fatalf("versions store must be removed by --purge, stat err = %v", err)
	}
}

func TestExecute_PurgeRemovesDirAndBackupSurvives(t *testing.T) {
	home := sandboxHome(t)
	gridctlDir := filepath.Join(home, ".gridctl")
	for _, d := range []string{"vault", "oauth", "state", "pins", "registry"} {
		if err := os.MkdirAll(filepath.Join(gridctlDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(gridctlDir, rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("vault/secrets.json", "{}")
	mustWrite("oauth/tokens.enc", "sealed")
	mustWrite("oauth/key", "machinekey")
	mustWrite("state/demo.json", "{}")
	mustWrite("pins/demo.json", "{}")

	m := &Managers{Home: home}
	doc, err := m.Execute(context.Background(), Options{Purge: true}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(gridctlDir); !os.IsNotExist(err) {
		t.Error(".gridctl must be removed by --purge")
	}
	if doc.BackupPath == "" {
		t.Fatal("purge must report a backup path")
	}
	if filepath.Dir(doc.BackupPath) != home {
		t.Errorf("purge backup at %s; must live OUTSIDE the purged tree, beside it", doc.BackupPath)
	}
	if _, err := os.Stat(doc.BackupPath); err != nil {
		t.Fatalf("purge backup must survive the purge: %v", err)
	}

	names := tarNames(t, doc.BackupPath)
	if !names[".gridctl/vault/secrets.json"] {
		t.Errorf("backup must include the vault; has %v", names)
	}
	if !names[".gridctl/pins/demo.json"] {
		t.Errorf("backup must include pins; has %v", names)
	}
	for n := range names {
		if n == ".gridctl/oauth/tokens.enc" || n == ".gridctl/oauth/key" || n == ".gridctl/state/demo.json" {
			t.Errorf("backup must exclude oauth and daemon state, found %s", n)
		}
	}
}

func TestExecute_Idempotent(t *testing.T) {
	home := sandboxHome(t)
	skills := &fakeSkills{statuses: []skillsync.ProjectionStatus{
		{Skill: "s", Client: "claude-code", State: skillsync.StateInSync},
	}}
	m := &Managers{Home: home, Skills: skills}

	if _, err := m.Execute(context.Background(), Options{}, nil); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	// Second run: the fake now reports nothing projected.
	skills.statuses = nil
	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if doc.Failed != 0 {
		t.Errorf("re-run on a clean system must not fail; doc=%+v", doc)
	}
}

func TestExecute_RuntimeUnavailableReported(t *testing.T) {
	home := sandboxHome(t)
	// One recorded daemon state file under the sandbox home.
	if err := state.Save(&state.DaemonState{StackName: "demo", PID: 999999}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m := &Managers{Home: home} // Runtime nil

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var containerRow *Row
	for i := range doc.Rows {
		if doc.Rows[i].Kind == "containers" {
			containerRow = &doc.Rows[i]
		}
	}
	if containerRow == nil || containerRow.Action != ActionSkipped {
		t.Fatalf("missing runtime must yield a skipped containers row, got %+v", containerRow)
	}
	if doc.Failed == 0 {
		t.Error("a skipped teardown must count as a failure so the exit code is 1 and the user re-runs")
	}
}

func TestExecute_ScopedToOwnStateFiles(t *testing.T) {
	home := sandboxHome(t)
	if err := state.Save(&state.DaemonState{StackName: "mine", PID: 999999}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rt := &fakeRuntime{}
	m := &Managers{Home: home, Runtime: rt}

	if _, err := m.Execute(context.Background(), Options{}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rt.downed) != 1 || rt.downed[0] != "mine" {
		t.Errorf("Down calls = %v; must be exactly the stacks in OUR state dir, never an engine-wide sweep", rt.downed)
	}
}

func TestExecute_SelfPIDDefersFinalize(t *testing.T) {
	home := sandboxHome(t)
	self := os.Getpid()
	if err := state.Save(&state.DaemonState{StackName: "gridctl", PID: self}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rt := &fakeRuntime{}
	m := &Managers{Home: home, Runtime: rt}

	doc, err := m.Execute(context.Background(), Options{Purge: true, SelfPID: self}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if doc.Finalize == nil {
		t.Fatal("SelfPID execution must return a Finalize")
	}
	// Our own state file and the gridctl dir survive until Finalize.
	if _, err := os.Stat(filepath.Join(home, ".gridctl")); err != nil {
		t.Fatal(".gridctl must survive until Finalize under SelfPID")
	}
	if err := doc.Finalize(); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl")); !os.IsNotExist(err) {
		t.Error("Finalize must complete the purge")
	}
}

func TestExecute_ForeignWiringNeverRemoved(t *testing.T) {
	home := sandboxHome(t)
	m := &Managers{Home: home, Wiring: &fakeWiring{rows: []wiring.Row{
		{Client: "cursor", Name: "gridctl", State: wiring.StateForeign, Target: filepath.Join(home, ".cursor", "mcp.json")},
	}}}

	doc, err := m.Execute(context.Background(), Options{Force: true}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range doc.Rows {
		if r.Kind == "wiring" && r.Action != ActionKeptForeign {
			t.Errorf("foreign wiring row action = %q; foreign entries are never removed, even forced", r.Action)
		}
	}
}

type fakeWiring struct {
	rows     []wiring.Row
	unlinked []string
	dropped  []string
}

func (f *fakeWiring) Statuses(context.Context, wiring.StatusOptions) ([]wiring.Row, error) {
	return f.rows, nil
}

func (f *fakeWiring) UnlinkClient(_ context.Context, _ provisioner.ClientProvisioner, _, name string, _, _ bool) (wiring.Result, error) {
	f.unlinked = append(f.unlinked, name)
	return wiring.Result{Action: wiring.ActionRemoved}, nil
}

func (f *fakeWiring) DropRecord(_ context.Context, client, _ string) (wiring.Result, error) {
	f.dropped = append(f.dropped, client)
	return wiring.Result{}, nil
}

func (f *fakeWiring) Registry() *provisioner.Registry { return provisioner.NewRegistry() }

func TestExecute_AgentsAndContextsCascade(t *testing.T) {
	home := sandboxHome(t)
	agents := &fakeAgents{statuses: []agentsync.ProjectionStatus{
		{Agent: "reviewer", Client: "claude-code", State: agentsync.StateDrifted},
		{Agent: "analyst", Client: "claude-code", State: "in-sync"},
	}}
	ctxs := &fakeContexts{statuses: []contexts.ClientStatus{
		{Slug: "claude-code", State: contexts.StateInSync, TargetPath: filepath.Join(home, ".claude", "rules", "gridctl.md")},
		{Slug: "gemini", State: contexts.StateDrifted, TargetPath: filepath.Join(home, ".gemini", "GEMINI.md")},
		{Slug: "zed", State: contexts.StateNeverSynced},
	}}
	m := &Managers{Home: home, Agents: agents, Contexts: ctxs}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(agents.unsynced) != 1 || len(agents.unsynced[0]) != 1 || agents.unsynced[0][0] != "analyst" {
		t.Errorf("agent Unsync received %v; drifted agent must be pre-filtered", agents.unsynced)
	}
	if len(ctxs.unsynced) != 1 || ctxs.unsynced[0] != "claude-code" {
		t.Errorf("context Unsync received %v; drifted and never-synced clients must be excluded", ctxs.unsynced)
	}
	want := map[string]bool{"agent/reviewer": true, "context/gemini": true}
	for _, k := range doc.Kept {
		delete(want, k)
	}
	if len(want) != 0 {
		t.Errorf("Kept missing %v (got %v)", want, doc.Kept)
	}
}

func TestBackup_DefaultTierIncludesTargetsAndLockfile(t *testing.T) {
	home := sandboxHome(t)
	gridctlDir := filepath.Join(home, ".gridctl")
	if err := os.MkdirAll(gridctlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gridctlDir, "project.lock.yaml"), []byte("version: 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "rules", "gridctl.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Managers{Home: home}
	doc := &Doc{Purge: false, Rows: []Row{{Kind: "context", Name: "claude-code", Path: target, Action: ActionWouldRemove}}}
	path, err := m.Backup(context.Background(), doc, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	names := tarNames(t, path)
	if !names[".claude/rules/gridctl.md"] {
		t.Errorf("backup must include the projection target; has %v", names)
	}
	if !names[".gridctl/project.lock.yaml"] {
		t.Errorf("backup must include the projection lockfile; has %v", names)
	}
}

func tarNames(t *testing.T, archive string) map[string]bool {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		names[hdr.Name] = true
	}
	return names
}

func TestExecute_ProgressStreamsBackupFirst(t *testing.T) {
	home := sandboxHome(t)
	skills := &fakeSkills{statuses: []skillsync.ProjectionStatus{
		{Skill: "s", Client: "claude-code", State: skillsync.StateInSync},
	}}
	m := &Managers{Home: home, Skills: skills}

	var phases []string
	var rowKinds []string
	progress := func(phase string, row *Row) {
		if row == nil {
			phases = append(phases, phase)
			return
		}
		rowKinds = append(rowKinds, row.Kind)
	}
	if _, err := m.Execute(context.Background(), Options{}, progress); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(phases) == 0 || phases[0] != "backup" {
		t.Errorf("first phase = %v, want backup first", phases)
	}
	if len(rowKinds) == 0 || rowKinds[0] != "backup" {
		t.Errorf("first row kind = %v; the backup row must stream before any removal", rowKinds)
	}
	for _, k := range rowKinds[1:] {
		if k == "backup" {
			t.Error("backup row emitted more than once")
		}
	}
}

func TestGridctlDir(t *testing.T) {
	m := &Managers{Home: "/tmp/x"}
	if got := m.GridctlDir(); got != filepath.Join("/tmp/x", ".gridctl") {
		t.Errorf("GridctlDir() = %q", got)
	}
}

func TestBackup_CapturesDirectoryTargets(t *testing.T) {
	home := sandboxHome(t)
	// A copy-channel skill projection is a DIRECTORY; the archive must
	// capture its files, not a bare directory header.
	target := filepath.Join(home, ".claude", "skills", "review-pr")
	if err := os.MkdirAll(filepath.Join(target, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"SKILL.md", "references/notes.md"} {
		if err := os.WriteFile(filepath.Join(target, f), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := &Managers{Home: home}
	doc := &Doc{Rows: []Row{{Kind: "skill", Name: "review-pr", Path: target, Action: ActionWouldRemove}}}
	path, err := m.Backup(context.Background(), doc, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	names := tarNames(t, path)
	if !names[".claude/skills/review-pr/SKILL.md"] || !names[".claude/skills/review-pr/references/notes.md"] {
		t.Errorf("directory target files missing from archive: %v", names)
	}
}

func TestExecute_CanceledContextAbortsBeforeStateDeletion(t *testing.T) {
	home := sandboxHome(t)
	if err := state.Save(&state.DaemonState{StackName: "demo", PID: 999999}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// A skill Statuses fake that cancels the context mid-cascade: the
	// cancellation lands after collect, before the later phases.
	ctx, cancel := context.WithCancel(context.Background())
	skills := &cancelingSkills{cancel: cancel}
	m := &Managers{Home: home, Skills: skills, Runtime: &fakeRuntime{}}

	_, err := m.Execute(ctx, Options{}, nil)
	if err == nil {
		t.Fatal("Execute must surface the cancellation")
	}
	// The state file must survive: deleting it after removals were
	// canceled would orphan lockfile-attested projections.
	if _, statErr := os.Stat(mustStatePath(t, "demo")); statErr != nil {
		t.Error("state file must not be deleted after a canceled cascade")
	}
}

// cancelingSkills reports one projection, then cancels the context from
// inside Unsync to simulate a client disconnect mid-cascade.
type cancelingSkills struct {
	cancel context.CancelFunc
}

func (c *cancelingSkills) Statuses(context.Context) ([]skillsync.ProjectionStatus, error) {
	return []skillsync.ProjectionStatus{{Skill: "s", Client: "claude-code", State: skillsync.StateInSync}}, nil
}

func (c *cancelingSkills) Unsync(ctx context.Context, names []string, _ skillsync.UnsyncOptions) ([]skillsync.UnsyncResult, error) {
	c.cancel()
	return nil, ctx.Err()
}

func mustStatePath(t *testing.T, name string) string {
	t.Helper()
	p, err := state.StatePath(name)
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	return p
}

func TestExecute_OrphanAdoptedOnlyForOwnHome(t *testing.T) {
	home := sandboxHome(t)
	findOrphanFn = func(int) (int, bool, error) { return 999999, true, nil }
	daemonHomeFn = func(int) (string, bool) { return home, true }
	m := &Managers{Home: home}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var stopped bool
	for _, r := range doc.Rows {
		if r.Kind == "daemon" && r.Name == "gridctl (foreground)" && r.Action == ActionStopped {
			stopped = true
		}
	}
	if !stopped {
		t.Errorf("own-home orphan must be stopped; rows: %+v", doc.Rows)
	}
}

func TestExecute_ForeignHomeOrphanNeverSignaled(t *testing.T) {
	home := sandboxHome(t)
	findOrphanFn = func(int) (int, bool, error) { return 999999, true, nil }
	daemonHomeFn = func(int) (string, bool) { return "/somebody/else", true }
	m := &Managers{Home: home}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range doc.Rows {
		if r.Kind == "daemon" && r.Action == ActionStopped {
			t.Fatalf("foreign-home daemon must never be signaled: %+v", r)
		}
	}
	var advisory bool
	for _, r := range doc.Rows {
		if r.Kind == "daemon" && r.Action == ActionSkipped {
			advisory = true
		}
	}
	if !advisory {
		t.Error("a foreign-home daemon must be reported, not silently ignored")
	}
}

func TestExecute_UnidentifiableOrphanNeverSignaled(t *testing.T) {
	home := sandboxHome(t)
	findOrphanFn = func(int) (int, bool, error) { return 999999, true, nil }
	daemonHomeFn = func(int) (string, bool) { return "", false } // old binary: no home field
	m := &Managers{Home: home}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range doc.Rows {
		if r.Action == ActionStopped {
			t.Fatalf("an orphan that cannot prove its home must never be signaled: %+v", r)
		}
	}
}

type fakeHooks struct {
	statuses     []hooksync.ProjectionStatus
	unsynced     [][]string
	force        bool
	actionByName map[string]string
	detailByName map[string]string
	errorByName  map[string]string
	onUnsync     func()
}

func (f *fakeHooks) Statuses(context.Context) ([]hooksync.ProjectionStatus, error) {
	return f.statuses, nil
}

func (f *fakeHooks) Unsync(_ context.Context, names []string, opts hooksync.UnsyncOptions) ([]hooksync.UnsyncResult, error) {
	copied := append([]string(nil), names...)
	f.unsynced = append(f.unsynced, copied)
	f.force = opts.Force
	if f.onUnsync != nil {
		f.onUnsync()
	}
	var out []hooksync.UnsyncResult
	for _, n := range names {
		action := hooksync.ActionRemoved
		if f.actionByName != nil && f.actionByName[n] != "" {
			action = f.actionByName[n]
		}
		res := hooksync.UnsyncResult{Hook: n, Action: action}
		for _, s := range f.statuses {
			if s.Hook == n {
				res.Client = s.Client
				res.Target = s.Target
				break
			}
		}
		if f.detailByName != nil {
			res.Detail = f.detailByName[n]
		}
		if f.errorByName != nil {
			res.Error = f.errorByName[n]
		}
		out = append(out, res)
	}
	return out, nil
}

func TestPreview_HookRows(t *testing.T) {
	home := sandboxHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	hooks := &fakeHooks{statuses: []hooksync.ProjectionStatus{
		{Hook: "clean", Client: "claude-code", State: hooksync.StateInSync, Target: settings, ScriptDir: filepath.Join(home, ".gridctl", "hooks", "clean")},
		{Hook: "edited", Client: "claude-code", State: hooksync.StateDrifted, Target: settings, ScriptDir: filepath.Join(home, ".gridctl", "hooks", "edited"),
			Detail: "script copy was edited since gridctl wrote it"},
	}}
	m := &Managers{Home: home, Hooks: hooks}

	doc, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	var clean, edited Row
	var sawClean, sawEdited bool
	for _, r := range doc.Rows {
		switch r.Name {
		case "clean":
			clean, sawClean = r, true
		case "edited":
			edited, sawEdited = r, true
		}
	}
	if !sawClean || clean.Kind != "hook" || clean.Action != ActionWouldRemove || clean.Client != "claude-code" || clean.Path != settings || clean.ScriptDir != hooks.statuses[0].ScriptDir {
		t.Errorf("in-sync hook row = %+v, want would-remove with client, path, and script dir", clean)
	}
	if !sawEdited || edited.Action != ActionKeptDrift || edited.Detail != hookKeptDetail {
		t.Errorf("drifted hook row = %+v, want kept-drift", edited)
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "hook/edited" {
		t.Errorf("Kept = %v, want [hook/edited]", doc.Kept)
	}

	forced, err := m.Preview(context.Background(), Options{Force: true})
	if err != nil {
		t.Fatalf("Preview force: %v", err)
	}
	for _, r := range forced.Rows {
		if r.Action == ActionKeptDrift {
			t.Errorf("forced preview still keeps %s", r.Name)
		}
	}
}

func TestExecute_HookDriftPreFilteredAndForcePassed(t *testing.T) {
	home := sandboxHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	hooks := &fakeHooks{statuses: []hooksync.ProjectionStatus{
		{Hook: "clean", Client: "claude-code", State: hooksync.StateInSync, Target: settings, ScriptDir: filepath.Join(home, ".gridctl", "hooks", "clean")},
		{Hook: "edited", Client: "claude-code", State: hooksync.StateDrifted, Target: settings, ScriptDir: filepath.Join(home, ".gridctl", "hooks", "edited"),
			Detail: "script copy was edited since gridctl wrote it"},
	}}
	m := &Managers{Home: home, Hooks: hooks}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(hooks.unsynced) != 1 || len(hooks.unsynced[0]) != 1 || hooks.unsynced[0][0] != "clean" {
		t.Fatalf("Unsync received %v; only the non-drifted name must be passed", hooks.unsynced)
	}
	if hooks.force {
		t.Fatal("Unsync Force = true, want false")
	}
	var keptRow Row
	var nKept int
	for _, r := range doc.Rows {
		if r.Kind == "hook" && r.Action == ActionKeptDrift {
			keptRow = r
			nKept++
		}
	}
	if nKept != 1 || keptRow.Name != "edited" || keptRow.Client != "claude-code" || keptRow.Path != settings || keptRow.Detail != hookKeptDetail {
		t.Errorf("kept row = %+v (count %d), want one kept-drift row for edited", keptRow, nKept)
	}
	if countKept(doc.Kept, "hook/edited") != 1 {
		t.Errorf("Kept = %v, want hook/edited once", doc.Kept)
	}

	forced, err := m.Execute(context.Background(), Options{Force: true}, nil)
	if err != nil {
		t.Fatalf("Execute force: %v", err)
	}
	if len(hooks.unsynced) != 2 || !sameNames(hooks.unsynced[1], []string{"clean", "edited"}) {
		t.Fatalf("forced Unsync received %v, want both names", hooks.unsynced)
	}
	if !hooks.force {
		t.Fatal("forced Unsync Force = false, want true")
	}
	for _, r := range forced.Rows {
		if r.Action == ActionKeptDrift {
			t.Errorf("forced execute still keeps %+v", r)
		}
	}
}

func TestExecute_HookUnsyncResultsMapped(t *testing.T) {
	home := sandboxHome(t)
	names := []string{"gone", "missing", "raced", "bad", "boom"}
	var statuses []hooksync.ProjectionStatus
	for _, n := range names {
		statuses = append(statuses, hooksync.ProjectionStatus{Hook: n, Client: "claude-code", State: hooksync.StateInSync, Target: filepath.Join(home, n+".json")})
	}
	hooks := &fakeHooks{
		statuses: statuses,
		actionByName: map[string]string{
			"gone":    hooksync.ActionRemoved,
			"missing": hooksync.ActionAlreadyGone,
			"raced":   hooksync.ActionSkippedDrift,
			"bad":     hooksync.ActionSkippedUnmanaged,
			"boom":    hooksync.ActionError,
		},
		detailByName: map[string]string{"bad": "settings file is not valid JSON or JSONC: unexpected end of input"},
		errorByName:  map[string]string{"boom": "write failed"},
	}
	m := &Managers{Home: home, Hooks: hooks}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	actions := map[string]string{}
	for _, r := range doc.Rows {
		if r.Kind == "hook" {
			actions[r.Name] = r.Action
		}
	}
	if actions["gone"] != ActionRemoved || actions["missing"] != ActionAlreadyGone || actions["raced"] != ActionKeptDrift {
		t.Errorf("ok actions = %v", actions)
	}
	if actions["bad"] != ActionFailed || actions["boom"] != ActionFailed {
		t.Errorf("failure actions = %v", actions)
	}
	if doc.Failed != 2 {
		t.Errorf("Failed = %d, want 2", doc.Failed)
	}
	if countKept(doc.Kept, "hook/raced") != 1 {
		t.Errorf("Kept = %v, want hook/raced once", doc.Kept)
	}
}

func TestExecute_HookUnparsableSettingsFails(t *testing.T) {
	home := sandboxHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	detail := hooksync.SettingsUnparsableDetail + "unexpected end of input"
	hooks := &fakeHooks{
		statuses: []hooksync.ProjectionStatus{{
			Hook: "broken", Client: "claude-code", State: hooksync.StateDrifted, Target: settings, Detail: detail,
		}},
		actionByName: map[string]string{"broken": hooksync.ActionSkippedUnmanaged},
		detailByName: map[string]string{"broken": detail},
	}
	m := &Managers{Home: home, Hooks: hooks}

	preview, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(preview.Kept) != 0 {
		t.Fatalf("preview Kept = %v, want empty", preview.Kept)
	}
	var skipped int
	for _, r := range preview.Rows {
		if r.Kind != "hook" {
			continue
		}
		if r.Action == ActionKeptDrift {
			t.Fatalf("parse error must not preview as kept-drift: %+v", r)
		}
		if r.Action == ActionSkipped && r.Name == "broken" && r.Detail == hookUnparsableDetail {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("preview rows = %+v, want one skipped hook row", preview.Rows)
	}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(hooks.unsynced) != 1 || len(hooks.unsynced[0]) != 1 || hooks.unsynced[0][0] != "broken" {
		t.Fatalf("Unsync received %v, want [broken]", hooks.unsynced)
	}
	var failed Row
	var nFailed int
	for _, r := range doc.Rows {
		if r.Kind == "hook" && r.Action == ActionFailed {
			failed = r
			nFailed++
		}
		if r.Action == ActionKeptDrift {
			t.Errorf("parse error emitted kept-drift: %+v", r)
		}
	}
	if nFailed != 1 || failed.Error != detail || doc.Failed != 1 || len(doc.Kept) != 0 {
		t.Errorf("failed row = %+v, Failed = %d, Kept = %v", failed, doc.Failed, doc.Kept)
	}

	forced, err := m.Execute(context.Background(), Options{Force: true}, nil)
	if err != nil {
		t.Fatalf("Execute force: %v", err)
	}
	if !hooks.force || len(hooks.unsynced) != 2 || hooks.unsynced[1][0] != "broken" {
		t.Fatalf("force must still unsync the parse-error name, got force=%v calls=%v", hooks.force, hooks.unsynced)
	}
	if forced.Failed != 1 || len(forced.Kept) != 0 {
		t.Errorf("forced parse error Failed = %d, Kept = %v", forced.Failed, forced.Kept)
	}
}

func TestExecute_MissingHookManagerCounted(t *testing.T) {
	home := sandboxHome(t)
	m := &Managers{Home: home, Missing: []string{"hook"}}

	doc, err := m.Execute(context.Background(), Options{}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var skipped bool
	for _, r := range doc.Rows {
		if r.Kind == "hook" && r.Action == ActionSkipped {
			skipped = true
		}
	}
	if !skipped || doc.Failed != 1 {
		t.Errorf("missing hook manager: skipped=%v Failed=%d rows=%+v", skipped, doc.Failed, doc.Rows)
	}
}

func TestExecute_HookUnsyncBeforePurge(t *testing.T) {
	home := sandboxHome(t)
	gridctlDir := filepath.Join(home, ".gridctl")
	if err := os.MkdirAll(gridctlDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hooks := &fakeHooks{
		statuses: []hooksync.ProjectionStatus{{
			Hook: "block", Client: "claude-code", State: hooksync.StateInSync,
			Target: filepath.Join(home, ".claude", "settings.json"),
		}},
		onUnsync: func() {
			if _, err := os.Stat(gridctlDir); err != nil {
				t.Errorf("hook unsync ran after %s was removed: %v", gridctlDir, err)
			}
		},
	}
	m := &Managers{Home: home, Hooks: hooks}

	if _, err := m.Execute(context.Background(), Options{Purge: true}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(hooks.unsynced) != 1 {
		t.Fatalf("Unsync calls = %d, want 1", len(hooks.unsynced))
	}
	if _, err := os.Stat(gridctlDir); !os.IsNotExist(err) {
		t.Fatalf("purge must remove %s, stat err = %v", gridctlDir, err)
	}
}

func TestBackup_IncludesHookScriptDir(t *testing.T) {
	home := sandboxHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block-force-push")
	script := filepath.Join(scriptDir, "scripts", "block.sh")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "hook.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Managers{Home: home, Hooks: &fakeHooks{statuses: []hooksync.ProjectionStatus{{
		Hook: "block-force-push", Client: "claude-code", State: hooksync.StateInSync,
		Target: settings, ScriptDir: scriptDir,
	}}}}

	doc, err := m.Preview(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	path, err := m.Backup(context.Background(), doc, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	names := tarNames(t, path)
	for _, want := range []string{
		".claude/settings.json",
		".gridctl/hooks/block-force-push/scripts/block.sh",
		".gridctl/hooks/block-force-push/hook.json",
	} {
		if !names[want] {
			t.Errorf("backup missing %s; has %v", want, names)
		}
	}
}

func countKept(kept []string, want string) int {
	n := 0
	for _, k := range kept {
		if k == want {
			n++
		}
	}
	return n
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, n := range got {
		seen[n]++
	}
	for _, n := range want {
		seen[n]--
		if seen[n] < 0 {
			return false
		}
	}
	return true
}
