package hooksync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/project"
)

// antigravityHome is a home with Antigravity initialized and one pinned
// hook whose handler carries an if rule.
func antigravityHome(t *testing.T) (home, registry string, mgr *Manager) {
	t.Helper()
	home, registry = testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	rewriteHook(t, registry, "block", ifHookRaw)
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, registry, NewManagerWithHome(home, registry)
}

func antigravityPath(home string) string {
	return filepath.Join(home, ".gemini", "config", "hooks.json")
}

var antigravityOnly = SyncOptions{Clients: []string{"antigravity"}}

func writeHooksFile(t *testing.T, home, content string) {
	t.Helper()
	path := antigravityPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hooksFileKey decodes one top-level key of the hooks file.
func hooksFileKey(t *testing.T, home, key string) (map[string]any, bool) {
	t.Helper()
	sf, err := readKeyedFile(antigravityPath(home))
	if err != nil || !sf.Parsed {
		t.Fatalf("hooks file = %+v, %v", sf, err)
	}
	v, ok, err := valueAt(sf, keyPointer(key))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return nil, false
	}
	m, _ := v.(map[string]any)
	return m, true
}

// userHooksFile has comments, a sibling hook definition, and a sibling
// non-hook key. Every one of its bytes must survive sync and unsync.
const userHooksFile = `// my hooks
{
  // audit every call
  "audit": {
    "enabled": true,
    "PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "audit.sh"}]}] // inline
  },
  "notes": [1, 2, 3]
}
`

func TestSyncAntigravity_CreatesFileWithOnlyTheKey(t *testing.T) {
	home, _, mgr := antigravityHome(t)
	res := syncOne(t, mgr, antigravityOnly)
	path := antigravityPath(home)
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@antigravity")
	if res.Action != ActionCopied || res.Detail != ifDetail || res.Channel != ChannelMergeKey || res.Target != path || res.ScriptDir != scriptDir {
		t.Fatalf("result = %+v", res)
	}
	if HasFailures([]SyncResult{res}) {
		t.Fatal("a copied row with a dropped if must not count as a failure")
	}
	want := `{
  "gridctl-block": {
    "enabled": true,
    "PreToolUse": [
      {
        "matcher": "run_command",
        "hooks": [
          {
            "type": "command",
            "command": "'` + filepath.Join(scriptDir, "scripts", "run.sh") + `'",
            "timeout": 600
          }
        ]
      }
    ]
  }
}
`
	if got := string(mustRead(t, path)); got != want {
		t.Fatalf("file =\n%s\nwant\n%s", got, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("file mode = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block")); !os.IsNotExist(err) {
		t.Fatalf("an antigravity sync wrote the claude-code script copy: %v", err)
	}

	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(project.KindHook, "antigravity", "block")
	if e == nil {
		t.Fatal("no lock entry")
	}
	if e.Channel != ChannelMergeKey || e.ConfigPath != path || e.ScriptDir != scriptDir || e.TreeHash == "" || !e.CreatedFile {
		t.Fatalf("lock entry = %+v", e)
	}
	value, _ := hooksFileKey(t, home, "gridctl-block")
	if hash, _ := valueHash(value); len(e.Hashes) != 1 || e.Hashes[0] != hash {
		t.Fatalf("hashes = %v, want the key's canonical hash %s", e.Hashes, hash)
	}

	if again := syncOne(t, mgr, antigravityOnly); again.Action != ActionUnchanged || again.Detail != ifDetail {
		t.Fatalf("second sync = %+v", again)
	}
	st := statusOf(t, mgr, "antigravity")
	if st.State != StateInSync || st.Detail != ifDetail || st.Channel != ChannelMergeKey || st.Render != RenderLossy || st.Target != path {
		t.Fatalf("status = %+v", st)
	}
}

func TestSyncAntigravity_EmptyObjectIsRewrittenWhole(t *testing.T) {
	home, _, mgr := antigravityHome(t)
	writeHooksFile(t, home, "{}\n")
	if res := syncOne(t, mgr, antigravityOnly); res.Action != ActionCopied || res.BackupPath == "" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.HasPrefix(string(mustRead(t, antigravityPath(home))), "{\n  \"gridctl-block\": {\n") {
		t.Fatalf("file =\n%s", mustRead(t, antigravityPath(home)))
	}
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if l.Get(project.KindHook, "antigravity", "block").CreatedFile {
		t.Fatal("a file the user created was recorded as created by gridctl")
	}
	un, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}})
	if err != nil || len(un) != 1 || un[0].Action != ActionRemoved {
		t.Fatalf("unsync = %+v, %v", un, err)
	}
	if got := string(mustRead(t, antigravityPath(home))); got != "{}\n" {
		t.Fatalf("emptied file = %q", got)
	}
}

