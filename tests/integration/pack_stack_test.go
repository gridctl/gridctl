//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// TestPackStack_Lifecycle drives a real pack-carried local-process stack:
// packops add, a race-built CLI launch, unchanged wiring against a second
// daemon, replace after a new commit, and remove.
func TestPackStack_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	build := func(source, name string, race bool) string {
		t.Helper()
		bin := filepath.Join(dir, name)
		args := []string{"build", "-o", bin, "."}
		if race {
			args = []string{"build", "-race", "-o", bin, "."}
		}
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = source
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		return bin
	}
	bin := build(filepath.Join(root, "cmd/gridctl"), "gridctl", true)
	mock := build(filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), "mock-stdio-server", false)

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(dir, key))
	}

	repoDir := filepath.Join(dir, "pack")
	writePackRepo(t, repoDir, mock, "one")
	port := freePort(t)
	otherPort := freePort(t)

	mgrs, imp := packStackEngine(t, home)
	if _, err := mgrs.Add(ctx, imp, packops.AddOptions{Repo: repoDir}); err != nil {
		t.Fatal(err)
	}
	locked, err := mgrs.LoadLockedPack("pack-stack")
	if err != nil {
		t.Fatal(err)
	}
	if locked.Stack == nil {
		t.Fatal("add did not record a stack")
	}
	pinned := filepath.Join(locked.Stack.CheckoutDir, "stack.yaml")
	if _, err := os.Stat(pinned); err != nil {
		t.Fatal(err)
	}

	otherPath := filepath.Join(dir, "other.yaml")
	if err := os.WriteFile(otherPath, []byte(localStackYAML("other-stack", mock)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = runGridctl(cleanupCtx, t, bin, home, "destroy", "pack-stack")
		_, _ = runGridctl(cleanupCtx, t, bin, home, "destroy", "other-stack")
	})

	stdout, err := runGridctl(ctx, t, bin, home, "pack", "apply", "pack-stack", "--port", strconv.Itoa(port), "--plain")
	if err != nil {
		t.Fatalf("pack apply: %v\n%s", err, stdout)
	}
	if strings.Contains(stdout, "Stopping running stack") || strings.Contains(stdout, "started successfully") || strings.Contains(stdout, "Gateway:") {
		t.Fatalf("deploy leaked onto stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "started") || !strings.Contains(stdout, "Applied") {
		t.Fatalf("stdout =\n%s", stdout)
	}
	st, err := state.Load("pack-stack")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.StackFile, locked.Stack.CheckoutDir) {
		t.Fatalf("StackFile = %s, checkout = %s", st.StackFile, locked.Stack.CheckoutDir)
	}
	if st.Port != port {
		t.Fatalf("pack port = %d, want %d", st.Port, port)
	}
	cfg, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "localhost:"+strconv.Itoa(port)) || strings.Contains(string(cfg), "localhost:"+strconv.Itoa(otherPort)) {
		t.Fatalf("wiring used the wrong port:\n%s", cfg)
	}

	statusOut, statusErr := runGridctl(ctx, t, bin, home, "pack", "status", "pack-stack", "--format", "json")
	if statusErr != nil {
		t.Fatalf("status: %v\n%s", statusErr, statusOut)
	}
	if !strings.Contains(statusOut, `"kind": "stack"`) || !strings.Contains(statusOut, `"state": "in-sync"`) {
		t.Fatalf("status =\n%s", statusOut)
	}

	if stdout, err = runGridctl(ctx, t, bin, home, "apply", otherPath, "--port", strconv.Itoa(otherPort)); err != nil {
		t.Fatalf("other daemon: %v\n%s", err, stdout)
	}
	stdout, err = runGridctl(ctx, t, bin, home, "pack", "apply", "pack-stack", "--port", strconv.Itoa(port), "--plain")
	if err != nil {
		t.Fatalf("second apply: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "unchanged") {
		t.Fatalf("second apply stdout =\n%s", stdout)
	}
	cfg, err = os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "localhost:"+strconv.Itoa(port)) || strings.Contains(string(cfg), "localhost:"+strconv.Itoa(otherPort)) {
		t.Fatalf("unchanged wiring drifted to the other daemon:\n%s", cfg)
	}

	writePackRepo(t, repoDir, mock, "two")
	if _, err := mgrs.Add(ctx, imp, packops.AddOptions{Repo: repoDir}); err != nil {
		t.Fatal(err)
	}
	staleOut, staleErr := runGridctl(ctx, t, bin, home, "pack", "status", "pack-stack", "--format", "json")
	if staleErr == nil {
		t.Fatalf("stale status exited 0:\n%s", staleOut)
	}
	if !strings.Contains(staleOut, "stale") {
		t.Fatalf("stale status =\n%s", staleOut)
	}
	oldCheckout := locked.Stack.CheckoutDir
	stdout, err = runGridctl(ctx, t, bin, home, "pack", "apply", "pack-stack", "--port", strconv.Itoa(port), "--plain")
	if err != nil {
		t.Fatalf("replace apply: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "replaced") || strings.Contains(stdout, "Stopping running stack") {
		t.Fatalf("replace stdout =\n%s", stdout)
	}
	replaced, err := mgrs.LoadLockedPack("pack-stack")
	if err != nil {
		t.Fatal(err)
	}
	st, err = state.Load("pack-stack")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.StackFile, replaced.Stack.CheckoutDir) || st.StackFile == filepath.Join(oldCheckout, "stack.yaml") {
		t.Fatalf("replaced StackFile = %s, new checkout = %s", st.StackFile, replaced.Stack.CheckoutDir)
	}
	if _, err := os.Stat(oldCheckout); !os.IsNotExist(err) {
		t.Fatalf("old checkout survived: %v", err)
	}

	stdout, err = runGridctl(ctx, t, bin, home, "pack", "remove", "pack-stack")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, stdout)
	}
	if _, err := state.Load("pack-stack"); !os.IsNotExist(err) {
		t.Fatalf("daemon state survived remove: %v", err)
	}
	if _, err := os.Stat(packops.PackCheckoutRoot(home, "pack-stack")); !os.IsNotExist(err) {
		t.Fatalf("checkout survived remove: %v", err)
	}
	if _, err := state.Load("other-stack"); err != nil {
		t.Fatalf("foreign daemon was stopped: %v", err)
	}
}

