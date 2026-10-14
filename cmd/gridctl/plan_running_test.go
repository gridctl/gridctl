package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestRunPlan_SamePathUsesDaemonPlan(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	path := writePlanStack(t, "name: plan-live\nmcp-servers:\n  - name: local-only\n    image: alpine\n    port: 3000\n")

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/api/stack/plan" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hasChanges":true,"items":[{"action":"add","kind":"mcp-server","name":"deepwiki"}],"summary":"1 to add"}`))
	}))
	t.Cleanup(srv.Close)
	port := mustPort(t, srv.URL)

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "plan-live",
		StackFile: abs,
		PID:       os.Getpid(),
		Port:      port,
	}); err != nil {
		t.Fatal(err)
	}
	withPlanJSON(t)

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), path) })
	if runErr != nil {
		t.Fatalf("runPlan: %v\n%s", runErr, out)
	}
	if hits != 1 {
		t.Fatalf("daemon requests = %d, want 1", hits)
	}
	diff := decodePlan(t, out)
	if !diff.HasChanges || len(diff.Items) != 1 || diff.Items[0].Action != config.DiffAdd || diff.Items[0].Name != "deepwiki" {
		t.Fatalf("diff = %+v, want one add named deepwiki", diff)
	}
}

func TestRunPlan_RunningDaemonUnreachableIsError(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	path := writePlanStack(t, "name: plan-down\nmcp-servers:\n  - name: a\n    image: alpine\n    port: 3000\n")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "plan-down",
		StackFile: abs,
		PID:       os.Getpid(),
		Port:      port,
	}); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), path) })
	if runErr == nil {
		t.Fatalf("expected error, stdout %q", out)
	}
	msg := runErr.Error()
	if !strings.Contains(msg, strconv.Itoa(port)) {
		t.Fatalf("error %q does not name port %d", msg, port)
	}
	if !strings.Contains(msg, "did not return its plan") {
		t.Fatalf("error %q does not state that the daemon did not return its plan", msg)
	}
	if strings.Contains(out, "No changes") {
		t.Fatalf("printed empty plan:\n%s", out)
	}
}

func TestRunPlan_DaemonNon200IsError(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	path := writePlanStack(t, "name: plan-500\nmcp-servers:\n  - name: a\n    image: alpine\n    port: 3000\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Failed to load stack file: missing variable(s): SECRET_KEY"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	port := mustPort(t, srv.URL)
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "plan-500",
		StackFile: abs,
		PID:       os.Getpid(),
		Port:      port,
	}); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), path) })
	if runErr == nil {
		t.Fatalf("expected error, stdout %q", out)
	}
	msg := runErr.Error()
	if !strings.Contains(msg, strconv.Itoa(port)) || !strings.Contains(msg, "HTTP 500") {
		t.Fatalf("error %q does not name port %d and HTTP 500", msg, port)
	}
	if !strings.Contains(msg, "missing variable(s): SECRET_KEY") {
		t.Fatalf("error %q dropped the value-free daemon message", msg)
	}
	if strings.Contains(out, "No changes") {
		t.Fatalf("fell back to an empty plan:\n%s", out)
	}
}

func TestRunPlan_StaleStateComparesAgainstEmpty(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	path := writePlanStack(t, "name: plan-stale\nmcp-servers:\n  - name: a\n    image: alpine\n    port: 3000\n")
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if cmd.Process == nil {
		t.Fatal("missing exited process")
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "plan should not call a stale daemon", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "plan-stale",
		StackFile: abs,
		PID:       cmd.Process.Pid,
		Port:      mustPort(t, srv.URL),
	}); err != nil {
		t.Fatal(err)
	}
	withPlanJSON(t)

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), path) })
	if runErr != nil {
		t.Fatalf("runPlan: %v\n%s", runErr, out)
	}
	if hits != 0 {
		t.Fatalf("stale state made %d HTTP calls", hits)
	}
	assertEmptyStackPlan(t, decodePlan(t, out), "a")
}

func TestRunPlan_MissingStateComparesAgainstEmpty(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	path := writePlanStack(t, "name: plan-absent\nmcp-servers:\n  - name: a\n    image: alpine\n    port: 3000\n")
	withPlanJSON(t)

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), path) })
	if runErr != nil {
		t.Fatalf("runPlan: %v\n%s", runErr, out)
	}
	assertEmptyStackPlan(t, decodePlan(t, out), "a")
}

func TestRunPlan_DifferentPathUsesRecordedFile(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	recorded := writePlanStack(t, "name: plan-copy\nmcp-servers:\n  - name: running\n    image: alpine\n    port: 3000\n")
	proposed := writePlanStack(t, "name: plan-copy\nmcp-servers:\n  - name: running\n    image: alpine\n    port: 3000\n  - name: extra\n    image: alpine\n    port: 3001\n")

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"hasChanges":true,"items":[{"action":"add","kind":"mcp-server","name":"from-daemon"}],"summary":"1 to add"}`))
	}))
	t.Cleanup(srv.Close)

	abs, err := filepath.Abs(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(&state.DaemonState{
		StackName: "plan-copy",
		StackFile: abs,
		PID:       os.Getpid(),
		Port:      mustPort(t, srv.URL),
	}); err != nil {
		t.Fatal(err)
	}
	withPlanJSON(t)

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), proposed) })
	if runErr != nil {
		t.Fatalf("runPlan: %v\n%s", runErr, out)
	}
	if hits != 0 {
		t.Fatalf("different-path plan called the daemon %d times", hits)
	}
	diff := decodePlan(t, out)
	var adds int
	for _, item := range diff.Items {
		if item.Kind == "network" {
			t.Fatalf("unexpected network item: %+v", item)
		}
		if item.Action == config.DiffAdd && item.Kind == "mcp-server" && item.Name == "extra" {
			adds++
		}
		if item.Name == "from-daemon" {
			t.Fatal("used the daemon plan for a different file")
		}
	}
	if !diff.HasChanges || adds != 1 {
		t.Fatalf("diff = %+v, want one add named extra", diff)
	}
}