func TestSyncAntigravity_OwnershipTable(t *testing.T) {
	t.Run("foreign key", func(t *testing.T) {
		home, _, mgr := antigravityHome(t)
		foreign := `{
  "gridctl-block": {"enabled": false}
}
`
		writeHooksFile(t, home, foreign)
		res := syncOne(t, mgr, antigravityOnly)
		if res.Action != ActionSkippedForeign || !strings.Contains(res.Detail, `key "gridctl-block"`) || !strings.Contains(res.Detail, "--force") {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, antigravityPath(home))) != foreign {
			t.Fatal("a foreign key was overwritten without --force")
		}
		if ok, _ := mgr.HasProjections(t.Context()); ok {
			t.Fatal("a foreign key was recorded")
		}
		forced := syncOne(t, mgr, SyncOptions{Clients: []string{"antigravity"}, Force: true})
		if forced.Action != ActionUpdated || forced.BackupPath == "" {
			t.Fatalf("forced = %+v", forced)
		}
		if string(mustRead(t, forced.BackupPath)) != foreign {
			t.Fatal("backup does not hold the foreign bytes")
		}
		if value, _ := hooksFileKey(t, home, "gridctl-block"); value["enabled"] != true {
			t.Fatalf("--force did not claim the key: %v", value)
		}
		if st := statusOf(t, mgr, "antigravity"); st.State != StateInSync {
			t.Fatalf("status after claim = %+v", st)
		}
	})

	t.Run("hand edit", func(t *testing.T) {
		home, _, mgr := antigravityHome(t)
		syncOne(t, mgr, antigravityOnly)
		path := antigravityPath(home)
		edited := strings.Replace(string(mustRead(t, path)), `"matcher": "run_command"`, `"matcher": "view_file"`, 1)
		writeHooksFile(t, home, edited)
		if st := statusOf(t, mgr, "antigravity"); st.State != StateDrifted || !strings.Contains(st.Detail, `key "gridctl-block"`) {
			t.Fatalf("status = %+v", st)
		}
		res := syncOne(t, mgr, antigravityOnly)
		if res.Action != ActionSkippedDrift || !strings.Contains(res.Detail, path) {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, path)) != edited {
			t.Fatal("a drifted key was rewritten without --force")
		}
		forced := syncOne(t, mgr, SyncOptions{Clients: []string{"antigravity"}, Force: true})
		if forced.Action != ActionUpdated || forced.BackupPath == "" || string(mustRead(t, forced.BackupPath)) != edited {
			t.Fatalf("forced = %+v", forced)
		}
		if !strings.Contains(string(mustRead(t, path)), `"matcher": "run_command"`) {
			t.Fatal("--force did not rewrite the key")
		}
	})

	t.Run("removed key", func(t *testing.T) {
		home, _, mgr := antigravityHome(t)
		syncOne(t, mgr, antigravityOnly)
		writeHooksFile(t, home, `{"other": 1}`)
		if st := statusOf(t, mgr, "antigravity"); st.State != StateTargetMissing || !strings.HasPrefix(st.Detail, keyRemovedDetail) {
			t.Fatalf("status = %+v", st)
		}
		res := syncOne(t, mgr, antigravityOnly)
		if res.Action != ActionUpdated || !strings.HasPrefix(res.Detail, keyRemovedDetail) {
			t.Fatalf("result = %+v", res)
		}
		if _, ok := hooksFileKey(t, home, "gridctl-block"); !ok {
			t.Fatal("the key was not restored")
		}
		if _, ok := hooksFileKey(t, home, "other"); !ok {
			t.Fatal("a sibling key was lost")
		}
	})

	t.Run("unparsable", func(t *testing.T) {
		home, _, mgr := antigravityHome(t)
		syncOne(t, mgr, antigravityOnly)
		writeHooksFile(t, home, "{ not json")
		for _, force := range []bool{false, true} {
			res := syncOne(t, mgr, SyncOptions{Clients: []string{"antigravity"}, Force: force})
			if res.Action != ActionSkippedUnmanaged || !SettingsUnparsable(res.Detail) {
				t.Fatalf("force=%v result = %+v", force, res)
			}
		}
		if string(mustRead(t, antigravityPath(home))) != "{ not json" {
			t.Fatal("an unparsable file was rewritten")
		}
		if st := statusOf(t, mgr, "antigravity"); st.State != StateDrifted || !SettingsUnparsable(st.Detail) {
			t.Fatalf("status = %+v", st)
		}
		un, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}, Force: true})
		if err != nil || len(un) != 1 || un[0].Action != ActionSkippedUnmanaged {
			t.Fatalf("unsync = %+v, %v", un, err)
		}
		if _, blocked := BlocksRemoval(un); !blocked {
			t.Fatal("an unparsable hooks file must block removal")
		}
	})

	t.Run("root is not an object", func(t *testing.T) {
		home, _, mgr := antigravityHome(t)
		writeHooksFile(t, home, "[]\n")
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"antigravity"}, Force: true})
		if res.Action != ActionSkippedUnmanaged || !strings.Contains(res.Detail, errRootNotObject.Error()) {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, antigravityPath(home))) != "[]\n" {
			t.Fatal("a non-object file was rewritten")
		}
	})
}

