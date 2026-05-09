package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setTempHomeRun isolates HOME (and therefore the registry/runs dirs)
// to a temp directory for the duration of the test. Each subtest gets
// a fresh registry and JSONL ledger.
func setTempHomeRun(t *testing.T) string {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	return dir
}

// writeTSSkill drops a SKILL.md + skill.ts pair under the temp HOME's
// registry root so loadRegistry() picks them up.
func writeTSSkill(t *testing.T, home, name, body string) {
	t.Helper()
	dir := filepath.Join(home, ".gridctl", "registry", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skillMD := "---\nname: " + name + "\ndescription: test fixture skill\nstate: active\n---\n\nFixture.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.ts"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill.ts: %v", err)
	}
}

func TestResolveRunInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		flag    string
		stdin   string
		setup   func(t *testing.T) string // returns flag override (e.g. file path)
		want    map[string]any
		wantErr bool
	}{
		{
			name: "empty flag yields empty map",
			flag: "",
			want: map[string]any{},
		},
		{
			name: "inline JSON",
			flag: `{"name":"world"}`,
			want: map[string]any{"name": "world"},
		},
		{
			name:  "stdin via -",
			flag:  "-",
			stdin: `{"hello":"there"}`,
			want:  map[string]any{"hello": "there"},
		},
		{
			name: "file via @prefix",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				p := filepath.Join(dir, "in.json")
				if err := os.WriteFile(p, []byte(`{"k":1}`), 0o644); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
				return "@" + p
			},
			want: map[string]any{"k": float64(1)},
		},
		{
			name:    "malformed JSON errors",
			flag:    "{not json",
			wantErr: true,
		},
		{
			name:    "missing file errors",
			flag:    "@/no/such/file/run-test",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flag := tc.flag
			if tc.setup != nil {
				flag = tc.setup(t)
			}
			got, raw, err := resolveRunInput(flag, strings.NewReader(tc.stdin))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (decoded=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("decoded = %s, want %s", gotJSON, wantJSON)
			}
			// raw bytes must be valid JSON object
			var probe any
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Errorf("raw is not valid JSON: %v (raw=%s)", err, raw)
			}
		})
	}
}

func TestRunRun_TSEndToEnd(t *testing.T) {
	home := setTempHomeRun(t)
	t.Cleanup(resetRunFlagsForTest)

	body := `export default async function (input: { name: string }) {
		return { greeting: "hello " + input.name };
	}`
	writeTSSkill(t, home, "hello-ts", body)

	runInput = `{"name":"test"}`
	runFormat = "json"
	runQuiet = true

	if err := runRun(context.Background(), "hello-ts"); err != nil {
		t.Fatalf("runRun: %v", err)
	}

	// The JSONL ledger must exist and contain a run_completed event.
	runsDir := filepath.Join(home, ".gridctl", "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatalf("reading runs dir %s: %v", runsDir, err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one run file")
	}
	data, err := os.ReadFile(filepath.Join(runsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("reading run file: %v", err)
	}
	if !strings.Contains(string(data), `"run_completed"`) {
		t.Errorf("expected run_completed event in ledger, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"status":"ok"`) {
		t.Errorf("expected ok status in ledger, got:\n%s", data)
	}
}

func TestRunRun_GoSkillReturnsDeferredError(t *testing.T) {
	home := setTempHomeRun(t)
	t.Cleanup(resetRunFlagsForTest)

	dir := filepath.Join(home, ".gridctl", "registry", "skills", "go-only")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: go-only\ndescription: test\nstate: active\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write skill.go: %v", err)
	}

	err := runRun(context.Background(), "go-only")
	if err == nil {
		t.Fatal("expected error for Go-handler skill, got nil")
	}
	if !strings.Contains(err.Error(), "Phase H") {
		t.Errorf("expected 'Phase H' in error, got: %v", err)
	}
}

func TestRunRun_MarkdownOnlyReturnsError(t *testing.T) {
	home := setTempHomeRun(t)
	t.Cleanup(resetRunFlagsForTest)

	dir := filepath.Join(home, ".gridctl", "registry", "skills", "markdown-only")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: markdown-only\ndescription: test\nstate: active\n---\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	err := runRun(context.Background(), "markdown-only")
	if err == nil {
		t.Fatal("expected error for markdown-only skill, got nil")
	}
	if !strings.Contains(err.Error(), "markdown-only") {
		t.Errorf("expected 'markdown-only' in error, got: %v", err)
	}
}

func TestRunRun_MissingSkillReturnsError(t *testing.T) {
	setTempHomeRun(t)
	t.Cleanup(resetRunFlagsForTest)

	err := runRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
}
