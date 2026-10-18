//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEnableDisable_Lifecycle(t *testing.T) {
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
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)

	stackPath := filepath.Join(dir, "stack.yaml")
	writeEnableStack(t, stackPath, mock, true)
	port := freePort(t)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "enable-demo")
	})

	out, err := runGridctl(ctx, t, bin, home, "apply", stackPath, "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	waitReady(t, base)
	names := enableToolNames(t, base)
	if !containsTool(names, "live__") || containsTool(names, "parked__") {
		t.Fatalf("tools/list = %v", names)
	}
	statusOut, err := runGridctl(ctx, t, bin, home, "status", "--json")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, `"name": "parked"`) || !strings.Contains(statusOut, `"enabled": false`) {
		t.Fatalf("status json missing disabled server:\n%s", statusOut)
	}
	if strings.Count(statusOut, `"enabled": false`) != 1 {
		t.Fatalf("expected one enabled:false, status=\n%s", statusOut)
	}

	overridePath := filepath.Join(dir, "stack.override.yaml")
	if err := os.WriteFile(overridePath, []byte("mcp-servers:\n  live:\n    enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	planOut, planErr := runGridctl(ctx, t, bin, home, "plan", stackPath)
	if planErr != nil {
		t.Fatalf("plan: %v\n%s", planErr, planOut)
	}
	if !strings.Contains(planOut, "(disable)") || !strings.Contains(planOut, "enabled: true → false") || !strings.Contains(planOut, "(override)") {
		t.Fatalf("plan = %s", planOut)
	}
	if err := os.Remove(overridePath); err != nil {
		t.Fatal(err)
	}

	_, stream := openMCPStream(t, base+"/mcp")
	defer stream.Close()
	enableOut, err := runGridctl(ctx, t, bin, home, "enable", "enable-demo", "parked", "--format", "json")
	if err != nil {
		t.Fatalf("enable: %v\n%s", err, enableOut)
	}
	if !strings.Contains(enableOut, `"changed": true`) || !strings.Contains(enableOut, `"target": "stack"`) {
		t.Fatalf("enable json = %s", enableOut)
	}
	if !waitForListChanged(t, stream) {
		t.Fatal("handshake session did not receive tools/list_changed")
	}
	names = enableToolNames(t, base)
	if !containsTool(names, "parked__") {
		t.Fatalf("parked tools missing after enable: %v", names)
	}
	again, err := runGridctl(ctx, t, bin, home, "enable", "enable-demo", "parked", "--format", "json")
	if err != nil {
		t.Fatalf("second enable: %v\n%s", err, again)
	}
	if !strings.Contains(again, `"changed": false`) {
		t.Fatalf("second enable = %s", again)
	}

	// AC 7: an explicit enabled: true is not a change. A true-to-false flip
	// is reported as disabled. A later env edit on a server that stays
	// disabled does no runtime work, and plan then reports no changes.
	insertAfterName(t, stackPath, "live", "    enabled: true\n")
	noChange := reloadJSON(ctx, t, bin, home, "enable-demo")
	if !strings.Contains(noChange, "no changes detected") || strings.Contains(noChange, `"disabled"`) {
		t.Fatalf("enabled: true reload = %s", noChange)
	}
	insertAfterName(t, stackPath, "parked", "    enabled: false\n")
	flipped := reloadJSON(ctx, t, bin, home, "enable-demo")
	if !strings.Contains(flipped, `"disabled"`) || !strings.Contains(flipped, "mcp-server:parked") || strings.Contains(flipped, "no changes detected") {
		t.Fatalf("disable reload = %s", flipped)
	}
	replaceOnce(t, stackPath, "name: parked\n    enabled: false\n", "name: parked\n")
	reenabled := reloadJSON(ctx, t, bin, home, "enable-demo")
	if !strings.Contains(reenabled, `"enabled"`) || !strings.Contains(reenabled, "mcp-server:parked") {
		t.Fatalf("enable reload = %s", reenabled)
	}

	pinsBefore := pinsSnapshot(t, home)
	if pinsBefore == "" {
		t.Fatal("expected a pin file before disable")
	}
	disableOut, err := runGridctl(ctx, t, bin, home, "disable", stackPath, "parked", "--format", "json")
	if err != nil {
		t.Fatalf("disable: %v\n%s", err, disableOut)
	}
	if !strings.Contains(disableOut, `"changed": true`) || !strings.Contains(disableOut, "mcp-server:parked") {
		t.Fatalf("disable json = %s", disableOut)
	}
	if pinsSnapshot(t, home) != pinsBefore {
		t.Fatal("disable changed pin files")
	}
	insertAfterName(t, stackPath, "parked", "    env:\n      A: \"1\"\n")
	dormant := reloadJSON(ctx, t, bin, home, "enable-demo")
	if strings.Contains(dormant, `"added"`) || strings.Contains(dormant, `"removed"`) || strings.Contains(dormant, `"modified"`) || strings.Contains(dormant, `"disabled"`) || strings.Contains(dormant, `"enabled"`) {
		t.Fatalf("dormant reload did runtime work: %s", dormant)
	}
	planOut, err = runGridctl(ctx, t, bin, home, "plan", stackPath)
	if err != nil {
		t.Fatalf("plan after dormant edit: %v\n%s", err, planOut)
	}
	if !strings.Contains(planOut, "No changes") {
		t.Fatalf("plan after dormant edit = %s", planOut)
	}
	groupNames := toolNamesAt(t, base+"/groups/bundle/mcp")
	if containsTool(groupNames, "parked__") {
		t.Fatalf("group still serves disabled tools: %v", groupNames)
	}
	if !containsTool(groupNames, "live__") {
		t.Fatalf("group dropped the remaining server: %v", groupNames)
	}

	// File edit that disables the running local process, then restart, so the
	// daemon child path (getRunningContainers) does not re-register it.
	data, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(data)
	if strings.Contains(edited, "name: live\n    enabled: true\n") {
		edited = strings.Replace(edited, "name: live\n    enabled: true\n", "name: live\n    enabled: false\n", 1)
	} else {
		edited = strings.Replace(edited, "name: live\n", "name: live\n    enabled: false\n", 1)
	}
	if !strings.Contains(edited, "name: live\n    enabled: false\n") {
		t.Fatalf("could not disable live in %s", data)
	}
	if err := os.WriteFile(stackPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err = runGridctl(ctx, t, bin, home, "destroy", "enable-demo"); err != nil {
		t.Fatalf("destroy: %v\n%s", err, out)
	}
	if out, err = runGridctl(ctx, t, bin, home, "apply", stackPath, "--port", strconv.Itoa(port)); err != nil {
		t.Fatalf("re-apply: %v\n%s", err, out)
	}
	waitReady(t, base)
	names = enableToolNames(t, base)
	if containsTool(names, "live__") || containsTool(names, "parked__") {
		t.Fatalf("restart registered a disabled server: %v", names)
	}

	planOut, err = runGridctl(ctx, t, bin, home, "plan", stackPath)
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, planOut)
	}
	if !strings.Contains(planOut, "No changes") {
		t.Fatalf("plan after restart = %s", planOut)
	}
}

