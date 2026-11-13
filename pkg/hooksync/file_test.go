package hooksync

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/project"
)

const (
	ifHookRaw = `{"name":"block","description":"block force push","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh","if":"Bash(git push *)"}]}]}}`
	ifDetail  = `dropped if "Bash(git push *)": the handler runs on every matched call`
)

// openCodeHome is a home with OpenCode initialized and one pinned hook
// whose handler carries an if rule.
func openCodeHome(t *testing.T) (home, registry string, mgr *Manager) {
	t.Helper()
	home, registry = testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	rewriteHook(t, registry, "block", ifHookRaw)
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, registry, NewManagerWithHome(home, registry)
}

func pluginPath(home, name string) string {
	return filepath.Join(home, ".config", "opencode", "plugins", "gridctl-hook-"+name+".ts")
}

func syncOne(t *testing.T, mgr *Manager, opts SyncOptions) SyncResult {
	t.Helper()
	results, err := mgr.Sync(t.Context(), []string{"block"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	return results[0]
}

func statusOf(t *testing.T, mgr *Manager, client string) ProjectionStatus {
	t.Helper()
	statuses, err := mgr.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statuses {
		if s.Hook == "block" && s.Client == client {
			return s
		}
	}
	t.Fatalf("no %s status in %+v", client, statuses)
	return ProjectionStatus{}
}

func backupFiles(t *testing.T, home, client, name string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, ".gridctl", "project-backups", "hook-file", client, name))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestSyncOpenCode_WritesPluginAndRecord(t *testing.T) {
	home, _, mgr := openCodeHome(t)
	res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
	path := pluginPath(home, "block")
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@opencode")
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
		t.Fatalf("plugin mode = %v", info.Mode().Perm())
	}
	data := mustRead(t, path)
	rules := pluginRules(t, data)
	if len(rules) != 1 || rules[0].Handlers[0].TimeoutMs != 600000 || rules[0].Handlers[0].Script != filepath.Join(scriptDir, "scripts", "run.sh") {
		t.Fatalf("rules = %+v", rules)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "scripts", "run.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block")); !os.IsNotExist(err) {
		t.Fatalf("an opencode sync wrote the claude-code script copy: %v", err)
	}

	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(project.KindHook, "opencode", "block")
	if e == nil {
		t.Fatal("no lock entry")
	}
	if e.Channel != ChannelDedicatedFile || e.Path != path || e.ConfigPath != path || e.ScriptDir != scriptDir || e.TreeHash == "" {
		t.Fatalf("lock entry = %+v", e)
	}
	if len(e.Hashes) != 1 || e.Hashes[0] != fileHash(data) {
		t.Fatalf("hashes = %v, want the file hash", e.Hashes)
	}

	again := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
	if again.Action != ActionUnchanged || again.Detail != ifDetail {
		t.Fatalf("second sync = %+v", again)
	}
	st := statusOf(t, mgr, "opencode")
	if st.State != StateInSync || st.Detail != ifDetail || st.Channel != ChannelDedicatedFile || st.Render != RenderLossy || st.Target != path {
		t.Fatalf("status = %+v", st)
	}
}

