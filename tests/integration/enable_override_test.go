//go:build integration && !windows

package integration

import (
	"context"
	"os"
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

// TestPackStack_OverrideEnable starts a pinned pack stack with a server
// disabled in the checkout, enables it through the override, and checks
// that enable and reload do not modify the checkout.
func TestPackStack_OverrideEnable(t *testing.T) {
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
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mock := buildRaceBinary(t, ctx, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), filepath.Join(dir, "mock"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)

	repoDir := filepath.Join(dir, "pack")
	writeDisabledPackRepo(t, repoDir, mock)
	port := freePort(t)
	mgrs, imp := packStackEngine(t, home)
	if _, err := mgrs.Add(ctx, imp, packops.AddOptions{Repo: repoDir}); err != nil {
		t.Fatal(err)
	}
	locked, err := mgrs.LoadLockedPack("pack-enable")
	if err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(locked.Stack.CheckoutDir, "stack.yaml")
	before, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "pack-enable")
	})

	out, err := runGridctl(ctx, t, bin, home, "pack", "apply", "pack-enable", "--port", strconv.Itoa(port), "--plain")
	if err != nil {
		logPath := filepath.Join(home, ".gridctl", "logs", "pack-enable.log")
		logBody, _ := os.ReadFile(logPath)
		t.Fatalf("pack apply: %v\n%s\nlog:\n%s", err, out, logBody)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	if containsTool(enableToolNames(t, base), "echo__") {
		t.Fatal("disabled pack server was registered")
	}

	enableOut, err := runGridctl(ctx, t, bin, home, "enable", pinned, "echo", "--format", "json")
	if err != nil {
		t.Fatalf("enable: %v\n%s", err, enableOut)
	}
	if !strings.Contains(enableOut, `"target": "override"`) || !strings.Contains(enableOut, packops.OverridePath(home, "pack-enable")) {
		t.Fatalf("enable json = %s", enableOut)
	}
	after, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("enable modified the pinned checkout")
	}
	if !containsTool(enableToolNames(t, base), "echo__") {
		t.Fatal("enabled pack server did not register")
	}

	statusOut, err := runGridctl(ctx, t, bin, home, "pack", "status", "pack-enable", "--format", "json")
	if err != nil {
		t.Fatalf("pack status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, `"state": "in-sync"`) || !strings.Contains(statusOut, "override:") {
		t.Fatalf("pack status = %s", statusOut)
	}
	planOut, err := runGridctl(ctx, t, bin, home, "plan", pinned)
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, planOut)
	}
	if !strings.Contains(planOut, "override:") {
		t.Fatalf("plan = %s", planOut)
	}

	override := packops.OverridePath(home, "pack-enable")
	if err := os.WriteFile(override, []byte("mcp-servers:\n  echo:\n    enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloadOut, err := runGridctl(ctx, t, bin, home, "reload", "pack-enable")
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, reloadOut)
	}
	if containsTool(enableToolNames(t, base), "echo__") {
		t.Fatal("reload did not apply the override edit")
	}
	after, err = os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("reload modified the pinned checkout")
	}
	st, err := state.Load("pack-enable")
	if err != nil {
		t.Fatal(err)
	}
	if st.StackFile != pinned && !strings.HasPrefix(st.StackFile, locked.Stack.CheckoutDir) {
		t.Fatalf("daemon left the checkout: %s", st.StackFile)
	}
}

func writeDisabledPackRepo(t *testing.T, repoDir, mock string) {
	t.Helper()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"gridctl-pack.yaml": "apiVersion: gridctl.dev/v1\nkind: Pack\nname: pack-enable\nwiring: true\nstack: stack.yaml\n",
		"stack.yaml":        "version: \"1\"\nname: pack-enable\nmcp-servers:\n  - name: echo\n    command: [\"" + mock + "\"]\n    enabled: false\n",
	}
	for path, content := range files {
		full := filepath.Join(repoDir, path)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wt.Commit("pack", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	}); err != nil {
		t.Fatal(err)
	}
}
