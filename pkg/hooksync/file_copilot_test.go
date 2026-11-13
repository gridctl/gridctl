package hooksync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/project"
)

// copilotHome is a home with Copilot CLI initialized and one pinned hook
// whose handler carries an if rule.
func copilotHome(t *testing.T) (home, registry string, mgr *Manager) {
	t.Helper()
	home, registry = testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	rewriteHook(t, registry, "block", ifHookRaw)
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, registry, NewManagerWithHome(home, registry)
}

func copilotPath(home, name string) string {
	return filepath.Join(home, ".copilot", "hooks", "gridctl-"+name+".json")
}

var copilotOnly = SyncOptions{Clients: []string{"copilot"}}

func TestSyncCopilot_WritesFileAndRecord(t *testing.T) {
	home, _, mgr := copilotHome(t)
	res := syncOne(t, mgr, copilotOnly)
	path := copilotPath(home, "block")
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@copilot")
	if res.Action != ActionCopied || res.Detail != ifDetail || res.Channel != ChannelDedicatedFile || res.Target != path || res.ScriptDir != scriptDir {
		t.Fatalf("result = %+v", res)
	}
	if HasFailures([]SyncResult{res}) {
		t.Fatal("a copied row with a dropped if must not count as a failure")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("file mode = %v", info.Mode().Perm())
	}
	if dir, err := os.Stat(filepath.Dir(path)); err != nil || dir.Mode().Perm() != 0o755 {
		t.Fatalf("hooks directory = %v, %v", dir, err)
	}
	data := mustRead(t, path)
	want := `{
  "version": 1,
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "exec": "` + filepath.Join(scriptDir, "scripts", "run.sh") + `",
        "args": [],
        "matcher": "Bash"
      }
    ]
  }
}
`
	if string(data) != want {
		t.Fatalf("file =\n%s\nwant\n%s", data, want)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block")); !os.IsNotExist(err) {
		t.Fatalf("a copilot sync wrote the claude-code script copy: %v", err)
	}

	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(project.KindHook, "copilot", "block")
	if e == nil {
		t.Fatal("no lock entry")
	}
	if e.Channel != ChannelDedicatedFile || e.Path != path || e.ConfigPath != path || e.ScriptDir != scriptDir || e.TreeHash == "" {
		t.Fatalf("lock entry = %+v", e)
	}
	if len(e.Hashes) != 1 || e.Hashes[0] != fileHash(data) {
		t.Fatalf("hashes = %v, want the file hash", e.Hashes)
	}

	if again := syncOne(t, mgr, copilotOnly); again.Action != ActionUnchanged || again.Detail != ifDetail {
		t.Fatalf("second sync = %+v", again)
	}
	st := statusOf(t, mgr, "copilot")
	if st.State != StateInSync || st.Detail != ifDetail || st.Channel != ChannelDedicatedFile || st.Render != RenderLossy || st.Target != path {
		t.Fatalf("status = %+v", st)
	}
}