func TestSyncOpenCode_OwnershipTable(t *testing.T) {
	t.Run("foreign file", func(t *testing.T) {
		home, _, mgr := openCodeHome(t)
		path := pluginPath(home, "block")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("// mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		if res.Action != ActionSkippedForeign || !strings.Contains(res.Detail, path) || !strings.Contains(res.Detail, "--force") {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, path)) != "// mine\n" {
			t.Fatal("a foreign file was overwritten without --force")
		}
		if ok, _ := mgr.HasProjections(t.Context()); ok {
			t.Fatal("a foreign file was recorded")
		}
		forced := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}, Force: true})
		if forced.Action != ActionUpdated || forced.BackupPath == "" {
			t.Fatalf("forced = %+v", forced)
		}
		if string(mustRead(t, forced.BackupPath)) != "// mine\n" {
			t.Fatal("backup does not hold the foreign bytes")
		}
		if !strings.HasPrefix(forced.BackupPath, filepath.Join(home, ".gridctl", "project-backups", "hook-file", "opencode", "block")+string(filepath.Separator)) {
			t.Fatalf("backup path = %s", forced.BackupPath)
		}
		if st := statusOf(t, mgr, "opencode"); st.State != StateInSync {
			t.Fatalf("status after claim = %+v", st)
		}
	})

	t.Run("hand edit", func(t *testing.T) {
		home, _, mgr := openCodeHome(t)
		syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		path := pluginPath(home, "block")
		edited := append(mustRead(t, path), []byte("// hand edit\n")...)
		if err := os.WriteFile(path, edited, 0o644); err != nil {
			t.Fatal(err)
		}
		if st := statusOf(t, mgr, "opencode"); st.State != StateDrifted || !strings.Contains(st.Detail, path) {
			t.Fatalf("status = %+v", st)
		}
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		if res.Action != ActionSkippedDrift || !strings.Contains(res.Detail, path) {
			t.Fatalf("result = %+v", res)
		}
		if string(mustRead(t, path)) != string(edited) {
			t.Fatal("a drifted file was rewritten without --force")
		}
		forced := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}, Force: true})
		if forced.Action != ActionUpdated || forced.BackupPath == "" {
			t.Fatalf("forced = %+v", forced)
		}
		if string(mustRead(t, forced.BackupPath)) != string(edited) {
			t.Fatal("backup does not hold the hand edit")
		}
		if strings.Contains(string(mustRead(t, path)), "hand edit") {
			t.Fatal("--force did not rewrite the plugin")
		}
		if got := backupFiles(t, home, "opencode", "block"); len(got) != 1 {
			t.Fatalf("backups = %v", got)
		}
	})

	t.Run("removed file", func(t *testing.T) {
		home, _, mgr := openCodeHome(t)
		syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		path := pluginPath(home, "block")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if st := statusOf(t, mgr, "opencode"); st.State != StateTargetMissing {
			t.Fatalf("status = %+v", st)
		}
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		if res.Action != ActionUpdated || !strings.HasPrefix(res.Detail, fileRemovedDetail) {
			t.Fatalf("result = %+v", res)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("script copy edit", func(t *testing.T) {
		home, _, mgr := openCodeHome(t)
		syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		script := filepath.Join(home, ".gridctl", "hooks", "block@opencode", "scripts", "run.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\n# edit\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if st := statusOf(t, mgr, "opencode"); st.State != StateDrifted || !strings.Contains(st.Detail, "script copy") {
			t.Fatalf("status = %+v", st)
		}
		if res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}}); res.Action != ActionSkippedDrift {
			t.Fatalf("result = %+v", res)
		}
		if res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}, Force: true}); res.Action != ActionUpdated {
			t.Fatalf("forced = %+v", res)
		}
		if strings.Contains(string(mustRead(t, script)), "# edit") {
			t.Fatal("--force did not restore the script copy")
		}
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		home, _, mgr := openCodeHome(t)
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}, DryRun: true})
		if res.Action != ActionWouldCopy {
			t.Fatalf("result = %+v", res)
		}
		if _, err := os.Stat(pluginPath(home, "block")); !os.IsNotExist(err) {
			t.Fatalf("dry run wrote the plugin: %v", err)
		}
	})
}

