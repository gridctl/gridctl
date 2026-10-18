package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/state"
)

type fakeSupervisor struct {
	enableErr   error
	disableErr  error
	stopErr     error
	restartErr  error
	query       RuntimeState
	queryErr    error
	linger      string
	enabled     []string
	disabled    []string
	stopped     []string
	restarted   []string
	reloaded    int
	logsCalled  bool
}

func (f *fakeSupervisor) Enable(_ context.Context, spec Spec) error {
	f.enabled = append(f.enabled, spec.Name)
	return f.enableErr
}

func (f *fakeSupervisor) Disable(_ context.Context, spec Spec) error {
	f.disabled = append(f.disabled, spec.Name)
	return f.disableErr
}

func (f *fakeSupervisor) Stop(_ context.Context, spec Spec) error {
	f.stopped = append(f.stopped, spec.Name)
	return f.stopErr
}

func (f *fakeSupervisor) Query(context.Context, Spec) (RuntimeState, error) {
	if f.query.State == "" && f.queryErr == nil {
		return RuntimeState{State: "running", Enabled: true, PID: 42}, nil
	}
	return f.query, f.queryErr
}

func (f *fakeSupervisor) Logs(context.Context, Spec, LogOptions) error {
	f.logsCalled = true
	return nil
}

func (f *fakeSupervisor) Linger(context.Context) (string, error) {
	if f.linger == "" {
		return "no", nil
	}
	return f.linger, nil
}

func (f *fakeSupervisor) DaemonReload(context.Context) error {
	f.reloaded++
	return nil
}

func (f *fakeSupervisor) Restart(_ context.Context, spec Spec) error {
	f.restarted = append(f.restarted, spec.Name)
	return f.restartErr
}

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

func testSpec(home string) Spec {
	spec := stackSpec(PlatformSystemd, false)
	spec.Home = home
	spec.UnitPath = filepath.Join(home, ".config", "systemd", "user", "gridctl-netops.service")
	spec.LogPath = filepath.Join(home, ".gridctl", "logs", "netops.log")
	spec.StackFile = filepath.Join(home, "stack.yaml")
	spec.Env = []KV{{Key: "GRIDCTL_HOME", Value: home}, {Key: "PATH", Value: "/usr/bin:/bin"}}
	spec.Argv = []string{"/usr/local/bin/gridctl", "apply", spec.StackFile, "--foreground", "--quiet", "--port", "8180", "--base-port", "9000"}
	return spec
}

