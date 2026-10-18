//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestServiceInstall_ForegroundAndUnit runs with no container runtime.
// It keeps XDG_CONFIG_HOME under a temporary GRIDCTL_HOME, runs the
// generated foreground command, observes /ready and status, and checks
// that a real install --no-enable writes the unit only under that home.
func TestServiceInstall_ForegroundAndUnit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, ".")
	build.Dir = filepath.Join(root, "cmd", "gridctl")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gridctl: %v\n%s", err, out)
	}
	mock := filepath.Join(dir, "mock-stdio-server")
	mockBuild := exec.CommandContext(ctx, "go", "build", "-o", mock, ".")
	mockBuild.Dir = filepath.Join(root, "examples", "_mock-servers", "local-stdio-server")
	if out, err := mockBuild.CombinedOutput(); err != nil {
		t.Fatalf("build mock: %v\n%s", err, out)
	}

	home := filepath.Join(dir, "home")
	for _, sub := range []string{"", ".config", ".local/share", ".local/state", ".cache", "run"} {
		if err := os.MkdirAll(filepath.Join(home, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte(localStackYAML("svc-install", mock)), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	base := port + 1000
	env := serviceInstallEnv(home)

	before := snapshotTree(t, dir)
	dry, dryErrOut, err := runServiceCmd(ctx, t, bin, env, "service", "install", "--stack", stackPath,
		"--port", strconv.Itoa(port), "--base-port", strconv.Itoa(base),
		"--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s\n%s", err, dry, dryErrOut)
	}
	var doc struct {
		ExecStart []string `json:"exec_start"`
		UnitPath  string   `json:"unit_path"`
		Text      string   `json:"text"`
		DryRun    bool     `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(dry), &doc); err != nil {
		t.Fatalf("dry-run json: %v\n%s", err, dry)
	}
	if !doc.DryRun || len(doc.ExecStart) < 3 || doc.Text == "" {
		t.Fatalf("dry-run doc = %+v", doc)
	}
	if !strings.HasPrefix(doc.UnitPath, home+string(os.PathSeparator)) {
		t.Fatalf("unit path %s is outside home %s", doc.UnitPath, home)
	}
	if _, err := os.Stat(doc.UnitPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote %s", doc.UnitPath)
	}
	assertOnlyUnderHome(t, dir, home, before)

	cmd := exec.Command(doc.ExecStart[0], doc.ExecStart[1:]...)
	cmd.Env = env
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_, _ = cmd.Process.Wait()
		}
	})
	readyURL := "http://127.0.0.1:" + strconv.Itoa(port) + "/ready"
	deadline := time.Now().Add(45 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(readyURL)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
		t.Fatalf("gateway did not become ready\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	statusOut, statusErr, err := runServiceCmd(ctx, t, bin, env, "status", "--json")
	if err != nil {
		t.Fatalf("status: %v\n%s\n%s", err, statusOut, statusErr)
	}
	if !strings.Contains(statusOut, `"name": "svc-install"`) && !strings.Contains(statusOut, `"name":"svc-install"`) {
		t.Fatalf("status does not list the stack:\n%s", statusOut)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("foreground exit: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("foreground process did not exit on SIGTERM")
	}
	stateFile := filepath.Join(home, ".gridctl", "state", "svc-install.json")
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatalf("state file remains after SIGTERM: %v", err)
	}

	beforeInstall := snapshotTree(t, dir)
	installOut, installErr, err := runServiceCmd(ctx, t, bin, env, "service", "install", "--stack", stackPath,
		"--port", strconv.Itoa(port), "--base-port", strconv.Itoa(base), "--no-enable")
	if err != nil {
		t.Fatalf("install: %v\n%s\n%s", err, installOut, installErr)
	}
	unit := filepath.Join(home, ".config", "systemd", "user", "gridctl-svc-install.service")
	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("unit file: %v\n%s", err, installOut)
	}
	lockPath := filepath.Join(home, ".gridctl", "project.lock.yaml")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockData), "kind: service") || !strings.Contains(string(lockData), "svc-install") {
		t.Fatalf("lockfile missing service entry:\n%s", lockData)
	}
	assertOnlyUnderHome(t, dir, home, beforeInstall)

	resetOut, resetErr, err := runServiceCmd(ctx, t, bin, env, "reset", "--dry-run", "--format", "json")
	if err != nil {
		t.Fatalf("reset dry-run: %v\n%s\n%s", err, resetOut, resetErr)
	}
	if !strings.Contains(resetOut, `"kind": "service"`) && !strings.Contains(resetOut, `"kind":"service"`) {
		t.Fatalf("reset dry-run missing service row:\n%s", resetOut)
	}
	uninstallOut, uninstallErr, err := runServiceCmd(ctx, t, bin, env, "service", "uninstall", "svc-install")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s\n%s", err, uninstallOut, uninstallErr)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("unit remains after uninstall: %v", err)
	}
	lockData, err = os.ReadFile(lockPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(lockData), "kind: service") {
		t.Fatalf("lock entry remains:\n%s", lockData)
	}
}

func serviceInstallEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GRIDCTL_HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_RUNTIME_DIR=" + filepath.Join(home, "run"),
	}
}

func runServiceCmd(ctx context.Context, t *testing.T, bin string, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func snapshotTree(t *testing.T, root string) map[string]struct{} {
	t.Helper()
	found := map[string]struct{}{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		found[path] = struct{}{}
		return nil
	})
	return found
}

func assertOnlyUnderHome(t *testing.T, root, home string, before map[string]struct{}) {
	t.Helper()
	after := snapshotTree(t, root)
	for path := range after {
		if _, ok := before[path]; ok {
			continue
		}
		if path != home && !strings.HasPrefix(path, home+string(os.PathSeparator)) {
			t.Errorf("wrote %s outside %s", path, home)
		}
	}
}
