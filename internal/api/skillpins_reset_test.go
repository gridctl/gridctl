package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/state"
)

func writeResetPin(t *testing.T, stack, skill string) (string, []byte) {
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
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func warnLines(log string) []string {
	var out []string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "level=WARN") {
			out = append(out, line)
		}
	}
	return out
}

func TestSkillPinResetter_SkipsOtherRunningStack(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	logs := captureSlog(t)
	_, _ = writeResetPin(t, "daily", "alpha")
	otherPath, otherBytes := writeResetPin(t, "other", "alpha")
	_, _ = writeResetPin(t, "stopped", "alpha")
	if err := state.Save(&state.DaemonState{StackName: "other", PID: os.Getpid(), Port: 1, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	ps := skillpins.New("daily")
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetSkillPinStore(ps)
	srv.SetStackName("daily")

	reset, err := srv.skillPinResetter()(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(reset) != 2 || reset[0] != "daily" || reset[1] != "stopped" {
		t.Fatalf("reset = %v, want [daily stopped]", reset)
	}
	if _, ok := ps.Get("alpha"); ok {
		t.Fatal("server store still holds alpha")
	}
	after, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(otherBytes) {
		t.Fatal("running stack pin file was rewritten")
	}
	warns := warnLines(logs.String())
	if len(warns) != 1 {
		t.Fatalf("warnings = %d, log:\n%s", len(warns), logs.String())
	}
	if !strings.Contains(warns[0], "other") || !strings.Contains(warns[0], "gridctl skill pins reset alpha --stack other") {
		t.Fatalf("warning = %s", warns[0])
	}
	if strings.Contains(warns[0], "daily") {
		t.Fatalf("warning named the server stack: %s", warns[0])
	}
}

func TestSkillPinResetter_UnreadableStateSkipped(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	logs := captureSlog(t)
	path, before := writeResetPin(t, "messy", "alpha")
	statePath, err := state.StatePath("messy")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetStackName("daily")
	reset, err := srv.skillPinResetter()(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(reset) != 0 {
		t.Fatalf("reset = %v, want none", reset)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unreadable stack pin file was rewritten")
	}
	warns := warnLines(logs.String())
	if len(warns) != 1 || !strings.Contains(warns[0], "messy") || !strings.Contains(warns[0], "corrupt") {
		t.Fatalf("warnings = %v", warns)
	}
}
