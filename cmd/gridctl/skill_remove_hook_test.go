package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestSkillRemoveHook_RefusesUnsyncRows(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, home string)
		client string
		action string
	}{
		{
			name: "skipped-drift",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				path := filepath.Join(home, ".claude", "settings.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "gridctl hook block", "hand edited", 1)
				if edited == string(data) {
					t.Fatalf("settings had no gridctl marker:\n%s", data)
				}
				if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			client: "claude-code",
			action: hooksync.ActionSkippedDrift,
		},
		{
			name: "skipped-unmanaged",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{not json"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			client: "claude-code",
			action: hooksync.ActionSkippedUnmanaged,
		},
		{
			name: "error",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				path := filepath.Join(home, ".gridctl", "project.lock.yaml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "client: claude-code", "client: retired", 1)
				if edited == string(data) {
					t.Fatalf("lock had no claude-code client:\n%s", data)
				}
				if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			client: "retired",
			action: hooksync.ActionError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("GRIDCTL_HOME", home)
			t.Setenv("HOME", home)
			if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			registry := filepath.Join(home, ".gridctl", "registry")
			installRemoveHook(t, registry, "block")
			ps := hookpins.New()
			if err := ps.Pin(t.Context(), hookpins.PinRequest{
				Name: "block",
				Dir:  filepath.Join(registry, "hooks", "block"),
			}); err != nil {
				t.Fatal(err)
			}
			mgr, err := newHookProjectManager()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := mgr.Sync(t.Context(), []string{"block"}, hooksync.SyncOptions{}); err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, home)
			err = runSkillRemoveHook("block")
			if err == nil {
				t.Fatal("remove succeeded, want refusal")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.client) || !strings.Contains(msg, "unsync") || !strings.Contains(msg, "--force") || !strings.Contains(msg, tc.action) {
				t.Fatalf("refusal = %q", msg)
			}
			if _, statErr := os.Stat(filepath.Join(registry, "hooks", "block", "hook.json")); statErr != nil {
				t.Fatalf("registry hook removed despite refusal: %v", statErr)
			}
		})
	}
}

func installRemoveHook(t *testing.T, registry, name string) {
	t.Helper()
	dir := filepath.Join(registry, "hooks", name, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho no\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"` + name + `","description":"block force push","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if _, err := skills.ParseHookJSON(raw, filepath.Join(registry, "hooks", name)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "hooks", name, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