// TestAntigravityKey_PreservesSiblings is the designated scenario: sync,
// an approved update, and unsync touch only the gridctl-<name> key. Every
// sibling key and comment keeps its bytes, and unsync restores the file
// the user had.
func TestAntigravityKey_PreservesSiblings(t *testing.T) {
	home, registry, mgr := antigravityHome(t)
	writeHooksFile(t, home, userHooksFile)
	path := antigravityPath(home)
	assertSiblings := func(stage string) {
		t.Helper()
		got := string(mustRead(t, path))
		prefix := strings.TrimSuffix(userHooksFile, "\n}\n")
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("%s: sibling bytes changed:\n%s", stage, got)
		}
		for _, key := range []string{"audit", "notes"} {
			if _, ok := hooksFileKey(t, home, key); !ok {
				t.Fatalf("%s: sibling key %s lost", stage, key)
			}
		}
	}

	if res := syncOne(t, mgr, antigravityOnly); res.Action != ActionCopied || res.BackupPath == "" {
		t.Fatalf("sync = %+v", res)
	}
	assertSiblings("sync")
	if string(mustRead(t, res0Backup(t, path))) != userHooksFile {
		t.Fatal("backup does not hold the user's file")
	}
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if l.Get(project.KindHook, "antigravity", "block").CreatedFile {
		t.Fatal("created_file recorded for an existing file")
	}

	rewriteHook(t, registry, "block", strings.Replace(ifHookRaw, `"matcher":"Bash"`, `"matcher":"Bash|Write"`, 1))
	approveHook(t, home, registry, "block")
	if res := syncOne(t, mgr, antigravityOnly); res.Action != ActionUpdated {
		t.Fatalf("approved update = %+v", res)
	}
	assertSiblings("update")
	if value, _ := hooksFileKey(t, home, "gridctl-block"); !strings.Contains(mustJSON(t, value), `"matcher":"run_command|write_to_file"`) {
		t.Fatalf("updated key = %v", value)
	}

	un, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}})
	if err != nil || len(un) != 1 || un[0].Action != ActionRemoved {
		t.Fatalf("unsync = %+v, %v", un, err)
	}
	if got := string(mustRead(t, path)); got != userHooksFile {
		t.Fatalf("unsync did not restore the user's file:\n%s\nwant\n%s", got, userHooksFile)
	}
}

