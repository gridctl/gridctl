package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/state"
)

type cmdFakeSupervisor struct {
	enableErr  error
	query      service.RuntimeState
	queryErr   error
	linger     string
	enabled    []string
	stopped    []string
	restarted  []string
	stopErr    error
	restartErr error
}

func (f *cmdFakeSupervisor) Enable(_ context.Context, spec service.Spec) error {
	f.enabled = append(f.enabled, spec.Name)
	return f.enableErr
}
func (f *cmdFakeSupervisor) Disable(context.Context, service.Spec) error { return nil }
func (f *cmdFakeSupervisor) Stop(_ context.Context, spec service.Spec) error {
	f.stopped = append(f.stopped, spec.Name)
	return f.stopErr
}
func (f *cmdFakeSupervisor) Query(context.Context, service.Spec) (service.RuntimeState, error) {
	if f.queryErr != nil {
		return service.RuntimeState{}, f.queryErr
	}
	if f.query.State == "" {
		return service.RuntimeState{State: "running", Enabled: true, PID: 7}, nil
	}
	return f.query, nil
}
func (f *cmdFakeSupervisor) Logs(context.Context, service.Spec, service.LogOptions) error { return nil }
func (f *cmdFakeSupervisor) Linger(context.Context) (string, error) {
	if f.linger == "" {
		return "no", nil
	}
	return f.linger, nil
}
func (f *cmdFakeSupervisor) DaemonReload(context.Context) error { return nil }
func (f *cmdFakeSupervisor) Restart(_ context.Context, spec service.Spec) error {
	f.restarted = append(f.restarted, spec.Name)
	return f.restartErr
}

func serviceTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "")
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE_FILE", "")
	return home
}

func resetServiceFlags(t *testing.T) {
	t.Helper()
	serviceStack = ""
	servicePack = ""
	servicePort = 8180
	serviceBasePort = 9000
	serviceWatch = false
	serviceDryRun = false
	serviceNoEnable = false
	serviceReplace = false
	serviceForce = false
	servicePassphraseFile = ""
	serviceBinary = ""
	serviceFormat = "table"
	serviceJSON = false
	serviceFormatChanged = false
	serviceGOOS = "linux"
	prev := newServiceSupervisor
	newServiceSupervisor = func() service.Supervisor { return &cmdFakeSupervisor{} }
	t.Cleanup(func() { newServiceSupervisor = prev; serviceGOOS = "linux" })
}

func writePlainStack(t *testing.T, home, name string) string {
	t.Helper()
	path := filepath.Join(home, name+".yaml")
	body := "version: \"1\"\nname: " + name + "\nmcp-servers:\n  - name: echo\n    command: [\"/bin/echo\"]\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestService_WindowsUnsupported(t *testing.T) {
	var buf bytes.Buffer
	serviceGOOS = "windows"
	t.Cleanup(func() { serviceGOOS = "linux" })
	if code := refuseUnsupportedService(&buf); code != serviceExitAttention {
		t.Fatalf("code = %d", code)
	}
	if strings.TrimSpace(buf.String()) != serviceUnsupportedMessage {
		t.Fatalf("message = %q", buf.String())
	}
}

func TestServiceInstall_DryRunJSONWritesNothing(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	path := writePlainStack(t, home, "netops")
	serviceStack = path
	serviceDryRun = true
	serviceJSON = true
	var stdout, stderr bytes.Buffer
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if doc["schema_version"] != float64(1) || doc["dry_run"] != true {
		t.Fatalf("doc = %#v", doc)
	}
	text, _ := doc["text"].(string)
	if !strings.Contains(text, "--foreground") || strings.Contains(text, "GRIDCTL_VAULT_PASSPHRASE") {
		t.Fatalf("text = %s", text)
	}
	unit, _ := doc["unit_path"].(string)
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote %s", unit)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "project.lock.yaml")); !os.IsNotExist(err) {
		t.Fatal("dry-run created a lockfile")
	}
}

