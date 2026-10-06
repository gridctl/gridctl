package packops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

const stackManifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: neteng
version: 1.0.0
description: Stack pack
skills: [alpha]
agents: [reviewer]
wiring: true
stack: stack.yaml
variables:
  GITHUB_TOKEN:
    required: true
    secret: true
    type: string
    description: Token used by the GitHub tools
`

const carriedStack = `version: "1"
name: neteng
mcp-servers:
  - name: local-tools
    command: ["echo", "hi"]
  - name: other
    command: ["echo", "there"]
  - name: third
    command: ["echo", "more"]
`

type fakeLauncher struct {
	result  LaunchResult
	err     error
	calls   []LaunchOptions
	stopped []string
}

func (f *fakeLauncher) Launch(_ context.Context, opts LaunchOptions) (LaunchResult, error) {
	f.calls = append(f.calls, opts)
	if f.err != nil {
		return LaunchResult{}, f.err
	}
	if f.result.StackName == "" {
		f.result = LaunchResult{StackName: "neteng", Port: 9191}
	}
	return f.result, nil
}

func (f *fakeLauncher) Stop(_ context.Context, name string) error {
	f.stopped = append(f.stopped, name)
	return nil
}

func TestAdd_CarriedStack(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, stackManifest, map[string]string{"stack.yaml": carriedStack})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Stack == nil || res.Doc.Stack.Path != "stack.yaml" || res.Doc.Stack.Name != "neteng" || res.Doc.Stack.Servers != 3 {
		t.Fatalf("stack summary = %+v", res.Doc.Stack)
	}
	locked, err := mgrs.LoadLockedPack("neteng")
	if err != nil {
		t.Fatal(err)
	}
	if locked.Stack == nil || locked.Stack.Name != "neteng" || locked.Stack.Path != "stack.yaml" {
		t.Fatalf("locked stack = %+v", locked.Stack)
	}
	pinned := filepath.Join(locked.Stack.CheckoutDir, "stack.yaml")
	if _, err := os.Stat(pinned); err != nil {
		t.Fatalf("checkout missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(locked.Stack.CheckoutDir, ".git")); !os.IsNotExist(err) {
		t.Fatal("checkout contains .git")
	}
	raw, err := os.ReadFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "version: 5") {
		t.Fatalf("lockfile =\n%s", raw)
	}
}

func TestAdd_DryRunDoesNotMaterialize(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, _ := os.UserHomeDir()
	repo := packFixture(t, stackManifest, map[string]string{"stack.yaml": carriedStack})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Stack == nil || res.Doc.Stack.Servers != 3 {
		t.Fatalf("dry-run summary = %+v", res.Doc.Stack)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "packs")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created a checkout: %v", err)
	}
}

func TestAdd_InlineTokenUnresolved(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, _ := os.UserHomeDir()
	secret := `version: "1"
name: neteng
mcp-servers:
  - name: tools
    command: ["echo"]
    env:
      API_TOKEN: literal-token