func packStackEngine(t *testing.T, home string) (*packops.Managers, *skills.Importer) {
	t.Helper()
	registryDir := filepath.Join(home, ".gridctl", "skills")
	store := registry.NewStore(registryDir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	imp := skills.NewImporter(store, registryDir, skills.LockFilePath(), slog.Default())
	sm, err := skillsync.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	am, err := agentsync.NewManager(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	wm, err := wiring.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	cm, err := contexts.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	return &packops.Managers{Skills: sm, Agents: am, Wiring: wm, Contexts: cm, Home: home}, imp
}

func writePackRepo(t *testing.T, repoDir, mock, marker string) {
	t.Helper()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		repo, err = git.PlainOpen(repoDir)
		if err != nil {
			t.Fatal(err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"gridctl-pack.yaml": `apiVersion: gridctl.dev/v1
kind: Pack
name: pack-stack
wiring: true
stack: stack.yaml
`,
		"stack.yaml":               localStackYAML("pack-stack", mock),
		"notes/" + marker + ".txt": marker,
	}
	for path, content := range files {
		full := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wt.Commit("pack "+marker, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	}); err != nil {
		t.Fatal(err)
	}
}

func localStackYAML(name, command string) string {
	return "version: \"1\"\nname: " + name + "\nmcp-servers:\n  - name: echo\n    command: [\"" + command + "\"]\n"
}

func runGridctl(ctx context.Context, t *testing.T, bin, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "GRIDCTL_HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil && stderr.Len() > 0 {
		out += "\n" + stderr.String()
	}
	return out, err
}