func TestEnableDisable_RelativeOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mock := buildRaceBinary(t, ctx, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), filepath.Join(dir, "mock"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	stackDir := filepath.Join(dir, "st")
	other := filepath.Join(dir, "other")
	if err := os.MkdirAll(stackDir, 0o755); err != nil || os.MkdirAll(other, 0o755) != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(stackDir, "stack.yaml")
	overridePath := filepath.Join(stackDir, "local.yaml")
	body := fmt.Sprintf("version: \"1\"\nname: rel-override\nmcp-servers:\n  - name: a\n    command: [%q]\n    enabled: false\n", mock)
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("mcp-servers: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = runGridctl(cctx, t, bin, home, "destroy", "rel-override")
	})
	out, err := runGridctlIn(ctx, t, bin, home, stackDir, "apply", "stack.yaml", "--override", "local.yaml", "--port", strconv.Itoa(port))
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	waitReady(t, "http://127.0.0.1:"+strconv.Itoa(port))
	statusOut, err := runGridctlIn(ctx, t, bin, home, other, "status", "--json")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, overridePath) || strings.Contains(statusOut, `"override": "local.yaml"`) {
		t.Fatalf("status did not report an absolute override:\n%s", statusOut)
	}
	enableOut, err := runGridctlIn(ctx, t, bin, home, other, "enable", "rel-override", "a", "--format", "json")
	if err != nil {
		t.Fatalf("enable: %v\n%s", err, enableOut)
	}
	if !strings.Contains(enableOut, overridePath) || strings.Contains(enableOut, `"file": "local.yaml"`) {
		t.Fatalf("enable wrote a relative path: %s", enableOut)
	}
	if _, err := os.Stat(filepath.Join(other, "local.yaml")); !os.IsNotExist(err) {
		t.Fatalf("enable created a stray override in the caller cwd: %v", err)
	}
	written, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "enabled: true") {
		t.Fatalf("daemon override was not updated: %s", written)
	}
	if !containsTool(enableToolNames(t, "http://127.0.0.1:"+strconv.Itoa(port)), "a__") {
		t.Fatal("enable from another directory did not register the server")
	}
}