func TestSyncCopilot_OwnershipTable(t *testing.T) {
	t.Run("foreign file", func(t *testing.T) {
		home, _, mgr := copilotHome(t)
		path := copilotPath(home, "block")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"version":1,"hooks":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		res := syncOne(t, mgr, copilotOnly)
		if res.Action != ActionSkippedForeign || !strings.Contains(res.Detail, path) || !strings.Contains(res.Detail, "--force") {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, path)) != `{"version":1,"hooks":{}}` {
			t.Fatal("a foreign file was overwritten without --force")
		}
		if ok, _ := mgr.HasProjections(t.Context()); ok {
			t.Fatal("a foreign file was recorded")
		}
		forced := syncOne(t, mgr, SyncOptions{Clients: []string{"copilot"}, Force: true})
		if forced.Action != ActionUpdated || forced.BackupPath == "" {
			t.Fatalf("forced = %+v", forced)
		}
		if string(mustRead(t, forced.BackupPath)) != `{"version":1,"hooks":{}}` {
			t.Fatal("backup does not hold the foreign bytes")
		}
		if !strings.HasPrefix(forced.BackupPath, filepath.Join(home, ".gridctl", "project-backups", "hook-file", "copilot", "block")+string(filepath.Separator)) {
			t.Fatalf("backup path = %s", forced.BackupPath)
		}
		if st := statusOf(t, mgr, "copilot"); st.State != StateInSync {
			t.Fatalf("status after claim = %+v", st)
		}
	})

	t.Run("hand edit", func(t *testing.T) {
		home, _, mgr := copilotHome(t)
		syncOne(t, mgr, copilotOnly)
		path := copilotPath(home, "block")
		edited := []byte(strings.Replace(string(mustRead(t, path)), `"matcher": "Bash"`, `"matcher": "Read"`, 1))
		if err := os.WriteFile(path, edited, 0o644); err != nil {
			t.Fatal(err)
		}
		if st := statusOf(t, mgr, "copilot"); st.State != StateDrifted || !strings.Contains(st.Detail, path) {
			t.Fatalf("status = %+v", st)
		}
		res := syncOne(t, mgr, copilotOnly)
		if res.Action != ActionSkippedDrift || !strings.Contains(res.Detail, path) {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, path)) != string(edited) {
			t.Fatal("a drifted file was rewritten without --force")
		}
		forced := syncOne(t, mgr, SyncOptions{Clients: []string{"copilot"}, Force: true})
		if forced.Action != ActionUpdated || forced.BackupPath == "" {
			t.Fatalf("forced = %+v", forced)
		}
		if string(mustRead(t, forced.BackupPath)) != string(edited) {
			t.Fatal("backup does not hold the hand edit")
		}
		if !strings.Contains(string(mustRead(t, path)), `"matcher": "Bash"`) {
			t.Fatal("--force did not rewrite the file")
		}
		if got := backupFiles(t, home, "copilot", "block"); len(got) != 1 || !strings.HasSuffix(got[0], "-gridctl-block.json") {
			t.Fatalf("backups = %v", got)
		}
	})

	t.Run("removed file", func(t *testing.T) {
		home, _, mgr := copilotHome(t)
		syncOne(t, mgr, copilotOnly)
		path := copilotPath(home, "block")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if st := statusOf(t, mgr, "copilot"); st.State != StateTargetMissing {
			t.Fatalf("status = %+v", st)
		}
		res := syncOne(t, mgr, copilotOnly)
		if res.Action != ActionUpdated || !strings.HasPrefix(res.Detail, fileRemovedDetail) {
			t.Fatalf("result = %+v", res)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUnsyncCopilot(t *testing.T) {
	home, _, mgr := copilotHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Clients: []string{"claude-code", "copilot"}}); err != nil {
		t.Fatal(err)
	}
	path := copilotPath(home, "block")
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@copilot")
	claudeDir := filepath.Join(home, ".gridctl", "hooks", "block")

	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"copilot"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedDrift || !strings.Contains(results[0].Detail, path) {
		t.Fatalf("drift unsync = %+v", results)
	}
	if blocked, ok := BlocksRemoval(results); !ok || blocked.Client != "copilot" {
		t.Fatalf("a drifted file must block removal, naming copilot: %+v %v", blocked, ok)
	}

	results, err = mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"copilot"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionRemoved || results[0].BackupPath == "" {
		t.Fatalf("forced unsync = %+v", results)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file remains: %v", err)
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("script copy remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "project-backups", "hook", "block@copilot")); err != nil {
		t.Fatalf("script copy was not backed up under its per-client name: %v", err)
	}
	if _, err := os.Stat(claudeDir); err != nil {
		t.Fatalf("a copilot unsync removed the claude-code copy: %v", err)
	}
	if st := statusOf(t, mgr, "claude-code"); st.State != StateInSync {
		t.Fatalf("claude-code status after copilot unsync = %+v", st)
	}

	syncOne(t, mgr, copilotOnly)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	results, err = mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"copilot"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionAlreadyGone {
		t.Fatalf("missing-file unsync = %+v", results)
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("script copy remains after already-gone: %v", err)
	}
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if l.Get(project.KindHook, "copilot", "block") != nil {
		t.Fatal("record was not purged on already-gone")
	}
	if l.Get(project.KindHook, "claude-code", "block") == nil {
		t.Fatal("the claude-code record was purged")
	}
}

func TestSyncCopilot_Unexpressible(t *testing.T) {
	t.Run("nothing expressible", func(t *testing.T) {
		home, registry, mgr := copilotHome(t)
		rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
		approveHook(t, home, registry, "block")
		res := syncOne(t, mgr, copilotOnly)
		if res.Action != ActionSkippedUnexpressible || res.Detail != `event "Stop" has no copilot mapping` {
			t.Fatalf("result = %+v", res)
		}
		if !HasFailures([]SyncResult{res}) {
			t.Fatal("skipped-unexpressible must count as a failure on a direct sync")
		}
		if _, err := os.Stat(copilotPath(home, "block")); !os.IsNotExist(err) {
			t.Fatalf("file written: %v", err)
		}
		if ok, _ := mgr.HasProjections(t.Context()); ok {
			t.Fatal("an unexpressible hook was recorded")
		}
	})

	t.Run("matcher passes through", func(t *testing.T) {
		home, registry, mgr := copilotHome(t)
		rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}],"PreToolUse":[{"matcher":"Task","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
		approveHook(t, home, registry, "block")
		res := syncOne(t, mgr, copilotOnly)
		if res.Action != ActionCopied || res.Detail != "dropped groups: Stop" {
			t.Fatalf("result = %+v", res)
		}
		if st := statusOf(t, mgr, "copilot"); st.State != StateInSync || st.Detail != "dropped groups: Stop" {
			t.Fatalf("status = %+v", st)
		}
		if !strings.Contains(string(mustRead(t, copilotPath(home, "block"))), `"matcher": "Task"`) {
			t.Fatal("a Claude matcher token was not passed through")
		}
	})
}

func TestUnenforced_CopilotIsATarget(t *testing.T) {
	gaps := Unenforced("copilot", []string{"block"}, nil)
	if len(gaps) != 1 || gaps[0].Reason != "not projected" {
		t.Fatalf("gaps = %+v", gaps)
	}
	if _, ok := FindUnsupported("copilot"); ok {
		t.Fatal("copilot is still listed as unsupported")
	}
}

// TestCopilotFile_ExecForm is the designated scenario: every handler in
// the projected file runs the pinned script copy through exec plus args,
// and no handler carries a shell string.
func TestCopilotFile_ExecForm(t *testing.T) {
	home, registry, mgr := copilotHome(t)
	rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"./scripts/run.sh","args":["--check","a b; rm -rf /"],"timeout":5}]}],"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	approveHook(t, home, registry, "block")
	if res := syncOne(t, mgr, copilotOnly); res.Action != ActionCopied {
		t.Fatalf("result = %+v", res)
	}
	var doc struct {
		Version int                         `json:"version"`
		Hooks   map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(mustRead(t, copilotPath(home, "block")), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Hooks) != 2 {
		t.Fatalf("file = %+v", doc)
	}
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@copilot")
	for event, handlers := range doc.Hooks {
		for _, h := range handlers {
			for _, shell := range []string{"command", "bash", "powershell"} {
				if _, ok := h[shell]; ok {
					t.Errorf("%s handler carries a %s string: %v", event, shell, h)
				}
			}
			exec, _ := h["exec"].(string)
			if exec != filepath.Join(scriptDir, "scripts", "run.sh") {
				t.Errorf("%s exec = %q, want the pinned script copy", event, exec)
			}
			if _, ok := h["args"].([]any); !ok {
				t.Errorf("%s handler has no args array: %v", event, h)
			}
		}
	}
	post := doc.Hooks["PostToolUse"][0]
	if args := post["args"].([]any); len(args) != 2 || args[1] != "a b; rm -rf /" {
		t.Fatalf("args were not passed as discrete strings: %v", args)
	}
	if post["timeoutSec"] != float64(5) || post["matcher"] != "Write|Edit" {
		t.Fatalf("post handler = %v", post)
	}
	if _, ok := doc.Hooks["PreToolUse"][0]["timeoutSec"]; ok {
		t.Fatal("timeoutSec written for a handler with no timeout")
	}
	if _, ok := doc.Hooks["PreToolUse"][0]["matcher"]; ok {
		t.Fatal("matcher written for a group with none")
	}
}