// res0Backup returns the oldest backup of path written beside it.
func res0Backup(t *testing.T, path string) string {
	t.Helper()
	matches, err := filepath.Glob(path + backupSuffix + "*")
	if err != nil || len(matches) == 0 {
		t.Fatalf("no backup beside %s: %v", path, err)
	}
	return matches[0]
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUnsyncAntigravity(t *testing.T) {
	home, _, mgr := antigravityHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Clients: []string{"claude-code", "antigravity"}}); err != nil {
		t.Fatal(err)
	}
	path := antigravityPath(home)
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@antigravity")
	claudeDir := filepath.Join(home, ".gridctl", "hooks", "block")

	synced := string(mustRead(t, path))
	edited := strings.Replace(synced, `"timeout": 600`, `"timeout": 5`, 1)
	writeHooksFile(t, home, edited)
	results, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedDrift || !strings.Contains(results[0].Detail, path) {
		t.Fatalf("drift unsync = %+v", results)
	}
	if blocked, ok := BlocksRemoval(results); !ok || blocked.Client != "antigravity" {
		t.Fatalf("a drifted key must block removal, naming antigravity: %+v %v", blocked, ok)
	}
	if string(mustRead(t, path)) != edited {
		t.Fatal("a drifted key was removed without --force")
	}

	results, err = mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionRemoved || results[0].BackupPath == "" {
		t.Fatalf("forced unsync = %+v", results)
	}
	if got := string(mustRead(t, path)); got != "{}\n" {
		t.Fatalf("a gridctl-created file was not emptied to {}: %q", got)
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("script copy remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "project-backups", "hook", "block@antigravity")); err != nil {
		t.Fatalf("script copy was not backed up under its per-client name: %v", err)
	}
	if _, err := os.Stat(claudeDir); err != nil {
		t.Fatalf("an antigravity unsync removed the claude-code copy: %v", err)
	}
	if st := statusOf(t, mgr, "claude-code"); st.State != StateInSync {
		t.Fatalf("claude-code status after antigravity unsync = %+v", st)
	}

	syncOne(t, mgr, antigravityOnly)
	writeHooksFile(t, home, `{"other": true}`)
	results, err = mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionAlreadyGone {
		t.Fatalf("missing-key unsync = %+v", results)
	}
	if string(mustRead(t, path)) != `{"other": true}` {
		t.Fatal("an already-gone unsync rewrote the file")
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("script copy remains after already-gone: %v", err)
	}
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if l.Get(project.KindHook, "antigravity", "block") != nil {
		t.Fatal("record was not purged on already-gone")
	}
	if l.Get(project.KindHook, "claude-code", "block") == nil {
		t.Fatal("the claude-code record was purged")
	}
}

func TestUnsyncAntigravity_DryRunWritesNothing(t *testing.T) {
	home, _, mgr := antigravityHome(t)
	syncOne(t, mgr, antigravityOnly)
	before := string(mustRead(t, antigravityPath(home)))
	results, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"antigravity"}, DryRun: true})
	if err != nil || len(results) != 1 || results[0].Action != ActionWouldRemove {
		t.Fatalf("dry-run unsync = %+v, %v", results, err)
	}
	if string(mustRead(t, antigravityPath(home))) != before {
		t.Fatal("dry-run unsync changed the file")
	}
	if st := statusOf(t, mgr, "antigravity"); st.State != StateInSync {
		t.Fatalf("status after dry run = %+v", st)
	}
}