func TestEnableDisable_PlanLabel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	stackPath := filepath.Join(dir, "stack.yaml")
	body := "version: \"1\"\nname: plan-demo\nnetwork:\n  name: plan-net\nmcp-servers:\n  - name: parked\n    command: [\"/bin/true\"]\n    enabled: false\n"
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runGridctl(ctx, t, bin, home, "plan", stackPath, "--format", "json")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"action": "add"`) {
		t.Fatalf("stopped plan = %s", out)
	}
	enabled := strings.Replace(body, "    enabled: false\n", "", 1)
	if err := os.WriteFile(stackPath, []byte(enabled), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stack.override.yaml"), []byte("mcp-servers:\n  parked:\n    enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := runGridctl(ctx, t, bin, home, "plan", stackPath)
	if err != nil {
		t.Fatalf("plan text: %v\n%s", err, text)
	}
	if !strings.Contains(text, "override:") {
		t.Fatalf("stopped plan did not name the override:\n%s", text)
	}
}

func buildRaceBinary(t *testing.T, ctx context.Context, source, bin string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, ".")
	cmd.Dir = source
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", source, err, out)
	}
	return bin
}

func writeEnableStack(t *testing.T, path, mock string, parked bool) {
	t.Helper()
	enabled := ""
	if parked {
		enabled = "    enabled: false\n"
	}
	body := fmt.Sprintf(`version: "1"
name: enable-demo
network:
  name: enable-demo-net
mcp-servers:
  - name: live
    command: ["%s"]
  - name: parked
    command: ["%s"]
%s
groups:
  bundle:
    servers:
      - live
      - parked
`, mock, mock, enabled)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("gateway at %s did not become ready", base)
}

func enableToolNames(t *testing.T, base string) []string {
	t.Helper()
	return toolNamesAt(t, base+"/mcp")
}

func toolNamesAt(t *testing.T, url string) []string {
	t.Helper()
	session := initializeMCP(t, url)
	body := postMCP(t, url, session, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var parsed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("tools/list %s: %v body=%s", url, err, body)
	}
	names := make([]string, 0, len(parsed.Result.Tools))
	for _, tool := range parsed.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func containsTool(names []string, prefix string) bool {
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func initializeMCP(t *testing.T, url string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"enable-test","version":"1"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize %s: %d", url, resp.StatusCode)
	}
	session := resp.Header.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatalf("initialize %s returned no session", url)
	}
	return session
}

func postMCP(t *testing.T, url, session, body string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post %s: %d %s", url, resp.StatusCode, data)
	}
	return data
}

func openMCPStream(t *testing.T, url string) (string, io.ReadCloser) {
	t.Helper()
	session := initializeMCP(t, url)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Mcp-Session-Id", session)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream status %d", resp.StatusCode)
	}
	return session, resp.Body
}

func waitForListChanged(t *testing.T, stream io.Reader) bool {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stream)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "notifications/tools/list_changed") {
				done <- true
				return
			}
		}
		done <- false
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(20 * time.Second):
		return false
	}
}

func reloadJSON(ctx context.Context, t *testing.T, bin, home, stack string) string {
	t.Helper()
	out, err := runGridctl(ctx, t, bin, home, "reload", stack, "--format", "json")
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, out)
	}
	return out
}

func insertAfterName(t *testing.T, path, name, line string) {
	t.Helper()
	replaceOnce(t, path, "name: "+name+"\n", "name: "+name+"\n"+line)
}

func replaceOnce(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("missing %q in %s", old, data)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGridctlIn(ctx context.Context, t *testing.T, bin, home, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
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

func pinsSnapshot(t *testing.T, home string) string {
	t.Helper()
	dir := filepath.Join(home, ".gridctl", "pins")
	var buf bytes.Buffer
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fmt.Fprintf(&buf, "%s %x\n", path, data)
		return nil
	})
	return buf.String()
}
