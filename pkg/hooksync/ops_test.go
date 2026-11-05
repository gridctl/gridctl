package hooksync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestSettingsUnparsable(t *testing.T) {
	if !SettingsUnparsable(SettingsUnparsableDetail + "unexpected end of input") {
		t.Fatal("parse-error prefix must match")
	}
	if SettingsUnparsable("script copy was edited since gridctl wrote it") {
		t.Fatal("script drift must not match the parse-error prefix")
	}
	if SettingsUnparsable("") {
		t.Fatal("empty detail must not match")
	}
}

func TestBlocksRemoval_RowKinds(t *testing.T) {
	cases := []struct {
		action  string
		blocked bool
	}{
		{ActionRemoved, false},
		{ActionAlreadyGone, false},
		{"", true},
		{ActionSkippedDrift, true},
		{ActionSkippedUnmanaged, true},
		{ActionError, true},
	}
	for _, tc := range cases {
		row, blocked := BlocksRemoval([]UnsyncResult{{Hook: "block", Client: "claude-code", Action: tc.action}})
		if blocked != tc.blocked {
			t.Errorf("%s blocked = %v, want %v", tc.action, blocked, tc.blocked)
		}
		if !tc.blocked {
			continue
		}
		msg := RemovalRefusal("block", row)
		wantAction := tc.action
		if wantAction == "" {
			wantAction = "error"
		}
		if !strings.Contains(msg, "claude-code") || !strings.Contains(msg, "unsync") || !strings.Contains(msg, "--force") || !strings.Contains(msg, wantAction) {
			t.Errorf("%s refusal = %q", tc.action, msg)
		}
	}
}

func TestSync_SettingsMergePreservesSiblings(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // keep this comment\n  \"permissions\": {\"allow\": [\"Bash\",],},\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\"matcher\": \"Write\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo hi\"}]}\n    ]\n  },\n  \"zebra\": true, // last\n}\n")
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionCopied {
		t.Fatalf("results = %+v", results)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "gridctl hook block") {
		t.Fatalf("projected group missing:\n%s", text)
	}
	if !strings.Contains(text, "\n") || !strings.Contains(text, "statusMessage") {
		t.Fatalf("inserted group is not pretty-printed:\n%s", text)
	}
	user := `{"matcher": "Write", "hooks": [{"type": "command", "command": "echo hi"}]}`
	if !strings.Contains(text, user) {
		t.Fatalf("user group was rewritten:\n%s", text)
	}
	prefix, _, ok := strings.Cut(string(original), user)
	if !ok {
		t.Fatal("fixture missing user group")
	}
	if !strings.HasPrefix(text, prefix) {
		t.Fatalf("bytes before the user group changed:\n%s", text)
	}
	suffix := string(original)[strings.Index(string(original), user)+len(user):]
	// The append inserts a comma and the new group before the array close.
	// Everything after the original array must still be present verbatim.
	arrayClose := strings.Index(suffix, "]")
	if arrayClose < 0 {
		t.Fatal("fixture missing array close")
	}
	after := suffix[arrayClose:]
	if !strings.HasSuffix(text, after) && !strings.Contains(text, after) {
		t.Fatalf("bytes after the patched array changed:\noriginal after=%q\nresult=\n%s", after, text)
	}
	if !strings.Contains(text, "// keep this comment") || !strings.Contains(text, `"zebra": true`) || !strings.Contains(text, `"permissions"`) {
		t.Fatalf("sibling content lost:\n%s", text)
	}
	if !strings.Contains(text, `{"allow": ["Bash",],}`) || !strings.Contains(text, `"zebra": true, // last`) {
		t.Fatalf("trailing commas outside the patch were dropped:\n%s", text)
	}
	backups := 0
	entries, err := os.ReadDir(claude)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), backupSuffix) {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("backup count = %d", backups)
	}

	again, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Action != ActionUnchanged {
		t.Fatalf("second sync = %+v", again)
	}
	second, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != text {
		t.Fatal("unchanged sync rewrote the settings file")
	}

	store := project.NewStore(home)
	l, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e := l.Get(project.KindHook, "claude-code", "block")
	if e == nil || e.ScriptDir == "" || e.TreeHash == "" || !strings.Contains(e.Path, "#hooks/block") {
		t.Fatalf("lock entry = %+v", e)
	}
	if _, err := os.Stat(e.ScriptDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "revision: 4") {
		t.Fatalf("lockfile was not raised to revision 4:\n%s", raw)
	}
}

