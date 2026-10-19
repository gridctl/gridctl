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

func TestApply_BoosterLaunchUsesBasePathAndPort(t *testing.T) {
	mgrs, _ := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	baseSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fragSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseDir := PackCheckoutDir(home, "infra", baseSHA)
	fragDir := PackCheckoutDir(home, "netops", fragSHA)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baseYAML := "version: \"1\"\nname: infra\nmcp-servers:\n  - name: base\n    command: [\"echo\", \"base\"]\n"
	fragYAML := "version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\"echo\", \"extra\"]\n"
	if err := os.WriteFile(filepath.Join(baseDir, "stack.yaml"), []byte(baseYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragDir, "servers.yaml"), []byte(fragYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"infra": {Repo: "https://example.com/infra.git", CommitSHA: baseSHA, Pack: &skills.LockedPack{
			Name:  "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir, ContentHash: "sha256:base"},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: fragSHA, Pack: &skills.LockedPack{
			Name: "netops",
			Fragment: &skills.LockedFragment{
				Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: fragDir, ContentHash: "sha256:frag",
			},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(baseDir, "stack.yaml")
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: pinned, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher := &fakeLauncher{result: LaunchResult{StackName: "infra", Port: 9191}}
	mgrs.Launcher = launcher

	doc, err := mgrs.Apply(context.Background(), "netops", ApplyOptions{Port: 8180, PortSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.calls) != 1 {
		t.Fatalf("launches = %+v reloads = %+v rows = %+v", launcher.calls, launcher.reloaded, doc.Rows)
	}
	call := launcher.calls[0]
	if call.Port != 9191 {
		t.Fatalf("port = %d, want running port 9191", call.Port)
	}
	if !strings.HasSuffix(call.StackPath, filepath.Join("infra", "composed", "stack.yaml")) {
		t.Fatalf("stack path = %s", call.StackPath)
	}
	if !strings.Contains(call.OverridePath, "infra.yaml") || strings.Contains(call.OverridePath, "netops.yaml") {
		t.Fatalf("override = %s", call.OverridePath)
	}
	if !call.Replace {
		t.Fatal("first attach must replace")
	}
	var stack Row
	for _, row := range doc.Rows {
		if row.Kind == "stack" {
			stack = row
		}
	}
	if stack.Action != "composed" || !strings.Contains(stack.Detail, "replaced") || !strings.Contains(stack.Detail, "--port ignored") {
		t.Fatalf("stack row = %+v", stack)
	}
}

func TestApply_ReloadFailureRestoresComposed(t *testing.T) {
	mgrs, _ := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	baseSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fragSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseDir := PackCheckoutDir(home, "infra", baseSHA)
	fragDir := PackCheckoutDir(home, "netops", fragSHA)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baseYAML := "version: \"1\"\nname: infra\nmcp-servers:\n  - name: base\n    command: [\"echo\", \"base\"]\n"
	fragYAML := "version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\"echo\", \"extra\"]\n"
	if err := os.WriteFile(filepath.Join(baseDir, "stack.yaml"), []byte(baseYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragDir, "servers.yaml"), []byte(fragYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	composed := ComposedStackPath(home, "infra")
	if err := os.MkdirAll(filepath.Dir(composed), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := []byte("# gridctl composed stack. Do not edit; re-run 'gridctl pack apply infra'.\n# layer: base pack=infra commit=" + baseSHA + " file=stack.yaml\nversion: \"1\"\nname: infra\n")
	if err := os.WriteFile(composed, previous, 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"infra": {Repo: "https://example.com/infra.git", CommitSHA: baseSHA, Pack: &skills.LockedPack{
			Name:  "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: fragSHA, Pack: &skills.LockedPack{
			Name:     "netops",
			Fragment: &skills.LockedFragment{Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: fragDir},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: composed, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher := &fakeLauncher{reloadErr: errReload}
	mgrs.Launcher = launcher
	doc, err := mgrs.Apply(context.Background(), "infra", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.reloaded) != 1 || len(launcher.calls) != 0 {
		t.Fatalf("reloads = %v launches = %v", launcher.reloaded, launcher.calls)
	}
	got, err := os.ReadFile(composed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(previous) {
		t.Fatalf("composed file was not restored:\n%s", got)
	}
	found := false
	for _, row := range doc.Rows {
		if row.Kind == "stack" && row.Action == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows = %+v", doc.Rows)
	}
}

var errReload = errString("reload failed")

type errString string

func (e errString) Error() string { return string(e) }

func TestRemove_BaseWithBoostersRefuses(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	baseDir := PackCheckoutDir(home, "infra", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	boosterDir := PackCheckoutDir(home, "netops", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(boosterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(boosterDir, "servers.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"infra": {Repo: "https://example.com/infra.git", CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Pack: &skills.LockedPack{
			Name:  "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Pack: &skills.LockedPack{
			Name:     "netops",
			Fragment: &skills.LockedFragment{Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: boosterDir},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	_, err = mgrs.Remove(context.Background(), imp, "infra", RemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "has attached boosters (netops)") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(baseDir); statErr != nil {
		t.Fatal("refusal wrote or deleted the checkout")
	}
	if _, err := mgrs.Remove(context.Background(), imp, "infra", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	statuses, err := mgrs.Statuses(context.Background(), StatusOptions{Pack: "netops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || len(statuses[0].Rows) == 0 {
		t.Fatalf("status = %+v", statuses)
	}
	row := statuses[0].Rows[0]
	if row.State != "target-missing" || row.Detail != `base pack 'infra' is not added` {
		t.Fatalf("row = %+v", row)
	}
	if row.Remediation != "re-add the base, then re-run 'gridctl pack add https://example.com/netops.git'" {
		t.Fatalf("remediation = %q", row.Remediation)
	}
	if !statuses[0].NeedsAttention {
		t.Fatal("target-missing booster is not attention")
	}
}

func TestApply_BoosterDryRunReportsWouldReplace(t *testing.T) {
	mgrs, home, baseDir, fragDir := composeFixture(t)
	old := PackCheckoutDir(home, "infra", "cccccccccccccccccccccccccccccccccccccccc")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(baseDir, "stack.yaml")
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: pinned, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	doc, err := mgrs.Apply(context.Background(), "netops", ApplyOptions{DryRun: true, Port: 8180})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.calls) != 0 || len(launcher.reloaded) != 0 {
		t.Fatalf("dry-run launched %+v reloaded %+v", launcher.calls, launcher.reloaded)
	}
	if _, statErr := os.Stat(ComposedStackPath(home, "infra")); !os.IsNotExist(statErr) {
		t.Fatalf("dry-run wrote composed file: %v", statErr)
	}
	if _, statErr := os.Stat(old); statErr != nil {
		t.Fatal("dry-run pruned a checkout")
	}
	var stack Row
	for _, row := range doc.Rows {
		if row.Kind == "stack" {
			stack = row
		}
	}
	if stack.Action != "would-replace" || stack.Detail != "composed into infra (would-replace)" {
		t.Fatalf("stack row = %+v", stack)
	}
	_ = fragDir
}

func TestApply_ReloadPrunesUnreferencedCheckouts(t *testing.T) {
	mgrs, home, baseDir, fragDir := composeFixture(t)
	composed := ComposedStackPath(home, "infra")
	if err := os.MkdirAll(filepath.Dir(composed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composed, []byte("version: \"1\"\nname: infra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldBase := PackCheckoutDir(home, "infra", "cccccccccccccccccccccccccccccccccccccccc")
	oldBooster := PackCheckoutDir(home, "netops", "dddddddddddddddddddddddddddddddddddddddd")
	if err := os.MkdirAll(oldBase, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldBooster, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: composed, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	doc, err := mgrs.Apply(context.Background(), "infra", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.reloaded) != 1 || len(launcher.calls) != 0 {
		t.Fatalf("reloads = %v launches = %v rows = %+v", launcher.reloaded, launcher.calls, doc.Rows)
	}
	if _, statErr := os.Stat(oldBase); !os.IsNotExist(statErr) {
		t.Fatalf("old base checkout remains: %v", statErr)
	}
	if _, statErr := os.Stat(oldBooster); !os.IsNotExist(statErr) {
		t.Fatalf("old booster checkout remains: %v", statErr)
	}
	if _, statErr := os.Stat(baseDir); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(fragDir); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(composed); statErr != nil {
		t.Fatal(statErr)
	}
}

func TestApply_StartThenUnchanged(t *testing.T) {
	mgrs, home, _, _ := composeFixture(t)
	launcher := &fakeLauncher{result: LaunchResult{StackName: "infra", Port: 9191}}
	mgrs.Launcher = launcher
	doc, err := mgrs.Apply(context.Background(), "infra", ApplyOptions{Port: 9191})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.calls) != 1 || launcher.calls[0].Replace {
		t.Fatalf("start calls = %+v", launcher.calls)
	}
	found := false
	for _, row := range doc.Rows {
		if row.Kind == "stack" && row.Action == "started" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	composed := ComposedStackPath(home, "infra")
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: composed, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher.calls = nil
	doc, err = mgrs.Apply(context.Background(), "infra", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.calls) != 0 || len(launcher.reloaded) != 0 {
		t.Fatalf("unchanged launched %+v reloaded %+v", launcher.calls, launcher.reloaded)
	}
	found = false
	for _, row := range doc.Rows {
		if row.Kind == "stack" && row.Action == "unchanged" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows = %+v", doc.Rows)
	}
}

func TestRemove_BoosterRecomposeReloads(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	baseSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	keepSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	dropSHA := "cccccccccccccccccccccccccccccccccccccccc"
	baseDir := PackCheckoutDir(home, "infra", baseSHA)
	keepDir := PackCheckoutDir(home, "netops", keepSHA)
	dropDir := PackCheckoutDir(home, "sre", dropSHA)
	for _, dir := range []string{baseDir, keepDir, dropDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(baseDir, "stack.yaml"), []byte("version: \"1\"\nname: infra\nmcp-servers:\n  - name: base\n    command: [\"echo\", \"base\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keepDir, "servers.yaml"), []byte("version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\"echo\", \"extra\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropDir, "servers.yaml"), []byte("version: \"1\"\nmcp-servers:\n  - name: sre\n    command: [\"echo\", \"sre\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	composed := ComposedStackPath(home, "infra")
	if err := os.MkdirAll(filepath.Dir(composed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composed, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"infra": {Repo: "https://example.com/infra.git", CommitSHA: baseSHA, Pack: &skills.LockedPack{
			Name:  "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: keepSHA, Pack: &skills.LockedPack{
			Name:     "netops",
			Fragment: &skills.LockedFragment{Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: keepDir},
		}},
		"sre": {Repo: "https://example.com/sre.git", CommitSHA: dropSHA, Pack: &skills.LockedPack{
			Name:     "sre",
			Fragment: &skills.LockedFragment{Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: dropDir},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: composed, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher := &fakeLauncher{}
	mgrs.Launcher = launcher
	doc, err := mgrs.Remove(context.Background(), imp, "sre", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.reloaded) != 1 || launcher.reloaded[0] != "infra" || len(launcher.calls) != 0 {
		t.Fatalf("reloads = %v launches = %v", launcher.reloaded, launcher.calls)
	}
	var stack Row
	for _, row := range doc.Rows {
		if row.Kind == "stack" {
			stack = row
		}
	}
	if stack.Action != "removed" || stack.Detail != "removed from infra (reloaded)" {
		t.Fatalf("row = %+v", stack)
	}
	if _, statErr := os.Stat(dropDir); !os.IsNotExist(statErr) {
		t.Fatalf("dropped checkout remains: %v", statErr)
	}
	if _, statErr := os.Stat(composed); statErr != nil {
		t.Fatal("composed file deleted on non-last remove")
	}
}

func TestRemove_LastBoosterReplacesPinned(t *testing.T) {
	mgrs, imp := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	baseSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fragSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseDir := PackCheckoutDir(home, "infra", baseSHA)
	fragDir := PackCheckoutDir(home, "netops", fragSHA)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "stack.yaml"), []byte("version: \"1\"\nname: infra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragDir, "servers.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	composed := ComposedStackPath(home, "infra")
	if err := os.MkdirAll(filepath.Dir(composed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composed, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"infra": {Repo: "https://example.com/infra.git", CommitSHA: baseSHA, Pack: &skills.LockedPack{
			Name:  "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: fragSHA, Pack: &skills.LockedPack{
			Name:     "netops",
			Fragment: &skills.LockedFragment{Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: fragDir},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "infra", StackFile: composed, PID: os.Getpid(), Port: 9191}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Delete("infra") })
	launcher := &fakeLauncher{result: LaunchResult{StackName: "infra", Port: 9191}}
	mgrs.Launcher = launcher
	doc, err := mgrs.Remove(context.Background(), imp, "netops", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(launcher.calls) != 1 {
		t.Fatalf("launches = %+v", launcher.calls)
	}
	call := launcher.calls[0]
	if !call.Replace || call.Port != 9191 || call.StackPath != filepath.Join(baseDir, "stack.yaml") {
		t.Fatalf("launch = %+v", call)
	}
	var stack Row
	for _, row := range doc.Rows {
		if row.Kind == "stack" {
			stack = row
		}
	}
	if stack.Detail != "removed from infra (replaced)" {
		t.Fatalf("row = %+v", stack)
	}
	if _, statErr := os.Stat(filepath.Dir(composed)); !os.IsNotExist(statErr) {
		t.Fatalf("composed dir remains: %v", statErr)
	}
}

func TestStatus_FragmentRowsAndStoppedBooster(t *testing.T) {
	mgrs, home, baseDir, fragDir := composeFixture(t)
	baseSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fragSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	composed := ComposedStackPath(home, "infra")
	if err := os.MkdirAll(filepath.Dir(composed), 0o755); err != nil {
		t.Fatal(err)
	}
	header := "# gridctl composed stack. Do not edit; re-run 'gridctl pack apply infra'.\n# layer: base pack=infra commit=" + baseSHA + " file=stack.yaml\n# layer: fragment pack=netops commit=" + fragSHA + " file=servers.yaml\nversion: \"1\"\nname: infra\n"
	if err := os.WriteFile(composed, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, err := mgrs.Statuses(context.Background(), StatusOptions{Pack: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	row := fragmentRow(t, statuses, "netops")
	if row.State != "in-sync" || row.Detail != "servers.yaml ("+fragSHA[:8]+")" {
		t.Fatalf("in-sync = %+v", row)
	}
	if statuses[0].NeedsAttention {
		t.Fatal("in-sync fragment is attention")
	}

	if err := os.WriteFile(composed, []byte(strings.Replace(header, fragSHA, "dddddddddddddddddddddddddddddddddddddddd", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, err = mgrs.Statuses(context.Background(), StatusOptions{Pack: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	row = fragmentRow(t, statuses, "netops")
	if row.State != "stale" || !statuses[0].NeedsAttention {
		t.Fatalf("stale = %+v attention=%v", row, statuses[0].NeedsAttention)
	}

	if err := os.RemoveAll(fragDir); err != nil {
		t.Fatal(err)
	}
	statuses, err = mgrs.Statuses(context.Background(), StatusOptions{Pack: "infra"})
	if err != nil {
		t.Fatal(err)
	}
	row = fragmentRow(t, statuses, "netops")
	if row.State != "missing" || !statuses[0].NeedsAttention {
		t.Fatalf("missing = %+v attention=%v", row, statuses[0].NeedsAttention)
	}
	if !strings.Contains(row.Remediation, "gridctl pack add") {
		t.Fatalf("remediation = %q", row.Remediation)
	}

	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragDir, "servers.yaml"), []byte("version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\"echo\", \"extra\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, err = mgrs.Statuses(context.Background(), StatusOptions{Pack: "netops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].NeedsAttention {
		t.Fatalf("stopped booster attention = %+v", statuses)
	}
	if statuses[0].Rows[0].Kind != "stack" || statuses[0].Rows[0].State != "missing" {
		t.Fatalf("stopped row = %+v", statuses[0].Rows[0])
	}
	_ = baseDir
}

func fragmentRow(t *testing.T, statuses []PackStatus, name string) Row {
	t.Helper()
	if len(statuses) != 1 {
		t.Fatalf("statuses = %+v", statuses)
	}
	for _, row := range statuses[0].Rows {
		if row.Kind == "fragment" && row.Name == name {
			return row
		}
	}
	t.Fatalf("no fragment row in %+v", statuses[0].Rows)
	return Row{}
}

func composeFixture(t *testing.T) (*Managers, string, string, string) {
	t.Helper()
	mgrs, _ := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	baseSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fragSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	baseDir := PackCheckoutDir(home, "infra", baseSHA)
	fragDir := PackCheckoutDir(home, "netops", fragSHA)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "stack.yaml"), []byte("version: \"1\"\nname: infra\nmcp-servers:\n  - name: base\n    command: [\"echo\", \"base\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragDir, "servers.yaml"), []byte("version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\"echo\", \"extra\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"infra": {Repo: "https://example.com/infra.git", CommitSHA: baseSHA, Pack: &skills.LockedPack{
			Name:  "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: fragSHA, Pack: &skills.LockedPack{
			Name:     "netops",
			Fragment: &skills.LockedFragment{Base: "infra", Path: "servers.yaml", Name: "infra", CheckoutDir: fragDir},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
	}
	return mgrs, home, baseDir, fragDir
}
