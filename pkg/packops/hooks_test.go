package packops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/pack"
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
	}, "", nil, nil, nil, nil, nil, nil)
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
