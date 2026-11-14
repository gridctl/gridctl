package packops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestResolvePackSelection_UnresolvedHook(t *testing.T) {
	m := &pack.Manifest{Hooks: pack.HookList{"present", "missing"}}
	clone := &skills.CloneResult{Hooks: []skills.DiscoveredHook{{Name: "present"}}}
	got := resolvePackSelection(m, clone, nil, nil)
	if len(got.hooks) != 1 || got.hooks[0] != "present" {
		t.Fatalf("hooks = %v", got.hooks)
	}
	if len(got.unresolved) != 1 || got.unresolved[0] != "hooks:missing" {
		t.Fatalf("unresolved = %v", got.unresolved)
	}
}

func TestAppendHookScan_BlocksFinding(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "scripts", "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nchmod 777 /tmp/x\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"block","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	def, err := skills.ParseHookJSON(raw, dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := &AddDoc{}
	appendHookScan(doc, &skills.CloneResult{Hooks: []skills.DiscoveredHook{{
		Name: "block", Dir: dir, Definition: def,
	}}}, []string{"block"}, false)
	if len(doc.Hooks) != 0 {
		t.Fatalf("hooks = %v, want none", doc.Hooks)
	}
	if len(doc.Skipped) != 1 || !strings.Contains(doc.Skipped[0], "block (hook):") || !strings.Contains(doc.Skipped[0], "security findings") {
		t.Fatalf("skipped = %v", doc.Skipped)
	}
}

func TestStatusRowsFor_HookPresence(t *testing.T) {
	home := t.TempDir()
	reg := filepath.Join(home, ".gridctl", "registry")
	good := filepath.Join(reg, "hooks", "good")
	if err := os.MkdirAll(filepath.Join(good, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "scripts", "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"good","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(good, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(reg, "hooks", "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "hook.json"), []byte(`{"extra":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Managers{Home: home, LockPath: filepath.Join(home, "missing.lock")}
	rows, attention := m.statusRowsFor(&skills.LockedPack{
		Name:  "hook-pack",
		Hooks: []string{"good", "bad", "gone"},
	}, "", nil, nil, nil, nil, nil, nil, nil)
	if !attention {
		t.Fatal("invalid and missing hooks should need attention")
	}
	got := map[string]Row{}
	for _, row := range rows {
		if row.Kind == "hook" {
			got[row.Name] = row
		}
	}
	if got["good"].State != "in-sync" {
		t.Fatalf("good = %+v", got["good"])
	}
	if got["gone"].State != "missing" || !strings.Contains(got["gone"].Detail, "not in the hook registry") {
		t.Fatalf("gone = %+v", got["gone"])
	}
	if got["bad"].State != "drifted" || !strings.Contains(got["bad"].Detail, "extra") || !strings.Contains(got["bad"].Remediation, "skill pins diff bad --kind hook") {
		t.Fatalf("bad = %+v", got["bad"])
	}
}

func TestRemovePackHooks_DeletesInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	dir := filepath.Join(home, ".gridctl", "registry", "hooks", "bad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), []byte(`{"extra":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var rows []Row
	if err := removePackHooks(t.Context(), home, []string{"bad"}, &rows); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("invalid hook directory still present: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "removed" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestApply_UnsupportedHookIsInformational(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Gemini CLI is detected by its settings file, not by ~/.gemini.
	if err := os.WriteFile(filepath.Join(home, ".gemini", "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "block"
	dir := filepath.Join(home, ".gridctl", "registry", "hooks", name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho no\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"block","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Pin(t.Context(), hookpins.PinRequest{Name: name, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".gridctl", "skills.lock.yaml")
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"hook-pack": {
			Repo: "https://example.com/hook-pack.git",
			Pack: &skills.LockedPack{Name: "hook-pack", Hooks: []string{name}},
		},
	}}
	if err := skills.WriteLockFile(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	mgrs := &Managers{
		Home:     home,
		LockPath: lockPath,
		Hooks:    hooksync.NewManagerWithHome(home, filepath.Join(home, ".gridctl", "registry")),
	}
	doc, err := mgrs.Apply(t.Context(), "hook-pack", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Applied != doc.Total || doc.Applied < 2 {
		t.Fatalf("applied %d/%d, want informational rows counted as applied\nrows=%+v", doc.Applied, doc.Total, doc.Rows)
	}
	var copied, unsupported bool
	for _, row := range doc.Rows {
		if row.Kind != "hook" || row.Name != name {
			continue
		}
		switch row.Action {
		case "copied":
			if row.Client == "claude-code" {
				copied = true
			}
		case hooksync.ActionSkippedUnsupported:
			if row.Client == "gemini" && row.Detail == "~/.gemini/settings.json hooks key" {
				unsupported = true
			}
		}
	}
	if !copied || !unsupported {
		t.Fatalf("rows = %+v", doc.Rows)
	}
}

func TestApply_UnexpressibleHookIsInformational(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	// OpenCode and Antigravity are detected; ~/.gemini/config is
	// Antigravity's and must not produce a Gemini CLI row without
	// ~/.gemini/settings.json.
	for _, d := range []string{".claude", ".config/opencode", ".gemini/config", ".cursor"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	name := "stop"
	dir := filepath.Join(home, ".gridctl", "registry", "hooks", name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho no\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"stop","description":"d","dialect":"claude-code","hooks":{"Stop":[{"hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Pin(t.Context(), hookpins.PinRequest{Name: name, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".gridctl", "skills.lock.yaml")
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"hook-pack": {
			Repo: "https://example.com/hook-pack.git",
			Pack: &skills.LockedPack{Name: "hook-pack", Hooks: []string{name}},
		},
	}}
	if err := skills.WriteLockFile(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	mgrs := &Managers{
		Home:     home,
		LockPath: lockPath,
		Hooks:    hooksync.NewManagerWithHome(home, filepath.Join(home, ".gridctl", "registry")),
	}
	doc, err := mgrs.Apply(t.Context(), "hook-pack", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Applied != doc.Total {
		t.Fatalf("applied %d/%d, want skipped-unexpressible counted as applied\nrows=%+v", doc.Applied, doc.Total, doc.Rows)
	}
	got := map[string]string{}
	for _, row := range doc.Rows {
		if row.Kind == "hook" {
			got[row.Client] = row.Action + ": " + row.Detail
		}
	}
	if got["opencode"] != `skipped-unexpressible: event "Stop" has no opencode mapping` {
		t.Fatalf("opencode row = %q\nrows=%+v", got["opencode"], doc.Rows)
	}
	if !strings.HasPrefix(got["cursor"], "skipped-unsupported: covered by the claude-code projection") {
		t.Fatalf("cursor row = %q", got["cursor"])
	}
	if _, ok := got["gemini"]; ok {
		t.Fatalf("an Antigravity-only ~/.gemini produced a Gemini CLI row: %+v", doc.Rows)
	}
	if got["antigravity"] != `skipped-unexpressible: event "Stop" has no antigravity mapping` {
		t.Fatalf("antigravity row = %q\nrows=%+v", got["antigravity"], doc.Rows)
	}
}

func TestTallyRows_HookInformationalActions(t *testing.T) {
	applied, failed := TallyRows([]Row{
		{Action: hooksync.ActionSkippedUnsupported},
		{Action: hooksync.ActionSkippedUnexpressible},
		{Action: hooksync.ActionSkippedDrift},
	})
	if applied != 2 || failed != 1 {
		t.Fatalf("applied=%d failed=%d", applied, failed)
	}
}

func TestTrimLockedPack_KeepsNamedHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills.lock.yaml")
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"hook-pack": {
			Repo: "https://example.com/pack.git",
			Pack: &skills.LockedPack{
				Name:   "hook-pack",
				Skills: []string{"alpha"},
				Hooks:  []string{"block"},
			},
		},
	}}
	if err := skills.WriteLockFile(path, lf); err != nil {
		t.Fatal(err)
	}
	if err := trimLockedPack(t.Context(), path, "hook-pack", []string{"hook/block"}); err != nil {
		t.Fatal(err)
	}
	got, err := skills.ReadLockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, src, ok := got.FindPackSource("hook-pack")
	if !ok || src.Pack == nil {
		t.Fatalf("pack record missing: %+v", got)
	}
	if len(src.Pack.Skills) != 0 {
		t.Fatalf("skills = %v", src.Pack.Skills)
	}
	if len(src.Pack.Hooks) != 1 || src.Pack.Hooks[0] != "block" {
		t.Fatalf("hooks = %v", src.Pack.Hooks)
	}
}

func TestBlockingHookUnsync_DryRunTreatsWouldRemoveAsRetired(t *testing.T) {
	retired := []hooksync.UnsyncResult{
		{Hook: "block", Client: "claude-code", Action: hooksync.ActionWouldRemove},
		{Hook: "block", Client: "other", Action: hooksync.ActionAlreadyGone},
	}
	if row, blocked := blockingHookUnsync(retired, true); blocked {
		t.Fatalf("dry-run would-remove blocked: %+v", row)
	}
	if _, blocked := blockingHookUnsync(retired, false); !blocked {
		t.Fatal("real run must treat would-remove as blocking")
	}
	mixed := []hooksync.UnsyncResult{
		{Hook: "block", Client: "claude-code", Action: hooksync.ActionRemoved},
		{Hook: "block", Client: "retired", Action: hooksync.ActionError, Error: "client is no longer a projection target"},
	}
	row, blocked := blockingHookUnsync(mixed, false)
	if !blocked || row.Client != "retired" || row.Action != hooksync.ActionError {
		t.Fatalf("mixed rows = %+v blocked=%v", row, blocked)
	}
}

func TestRemove_KeepsHookWhenUnsyncBlocked(t *testing.T) {
	t.Run("skipped-unmanaged", func(t *testing.T) {
		mgrs, imp, home := seedProjectedHookPack(t)
		settings := filepath.Join(home, ".claude", "settings.json")
		if err := os.WriteFile(settings, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		doc, err := mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{Force: true})
		if err != nil {
			t.Fatal(err)
		}
		assertHookKept(t, mgrs, home, doc, "claude-code", hooksync.ActionSkippedUnmanaged, "{not json")
	})

	t.Run("error", func(t *testing.T) {
		mgrs, imp, home := seedProjectedHookPack(t)
		settings := filepath.Join(home, ".claude", "settings.json")
		before, err := os.ReadFile(settings)
		if err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(home, ".gridctl", "project.lock.yaml")
		data, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		edited := strings.Replace(string(data), "client: claude-code", "client: retired", 1)
		if edited == string(data) {
			t.Fatalf("lock had no claude-code client:\n%s", data)
		}
		if err := os.WriteFile(lockPath, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		doc, err := mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{})
		if err != nil {
			t.Fatal(err)
		}
		assertHookKept(t, mgrs, home, doc, "retired", hooksync.ActionError, string(before))
	})

	t.Run("dry-run blocked", func(t *testing.T) {
		mgrs, imp, home := seedProjectedHookPack(t)
		settings := filepath.Join(home, ".claude", "settings.json")
		if err := os.WriteFile(settings, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		doc, err := mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{Force: true, DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if !doc.DryRun {
			t.Fatal("dry_run flag missing")
		}
		assertHookKept(t, mgrs, home, doc, "claude-code", hooksync.ActionSkippedUnmanaged, "{not json")
	})

	t.Run("dry-run retired", func(t *testing.T) {
		mgrs, imp, home := seedProjectedHookPack(t)
		settings := filepath.Join(home, ".claude", "settings.json")
		beforeSettings, err := os.ReadFile(settings)
		if err != nil {
			t.Fatal(err)
		}
		registry := filepath.Join(home, ".gridctl", "registry", "hooks", "block")
		doc, err := mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{Force: true, DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if !doc.DryRun {
			t.Fatal("dry_run flag missing")
		}
		if len(doc.Kept) != 0 {
			t.Fatalf("kept = %v, want empty", doc.Kept)
		}
		var hookRows []Row
		for _, row := range doc.Rows {
			if row.Kind == "hook" {
				hookRows = append(hookRows, row)
			}
		}
		if len(hookRows) != 1 || hookRows[0].Action != "would-remove" {
			t.Fatalf("hook rows = %+v", hookRows)
		}
		gotSettings, err := os.ReadFile(settings)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotSettings) != string(beforeSettings) {
			t.Fatal("dry run wrote the settings file")
		}
		if _, err := os.Stat(filepath.Join(registry, "hook.json")); err != nil {
			t.Fatalf("dry run removed the registry hook: %v", err)
		}
		assertHookPin(t, home, "block", true)
		assertHookProjection(t, home, "block", true)

		doc, err = mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{Force: true})
		if err != nil {
			t.Fatal(err)
		}
		var removed bool
		for _, row := range doc.Rows {
			if row.Kind == "hook" && row.Action == "removed" {
				removed = true
			}
		}
		if !removed {
			t.Fatalf("rows = %+v, want a removed hook row", doc.Rows)
		}
		if _, err := os.Stat(registry); !os.IsNotExist(err) {
			t.Fatalf("registry hook still present: %v", err)
		}
		assertHookPin(t, home, "block", false)
	})
}

func TestRemove_RemovesNeverProjectedHook(t *testing.T) {
	mgrs, imp, home := seedHookPack(t)
	registry := filepath.Join(home, ".gridctl", "registry", "hooks", "block")

	// A hook with no projection record is absent from unsync results.
	// The preview must still report would-remove, not treat that absence
	// as a block, and must not write.
	doc, err := mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.DryRun {
		t.Fatal("dry_run flag missing")
	}
	if len(doc.Kept) != 0 {
		t.Fatalf("kept = %v, want empty", doc.Kept)
	}
	var hookRows []Row
	for _, row := range doc.Rows {
		if row.Kind == "hook" {
			hookRows = append(hookRows, row)
		}
	}
	if len(hookRows) != 1 || hookRows[0].Name != "block" || hookRows[0].Action != "would-remove" {
		t.Fatalf("hook rows = %+v", hookRows)
	}
	if _, err := os.Stat(filepath.Join(registry, "hook.json")); err != nil {
		t.Fatalf("dry run removed the registry hook: %v", err)
	}
	assertHookPin(t, home, "block", true)
	assertHookProjection(t, home, "block", false)
	locked, err := mgrs.LoadLockedPack("hook-pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(locked.Hooks) != 1 || locked.Hooks[0] != "block" {
		t.Fatalf("pack hooks = %v", locked.Hooks)
	}

	doc, err = mgrs.Remove(t.Context(), imp, "hook-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Kept) != 0 {
		t.Fatalf("kept = %v", doc.Kept)
	}
	var removed bool
	for _, row := range doc.Rows {
		if row.Kind == "hook" && row.Action == "removed" && row.Detail == "registry entry removed" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	if _, err := os.Stat(registry); !os.IsNotExist(err) {
		t.Fatalf("never-projected hook still present: %v", err)
	}
	assertHookPin(t, home, "block", false)
	if _, err := mgrs.LoadLockedPack("hook-pack"); err == nil {
		t.Fatal("pack record kept for a fully removed hook")
	}
}

// seedProjectedHookPack installs hook "block", projects it to Claude Code,
// and returns the engine rooted at that home. GRIDCTL_HOME is pinned to
// the temp home so removePackHooks resets the pin file the test seeded.
func seedProjectedHookPack(t *testing.T) (*Managers, *skills.Importer, string) {
	t.Helper()
	mgrs, imp, home := seedHookPack(t)
	doc, err := mgrs.Apply(t.Context(), "hook-pack", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var copied bool
	for _, row := range doc.Rows {
		if row.Kind == "hook" && row.Name == "block" && row.Action == "copied" && row.Client == "claude-code" {
			copied = true
		}
	}
	if !copied {
		t.Fatalf("apply rows = %+v, want a copied hook row", doc.Rows)
	}
	return mgrs, imp, home
}

func seedHookPack(t *testing.T) (*Managers, *skills.Importer, string) {
	t.Helper()
	// Drop an inherited GRIDCTL_HOME before testEnv builds managers, then
	// pin it to the temp home so pin reset and projection share one tree.
	t.Setenv("GRIDCTL_HOME", "")
	mgrs, imp := testEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIDCTL_HOME", home)
	name := "block"
	dir := filepath.Join(home, ".gridctl", "registry", "hooks", name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\necho no\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"block","description":"d","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Pin(t.Context(), hookpins.PinRequest{Name: name, Dir: dir}); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, ".gridctl", "skills.lock.yaml")
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"hook-pack": {
			Repo: "https://example.com/hook-pack.git",
			Pack: &skills.LockedPack{Name: "hook-pack", Hooks: []string{name}},
		},
	}}
	if err := skills.WriteLockFile(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	mgrs.LockPath = lockPath
	mgrs.Hooks = hooksync.NewManagerWithHome(home, filepath.Join(home, ".gridctl", "registry"))
	return mgrs, imp, home
}

func assertHookKept(t *testing.T, mgrs *Managers, home string, doc *RemoveDoc, client, action, settings string) {
	t.Helper()
	var hookRows []Row
	for _, row := range doc.Rows {
		if row.Kind != "hook" {
			continue
		}
		if row.Action == "removed" || row.Action == "would-remove" {
			t.Fatalf("hook row retired despite a block: %+v", row)
		}
		hookRows = append(hookRows, row)
	}
	if len(hookRows) != 1 {
		t.Fatalf("hook rows = %+v", hookRows)
	}
	row := hookRows[0]
	if row.Action != action || row.Client != client {
		t.Fatalf("row = %+v, want action %s client %s", row, action, client)
	}
	if !strings.Contains(row.Remediation, "unsync --kind hook block --force") {
		t.Fatalf("remediation = %q", row.Remediation)
	}
	if row.Detail == "" {
		t.Fatal("refusal row has no detail, so the remediation will not print")
	}
	if !containsString(doc.Kept, "hook/block") {
		t.Fatalf("kept = %v", doc.Kept)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "registry", "hooks", "block", "hook.json")); err != nil {
		t.Fatalf("registry hook removed: %v", err)
	}
	assertHookPin(t, home, "block", true)
	locked, err := mgrs.LoadLockedPack("hook-pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(locked.Hooks) != 1 || locked.Hooks[0] != "block" {
		t.Fatalf("pack hooks = %v", locked.Hooks)
	}
	got, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != settings {
		t.Fatalf("settings changed:\n%s", got)
	}
	assertHookProjection(t, home, "block", true)
	script := filepath.Join(home, ".gridctl", "hooks", "block")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("script copy removed: %v", err)
	}
}

func assertHookPin(t *testing.T, home, name string, want bool) {
	t.Helper()
	pins := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := pins.Load(); err != nil {
		t.Fatal(err)
	}
	_, ok := pins.Get(name)
	if ok != want {
		t.Fatalf("pin present = %v, want %v", ok, want)
	}
}

func assertHookProjection(t *testing.T, home, name string, want bool) {
	t.Helper()
	l, err := project.NewStore(home).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range l.Entries(project.KindHook) {
		if e.Source == name {
			found = true
		}
	}
	if found != want {
		t.Fatalf("projection present = %v, want %v", found, want)
	}
}

func TestNameDriftedHookClients(t *testing.T) {
	rows := []Row{
		{Kind: "hook", Name: "block", Action: "skipped-drift"},
		{Kind: "skill", Name: "block", Action: "skipped-drift"},
		{Kind: "hook", Name: "other", Action: "removed", Client: "claude-code"},
	}
	nameDriftedHookClients(rows, map[string][]string{"block": {"claude-code", "opencode"}})
	if rows[0].Client != "claude-code,opencode" || rows[1].Client != "" || rows[2].Client != "claude-code" {
		t.Fatalf("rows = %+v", rows)
	}
}