func TestServiceInstall_VaultNeedsRefusal(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	path := filepath.Join(home, "needs.yaml")
	body := "version: \"1\"\nname: needs\nmcp-servers:\n  - name: demo\n    command: [\"/bin/echo\"]\n    env:\n      TOKEN: \"${var:FROM_ENV}\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FROM_ENV", "present")
	dir, err := state.VaultDir()
	if err != nil {
		t.Fatal(err)
	}
	store := newLockedStore(t, dir)
	if !store {
		t.Fatal("expected a locked store")
	}
	serviceStack = path
	var stdout, stderr bytes.Buffer
	code := runServiceInstall(t.Context(), &stdout, &stderr)
	if code != serviceExitAttention {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--passphrase-file") || !strings.Contains(stderr.String(), "gridctl var") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "present") {
		t.Fatal("refusal echoed an environment value")
	}
}

func TestServiceInstall_EnableFailureKeepsRecord(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	fake := &cmdFakeSupervisor{enableErr: errString("systemctl failed")}
	newServiceSupervisor = func() service.Supervisor { return fake }
	path := writePlainStack(t, home, "netops")
	serviceStack = path
	var stdout, stderr bytes.Buffer
	code := runServiceInstall(t.Context(), &stdout, &stderr)
	if code != serviceExitAttention {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Finish by hand") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", "gridctl-netops.service")); err != nil {
		t.Fatal(err)
	}
}

func TestDestroy_SupervisedDoesNotKill(t *testing.T) {
	home := serviceTestHome(t)
	mgr := service.NewManager(home, &cmdFakeSupervisor{})
	spec := service.Spec{
		Platform:  service.PlatformSystemd,
		Name:      "netops",
		StackName: "netops",
		Label:     "dev.gridctl.netops",
		Binary:    "/usr/local/bin/gridctl",
		Argv:      []string{"/usr/local/bin/gridctl", "apply", filepath.Join(home, "stack.yaml"), "--foreground", "--quiet", "--port", "8180", "--base-port", "9000"},
		Env:       []service.KV{{Key: "GRIDCTL_HOME", Value: home}, {Key: "PATH", Value: "/bin"}},
		Home:      home,
		UnitPath:  filepath.Join(home, ".config", "systemd", "user", "gridctl-netops.service"),
		StackFile: filepath.Join(home, "stack.yaml"),
		Port:      8180,
		BasePort:  9000,
	}
	if _, err := mgr.Install(t.Context(), service.InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{StackName: "netops", StackFile: spec.StackFile, PID: os.Getpid(), Port: 1}); err != nil {
		t.Fatal(err)
	}
	killed := false
	prevKill := killDaemonForDestroy
	killDaemonForDestroy = func(*state.DaemonState) error {
		killed = true
		return nil
	}
	t.Cleanup(func() { killDaemonForDestroy = prevKill })
	fake := &cmdFakeSupervisor{query: service.RuntimeState{State: "running", Enabled: true, PID: os.Getpid()}}
	prevStop := stopSupervisedService
	stopSupervisedService = func(ctx context.Context, name string) (supervisedStop, error) {
		report, err := service.NewManager(home, fake).Stop(ctx, name)
		return supervisedStop{
			present:     report.Present,
			kind:        "systemd unit",
			path:        report.Path,
			ownedPID:    report.OwnedPID,
			stoppedUnit: report.StoppedUnit,
		}, err
	}
	t.Cleanup(func() { stopSupervisedService = prevStop })

	if err := runDestroy("netops"); err != nil {
		t.Fatal(err)
	}
	if killed {
		t.Fatal("KillDaemon was called for a supervised gateway")
	}
	if len(fake.stopped) != 1 {
		t.Fatalf("stop calls = %v", fake.stopped)
	}
}