func TestSyncAntigravity_TwoHooksShareTheFile(t *testing.T) {
	home, registry, mgr := antigravityHome(t)
	installHook(t, registry, "audit", "#!/bin/sh\nexit 0\n")
	pinHook(t, home, registry, "audit")
	results, err := mgr.Sync(t.Context(), nil, antigravityOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Action != ActionCopied || results[1].Action != ActionCopied {
		t.Fatalf("results = %+v", results)
	}
	for _, key := range []string{"gridctl-audit", "gridctl-block"} {
		if _, ok := hooksFileKey(t, home, key); !ok {
			t.Fatalf("key %s missing", key)
		}
	}
	un, err := mgr.Unsync(t.Context(), []string{"audit"}, UnsyncOptions{Clients: []string{"antigravity"}})
	if err != nil || len(un) != 1 || un[0].Action != ActionRemoved {
		t.Fatalf("unsync = %+v, %v", un, err)
	}
	if _, ok := hooksFileKey(t, home, "gridctl-audit"); ok {
		t.Fatal("unsynced key remains")
	}
	if st := statusOf(t, mgr, "antigravity"); st.State != StateInSync {
		t.Fatalf("the other hook's status = %+v", st)
	}
}

func TestSyncAntigravity_UnquotableIsAnError(t *testing.T) {
	home, registry, mgr := antigravityHome(t)
	rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh","args":["it's"]}]}]}}`)
	approveHook(t, home, registry, "block")
	res := syncOne(t, mgr, antigravityOnly)
	if res.Action != ActionError || !strings.Contains(res.Error, `"it's"`) {
		t.Fatalf("result = %+v", res)
	}
	if !HasFailures([]SyncResult{res}) {
		t.Fatal("an unquotable value must fail the sync")
	}
	if _, err := os.Stat(antigravityPath(home)); !os.IsNotExist(err) {
		t.Fatalf("hooks file written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block@antigravity")); !os.IsNotExist(err) {
		t.Fatalf("script copy written: %v", err)
	}
	if ok, _ := mgr.HasProjections(t.Context()); ok {
		t.Fatal("an unquotable hook was recorded")
	}

	// The same rule applies to the script directory under the manager home.
	quoted := filepath.Join(t.TempDir(), "o'brien")
	if err := os.MkdirAll(filepath.Join(quoted, ".gemini", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(quoted, ".gridctl", "registry")
	installHook(t, reg, "block", "#!/bin/sh\necho no\n")
	pinHook(t, quoted, reg, "block")
	res = syncOne(t, NewManagerWithHome(quoted, reg), antigravityOnly)
	if res.Action != ActionError || !strings.Contains(res.Error, "o'brien") {
		t.Fatalf("quoted home result = %+v", res)
	}
	if _, err := os.Stat(antigravityPath(quoted)); !os.IsNotExist(err) {
		t.Fatalf("hooks file written under a quoted home: %v", err)
	}
}

func TestSyncAntigravity_Unexpressible(t *testing.T) {
	t.Run("nothing expressible", func(t *testing.T) {
		home, registry, mgr := antigravityHome(t)
		rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}],"PreToolUse":[{"matcher":"Task","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
		approveHook(t, home, registry, "block")
		res := syncOne(t, mgr, antigravityOnly)
		if res.Action != ActionSkippedUnexpressible || res.Detail != `matcher token "Task" has no antigravity tool; event "Stop" has no antigravity mapping` {
			t.Fatalf("result = %+v", res)
		}
		if !HasFailures([]SyncResult{res}) {
			t.Fatal("skipped-unexpressible must count as a failure on a direct sync")
		}
		if _, err := os.Stat(antigravityPath(home)); !os.IsNotExist(err) {
			t.Fatalf("hooks file written: %v", err)
		}
		if ok, _ := mgr.HasProjections(t.Context()); ok {
			t.Fatal("an unexpressible hook was recorded")
		}
	})

	t.Run("mixed", func(t *testing.T) {
		home, registry, mgr := antigravityHome(t)
		rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
		approveHook(t, home, registry, "block")
		res := syncOne(t, mgr, antigravityOnly)
		if res.Action != ActionCopied || res.Detail != "dropped groups: Stop" {
			t.Fatalf("result = %+v", res)
		}
		st := statusOf(t, mgr, "antigravity")
		if st.State != StateInSync || st.Detail != "dropped groups: Stop" {
			t.Fatalf("status = %+v", st)
		}
		if gaps := Unenforced("antigravity", []string{"block"}, []ProjectionStatus{st}); len(gaps) != 0 {
			t.Fatalf("requires.hooks reports the partly expressible hook unenforced: %+v", gaps)
		}
		if _, ok := hooksFileKey(t, home, "gridctl-block"); !ok {
			t.Fatal("key missing")
		}
	})
}

func TestSyncAntigravity_PinDriftLeavesKey(t *testing.T) {
	home, registry, mgr := antigravityHome(t)
	syncOne(t, mgr, antigravityOnly)
	before := string(mustRead(t, antigravityPath(home)))
	rewriteHook(t, registry, "block", strings.Replace(ifHookRaw, `"matcher":"Bash"`, `"matcher":"Read"`, 1))
	if res := syncOne(t, mgr, SyncOptions{Clients: []string{"antigravity"}, Force: true}); res.Action != ActionSkippedPinDrift {
		t.Fatalf("unapproved edit = %+v", res)
	}
	if string(mustRead(t, antigravityPath(home))) != before {
		t.Fatal("an unapproved registry edit changed the key")
	}
	if st := statusOf(t, mgr, "antigravity"); st.State != StateStale || !strings.Contains(st.Detail, "pin drift") {
		t.Fatalf("status = %+v", st)
	}
}

func TestAntigravity_ChannelComesFromTheTargetTable(t *testing.T) {
	home, _, mgr := antigravityHome(t)
	syncOne(t, mgr, antigravityOnly)
	store := project.NewStore(home)
	err := store.Mutate(t.Context(), false, func(pl *project.Lock) error {
		var entries []*project.Entry
		for _, e := range pl.Entries(project.KindHook) {
			c := *e
			c.Channel = ChannelDedicatedFile
			c.Path = c.ConfigPath
			entries = append(entries, &c)
		}
		if err := pl.ReplaceKind(project.KindHook, entries); err != nil {
			return err
		}
		return pl.Save()
	})
	if err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t, mgr, "antigravity"); st.State != StateInSync || st.Channel != ChannelMergeKey {
		t.Fatalf("status = %+v", st)
	}
	if res := syncOne(t, mgr, antigravityOnly); res.Action != ActionUnchanged {
		t.Fatalf("sync = %+v", res)
	}
	l, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if e := l.Get(project.KindHook, "antigravity", "block"); e.Channel != ChannelMergeKey || e.Path != compositePath(antigravityPath(home), "block") {
		t.Fatalf("entry after save = %+v", e)
	}
}

func TestAntigravity_DetectionAndTarget(t *testing.T) {
	for _, dir := range []string{".gemini/config", ".gemini/antigravity", ".gemini/antigravity-cli", ".gemini/antigravity-ide"} {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		target, _ := FindTarget("antigravity")
		if !target.available(home) {
			t.Errorf("%s does not detect antigravity", dir)
		}
	}
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	if target, _ := FindTarget("antigravity"); target.available(bare) {
		t.Error("a bare ~/.gemini detected antigravity")
	}
	gaps := Unenforced("antigravity", []string{"block"}, nil)
	if len(gaps) != 1 || gaps[0].Reason != "not projected" {
		t.Fatalf("gaps = %+v", gaps)
	}
	if _, ok := FindUnsupported("antigravity"); ok {
		t.Fatal("antigravity is still listed as unsupported")
	}
}