func TestSync_ProjectsPinnedBytesOnly(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho pinned\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	beforeSettings, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, ".gridctl", "hooks", "block", "scripts", "run.sh")
	beforeScript, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}

	hookJSON := filepath.Join(registry, "hooks", "block", "hook.json")
	edited := strings.ReplaceAll(string(mustRead(t, hookJSON)), "block force push", "edited description")
	if err := os.WriteFile(hookJSON, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "hooks", "block", "scripts", "run.sh"), []byte("#!/bin/sh\necho edited\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedPinDrift || !strings.Contains(results[0].Remediation, "--expect ") {
		t.Fatalf("unapproved sync = %+v", results)
	}
	afterSettings, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	afterScript, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterSettings) != string(beforeSettings) || string(afterScript) != string(beforeScript) {
		t.Fatal("unapproved registry edit reached the settings file or script copy")
	}

	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Load(); err != nil {
		t.Fatal(err)
	}
	hash, err := pins.CurrentCompositeHash("block", registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := pins.Approve(t.Context(), "block", registry, hookpins.ApproveRequest{ExpectedHash: hash, Reason: "reviewed"}); err != nil {
		t.Fatal(err)
	}
	updated, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].Action != ActionUpdated {
		t.Fatalf("approved sync = %+v", updated)
	}
	got, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join(registry, "hooks", "block", "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(pinned) {
		t.Fatalf("script copy = %q, pinned = %q", got, pinned)
	}
}

func TestSync_DecisionTable(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	original, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(original), "gridctl hook block", "hand edited", 1)
	if err := os.WriteFile(settings, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	statuses, err := mgr.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].State != StateDrifted {
		t.Fatalf("status after hand edit = %+v", statuses)
	}
	skipped, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if skipped[0].Action != ActionSkippedDrift {
		t.Fatalf("sync = %+v", skipped)
	}
	forced, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if forced[0].Action != ActionUpdated {
		t.Fatalf("force = %+v", forced)
	}
	restored, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(restored), "gridctl hook block") {
		t.Fatalf("force did not restore the group:\n%s", restored)
	}

	// A user group edit is not drift.
	userFile := strings.Replace(string(restored), `"allow"`, `"deny"`, 1)
	if !strings.Contains(string(restored), `"allow"`) {
		// The created file has no permissions key. Append a user group instead.
		userFile = string(restored)
	}
	_ = userFile
	store := project.NewStore(home)
	if err := os.Remove(store.Path()); err != nil {
		t.Fatal(err)
	}
	foreign, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if foreign[0].Action != ActionSkippedForeign || foreign[0].Detail != foreignDetail {
		t.Fatalf("foreign = %+v", foreign)
	}
	reclaimed, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed[0].Action != ActionUpdated {
		t.Fatalf("force reclaim = %+v", reclaimed)
	}
}