func TestStop_NotesInstalledServices(t *testing.T) {
	home := serviceTestHome(t)
	stubFindOrphan(t, func(int) (int, bool, error) { return 0, false, nil })
	if err := project.NewStore(home).Mutate(t.Context(), false, func(l *project.Lock) error {
		if err := l.Set(&project.Entry{
			Kind: project.KindService, Client: "systemd", Source: "netops",
			Path: filepath.Join(home, "unit.service"), SyncedAt: time.Now(),
			Extra: map[string]any{},
		}); err != nil {
			return err
		}
		return l.Save()
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = runStop()
	_ = w.Close()
	os.Stdout = old
	_, _ = buf.ReadFrom(r)
	if err == nil {
		t.Fatal("expected no-daemon error")
	}
	if !strings.Contains(buf.String(), "gridctl stop does not manage installed services") {
		t.Fatalf("output = %q, err = %v", buf.String(), err)
	}
}

func useDestroyStop(t *testing.T, home string, fake *cmdFakeSupervisor) {
	t.Helper()
	prev := stopSupervisedService
	stopSupervisedService = func(ctx context.Context, name string) (supervisedStop, error) {
		report, err := service.NewManager(home, fake).Stop(ctx, name)
		return supervisedStop{
			present: report.Present, kind: "systemd unit", path: report.Path,
			ownedPID: report.OwnedPID, stoppedUnit: report.StoppedUnit,
		}, err
	}
	t.Cleanup(func() { stopSupervisedService = prev })
}

func installNetops(t *testing.T, home string) service.Spec {
	t.Helper()
	spec := service.Spec{
		Platform: service.PlatformSystemd, Name: "netops", StackName: "netops",
		Label: "dev.gridctl.netops", Binary: "/usr/local/bin/gridctl",
		Argv:      []string{"/usr/local/bin/gridctl", "apply", filepath.Join(home, "stack.yaml"), "--foreground", "--quiet", "--port", "8180", "--base-port", "9000"},
		Env:       []service.KV{{Key: "GRIDCTL_HOME", Value: home}, {Key: "PATH", Value: "/bin"}},
		Home:      home,
		UnitPath:  filepath.Join(home, ".config", "systemd", "user", "gridctl-netops.service"),
		StackFile: filepath.Join(home, "stack.yaml"),
		Port:      8180, BasePort: 9000,
	}
	if _, err := service.NewManager(home, &cmdFakeSupervisor{}).Install(t.Context(), service.InstallOptions{Spec: spec, NoEnable: true}); err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestDestroy_InactiveUnitKillsManualDaemon(t *testing.T) {
	home := serviceTestHome(t)
	installNetops(t, home)
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if err := state.Save(&state.DaemonState{StackName: "netops", PID: cmd.Process.Pid, Port: 9, Home: home}); err != nil {
		t.Fatal(err)
	}
	killed := false
	prevKill := killDaemonForDestroy
	killDaemonForDestroy = func(*state.DaemonState) error {
		killed = true
		return nil
	}
	t.Cleanup(func() { killDaemonForDestroy = prevKill })
	fake := &cmdFakeSupervisor{query: service.RuntimeState{State: "inactive"}}
	useDestroyStop(t, home, fake)
	if err := runDestroy("netops"); err != nil {
		t.Fatal(err)
	}
	if !killed {
		t.Fatal("manual daemon was not killed")
	}
	if _, err := state.Load("netops"); !os.IsNotExist(err) {
		t.Fatalf("state file remained: %v", err)
	}
}

func TestDestroy_NotLoadedStillDestroys(t *testing.T) {
	home := serviceTestHome(t)
	installNetops(t, home)
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if err := state.Save(&state.DaemonState{StackName: "netops", PID: cmd.Process.Pid, Port: 9, Home: home}); err != nil {
		t.Fatal(err)
	}
	killed := false
	prevKill := killDaemonForDestroy
	killDaemonForDestroy = func(*state.DaemonState) error {
		killed = true
		return nil
	}
	t.Cleanup(func() { killDaemonForDestroy = prevKill })
	fake := &cmdFakeSupervisor{query: service.RuntimeState{State: "not-loaded"}}
	useDestroyStop(t, home, fake)
	if err := runDestroy("netops"); err != nil {
		t.Fatal(err)
	}
	if !killed {
		t.Fatal("not-loaded unit blocked destroy")
	}
	if len(fake.stopped) != 0 {
		t.Fatalf("stop calls = %v", fake.stopped)
	}
}

func TestDestroy_LoadedStopFailureDoesNotKill(t *testing.T) {
	home := serviceTestHome(t)
	installNetops(t, home)
	if err := state.Save(&state.DaemonState{StackName: "netops", PID: os.Getpid(), Port: 9, Home: home}); err != nil {
		t.Fatal(err)
	}
	killed := false
	prevKill := killDaemonForDestroy
	killDaemonForDestroy = func(*state.DaemonState) error {
		killed = true
		return nil
	}
	t.Cleanup(func() { killDaemonForDestroy = prevKill })
	fake := &cmdFakeSupervisor{
		query:   service.RuntimeState{State: "running", Enabled: true, PID: os.Getpid()},
		stopErr: errString("stop failed"),
	}
	useDestroyStop(t, home, fake)
	if err := runDestroy("netops"); err == nil {
		t.Fatal("expected stop failure")
	}
	if killed {
		t.Fatal("KillDaemon ran after a loaded stop failure")
	}
}

func TestServiceStatus_JSON(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	installNetops(t, home)
	serviceJSON = true
	var stdout, stderr bytes.Buffer
	code := runServiceStatus(t.Context(), &stdout, &stderr, "")
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	var doc struct {
		SchemaVersion int `json:"schema_version"`
		Services      []service.Row
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != 1 || len(doc.Services) != 1 || doc.Services[0].State != "running" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestServiceUninstall_AlreadyGone(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	spec := installNetops(t, home)
	if err := os.Remove(spec.UnitPath); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runServiceUninstall(t.Context(), &stdout, &stderr, "netops")
	if code != 0 {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already-gone") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestServiceLogs_RejectsBadFormat(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	serviceFormat = "yaml"
	serviceFormatChanged = true
	var stdout, stderr bytes.Buffer
	code := runServiceLogs(t.Context(), &stdout, &stderr, "netops")
	if code != serviceExitInfra {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid --format") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	_ = home
}

func TestServiceInstall_LockedNoteOnlyWhenEncrypted(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	path := writePlainStack(t, home, "plain")
	serviceStack = path
	serviceDryRun = true
	serviceJSON = true
	var stdout, stderr bytes.Buffer
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d\n%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "stays locked") {
		t.Fatalf("plaintext store noted a lock: %s", stdout.String())
	}
	resetServiceFlags(t)
	dir, err := state.VaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if !newLockedStore(t, dir) {
		t.Fatal("store did not lock")
	}
	serviceStack = path
	serviceDryRun = true
	serviceJSON = true
	stdout.Reset()
	stderr.Reset()
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != 0 {
		t.Fatalf("locked code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stays locked") {
		t.Fatalf("missing locked note: %s", stdout.String())
	}
}

func TestServiceInstall_SecretsSetsAndCredentialRef(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	dir, err := state.VaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if !newLockedStore(t, dir) {
		t.Fatal("store did not lock")
	}
	sets := filepath.Join(home, "sets.yaml")
	body := "version: \"1\"\nname: sets\nsecrets:\n  sets:\n    - production\nmcp-servers:\n  - name: demo\n    command: [\"/bin/echo\"]\n"
	if err := os.WriteFile(sets, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	serviceStack = sets
	var stdout, stderr bytes.Buffer
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != serviceExitAttention {
		t.Fatalf("sets code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "secrets.sets") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	cred := filepath.Join(home, "cred.yaml")
	body = "version: \"1\"\nname: creds\nmcp-servers:\n  - name: src\n    port: 3000\n    source:\n      type: git\n      url: https://github.com/example/repo.git\n      ref: main\n      auth:\n        method: token\n        credential_ref: \"${vault:GIT_TOKEN}\"\n"
	if err := os.WriteFile(cred, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	serviceStack = cred
	stderr.Reset()
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != serviceExitAttention {
		t.Fatalf("cred code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "credential_ref") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestServiceInstall_PassphraseFileRefused(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	path := writePlainStack(t, home, "plain")
	serviceStack = path
	bad := filepath.Join(home, "pass")
	if err := os.WriteFile(bad, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	servicePassphraseFile = bad
	var stdout, stderr bytes.Buffer
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != serviceExitAttention {
		t.Fatalf("mode code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "mode") || strings.Contains(stderr.String(), "secret") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	dir, err := state.VaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if !newLockedStore(t, dir) {
		t.Fatal("store did not lock")
	}
	good := filepath.Join(home, "pass-ok")
	if err := os.WriteFile(good, []byte("wrong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	servicePassphraseFile = good
	stderr.Reset()
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != serviceExitAttention {
		t.Fatalf("pass code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not unlock") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestServiceInstall_PackGateAndNewerLock(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	checkout := filepath.Join(home, "packs", "team")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(checkout, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("version: \"1\"\nname: team\nmcp-servers:\n  - name: echo\n    command: [\"/bin/echo\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	required := "true"
	lock := "version: 5\nsources:\n  pack:\n    repo: https://example.com/p\n    pack:\n      name: team\n      variables:\n        TEAM_TOKEN:\n          required: " + required + "\n      stack:\n        path: stack.yaml\n        name: team\n        content_hash: abc\n        checkout_dir: " + checkout + "\n"
	lockPath := filepath.Join(home, ".gridctl", "skills.lock.yaml")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	servicePack = "team"
	var stdout, stderr bytes.Buffer
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != serviceExitAttention {
		t.Fatalf("gate code = %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "gridctl var set TEAM_TOKEN") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if err := os.WriteFile(lockPath, []byte("version: 99\nsources: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runServiceInstall(t.Context(), &stdout, &stderr); code != serviceExitInfra {
		t.Fatalf("newer code = %d\n%s", code, stderr.String())
	}
}

func TestLogs_ServiceRedirectsWhenFileExists(t *testing.T) {
	home := serviceTestHome(t)
	installNetops(t, home)
	path, err := state.LogPath("netops")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale daemon line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runLogs(t.Context(), &buf, "netops"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "gridctl service logs netops") {
		t.Fatalf("output = %s", buf.String())
	}
	if strings.Contains(buf.String(), "stale daemon line") {
		t.Fatal("tailed the stale daemon log")
	}
}

func TestReload_PrintsSupervisedMessage(t *testing.T) {
	home := serviceTestHome(t)
	installNetops(t, home)
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	reloadFormat = "table"
	t.Cleanup(func() {
		os.Stdout = oldOut
		os.Stderr = oldErr
		reloadFormat = "text"
	})
	noteSupervisedReload("netops")
	_ = w.Close()
	os.Stdout = oldOut
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if !strings.Contains(buf.String(), "reloaded in place and not restarted") {
		t.Fatalf("output = %q", buf.String())
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = ew
	reloadFormat = "json"
	noteSupervisedReload("netops")
	_ = ew.Close()
	os.Stderr = oldErr
	buf.Reset()
	_, _ = buf.ReadFrom(er)
	if !strings.Contains(buf.String(), "reloaded in place and not restarted") {
		t.Fatalf("stderr = %q", buf.String())
	}
	_ = home
}

func TestDoctor_ServiceRows(t *testing.T) {
	home := serviceTestHome(t)
	resetServiceFlags(t)
	var checks []doctorCheck
	checkServices(t.Context(), &checks)
	found := false
	for _, c := range checks {
		if c.ID == "service.linger" && c.Status == doctorStatusInfo {
			found = true
		}
	}
	if !found {
		t.Fatalf("linger info missing: %+v", checks)
	}
	installNetops(t, home)
	newServiceSupervisor = func() service.Supervisor {
		return &cmdFakeSupervisor{query: service.RuntimeState{State: "inactive", Enabled: false}, linger: "no"}
	}
	checks = nil
	checkServices(t.Context(), &checks)
	var name, linger doctorCheck
	for _, c := range checks {
		if c.ID == "service.netops" {
			name = c
		}
		if c.ID == "service.linger" {
			linger = c
		}
	}
	if name.Status != doctorStatusWarn || !strings.Contains(name.Message, "inactive") {
		t.Fatalf("service row = %+v", name)
	}
	if linger.Status != doctorStatusWarn || !strings.Contains(linger.Message, "linger is off") {
		t.Fatalf("linger row = %+v", linger)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func newLockedStore(t *testing.T, dir string) bool {
	t.Helper()
	// Imported in the vault test path via a local helper in this file's
	// sibling. The store package is used through a small wrapper below.
	return lockTestStore(t, dir)
}
