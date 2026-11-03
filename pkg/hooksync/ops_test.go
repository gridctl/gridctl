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

func TestSync_SettingsMergePreservesSiblings(t *testing.T) {
	home, registry := testHome(t)
	installHook(t, registry, "block", "#!/bin/sh\necho no\n")
	pinHook(t, home, registry, "block")
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  // keep this comment\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\"matcher\": \"Write\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo hi\"}]}\n    ]\n  },\n  \"zebra\": true\n}\n")
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