func TestSync_MixedReplaceAndRemoveKeepsIndexes(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "alpha", "#!/bin/sh\necho alpha\n")
	installHook(t, registry, "beta", "#!/bin/sh\necho beta\n")
	pinHook(t, home, registry, "alpha")
	pinHook(t, home, registry, "beta")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	user := `{"matcher": "Write", "hooks": [{"type": "command", "command": "echo hi"}]}`
	original := []byte("{\n  // keep this comment\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"hooks\": {\n    \"PreToolUse\": [\n      " + user + "\n    ]\n  }\n}\n")
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	if _, err := mgr.Sync(t.Context(), []string{"alpha", "beta"}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	rewriteHook(t, registry, "alpha", `{
  "name": "alpha",
  "description": "block force push",
  "dialect": "claude-code",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/run.sh"}]},
      {"matcher": "Write", "hooks": [{"type": "command", "command": "./scripts/run.sh"}]}
    ]
  }
}`)
	rewriteHook(t, registry, "beta", `{
  "name": "beta",
  "description": "block force push",
  "dialect": "claude-code",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash(git *)", "hooks": [{"type": "command", "command": "./scripts/run.sh"}]}
    ]
  }
}`)
	approveHook(t, home, registry, "alpha")
	approveHook(t, home, registry, "beta")

	results, err := mgr.Sync(t.Context(), []string{"alpha", "beta"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Action != ActionUpdated || results[1].Action != ActionUpdated {
		t.Fatalf("results = %+v", results)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	prefix, _, ok := strings.Cut(string(original), user)
	if !ok || !strings.HasPrefix(text, prefix) || !strings.Contains(text, user) {
		t.Fatalf("user group was rewritten:\n%s", text)
	}
	if strings.Count(text, "gridctl hook alpha") != 2 || !strings.Contains(text, "Bash(git *)") {
		t.Fatalf("mixed update did not land both hooks:\n%s", text)
	}
	if !strings.Contains(text, "// keep this comment") || !strings.Contains(text, `"permissions"`) {
		t.Fatalf("sibling content lost:\n%s", text)
	}
}

func TestSync_UnparseableNotOverridable(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedUnmanaged || !strings.Contains(results[0].Detail, "not valid JSON") {
		t.Fatalf("results = %+v", results)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{not json" {
		t.Fatalf("unparseable file was written: %s", got)
	}
}

func TestUnsync_RemovesOnlyGridctlGroup(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // keep\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\"matcher\": \"Write\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo hi\"}]}\n    ]\n  }\n}\n")
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	results, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionRemoved {
		t.Fatalf("unsync = %+v", results)
	}
	got, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Contains(text, "gridctl hook block") {
		t.Fatalf("gridctl group remained:\n%s", text)
	}
	if !strings.Contains(text, "// keep") || !strings.Contains(text, `"permissions"`) || !strings.Contains(text, "echo hi") {
		t.Fatalf("sibling content lost:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "hooks", "block")); !os.IsNotExist(err) {
		t.Fatalf("script copy still present: %v", err)
	}
	again, err := mgr.Unsync(t.Context(), []string{"block"}, UnsyncOptions{})
	if err == nil || !strings.Contains(err.Error(), "not projected") {
		t.Fatalf("second unsync = %v, %+v", err, again)
	}
}

// writeMergedGroup replaces the settings file with one hand-merged group
// whose handlers point at both hooks' script directories, followed by a
// user group and a trailing comma.
func writeMergedGroup(t *testing.T, home, settings string) string {
	t.Helper()
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	a := l.Get(project.KindHook, "claude-code", "alpha")
	b := l.Get(project.KindHook, "claude-code", "beta")
	if a == nil || b == nil {
		t.Fatal("hooks were not recorded")
	}
	merged := `{"matcher": "Bash", "hooks": [{"type": "command", "command": "` + filepath.Join(a.ScriptDir, "scripts", "run.sh") + `"}, {"type": "command", "command": "` + filepath.Join(b.ScriptDir, "scripts", "run.sh") + `"}]}`
	data := "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      " + merged + ",\n      {\"matcher\": \"Write\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo user\"}]},\n    ]\n  }\n}\n"
	if err := os.WriteFile(settings, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return merged
}

func TestMergedGroup_ForceKeepsFollowingUserGroup(t *testing.T) {
	for _, mode := range []string{"unsync", "sync"} {
		t.Run(mode, func(t *testing.T) {
			home, registry := testHome(t)
			for _, name := range []string{"alpha", "beta"} {
				installHook(t, registry, name, "#!/bin/sh\necho no\n")
				pinHook(t, home, registry, name)
			}
			claude := filepath.Join(home, ".claude")
			if err := os.MkdirAll(claude, 0o755); err != nil {
				t.Fatal(err)
			}
			settings := filepath.Join(claude, "settings.json")
			mgr := NewManagerWithHome(home, registry)
			if _, err := mgr.Sync(t.Context(), []string{"alpha", "beta"}, SyncOptions{}); err != nil {
				t.Fatal(err)
			}
			merged := writeMergedGroup(t, home, settings)

			if mode == "unsync" {
				results, err := mgr.Unsync(t.Context(), nil, UnsyncOptions{All: true, Force: true})
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range results {
					if r.Action != ActionRemoved {
						t.Fatalf("unsync = %+v", results)
					}
				}
			} else {
				results, err := mgr.Sync(t.Context(), []string{"alpha", "beta"}, SyncOptions{Force: true})
				if err != nil {
					t.Fatal(err)
				}
				for _, r := range results {
					if r.Action != ActionUpdated {
						t.Fatalf("sync = %+v", results)
					}
				}
			}
			got, err := os.ReadFile(settings)
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			if !strings.Contains(text, "echo user") || !strings.Contains(text, `"PreToolUse"`) {
				t.Fatalf("user group after the merged group was deleted:\n%s", text)
			}
			if strings.Contains(text, merged) {
				t.Fatalf("merged group survived:\n%s", text)
			}
			if mode == "sync" && (strings.Count(text, "gridctl hook alpha") != 1 || strings.Count(text, "gridctl hook beta") != 1) {
				t.Fatalf("sync did not restore one group per hook:\n%s", text)
			}
		})
	}
}

func TestSync_UnsupportedAndUnknown(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Clients: []string{"gemini"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionSkippedUnsupported || results[0].Detail != "~/.gemini/settings.json hooks key" {
		t.Fatalf("unsupported = %+v", results)
	}
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Clients: []string{"nonsense"}}); err == nil || !strings.Contains(err.Error(), "unknown client") {
		t.Fatalf("unknown client err = %v", err)
	}
	detected := mgr.DetectedUnsupported()
	if len(detected) != 1 || detected[0].Slug != "gemini" {
		t.Fatalf("detected = %+v", detected)
	}
}

func TestUnenforced_ReasonPerState(t *testing.T) {
	statuses := []ProjectionStatus{
		{Hook: "drifted", Client: "claude-code", State: StateDrifted},
		{Hook: "stale", Client: "claude-code", State: StateStale, Detail: "planned render differs"},
		{Hook: "pinned", Client: "claude-code", State: StateStale, Detail: "pin drift"},
		{Hook: "missing", Client: "claude-code", State: StateTargetMissing},
		{Hook: "ready", Client: "claude-code", State: StateInSync},
	}
	got := map[string]string{}
	for _, row := range Unenforced("claude-code", []string{"drifted", "stale", "pinned", "missing", "ready", "absent"}, statuses) {
		got[row.Name] = row.Reason
	}
	want := map[string]string{
		"drifted": "drifted",
		"stale":   "stale",
		"pinned":  "pin drift",
		"missing": "not projected",
		"absent":  "not projected",
	}
	for name, reason := range want {
		if got[name] != reason {
			t.Errorf("%s reason = %q, want %q", name, got[name], reason)
		}
	}
	if _, ok := got["ready"]; ok {
		t.Fatal("in-sync hook was reported unenforced")
	}
	other := Unenforced("gemini", []string{"block"}, statuses)
	if len(other) != 1 || other[0].Reason != "client has no hook target" {
		t.Fatalf("gemini = %+v", other)
	}
}

func TestRevision3RaisesTo4(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := project.NewStore(home)
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("version: 1\nrevision: 3\nprojections: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	if _, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "revision: 4") || !strings.Contains(string(raw), "script_dir:") {
		t.Fatalf("lockfile:\n%s", raw)
	}
}

func testHome(t *testing.T) (home, registry string) {
	t.Helper()
	home = t.TempDir()
	registry = filepath.Join(home, ".gridctl", "registry")
	return home, registry
}

func installHook(t *testing.T, registry, name, script string) {
	t.Helper()
	dir := filepath.Join(registry, "hooks", name, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"` + name + `","description":"block force push","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(registry, "hooks", name, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.ParseHookJSON(raw, filepath.Join(registry, "hooks", name)); err != nil {
		t.Fatal(err)
	}
}

func pinHook(t *testing.T, home, registry, name string) {
	t.Helper()
	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Pin(t.Context(), hookpins.PinRequest{Name: name, Dir: filepath.Join(registry, "hooks", name)}); err != nil {
		t.Fatal(err)
	}
}

func rewriteHook(t *testing.T, registry, name, raw string) {
	t.Helper()
	dir := filepath.Join(registry, "hooks", name)
	if _, err := skills.ParseHookJSON([]byte(raw), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func approveHook(t *testing.T, home, registry, name string) {
	t.Helper()
	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Load(); err != nil {
		t.Fatal(err)
	}
	hash, err := pins.CurrentCompositeHash(name, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := pins.Approve(t.Context(), name, registry, hookpins.ApproveRequest{ExpectedHash: hash, Reason: "reviewed"}); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSync_RestoresRemovedScriptCopy(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	first, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Action != ActionCopied {
		t.Fatalf("first = %+v", first)
	}
	scriptDir := first[0].ScriptDir
	settings := first[0].Target
	before, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(scriptDir); err != nil {
		t.Fatal(err)
	}

	statuses, err := mgr.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].State != StateTargetMissing || !strings.Contains(statuses[0].Detail, "script directory is gone") {
		t.Fatalf("status = %+v", statuses)
	}
	skipped, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0].Action != ActionUpdated || skipped[0].Detail != scriptRemovedDetail {
		t.Fatalf("sync after delete = %+v", skipped)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "scripts", "run.sh")); err != nil {
		t.Fatalf("script was not restored: %v", err)
	}
	after, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("settings rewritten while restoring scripts:\n%s", after)
	}
	again, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Action != ActionUnchanged {
		t.Fatalf("second sync = %+v", again)
	}

	if err := os.RemoveAll(scriptDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scriptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	empty, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 1 || empty[0].Action != ActionUpdated || empty[0].Detail != scriptRemovedDetail {
		t.Fatalf("sync after empty dir = %+v", empty)
	}
	if _, err := os.Stat(filepath.Join(scriptDir, "hook.json")); err != nil {
		t.Fatalf("empty copy was not restored: %v", err)
	}
}

func TestSync_ForceScriptDriftDoesNotDuplicateGroups(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	first, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(first[0].ScriptDir, "scripts", "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho edited\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := first[0].Target
	before := mustRead(t, settings)
	skipped, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if skipped[0].Action != ActionSkippedDrift || !strings.Contains(skipped[0].Detail, "script copy was edited") {
		t.Fatalf("sync = %+v", skipped)
	}
	forced, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if forced[0].Action != ActionUpdated {
		t.Fatalf("force = %+v", forced)
	}
	got := mustRead(t, script)
	if string(got) != "#!/bin/sh\necho no\n" {
		t.Fatalf("script = %q", got)
	}
	after := mustRead(t, settings)
	if string(after) != string(before) {
		t.Fatalf("force rewrote settings:\n%s", after)
	}
	if strings.Count(string(after), "gridctl hook block") != 1 {
		t.Fatalf("group duplicated:\n%s", after)
	}
}

func TestSync_PreservesSettingsMode(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, []byte("{\n  \"env\": {\"TOKEN\": \"secret\"}\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionCopied {
		t.Fatalf("results = %+v", results)
	}
	info, err := os.Stat(settings)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode = %o, want 600", info.Mode().Perm())
	}
	var backups int
	entries, err := os.ReadDir(claude)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.Contains(e.Name(), backupSuffix) {
			continue
		}
		backups++
		bi, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if bi.Mode().Perm() != 0o600 {
			t.Fatalf("backup %s mode = %o, want 600", e.Name(), bi.Mode().Perm())
		}
	}
	if backups != 1 {
		t.Fatalf("backup count = %d", backups)
	}
}

func TestSync_InsertsHooksKeyOnOwnLine(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // keep this comment\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"env\": {\"TOKEN\": \"secret\"}\n}\n")
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionCopied {
		t.Fatalf("results = %+v", results)
	}
	got := string(mustRead(t, settings))
	if !strings.HasPrefix(got, "{\n  // keep this comment\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"env\": {\"TOKEN\": \"secret\"}") {
		t.Fatalf("sibling prefix changed:\n%s", got)
	}
	if strings.Contains(got, `,"hooks":`) || strings.Contains(got, `,"PreToolUse":`) {
		t.Fatalf("inserted key is glued to the previous value:\n%s", got)
	}
	if !strings.Contains(got, "\n  \"hooks\": {") {
		t.Fatalf("hooks key is not on its own line with the file indent:\n%s", got)
	}
	if !strings.Contains(got, "\n    \"PreToolUse\": [") || !strings.Contains(got, "\n        \"matcher\"") {
		t.Fatalf("inserted subtree is not pretty-printed:\n%s", got)
	}
	if !strings.Contains(got, "// keep this comment") || !strings.Contains(got, `"permissions"`) {
		t.Fatalf("sibling content lost:\n%s", got)
	}
}

func TestSync_InsertsMissingEventArrayOnOwnLine(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"hooks\": {\n    \"PostToolUse\": []\n  },\n  \"zebra\": true\n}\n")
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithHome(home, registry)
	results, err := mgr.Sync(t.Context(), []string{"block"}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionCopied {
		t.Fatalf("results = %+v", results)
	}
	got := string(mustRead(t, settings))
	if !strings.Contains(got, "\"PostToolUse\": []") || !strings.Contains(got, "\"zebra\": true") {
		t.Fatalf("sibling content lost:\n%s", got)
	}
	if strings.Contains(got, `,"PreToolUse":`) {
		t.Fatalf("event key is glued:\n%s", got)
	}
	if !strings.Contains(got, "\n    \"PreToolUse\": [") {
		t.Fatalf("event array is not on its own line:\n%s", got)
	}
}
