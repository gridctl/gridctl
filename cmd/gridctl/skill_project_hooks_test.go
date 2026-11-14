package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/hooksync"
)

func TestRunHookProjectSync_CoveredClientNote(t *testing.T) {
	home := t.TempDir()
	registry := filepath.Join(home, ".gridctl", "registry")
	for _, d := range []string{".claude", ".cursor", ".gemini/config"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mgr := hooksync.NewManagerWithHome(home, registry)
	var stdout, stderr bytes.Buffer
	exit := runHookProjectSync(t.Context(), &stdout, &stderr, mgr, nil, hooksync.SyncOptions{}, "", true)
	if exit != ctxExitOK {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout.String(), stderr.String())
	}
	note := "note: cursor reads the claude-code projection (~/.claude/settings.json); no separate target"
	if !strings.Contains(stderr.String(), note) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "note: gemini") {
		t.Fatalf("an Antigravity-only ~/.gemini printed a Gemini CLI note: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "note: antigravity") {
		t.Fatalf("antigravity is a hook target and must not get a note: %q", stderr.String())
	}

	stderr.Reset()
	if err := os.WriteFile(filepath.Join(home, ".gemini", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHookProjectSync(t.Context(), &stdout, &stderr, mgr, nil, hooksync.SyncOptions{}, "", true)
	if !strings.Contains(stderr.String(), "note: gemini (") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHookProjectActionLabel_Unexpressible(t *testing.T) {
	if got := hookProjectActionLabel(hooksync.ActionSkippedUnexpressible); got != "— skipped-unexpressible" {
		t.Fatalf("label = %q", got)
	}
}

func TestSkillProjectHelp_ListsHookTargets(t *testing.T) {
	for _, want := range []string{
		"~/.claude/settings.json",
		"~/.config/opencode/plugins/gridctl-hook-<name>.ts",
		"~/.gridctl/hooks/<name>@opencode/",
		"~/.copilot/hooks/gridctl-<name>.json",
		"~/.gridctl/hooks/<name>@copilot/",
		"~/.gemini/config/hooks.json",
		"gridctl-<name>",
		"~/.gridctl/hooks/<name>@antigravity/",
		"read the claude-code projection",
		"--copy does not apply to hooks",
		"Adopt is refused",
	} {
		if !strings.Contains(skillProjectCmd.Long, want) {
			t.Errorf("help is missing %q", want)
		}
	}
}
