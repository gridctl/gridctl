package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestBuildRestartPlan(t *testing.T) {
	st := state.DaemonState{StackName: "netops", StackFile: "/home/u/netops.yaml", Port: 8181}
	entry := &project.Entry{
		Source: "netops",
		Client: "systemd",
		Path:   "/home/u/.config/systemd/user/gridctl-netops.service",
		Extra: map[string]any{
			"service_name": "netops",
			"stack_file":   "/home/u/netops.yaml",
			"label":        "dev.gridctl.netops",
			"binary":       "/usr/local/bin/gridctl",
			"port":         8181,
			"base_port":    9000,
		},
	}

	t.Run("linux service", func(t *testing.T) {
		plan := buildRestartPlan(st, entry, "", false, "linux")
		if plan.Kind != "service" || plan.Line != "systemctl --user restart gridctl-netops.service" {
			t.Fatalf("plan = %+v", plan)
		}
		if plan.Spec == nil || plan.Spec.Name != "netops" || plan.Spec.StackFile != "/home/u/netops.yaml" {
			t.Fatalf("spec = %+v", plan.Spec)
		}
	})

	t.Run("darwin service", func(t *testing.T) {
		plan := buildRestartPlan(st, entry, "", false, "darwin")
		want := "gridctl upgrade --restart (launchd label dev.gridctl.netops)"
		if plan.Line != want {
			t.Fatalf("line = %q", plan.Line)
		}
	})

	t.Run("plain", func(t *testing.T) {
		plan := buildRestartPlan(st, nil, "/home/u/override.yaml", false, "linux")
		want := "gridctl destroy netops && gridctl apply /home/u/netops.yaml --port 8181 --override /home/u/override.yaml"
		if plan.Kind != "plain" || plan.Line != want {
			t.Fatalf("plan = %+v", plan)
		}
		assertNote(t, plan, "not reproduced: "+plainNotReproduced)
		if len(plan.Steps) != 2 || plan.Steps[1][0] != "apply" {
			t.Fatalf("steps = %#v", plan.Steps)
		}
	})

	t.Run("stackless", func(t *testing.T) {
		plan := buildRestartPlan(state.DaemonState{StackName: "gridctl", Port: 8180}, nil, "", false, "linux")
		want := "gridctl destroy gridctl && gridctl serve --port 8180"
		if plan.Kind != "stackless" || plan.Line != want {
			t.Fatalf("plan = %+v", plan)
		}
		assertNote(t, plan, "not reproduced: "+stacklessNotReproduced)
	})

	t.Run("stack file disagreement", func(t *testing.T) {
		bad := *entry
		bad.Extra = map[string]any{
			"service_name": "netops",
			"stack_file":   "/other/netops.yaml",
			"label":        "dev.gridctl.netops",
		}
		plan := buildRestartPlan(st, &bad, "", false, "linux")
		if plan.Kind != "plain" || plan.Spec != nil {
			t.Fatalf("plan = %+v", plan)
		}
		assertNote(t, plan, "does not match daemon state /home/u/netops.yaml")
		if !strings.Contains(plan.Line, "gridctl apply /home/u/netops.yaml") {
			t.Fatalf("line = %q", plan.Line)
		}
	})

	t.Run("unreachable status", func(t *testing.T) {
		plan := buildRestartPlan(st, nil, "/should-not-appear", true, "linux")
		if strings.Contains(plan.Line, "--override") {
			t.Fatalf("line included override: %q", plan.Line)
		}
		assertNote(t, plan, "override could not be read")
	})
}

func TestExecuteRestart_Subprocess(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "args")
	script := filepath.Join(t.TempDir(), "gridctl")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr, yes: true}
	plan := buildRestartPlan(state.DaemonState{
		StackName: "lab", StackFile: "/tmp/lab.yaml", Port: 8181,
	}, nil, "", true, "linux")
	// The not-reproduced list is printed before the restart runs.
	writeDaemonPlans(&stdout, []restartPlan{plan}, true)
	if err := u.executeRestart(t.Context(), script, []restartPlan{plan}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "not reproduced:") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(logged)
	if !strings.Contains(text, "destroy lab") || !strings.Contains(text, "apply /tmp/lab.yaml --port 8181") {
		t.Fatalf("args =\n%s", text)
	}
	if strings.Contains(text, "--override") {
		t.Fatalf("unread override was passed:\n%s", text)
	}
}

func TestExecuteRestart_StopsAtFirstFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "gridctl")
	body := "#!/bin/sh\nif [ \"$1\" = \"destroy\" ] && [ \"$2\" = \"bad\" ]; then exit 1; fi\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr}
	plans := []restartPlan{
		{Name: "ok", Kind: "plain", Steps: [][]string{{"destroy", "ok"}}},
		{Name: "bad", Kind: "plain", Steps: [][]string{{"destroy", "bad"}}},
		{Name: "later", Kind: "plain", Steps: [][]string{{"destroy", "later"}}},
	}
	err := u.executeRestart(t.Context(), script, plans)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(stdout.String(), "Restarted ok") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Not restarted: bad") || !strings.Contains(stderr.String(), "Not restarted: later (not attempted)") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestExecuteRestart_ServiceUsesSupervisor(t *testing.T) {
	prev := restartSupervisor
	var got service.Spec
	restartSupervisor = func() service.Supervisor {
		return &recordingSupervisor{restart: func(spec service.Spec) { got = spec }}
	}
	t.Cleanup(func() { restartSupervisor = prev })

	var stdout, stderr strings.Builder
	u := &upgrader{stdout: &stdout, stderr: &stderr}
	plan := buildRestartPlan(state.DaemonState{
		StackName: "netops", StackFile: "/home/u/netops.yaml", Port: 8181,
	}, &project.Entry{
		Source: "netops",
		Extra:  map[string]any{"service_name": "netops", "stack_file": "/home/u/netops.yaml", "label": "dev.gridctl.netops"},
	}, "", false, "linux")
	if err := u.executeRestart(context.Background(), "/bin/true", []restartPlan{plan}); err != nil {
		t.Fatal(err)
	}
	if got.Name != "netops" {
		t.Fatalf("spec = %+v", got)
	}
}

type recordingSupervisor struct {
	service.Supervisor
	restart func(service.Spec)
}

func (r *recordingSupervisor) Restart(_ context.Context, spec service.Spec) error {
	r.restart(spec)
	return nil
}

func assertNote(t *testing.T, plan restartPlan, want string) {
	t.Helper()
	for _, note := range plan.Notes {
		if strings.Contains(note, want) {
			return
		}
	}
	t.Fatalf("notes = %v, missing %q", plan.Notes, want)
}

func TestDaemonVersionWarning(t *testing.T) {
	got := daemonVersionWarning("netops", "v1.0.0", "v1.1.0", "systemctl --user restart gridctl-netops.service")
	want := "warning: netops runs gridctl v1.0.0; this CLI is v1.1.0. Restart it: systemctl --user restart gridctl-netops.service"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestRestartContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	var stderr strings.Builder
	u := &upgrader{stdout: &strings.Builder{}, stderr: &stderr}
	err := u.executeRestart(ctx, "/bin/true", []restartPlan{{Name: "lab", Steps: [][]string{{"version"}}}})
	if err == nil {
		t.Fatal("expected cancel")
	}
}