`
	repo := packFixture(t, stackManifest, map[string]string{"stack.yaml": secret})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(res.Doc.Unresolved, "stack:stack.yaml") {
		t.Fatalf("unresolved = %v", res.Doc.Unresolved)
	}
	if len(res.Doc.Warnings) == 0 || !strings.Contains(strings.Join(res.Doc.Warnings, "\n"), "inline literal") {
		t.Fatalf("warnings = %v", res.Doc.Warnings)
	}
	if res.Doc.Stack != nil {
		t.Fatalf("resolved summary on a rejected stack: %+v", res.Doc.Stack)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "packs")); !os.IsNotExist(err) {
		t.Fatal("rejected stack created a checkout")
	}
	if !containsString(res.Doc.Skills, "alpha") {
		t.Fatalf("rest of pack did not import: %+v", res.Doc)
	}
}

func TestAdd_ExtendsEscapes(t *testing.T) {
	mgrs, imp := testEnv(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: \"1\"\nname: parent\nmcp-servers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := packFixture(t, stackManifest, map[string]string{
		"stack.yaml": "version: \"1\"\nname: child\nextends: " + outside + "\nmcp-servers: []\n",
	})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(res.Doc.Unresolved, "stack:stack.yaml") {
		t.Fatalf("unresolved = %v", res.Doc.Unresolved)
	}
	joined := strings.Join(res.Doc.Warnings, "\n")
	if !strings.Contains(joined, outside) && !strings.Contains(joined, "escapes") {
		t.Fatalf("warnings = %v", res.Doc.Warnings)
	}
}

func TestAdd_LinkWarning(t *testing.T) {
	mgrs, imp := testEnv(t)
	withLink := carriedStack + "link:\n  - client: claude\n"
	repo := packFixture(t, stackManifest, map[string]string{"stack.yaml": withLink})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.Doc.Warnings, "\n"), stackLinkWarning) {
		t.Fatalf("warnings = %v", res.Doc.Warnings)
	}
}

func TestPreview_ReportsStackWithoutCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := packFixture(t, stackManifest, map[string]string{"stack.yaml": carriedStack})
	res, err := Preview(context.Background(), PreviewOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stack == nil || res.Stack.Name != "neteng" || res.Stack.Servers != 3 {
		t.Fatalf("preview stack = %+v", res.Stack)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "packs")); !os.IsNotExist(err) {
		t.Fatal("preview materialized a checkout")
	}
}

func TestApply_StackOutcomes(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, strings.Replace(stackManifest, "wiring: true", "wiring: false", 1), map[string]string{"stack.yaml": carriedStack})
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	locked, err := mgrs.LoadLockedPack("neteng")
	if err != nil {
		t.Fatal(err)
	}
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	required := true
	mgrs.StoredVariables = func() (map[string]bool, bool) {
		return map[string]bool{}, true
	}
	_ = required

	doc, err := mgrs.Apply(context.Background(), "neteng", ApplyOptions{Port: 8180})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Kind != "stack" || doc.Rows[0].Action != "skipped-variables" {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	if !strings.Contains(doc.Rows[0].Detail, "GITHUB_TOKEN") || !strings.Contains(doc.Rows[0].Remediation, "gridctl var set GITHUB_TOKEN") {
		t.Fatalf("variable row = %+v", doc.Rows[0])
	}
	if len(launcher.calls) != 0 {
		t.Fatal("launcher ran with unmet variables")
	}
	applied, failed := TallyRows(doc.Rows)
	if failed == 0 || applied+failed != len(doc.Rows) {
		t.Fatalf("tally applied=%d failed=%d rows=%d", applied, failed, len(doc.Rows))
	}

	mgrs.StoredVariables = func() (map[string]bool, bool) {
		return map[string]bool{"GITHUB_TOKEN": true}, true
	}
	doc, err = mgrs.Apply(context.Background(), "neteng", ApplyOptions{Port: 8282})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "started" {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	if len(launcher.calls) != 1 || launcher.calls[0].Replace || launcher.calls[0].Port != 8282 {
		t.Fatalf("launch = %+v", launcher.calls)
	}
	if launcher.calls[0].StackPath != filepath.Join(locked.Stack.CheckoutDir, "stack.yaml") {
		t.Fatalf("stack path = %s", launcher.calls[0].StackPath)
	}

	// Second apply with a running daemon on the pinned path is unchanged
	// and does not launch. A decoy daemon on another port must not be used.
	if err := state.Save(&state.DaemonState{
		StackName: "neteng", StackFile: launcher.calls[0].StackPath, PID: os.Getpid(), Port: 9191,
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "aaa-other", StackFile: "/tmp/other.yaml", PID: os.Getpid(), Port: 7777,
	}); err != nil {
		t.Fatal(err)
	}
	before := len(launcher.calls)
	doc, err = mgrs.Apply(context.Background(), "neteng", ApplyOptions{Port: 8282})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "unchanged" || len(launcher.calls) != before {
		t.Fatalf("second apply = %+v calls=%d", doc.Rows[0], len(launcher.calls))
	}

	// Dry-run while unchanged does not launch.
	doc, err = mgrs.Apply(context.Background(), "neteng", ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "unchanged" {
		t.Fatalf("dry-run unchanged = %+v", doc.Rows[0])
	}
}

func TestApply_SkippedRunningAndForce(t *testing.T) {
	mgrs, _ := testEnv(t)
	home, _ := os.UserHomeDir()
	checkout := PackCheckoutDir(home, "neteng", "abc")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "stack.yaml"), []byte(carriedStack), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStackLock(t, home, checkout)
	if err := state.Save(&state.DaemonState{
		StackName: "neteng", StackFile: "/tmp/hand-started.yaml", PID: os.Getpid(), Port: 7000,
	}); err != nil {
		t.Fatal(err)
	}
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	mgrs.StoredVariables = func() (map[string]bool, bool) { return map[string]bool{"GITHUB_TOKEN": true}, true }

	doc, err := mgrs.Apply(context.Background(), "neteng", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "skipped-running" || len(launcher.calls) != 0 {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	if doc.Rows[0].Kind != "stack" {
		t.Fatal("stack row is not first")
	}
	wiring := rowByKind(doc.Rows, "wiring")
	if wiring == nil || wiring.Action != "skipped-unavailable" || !strings.Contains(wiring.Detail, "pack stack is not running") {
		t.Fatalf("wiring = %+v", wiring)
	}

	doc, err = mgrs.Apply(context.Background(), "neteng", ApplyOptions{Force: true, Port: 8180})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "replaced" || len(launcher.calls) != 1 || !launcher.calls[0].Replace {
		t.Fatalf("force = %+v calls=%+v", doc.Rows[0], launcher.calls)
	}
}

func TestApply_NilLauncherSkips(t *testing.T) {
	mgrs, _ := testEnv(t)
	home, _ := os.UserHomeDir()
	checkout := PackCheckoutDir(home, "neteng", "abc")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "stack.yaml"), []byte(carriedStack), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStackLock(t, home, checkout)
	doc, err := mgrs.Apply(context.Background(), "neteng", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "skipped-unavailable" || !strings.Contains(doc.Rows[0].Remediation, "gridctl pack apply neteng") {
		t.Fatalf("row = %+v", doc.Rows[0])
	}
	wiring := rowByKind(doc.Rows, "wiring")
	if wiring == nil || wiring.Action != "skipped-unavailable" {
		t.Fatalf("wiring = %+v", wiring)
	}
}

func TestApply_WiringUsesPackPort(t *testing.T) {
	mgrs, _ := testEnv(t)
	home, _ := os.UserHomeDir()
	checkout := PackCheckoutDir(home, "neteng", "abc")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "stack.yaml"), []byte(carriedStack), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStackLock(t, home, checkout)
	pinned := filepath.Join(checkout, "stack.yaml")
	if err := state.Save(&state.DaemonState{StackName: "aaa-other", StackFile: "/tmp/other.yaml", PID: os.Getpid(), Port: 7777}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "neteng", StackFile: pinned, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	mgrs.StoredVariables = func() (map[string]bool, bool) { return map[string]bool{"GITHUB_TOKEN": true}, true }
	doc, err := mgrs.Apply(context.Background(), "neteng", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "unchanged" {
		t.Fatalf("stack = %+v", doc.Rows[0])
	}
	cfg, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "localhost:9191") || strings.Contains(string(cfg), "localhost:7777") {
		t.Fatalf("wiring config =\n%s", cfg)
	}
}

func TestStatuses_StackRows(t *testing.T) {
	mgrs, _ := testEnv(t)
	home, _ := os.UserHomeDir()
	checkout := PackCheckoutDir(home, "neteng", "newsha")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "stack.yaml"), []byte(carriedStack), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStackLock(t, home, checkout)

	statuses, err := mgrs.Statuses(context.Background(), StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].Info.Counts.Stack {
		t.Fatalf("status = %+v", statuses)
	}
	if statuses[0].Rows[0].Kind != "stack" || statuses[0].Rows[0].State != "missing" {
		t.Fatalf("missing row = %+v", statuses[0].Rows[0])
	}
	if statuses[0].NeedsAttention {
		t.Fatal("missing stack is attention")
	}
	if statuses[0].Info.Applied {
		t.Fatal("missing stack-only pack should not be applied")
	}

	old := PackCheckoutDir(home, "neteng", "oldsha")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "neteng", StackFile: filepath.Join(old, "stack.yaml"), PID: os.Getpid(), Port: 8180}); err != nil {
		t.Fatal(err)
	}
	statuses, err = mgrs.Statuses(context.Background(), StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Rows[0].State != "stale" || !strings.Contains(statuses[0].Rows[0].Detail, "oldsha") {
		t.Fatalf("stale = %+v", statuses[0].Rows[0])
	}
	if !statuses[0].Info.Applied || !statuses[0].NeedsAttention {
		t.Fatalf("stale applied=%v attention=%v", statuses[0].Info.Applied, statuses[0].NeedsAttention)
	}

	if err := state.Save(&state.DaemonState{StackName: "neteng", StackFile: "/tmp/hand.yaml", PID: os.Getpid(), Port: 8180}); err != nil {
		t.Fatal(err)
	}
	statuses, err = mgrs.Statuses(context.Background(), StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Rows[0].State != "drifted" || !strings.Contains(statuses[0].Rows[0].Detail, "/tmp/hand.yaml") {
		t.Fatalf("drifted = %+v", statuses[0].Rows[0])
	}

	if err := os.RemoveAll(PackCheckoutRoot(home, "neteng")); err != nil {
		t.Fatal(err)
	}
	statuses, err = mgrs.Statuses(context.Background(), StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Rows[0].State != "target-missing" || !statuses[0].NeedsAttention {
		t.Fatalf("target-missing = %+v attention=%v", statuses[0].Rows[0], statuses[0].NeedsAttention)
	}
}

func TestRemove_StopsPackDaemonOnly(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, _ := os.UserHomeDir()
	checkout := PackCheckoutDir(home, "neteng", "abc")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "stack.yaml"), []byte(carriedStack), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStackLock(t, home, checkout)
	pinned := filepath.Join(checkout, "stack.yaml")
	if err := state.Save(&state.DaemonState{StackName: "neteng", StackFile: pinned, PID: os.Getpid(), Port: 8180}); err != nil {
		t.Fatal(err)
	}
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher

	doc, err := mgrs.Remove(context.Background(), imp, "neteng", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Kind != "stack" || doc.Rows[0].Action != "would-remove" {
		t.Fatalf("dry-run rows = %+v", doc.Rows)
	}
	if _, err := os.Stat(checkout); err != nil {
		t.Fatal("dry-run deleted the checkout")
	}

	doc, err = mgrs.Remove(context.Background(), imp, "neteng", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Rows[0].Action != "removed" || len(launcher.stopped) != 1 || launcher.stopped[0] != "neteng" {
		t.Fatalf("remove = %+v stopped=%v", doc.Rows[0], launcher.stopped)
	}
	if _, err := os.Stat(PackCheckoutRoot(home, "neteng")); !os.IsNotExist(err) {
		t.Fatal("checkout survived remove")
	}
}

func TestRemove_LeavesForeignDaemon(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, _ := os.UserHomeDir()
	checkout := PackCheckoutDir(home, "neteng", "abc")
	if err := os.MkdirAll(checkout, 0o700); err != nil {
		t.Fatal(err)
	}
	writeStackLock(t, home, checkout)
	if err := state.Save(&state.DaemonState{StackName: "neteng", StackFile: "/tmp/hand.yaml", PID: os.Getpid(), Port: 8180}); err != nil {
		t.Fatal(err)
	}
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	doc, err := mgrs.Remove(context.Background(), imp, "neteng", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rowByKind(doc.Rows, "stack") != nil {
		t.Fatalf("foreign daemon got a stack row: %+v", doc.Rows)
	}
	if len(launcher.stopped) != 0 {
		t.Fatal("foreign daemon was stopped")
	}
	if _, err := os.Stat(PackCheckoutRoot(home, "neteng")); !os.IsNotExist(err) {
		t.Fatal("pack checkout survived a foreign daemon")
	}
}

func TestPruneOtherCheckouts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	keep := PackCheckoutDir(home, "neteng", "new")
	old := PackCheckoutDir(home, "neteng", "old")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "stack.yaml"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	referenced := PackCheckoutDir(home, "neteng", "live")
	if err := os.MkdirAll(referenced, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "other", StackFile: filepath.Join(referenced, "stack.yaml"), PID: os.Getpid(), Port: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := pruneOtherCheckouts(home, "neteng", keep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("unreferenced checkout survived")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(referenced); err != nil {
		t.Fatal("referenced checkout was pruned")
	}
}

func TestTallyRows_StackActions(t *testing.T) {
	applied, failed := TallyRows([]Row{
		{Action: "started"}, {Action: "replaced"}, {Action: "unchanged"},
		{Action: "would-start"}, {Action: "would-replace"},
		{Action: "skipped-variables"}, {Action: "skipped-running"}, {Action: "skipped-unavailable"},
	})
	if applied != 5 || failed != 3 {
		t.Fatalf("applied=%d failed=%d", applied, failed)
	}
}

func writeStackLock(t *testing.T, home, checkout string) {
	t.Helper()
	path := filepath.Join(home, ".gridctl", "skills.lock.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	required := true
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"neteng": {
			Repo: "https://example.com/neteng",
			Pack: &skills.LockedPack{
				Name:    "neteng",
				Wiring:  true,
				Skills:  []string{"alpha"},
				Stack:   &skills.LockedStack{Path: "stack.yaml", Name: "neteng", ContentHash: "abc", CheckoutDir: checkout},
				Variables: map[string]skills.LockedVariableDeclaration{
					"GITHUB_TOKEN": {Required: &required},
				},
			},
		},
	}}
	if err := skills.WriteLockFile(path, lf); err != nil {
		t.Fatal(err)
	}
}

func rowByKind(rows []Row, kind string) *Row {
	for i := range rows {
		if rows[i].Kind == kind {
			return &rows[i]
		}
	}
	return nil
}
