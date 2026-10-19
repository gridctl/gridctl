//go:build integration && !windows

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestPackStack_Composition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gridctl")
	buildRace(ctx, t, filepath.Join(root, "cmd/gridctl"), bin, true)
	mock := filepath.Join(dir, "mock-stdio-server")
	buildRace(ctx, t, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), mock, false)

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)

	baseRepo := filepath.Join(dir, "base")
	writePackRepo(t, baseRepo, mock, "base")
	port := freePort(t)
	mgrs, imp := packStackEngine(t, home)
	if _, err := mgrs.Add(ctx, imp, packops.AddOptions{Repo: baseRepo}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = runGridctl(cleanupCtx, t, bin, home, "destroy", "pack-stack")
	})
	if out, err := runGridctl(ctx, t, bin, home, "pack", "apply", "pack-stack", "--port", strconv.Itoa(port), "--plain"); err != nil {
		t.Fatalf("apply base: %v\n%s", err, out)
	}
	started, err := state.Load("pack-stack")
	if err != nil {
		t.Fatal(err)
	}
	if started.Port != port {
		t.Fatalf("base port = %d, want %d", started.Port, port)
	}

	bad := filepath.Join(dir, "bad-fragment")
	writeBoosterRepo(t, bad, "bad-booster", "servers.yaml", "version: \"1\"\ngateway:\n  port: 1\nmcp-servers:\n  - name: extra\n    command: [\""+mock+"\"]\n")
	out, err := runGridctl(ctx, t, bin, home, "pack", "add", bad)
	if err == nil || !strings.Contains(out, "not allowed in a stack fragment") {
		t.Fatalf("gateway fragment err=%v\n%s", err, out)
	}

	collide := filepath.Join(dir, "collide")
	writeBoosterRepo(t, collide, "collide-booster", "servers.yaml", "version: \"1\"\nmcp-servers:\n  - name: echo\n    command: [\""+mock+"\"]\n")
	out, err = runGridctl(ctx, t, bin, home, "pack", "add", collide)
	if err == nil || !strings.Contains(out, "pack 'pack-stack'") || !strings.Contains(out, "pack 'collide-booster'") {
		t.Fatalf("collision err=%v\n%s", err, out)
	}

	booster := filepath.Join(dir, "booster")
	writeBoosterRepo(t, booster, "booster-pack", "servers.yaml", "version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\""+mock+"\"]\n")
	if out, err := runGridctl(ctx, t, bin, home, "pack", "add", booster); err != nil {
		t.Fatalf("add booster: %v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "apply", "booster-pack", "--plain")
	if err != nil {
		t.Fatalf("apply booster: %v\n%s", err, out)
	}
	if !strings.Contains(out, "replaced") {
		t.Fatalf("first attach stdout =\n%s", out)
	}
	running, err := state.Load("pack-stack")
	if err != nil {
		t.Fatal(err)
	}
	if running.Port != port {
		t.Fatalf("composed port = %d, want %d", running.Port, port)
	}
	if !strings.Contains(running.StackFile, filepath.Join("pack-stack", "composed", "stack.yaml")) {
		t.Fatalf("StackFile = %s", running.StackFile)
	}
	waitForTools(t, port, "echo__echo", "extra__echo")
	composed := filepath.Join(home, ".gridctl", "packs", "pack-stack", "composed", "stack.yaml")
	out, err = runGridctl(ctx, t, bin, home, "plan", composed, "--format", "json")
	if err != nil {
		t.Fatalf("plan json err=%v\n%s", err, out)
	}
	assertPlanLayers(t, out)
	out, err = runGridctl(ctx, t, bin, home, "plan", composed)
	if err != nil || !strings.Contains(out, "Layers:") {
		t.Fatalf("plan text err=%v\n%s", err, out)
	}
	if out, err = runGridctl(ctx, t, bin, home, "reload", "pack-stack"); err != nil {
		t.Fatalf("reload: %v\n%s", err, out)
	}

	out, err = runGridctl(ctx, t, bin, home, "pack", "apply", "booster-pack", "--plain")
	if err != nil || !strings.Contains(out, "unchanged") {
		t.Fatalf("second apply err=%v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "apply", "pack-stack", "--plain")
	if err != nil || !strings.Contains(out, "unchanged") {
		t.Fatalf("base apply err=%v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "status", "pack-stack", "--plain")
	if err != nil || !strings.Contains(out, "fragment") || !strings.Contains(out, "in-sync") {
		t.Fatalf("status err=%v\n%s", err, out)
	}

	writeBoosterRepo(t, booster, "booster-pack", "servers.yaml", "version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\""+mock+"\"]\n  - name: third\n    command: [\""+mock+"\"]\n")
	if out, err := runGridctl(ctx, t, bin, home, "pack", "add", booster); err != nil {
		t.Fatalf("re-add booster: %v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "apply", "booster-pack", "--plain")
	if err != nil || !strings.Contains(out, "reloaded") {
		t.Fatalf("reload apply err=%v\n%s", err, out)
	}

	out, err = runGridctl(ctx, t, bin, home, "pack", "remove", "pack-stack")
	if err == nil || !strings.Contains(out, "attached boosters (booster-pack)") {
		t.Fatalf("remove base err=%v\n%s", err, out)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "remove", "booster-pack")
	if err != nil {
		t.Fatalf("remove booster: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "packs", "pack-stack", "composed")); !os.IsNotExist(err) {
		t.Fatalf("composed dir still present: %v", err)
	}
	after, err := state.Load("pack-stack")
	if err != nil || !state.IsRunning(after) {
		t.Fatalf("base daemon after remove: %+v err=%v", after, err)
	}
	if strings.Contains(after.StackFile, "composed") {
		t.Fatalf("StackFile still composed: %s", after.StackFile)
	}
	out, err = runGridctl(ctx, t, bin, home, "pack", "status", "pack-stack", "--plain")
	if err != nil || strings.Contains(out, "fragment") {
		t.Fatalf("status after remove err=%v\n%s", err, out)
	}
}

func buildRace(ctx context.Context, t *testing.T, source, bin string, race bool) {
	t.Helper()
	args := []string{"build", "-o", bin, "."}
	if race {
		args = []string{"build", "-race", "-o", bin, "."}
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = source
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", bin, err, out)
	}
}

func writeBoosterRepo(t *testing.T, repoDir, name, file, fragment string) {
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
	manifest := "apiVersion: gridctl.dev/v1\nkind: Pack\nname: " + name + "\nstack:\n  extends: pack-stack\n  file: " + file + "\n"
	files := map[string]string{"gridctl-pack.yaml": manifest, file: fragment}
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
	if _, err := wt.Commit("booster "+name+" "+file, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertPlanLayers(t *testing.T, out string) {
	t.Helper()
	var doc struct {
		Layers []map[string]any `json:"layers"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("plan json: %v\n%s", err, out)
	}
	if len(doc.Layers) < 2 {
		t.Fatalf("layers = %#v", doc.Layers)
	}
	for _, layer := range doc.Layers {
		if len(layer) != 3 {
			t.Fatalf("layer keys = %#v", layer)
		}
		for _, key := range []string{"pack", "commit", "file"} {
			if _, ok := layer[key]; !ok {
				t.Fatalf("layer missing %s: %#v", key, layer)
			}
		}
	}
}

func waitForTools(t *testing.T, port int, want ...string) {
	t.Helper()
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp"
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		names, err := softToolNames(url)
		if err == nil {
			have := map[string]bool{}
			for _, name := range names {
				have[name] = true
			}
			ok := true
			for _, name := range want {
				if !have[name] {
					ok = false
				}
			}
			if ok {
				return
			}
			last = errString("tools " + strings.Join(names, ","))
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("tools/list missing %v: %v", want, last)
}

type errString string

func (e errString) Error() string { return string(e) }