func TestInstall_DryRunWritesNothing(t *testing.T) {
	home := testHome(t)
	mgr := NewManager(home, &fakeSupervisor{})
	lockPath := mgr.store.Path()
	before, err := os.Stat(lockPath)
	lockMissing := os.IsNotExist(err)
	var mtime time.Time
	if !lockMissing {
		mtime = before.ModTime()
	}

	res, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == "" || !res.DryRun || !res.WouldEnable {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(res.UnitPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote %s", res.UnitPath)
	}
	after, err := os.Stat(lockPath)
	if lockMissing {
		if err == nil {
			t.Fatal("dry-run created a lockfile")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(mtime) {
		t.Fatalf("lockfile mtime changed: %s -> %s", mtime, after.ModTime())
	}
}

func TestInstall_DryRunConflictDoesNotWrite(t *testing.T) {
	home := testHome(t)
	mgr := NewManager(home, &fakeSupervisor{})
	spec := testSpec(home)
	err := mgr.store.Mutate(t.Context(), false, func(l *project.Lock) error {
		if err := l.Set(&project.Entry{
			Kind: project.KindWiring, Client: "claude", Source: "gridctl", Path: spec.UnitPath, SyncedAt: time.Now(),
		}); err != nil {
			return err
		}
		return l.Save()
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(mgr.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Install(t.Context(), InstallOptions{Spec: spec, DryRun: true})
	if err == nil {
		t.Fatal("expected path conflict")
	}
	after, err := os.Stat(mgr.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("conflict dry-run changed the lockfile")
	}
	if _, statErr := os.Stat(spec.UnitPath); !os.IsNotExist(statErr) {
		t.Fatal("conflict dry-run wrote the unit")
	}
}

func TestInstall_RecordsBeforeWriteAndEnables(t *testing.T) {
	home := testHome(t)
	fake := &fakeSupervisor{linger: "no"}
	mgr := NewManager(home, fake)
	res, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Enabled || res.PID != 42 || res.Linger != "no" {
		t.Fatalf("result = %+v", res)
	}
	if len(fake.enabled) != 1 {
		t.Fatalf("enable calls = %v", fake.enabled)
	}
	data, err := os.ReadFile(res.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	l, err := mgr.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entry := l.Get(project.KindService, "systemd", "netops")
	if entry == nil {
		t.Fatal("missing lock entry")
	}
	if entry.InstalledHash != Hash(string(data)) {
		t.Fatalf("hash = %s, file hash = %s", entry.InstalledHash, Hash(string(data)))
	}
	if !entry.CreatedByGridctl {
		t.Fatal("expected created_by_gridctl")
	}
	if extraString(entry.Extra["service_name"]) != "netops" {
		t.Fatalf("extra = %#v", entry.Extra)
	}
}

func TestInstall_DriftWithoutForce(t *testing.T) {
	home := testHome(t)
	fake := &fakeSupervisor{}
	mgr := NewManager(home, fake)
	spec := testSpec(home)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true})
	if err == nil {
		t.Fatal("expected drift refusal")
	}
	got, err := os.ReadFile(spec.UnitPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hand edit\n" {
		t.Fatalf("unit was rewritten: %q", got)
	}
}

func TestInstall_ReplaceStopsRunningDaemon(t *testing.T) {
	home := testHome(t)
	cmd := startSleep(t)
	// Reap the child when KillDaemon signals it so IsRunning goes false
	// without waiting out the five-second SIGKILL fallback.
	go func() { _ = cmd.Wait() }()
	if err := state.Save(&state.DaemonState{
		StackName: "netops",
		StackFile: filepath.Join(home, "stack.yaml"),
		PID:       cmd.Process.Pid,
		Port:      8180,
		StartedAt: time.Now(),
		Home:      home,
	}); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(home, &fakeSupervisor{})
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), NoEnable: true}); err == nil {
		t.Fatal("expected running-daemon refusal")
	}
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), NoEnable: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Load("netops"); !os.IsNotExist(err) {
		t.Fatalf("state file still present: %v", err)
	}
}

func TestUninstall_KeepsDrift(t *testing.T) {
	home := testHome(t)
	fake := &fakeSupervisor{}
	mgr := NewManager(home, fake)
	spec := testSpec(home)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Uninstall(t.Context(), "netops", UninstallOptions{})
	if err == nil {
		t.Fatal("expected drift exit")
	}
	if res.Action != "kept-drift" {
		t.Fatalf("action = %s", res.Action)
	}
	if len(fake.disabled) != 0 {
		t.Fatal("drifted uninstall stopped the unit")
	}
	if _, err := os.Stat(spec.UnitPath); err != nil {
		t.Fatal(err)
	}
}

func TestStatus_NotRunning(t *testing.T) {
	home := testHome(t)
	fake := &fakeSupervisor{query: RuntimeState{State: "inactive", Enabled: false}}
	mgr := NewManager(home, fake)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	rows, err := mgr.Status(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != "inactive" || rows[0].Enabled != "no" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Remediation == "" {
		t.Fatal("missing remediation")
	}
}

func TestInstall_ForeignFileRefused(t *testing.T) {
	home := testHome(t)
	spec := testSpec(home)
	if err := os.MkdirAll(filepath.Dir(spec.UnitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(home, &fakeSupervisor{})
	_, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true})
	if err == nil || !strings.Contains(err.Error(), "not recorded") {
		t.Fatalf("err = %v", err)
	}
	got, err := os.ReadFile(spec.UnitPath)
	if err != nil || string(got) != "user unit\n" {
		t.Fatalf("unit = %q, err = %v", got, err)
	}
	_, err = mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true, DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "not recorded") {
		t.Fatalf("dry-run err = %v", err)
	}
}

func TestInstall_ForeignFileForceBacksUp(t *testing.T) {
	home := testHome(t)
	spec := testSpec(home)
	if err := os.MkdirAll(filepath.Dir(spec.UnitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(home, &fakeSupervisor{})
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(spec.UnitPath + ".gridctl-backup-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("backups = %v, err = %v", matches, err)
	}
	backed, err := os.ReadFile(matches[0])
	if err != nil || string(backed) != "user unit\n" {
		t.Fatalf("backup = %q, err = %v", backed, err)
	}
	l, err := mgr.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entry := l.Get(project.KindService, "systemd", "netops")
	if entry == nil || !entry.CreatedByGridctl {
		t.Fatalf("entry = %+v, want created_by_gridctl after the replacement", entry)
	}
}

func TestInstall_DriftRefusedBeforeReplace(t *testing.T) {
	home := testHome(t)
	spec := testSpec(home)
	mgr := NewManager(home, &fakeSupervisor{})
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := startSleep(t)
	go func() { _ = cmd.Wait() }()
	if err := state.Save(&state.DaemonState{
		StackName: "netops", PID: cmd.Process.Pid, Port: 8180, StartedAt: time.Now(), Home: home,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true, Replace: true})
	if err == nil || !strings.Contains(err.Error(), "edited") {
		t.Fatalf("err = %v", err)
	}
	if !state.IsRunning(&state.DaemonState{PID: cmd.Process.Pid}) {
		t.Fatal("drift refusal killed the daemon")
	}
}

func TestInstall_ReinstallOwnedRestarts(t *testing.T) {
	home := testHome(t)
	cmd := startSleep(t)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	fake := &fakeSupervisor{query: RuntimeState{State: "running", Enabled: true, PID: cmd.Process.Pid}}
	mgr := NewManager(home, fake)
	spec := testSpec(home)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "netops", PID: cmd.Process.Pid, Port: 8180, StartedAt: time.Now(), Home: home,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if len(fake.restarted) != 1 {
		t.Fatalf("restart calls = %v", fake.restarted)
	}
	if !state.IsRunning(&state.DaemonState{PID: cmd.Process.Pid}) {
		t.Fatal("reinstall killed the supervised process")
	}
}

func TestInstall_WriteFailureRollsBack(t *testing.T) {
	home := testHome(t)
	spec := testSpec(home)
	dir := filepath.Dir(spec.UnitPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	mgr := NewManager(home, &fakeSupervisor{})
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err == nil {
		t.Fatal("expected write failure")
	}
	l, err := mgr.store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if entry := l.Get(project.KindService, "systemd", "netops"); entry != nil {
		t.Fatalf("lock entry remained after write failure: %+v", entry)
	}
}

func TestStatus_StalePack(t *testing.T) {
	home := testHome(t)
	spec := testSpec(home)
	spec.Pack = "team"
	spec.StackFile = filepath.Join(home, "old", "stack.yaml")
	mgr := NewManager(home, &fakeSupervisor{})
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".gridctl", "skills.lock.yaml")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: 5\nsources:\n  pack:\n    repo: https://example.com/p\n    pack:\n      name: team\n      stack:\n        path: stack.yaml\n        name: team\n        content_hash: abc\n        checkout_dir: " + filepath.Join(home, "new") + "\n"
	if err := os.WriteFile(lockPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := mgr.Status(t.Context(), "netops")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != "stale" {
		t.Fatalf("rows = %+v", rows)
	}
	if !strings.Contains(rows[0].Remediation, "gridctl service install --pack team") {
		t.Fatalf("remediation = %q", rows[0].Remediation)
	}
}

func TestStatus_MissingSupervisor(t *testing.T) {
	home := testHome(t)
	fake := &fakeSupervisor{queryErr: &InfraError{Msg: "systemctl not found"}}
	mgr := NewManager(home, fake)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Status(t.Context(), "")
	if !isInfra(err) {
		t.Fatalf("err = %v, want infra", err)
	}
}

func TestUninstall_AlreadyGone(t *testing.T) {
	home := testHome(t)
	mgr := NewManager(home, &fakeSupervisor{})
	spec := testSpec(home)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(spec.UnitPath); err != nil {
		t.Fatal(err)
	}
	res, err := mgr.Uninstall(t.Context(), "netops", UninstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "already-gone" {
		t.Fatalf("action = %s", res.Action)
	}
}

func TestPreview_ForceRemovesDrift(t *testing.T) {
	home := testHome(t)
	mgr := NewManager(home, &fakeSupervisor{})
	spec := testSpec(home)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.UnitPath, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept, err := mgr.Preview(t.Context(), false)
	if err != nil || len(kept) != 1 || kept[0].Action != "kept-drift" {
		t.Fatalf("preview = %+v, err = %v", kept, err)
	}
	forced, err := mgr.Preview(t.Context(), true)
	if err != nil || len(forced) != 1 || forced[0].Action != "would-remove" {
		t.Fatalf("force preview = %+v, err = %v", forced, err)
	}
}

func TestStop_NotLoadedIsUnsupervised(t *testing.T) {
	home := testHome(t)
	fake := &fakeSupervisor{query: RuntimeState{State: "not-loaded"}}
	mgr := NewManager(home, fake)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	report, err := mgr.Stop(t.Context(), "netops")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Present || !report.NotLoaded || report.OwnedPID != 0 || report.StoppedUnit {
		t.Fatalf("report = %+v", report)
	}
	if len(fake.stopped) != 0 {
		t.Fatalf("stop calls = %v", fake.stopped)
	}
}

func TestStop_InactiveDoesNotOwnManualPID(t *testing.T) {
	home := testHome(t)
	cmd := startSleep(t)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	fake := &fakeSupervisor{query: RuntimeState{State: "inactive", PID: 0}}
	mgr := NewManager(home, fake)
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: testSpec(home), NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "netops", PID: cmd.Process.Pid, Port: 8180, StartedAt: time.Now(), Home: home,
	}); err != nil {
		t.Fatal(err)
	}
	report, err := mgr.Stop(t.Context(), "netops")
	if err != nil {
		t.Fatal(err)
	}
	if report.OwnedPID != 0 || !report.StoppedUnit || report.NotLoaded {
		t.Fatalf("report = %+v", report)
	}
	if len(fake.stopped) != 1 {
		t.Fatalf("stop calls = %v", fake.stopped)
	}
}

func TestInstall_LaunchdCreatesLogDir(t *testing.T) {
	home := testHome(t)
	spec := testSpec(home)
	spec.Platform = PlatformLaunchd
	spec.UnitPath = filepath.Join(home, "Library", "LaunchAgents", "dev.gridctl.netops.plist")
	spec.LogPath = filepath.Join(home, ".gridctl", "logs", "netops.log")
	mgr := NewManager(home, &fakeSupervisor{})
	if _, err := mgr.Install(t.Context(), InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(spec.LogPath))
	if err != nil || !info.IsDir() {
		t.Fatalf("log dir: %v", err)
	}
}

func startSleep(t *testing.T) *exec.Cmd {
	t.Helper()
	return startSleepProcess(t)
}