func TestUnsyncOpenCode(t *testing.T) {
	home, _, mgr := openCodeHome(t)
	syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
	path := pluginPath(home, "block")
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@opencode")

	if err := os.WriteFile(path, []byte("// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"opencode"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedDrift || !strings.Contains(results[0].Detail, path) {
		t.Fatalf("drift unsync = %+v", results)
	}
	if _, blocked := BlocksRemoval(results); !blocked {
		t.Fatal("a drifted plugin must block removal")
	}
	if string(mustRead(t, path)) != "// edited\n" {
		t.Fatal("a drifted plugin was removed without --force")
	}
	if _, err := os.Stat(scriptDir); err != nil {
		t.Fatal("a refused unsync removed the script copy")
	}

	results, err = mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"opencode"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionRemoved || results[0].BackupPath == "" {
		t.Fatalf("forced unsync = %+v", results)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plugin remains: %v", err)
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("script copy remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "project-backups", "hook", "block@opencode")); err != nil {
		t.Fatalf("script copy was not backed up under its per-client name: %v", err)
	}
	if ok, _ := mgr.HasProjections(t.Context()); ok {
		t.Fatal("record was not purged")
	}

	syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	results, err = mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"opencode"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionAlreadyGone {
		t.Fatalf("missing-file unsync = %+v", results)
	}
	if ok, _ := mgr.HasProjections(t.Context()); ok {
		t.Fatal("record was not purged on already-gone")
	}
	if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
		t.Fatalf("script copy remains after already-gone: %v", err)
	}
}

func TestPerClientScriptCopies(t *testing.T) {
	home, registry, mgr := openCodeHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	claudeDir := filepath.Join(home, ".gridctl", "hooks", "block")
	openDir := filepath.Join(home, ".gridctl", "hooks", "block@opencode")
	for _, r := range results {
		want := map[string]string{"claude-code": claudeDir, "opencode": openDir}[r.Client]
		if r.Action != ActionCopied || r.ScriptDir != want {
			t.Fatalf("row = %+v", r)
		}
	}

	// An approved script change synced to opencode only leaves the
	// claude-code copy and record alone: that row is stale, not drifted.
	claudeBefore := mustRead(t, filepath.Join(claudeDir, "scripts", "run.sh"))
	if err := os.WriteFile(filepath.Join(registry, "hooks", "block", "scripts", "run.sh"), []byte("#!/bin/sh\necho approved\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	approveHook(t, home, registry, "block")
	if res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}}); res.Action != ActionUpdated {
		t.Fatalf("opencode resync = %+v", res)
	}
	if string(mustRead(t, filepath.Join(openDir, "scripts", "run.sh"))) != "#!/bin/sh\necho approved\n" {
		t.Fatal("opencode copy was not refreshed")
	}
	if string(mustRead(t, filepath.Join(claudeDir, "scripts", "run.sh"))) != string(claudeBefore) {
		t.Fatal("an opencode sync changed the bytes claude-code runs")
	}
	claude := statusOf(t, mgr, "claude-code")
	if claude.State != StateStale || !strings.Contains(claude.Detail, "registry scripts differ from the synced copy") {
		t.Fatalf("claude-code status = %+v", claude)
	}
	if st := statusOf(t, mgr, "opencode"); st.State != StateInSync {
		t.Fatalf("opencode status = %+v", st)
	}

	// Each unsync takes only its own copy and record.
	if _, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"opencode"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(openDir); !os.IsNotExist(err) {
		t.Fatalf("opencode copy remains: %v", err)
	}
	if _, err := os.Stat(claudeDir); err != nil {
		t.Fatalf("opencode unsync removed the claude-code copy: %v", err)
	}
	if st := statusOf(t, mgr, "claude-code"); st.State != StateStale {
		t.Fatalf("claude-code status after opencode unsync = %+v", st)
	}

	if res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}}); res.Action != ActionCopied {
		t.Fatalf("opencode re-sync = %+v", res)
	}
	results2, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"claude-code"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 1 || results2[0].Action != ActionRemoved {
		t.Fatalf("claude-code unsync = %+v", results2)
	}
	if _, err := os.Stat(claudeDir); !os.IsNotExist(err) {
		t.Fatalf("claude-code copy remains: %v", err)
	}
	if _, err := os.Stat(openDir); err != nil {
		t.Fatalf("claude-code unsync removed the opencode copy: %v", err)
	}
	if _, err := os.Stat(pluginPath(home, "block")); err != nil {
		t.Fatalf("claude-code unsync removed the plugin: %v", err)
	}
	if st := statusOf(t, mgr, "opencode"); st.State != StateInSync {
		t.Fatalf("opencode status after claude-code unsync = %+v", st)
	}
}

