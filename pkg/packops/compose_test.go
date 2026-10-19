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
			Name: "infra",
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
			Name: "infra",
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: "infra", CheckoutDir: baseDir},
		}},
		"netops": {Repo: "https://example.com/netops.git", CommitSHA: fragSHA, Pack: &skills.LockedPack{
			Name: "netops",
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
	if row.State != "target-missing" || row.Detail != `base pack "infra" is not added` {
		t.Fatalf("row = %+v", row)
	}
	if row.Remediation != "re-add the base, then re-run 'gridctl pack add https://example.com/netops.git'" {
		t.Fatalf("remediation = %q", row.Remediation)
	}
	if !statuses[0].NeedsAttention {
		t.Fatal("target-missing booster is not attention")
	}
}
