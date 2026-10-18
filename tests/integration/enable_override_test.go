//go:build integration && !windows

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// TestPackStack_OverrideWatch covers the --watch half of creating and
// deleting a pack override when ~/.gridctl/overrides did not exist at start.
func TestPackStack_OverrideWatch(t *testing.T) {
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
	override := packops.OverridePath(home, "pack-enable")
	overrideDir := filepath.Dir(override)
	if _, err := os.Stat(overrideDir); !os.IsNotExist(err) {
		t.Fatalf("overrides directory should be absent at start: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "pack-enable")
	})

	// --override-optional is how a missing pack override is selected. The
	// file and its directory are absent so the daemon must create the
	// directory before the second watcher can see create and delete.
	out, err := runGridctl(ctx, t, bin, home, "apply", pinned, "--watch", "--override", override, "--override-optional", "--port", strconv.Itoa(port))
	if err != nil {
		logBody, _ := os.ReadFile(filepath.Join(home, ".gridctl", "logs", "pack-enable.log"))
		t.Fatalf("apply: %v\n%s\nlog:\n%s", err, out, logBody)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	if _, err := os.Stat(overrideDir); err != nil {
		t.Fatalf("daemon did not create the override directory: %v", err)
	}
	if containsTool(enableToolNames(t, base), "echo__") {
		t.Fatal("disabled pack server was registered")
	}

	if err := os.WriteFile(override, []byte("mcp-servers:\n  echo:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForToolPrefix(t, base, "echo__", true)
	if err := os.Remove(override); err != nil {
		t.Fatal(err)
	}
	waitForToolPrefix(t, base, "echo__", false)
	after, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("watch reload modified the pinned checkout")
	}
}

func waitForToolPrefix(t *testing.T, base, prefix string, want bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		names, err := softToolNames(base + "/mcp")
		if err == nil {
			last = strings.Join(names, ",")
			if containsTool(names, prefix) == want {
				return
			}
		} else {
			last = err.Error()
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatalf("tool %s present=%v did not settle, last=%s", prefix, want, last)
}

func softToolNames(url string) ([]string, error) {
	session, err := softInitialize(url)
	if err != nil {
		return nil, err
	}
	body, err := softPost(url, session, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(parsed.Result.Tools))
	for _, tool := range parsed.Result.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
}

func softInitialize(url string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"enable-test","version":"1"}}}`))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("initialize %s: %d", url, resp.StatusCode)
	}
	session := resp.Header.Get("Mcp-Session-Id")
	if session == "" {
		return "", fmt.Errorf("initialize %s returned no session", url)
	}
	return session, nil
}

func softPost(url, session, body string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("post %s: %d %s", url, resp.StatusCode, data)
	}
	return data, nil
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