// rewriteAsLegacy stamps every hook entry the way a pre-feature binary
// saves them: channel merge-key and a composite Path.
func rewriteAsLegacy(t *testing.T, home string) {
	t.Helper()
	store := project.NewStore(home)
	err := store.Mutate(t.Context(), false, func(pl *project.Lock) error {
		var entries []*project.Entry
		for _, e := range pl.Entries(project.KindHook) {
			c := *e
			c.Channel = ChannelMergeKey
			c.Path = c.ConfigPath + "#hooks/" + c.Source
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
	raw := string(mustRead(t, store.Path()))
	if !strings.Contains(raw, "channel: merge-key") || !strings.Contains(raw, ".ts#hooks/block") {
		t.Fatalf("lockfile was not rewritten:\n%s", raw)
	}
}

func comparableStatuses(t *testing.T, mgr *Manager) []ProjectionStatus {
	t.Helper()
	statuses, err := mgr.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := range statuses {
		statuses[i].SyncedAt = nil
	}
	return statuses
}

func TestLegacyRewrittenLockfile_BehavesIdentically(t *testing.T) {
	type scenario func(t *testing.T, home, registry string, mgr *Manager) []any
	steps := map[string]scenario{
		"in sync": func(t *testing.T, home, registry string, mgr *Manager) []any {
			res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
			res.BackupPath = ""
			return []any{comparableStatuses(t, mgr), res}
		},
		"hand edit": func(t *testing.T, home, registry string, mgr *Manager) []any {
			if err := os.WriteFile(pluginPath(home, "block"), []byte("// edit\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
			un, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"opencode"}})
			if err != nil {
				t.Fatal(err)
			}
			return []any{comparableStatuses(t, mgr), res, un}
		},
		"unsync": func(t *testing.T, home, registry string, mgr *Manager) []any {
			un, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{Clients: []string{"opencode"}})
			if err != nil {
				t.Fatal(err)
			}
			for i := range un {
				un[i].BackupPath = filepath.Dir(un[i].BackupPath)
			}
			_, pluginErr := os.Stat(pluginPath(home, "block"))
			_, dirErr := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block@opencode"))
			return []any{comparableStatuses(t, mgr), un, os.IsNotExist(pluginErr), os.IsNotExist(dirErr)}
		},
	}
	for name, step := range steps {
		t.Run(name, func(t *testing.T) {
			run := func(legacy bool) []any {
				home, registry, mgr := openCodeHome(t)
				syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
				if legacy {
					rewriteAsLegacy(t, home)
				}
				out := step(t, home, registry, mgr)
				// Paths differ per temp home; compare with the home removed.
				return []any{strings.ReplaceAll(stringify(out), home, "<HOME>")}
			}
			current, legacy := run(false), run(true)
			if !reflect.DeepEqual(current, legacy) {
				t.Fatalf("legacy lockfile diverged:\ncurrent %v\nlegacy  %v", current, legacy)
			}
		})
	}

	// A save by this binary restores the target's channel and the file Path.
	home, _, mgr := openCodeHome(t)
	syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
	rewriteAsLegacy(t, home)
	if res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}}); res.Action != ActionUnchanged {
		t.Fatalf("sync over a legacy entry = %+v", res)
	}
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(project.KindHook, "opencode", "block")
	if e.Channel != ChannelDedicatedFile || e.Path != pluginPath(home, "block") {
		t.Fatalf("entry after save = %+v", e)
	}
}

func stringify(v []any) string {
	return fmt.Sprintf("%+v", v)
}

