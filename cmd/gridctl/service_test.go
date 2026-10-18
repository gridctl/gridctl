package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/state"
)

type cmdFakeSupervisor struct {
	enableErr error
	query     service.RuntimeState
	linger    string
	enabled   []string
	stopped   []string
	stopErr   error
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
	fake := &cmdFakeSupervisor{}
	prevStop := stopSupervisedService
	stopSupervisedService = func(ctx context.Context, name string) (bool, string, string, error) {
		stopped, path, err := service.NewManager(home, fake).Stop(ctx, name)
		return stopped, "systemd unit", path, err
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

type errString string

func (e errString) Error() string { return string(e) }

func newLockedStore(t *testing.T, dir string) bool {
	t.Helper()
	// Imported in the vault test path via a local helper in this file's
	// sibling. The store package is used through a small wrapper below.
	return lockTestStore(t, dir)
}