func TestSamePlanPath_SymlinkAndEmpty(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(target, []byte("name: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if !samePlanPath(link, target) {
		t.Fatal("symlink and target should match")
	}
	other := filepath.Join(dir, "other.yaml")
	if err := os.WriteFile(other, []byte("name: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if samePlanPath(other, target) {
		t.Fatal("different files matched")
	}
	if samePlanPath("", target) || samePlanPath(target, "") {
		t.Fatal("empty path matched a stack file")
	}
}

func writePlanStack(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func withPlanJSON(t *testing.T) {
	t.Helper()
	origFormat, origRuntime := planFormat, runtimeFlag
	planFormat, runtimeFlag = "json", "unavailable-test-runtime"
	t.Cleanup(func() {
		planFormat, runtimeFlag = origFormat, origRuntime
	})
}

func decodePlan(t *testing.T, out string) config.PlanDiff {
	t.Helper()
	var diff config.PlanDiff
	if err := json.Unmarshal([]byte(out), &diff); err != nil {
		t.Fatalf("stdout is not a plan JSON document: %v\n%s", err, out)
	}
	return diff
}

func assertEmptyStackPlan(t *testing.T, diff config.PlanDiff, server string) {
	t.Helper()
	if !diff.HasChanges {
		t.Fatal("expected changes against an empty stack")
	}
	var adds, networks int
	for _, item := range diff.Items {
		if item.Action == config.DiffAdd && item.Kind == "mcp-server" && item.Name == server {
			adds++
		}
		if item.Kind == "network" {
			networks++
		}
	}
	if adds != 1 || networks != 1 {
		t.Fatalf("diff = %+v, want one mcp-server add and one network change", diff)
	}
}
