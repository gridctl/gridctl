package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestClassifySkillPinStack(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())

	class, st, err := classifySkillPinStack("stopped")
	if err != nil {
		t.Fatal(err)
	}
	if class != skillPinStackStopped || st != nil {
		t.Fatalf("missing state = %v %+v, want stopped", class, st)
	}

	if err := state.Save(&state.DaemonState{StackName: "running", PID: os.Getpid(), Port: 9, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	class, st, err = classifySkillPinStack("running")
	if err != nil {
		t.Fatal(err)
	}
	if class != skillPinStackRunning || st == nil || st.Port != 9 {
		t.Fatalf("running state = %v %+v", class, st)
	}

	path, err := state.StatePath("garbage")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	class, st, err = classifySkillPinStack("garbage")
	if err == nil || class != skillPinStackUnreadable || st != nil {
		t.Fatalf("garbage = %v %+v err=%v", class, st, err)
	}
}

func TestResetSkillPinsForRemoval_ClassifiesAndInjectsAPI(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	writeCLIPin(t, "running", "alpha")
	stoppedPath := writeCLIPin(t, "stopped", "alpha")
	garbagePath := writeCLIPin(t, "garbage", "alpha")
	if err := state.Save(&state.DaemonState{StackName: "running", PID: os.Getpid(), Port: 9, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	statePath, err := state.StatePath("garbage")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	garbageBefore, err := os.ReadFile(garbagePath)
	if err != nil {
		t.Fatal(err)
	}
	runningPath, err := state.SkillPinsPath("running")
	if err != nil {
		t.Fatal(err)
	}
	runningBefore, err := os.ReadFile(runningPath)
	if err != nil {
		t.Fatal(err)
	}

	var called []string
	reset, err := resetSkillPinsForRemoval(context.Background(), "alpha", func(st *state.DaemonState, skill string) (bool, error) {
		called = append(called, st.StackName+":"+skill)
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(called) != 1 || called[0] != "running:alpha" {
		t.Fatalf("api calls = %v", called)
	}
	if len(reset) != 2 || reset[0] != "running" || reset[1] != "stopped" {
		t.Fatalf("reset = %v, want [running stopped]", reset)
	}
	runningAfter, err := os.ReadFile(runningPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(runningBefore) != string(runningAfter) {
		t.Fatal("running stack pin file was rewritten; the API leg must own it")
	}
	fresh := skillpins.New("stopped")
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := fresh.Get("alpha"); ok {
		t.Fatal("stopped stack still holds alpha")
	}
	if _, err := os.Stat(stoppedPath); err != nil {
		t.Fatal(err)
	}
	garbageAfter, err := os.ReadFile(garbagePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(garbageBefore) != string(garbageAfter) {
		t.Fatal("unreadable stack pin file was rewritten")
	}
	if !strings.Contains(logs.String(), "garbage") || !strings.Contains(logs.String(), "corrupt") {
		t.Fatalf("log missing unreadable warning:\n%s", logs.String())
	}

	// A refused API call warns with the manual remedy and does not count the stack.
	logs.Reset()
	refused, err := resetSkillPinsForRemoval(context.Background(), "alpha", func(*state.DaemonState, string) (bool, error) {
		return false, errors.New("refused")
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stack := range refused {
		if stack == "running" {
			t.Fatalf("refused API stack counted: %v", refused)
		}
	}
	if !strings.Contains(logs.String(), "gridctl skill pins reset alpha --stack running") {
		t.Fatalf("log missing remedy:\n%s", logs.String())
	}
}

func writeCLIPin(t *testing.T, stack, skill string) string {
	t.Helper()
	path, err := state.SkillPinsPath(stack)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pf := &skillpins.PinFile{
		Version:   "1",
		Stack:     stack,
		CreatedAt: when,
		Skills: map[string]*skillpins.SkillPin{
			skill: {
				SkillHash:      "abc",
				Status:         skillpins.StatusPinned,
				PinnedAt:       when,
				LastVerifiedAt: when,
			},
		},
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