func TestSyncOpenCode_Unexpressible(t *testing.T) {
	t.Run("nothing expressible", func(t *testing.T) {
		home, registry, mgr := openCodeHome(t)
		rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}],"PreToolUse":[{"matcher":"Task","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
		approveHook(t, home, registry, "block")
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		if res.Action != ActionSkippedUnexpressible || res.Detail != `matcher token "Task" has no opencode tool; event "Stop" has no opencode mapping` {
			t.Fatalf("result = %+v", res)
		}
		if !HasFailures([]SyncResult{res}) {
			t.Fatal("skipped-unexpressible must count as a failure on a direct sync")
		}
		if _, err := os.Stat(pluginPath(home, "block")); !os.IsNotExist(err) {
			t.Fatalf("plugin written: %v", err)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block@opencode")); !os.IsNotExist(err) {
			t.Fatalf("script copy written: %v", err)
		}
		if ok, _ := mgr.HasProjections(t.Context()); ok {
			t.Fatal("an unexpressible hook was recorded")
		}
	})

	t.Run("mixed", func(t *testing.T) {
		home, registry, mgr := openCodeHome(t)
		rewriteHook(t, registry, "block", `{"name":"block","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
		approveHook(t, home, registry, "block")
		res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
		if res.Action != ActionCopied || res.Detail != "dropped groups: Stop" {
			t.Fatalf("result = %+v", res)
		}
		if HasFailures([]SyncResult{res}) {
			t.Fatal("a partly expressible hook must not fail the sync")
		}
		st := statusOf(t, mgr, "opencode")
		if st.State != StateInSync || st.Detail != "dropped groups: Stop" {
			t.Fatalf("status = %+v", st)
		}
		if gaps := Unenforced("opencode", []string{"block"}, []ProjectionStatus{st}); len(gaps) != 0 {
			t.Fatalf("requires.hooks reports the partly expressible hook unenforced: %+v", gaps)
		}
	})
}

// TestOpenCodePlugin_DerivesFromPinnedDefinition is the designated
// scenario: an unapproved registry edit never changes the plugin bytes,
// and the plugin bytes are a pure function of the pinned definition.
func TestOpenCodePlugin_DerivesFromPinnedDefinition(t *testing.T) {
	home, registry, mgr := openCodeHome(t)
	syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}})
	path := pluginPath(home, "block")
	pinned := mustRead(t, path)
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block@opencode")

	target, _ := FindTarget("opencode")
	want, err := renderFor(target, []byte(ifHookRaw), "block", scriptDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(want.Bytes) != string(pinned) {
		t.Fatal("plugin bytes are not the render of the pinned definition")
	}

	store := project.NewStore(home)
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	recorded := *before.Get(project.KindHook, "opencode", "block")

	edited := strings.Replace(ifHookRaw, `"matcher":"Bash"`, `"matcher":"Bash|Write"`, 1)
	rewriteHook(t, registry, "block", edited)
	res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}, Force: true})
	if res.Action != ActionSkippedPinDrift {
		t.Fatalf("unapproved edit = %+v", res)
	}
	if string(mustRead(t, path)) != string(pinned) {
		t.Fatal("an unapproved registry edit changed the plugin bytes")
	}
	after, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Get(project.KindHook, "opencode", "block"); !reflect.DeepEqual(got.Hashes, recorded.Hashes) || got.TreeHash != recorded.TreeHash {
		t.Fatalf("record changed: %+v", got)
	}
	if st := statusOf(t, mgr, "opencode"); st.State != StateStale || !strings.Contains(st.Detail, "pin drift") {
		t.Fatalf("status = %+v", st)
	}

	approveHook(t, home, registry, "block")
	if res := syncOne(t, mgr, SyncOptions{Clients: []string{"opencode"}}); res.Action != ActionUpdated {
		t.Fatalf("approved edit = %+v", res)
	}
	approved, err := renderFor(target, []byte(edited), "block", scriptDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, path)) != string(approved.Bytes) {
		t.Fatal("plugin bytes are not the render of the approved definition")
	}
}

func TestDetection_CoveredAndGemini(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	mgr := NewManagerWithHome(home, registry)
	for _, d := range []string{".gemini/config", ".cursor", ".config/devin"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var slugs []string
	for _, c := range mgr.DetectedUnsupported() {
		slugs = append(slugs, c.Slug)
	}
	if !reflect.DeepEqual(slugs, []string{"cursor", "windsurf", "antigravity"}) {
		t.Fatalf("detected = %v; an Antigravity-only ~/.gemini must not detect Gemini CLI", slugs)
	}
	if err := os.WriteFile(filepath.Join(home, ".gemini", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	slugs = nil
	for _, c := range mgr.DetectedUnsupported() {
		slugs = append(slugs, c.Slug)
	}
	if !slices.Contains(slugs, "gemini") {
		t.Fatalf("detected = %v; ~/.gemini/settings.json must detect Gemini CLI", slugs)
	}

	cursor, _ := FindUnsupported("cursor")
	if cursor.CoveredNote() != "note: cursor reads the claude-code projection (~/.claude/settings.json); no separate target" {
		t.Fatalf("note = %q", cursor.CoveredNote())
	}
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Clients: []string{"cursor", "windsurf"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != ActionSkippedUnsupported || !strings.HasPrefix(r.Detail, "covered by the claude-code projection; ") {
			t.Fatalf("covered row = %+v", r)
		}
	}
	if _, ok := FindUnsupported("opencode"); ok {
		t.Fatal("opencode is still listed as unsupported")
	}
	known := KnownSlugs()
	slices.Sort(known)
	if !reflect.DeepEqual(known, []string{"antigravity", "claude-code", "copilot", "cursor", "gemini", "opencode", "windsurf"}) {
		t.Fatalf("known slugs = %v", known)
	}
}

func TestSync_DefaultSkipsUndetectedTargetWhenAnotherIsDetected(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Client != "claude-code" || HasFailures(results) {
		t.Fatalf("results = %+v", results)
	}

	bare, bareRegistry := testHome(t)
	installHook(t, bareRegistry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, bare, bareRegistry, "block")
	none, err := NewManagerWithHome(bare, bareRegistry).Sync(t.Context(), nil, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 2 || !HasFailures(none) {
		t.Fatalf("no detected target = %+v", none)
	}
	for _, r := range none {
		if r.Action != ActionSkippedUnavailable {
			t.Fatalf("row = %+v", r)
		}
	}
	if _, err := NewManagerWithHome(bare, bareRegistry).Sync(t.Context(), nil, SyncOptions{Clients: []string{"opencode"}}); err != nil {
		t.Fatal(err)
	}
}

func TestUnenforced_OpenCodeIsATarget(t *testing.T) {
	gaps := Unenforced("opencode", []string{"block"}, nil)
	if len(gaps) != 1 || gaps[0].Reason != "not projected" {
		t.Fatalf("gaps = %+v", gaps)
	}
	gaps = Unenforced("opencode", []string{"block"}, []ProjectionStatus{{Hook: "block", Client: "opencode", State: StateDrifted}})
	if len(gaps) != 1 || gaps[0].Reason != "drifted" {
		t.Fatalf("gaps = %+v", gaps)
	}
}
