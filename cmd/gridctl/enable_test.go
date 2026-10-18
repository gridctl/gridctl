package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestApplyOverrideFlags_Absolutizes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "local.yaml"), []byte("mcp-servers: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	overrideFlag = "local.yaml"
	noOverrideFlag = false
	overrideOptional = false
	t.Cleanup(func() {
		overrideFlag = ""
		noOverrideFlag = false
		overrideOptional = false
	})
	var cfg controller.Config
	if err := applyOverrideFlags(&cfg); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "local.yaml")
	if cfg.OverridePath != want {
		t.Fatalf("override = %s, want %s", cfg.OverridePath, want)
	}
}

func TestWriteEnabled_ConcurrentEdit(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	body := "name: demo\nnetwork:\n  name: n\nmcp-servers:\n  - name: one\n    command: [/bin/true]\n"
	if err := os.WriteFile(stackPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := enableBetweenReadsHook
	enableBetweenReadsHook = func() {
		if err := os.WriteFile(stackPath, []byte(body+"# changed\n"), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { enableBetweenReadsHook = prev })

	_, err := writeEnabled(stackPath, stackPath, "stack", "one", false)
	if err == nil || !strings.Contains(err.Error(), "re-run") {
		t.Fatalf("err = %v", err)
	}
	data, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "enabled:") {
		t.Fatalf("wrote over a concurrent edit: %s", data)
	}
}

func TestWriteEnabled_NewOverrideAppears(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(dir, "stack.yaml")
	override := filepath.Join(dir, "stack.override.yaml")
	body := "name: demo\nnetwork:\n  name: n\nmcp-servers:\n  - name: one\n    command: [/bin/true]\n"
	if err := os.WriteFile(stackPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := "mcp-servers:\n  other:\n    replicas: 1\n"
	prev := enableBetweenReadsHook
	enableBetweenReadsHook = func() {
		if err := os.WriteFile(override, []byte(marker), 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { enableBetweenReadsHook = prev })

	_, err := writeEnabled(stackPath, override, "override", "one", false)
	if err == nil || !strings.Contains(err.Error(), "re-run") {
		t.Fatalf("err = %v", err)
	}
	data, err := os.ReadFile(override)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != marker {
		t.Fatalf("overwrote the concurrent override: %s", data)
	}
}

func TestSelectEnableTarget_PackBeforeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	checkout := filepath.Join(home, ".gridctl", "packs", "demo", "abc")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(checkout, "stack.yaml")
	if err := os.WriteFile(stack, []byte("name: demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := skills.WriteLockFile(skills.LockFilePath(), &skills.LockFile{Sources: map[string]skills.LockedSource{
		"demo": {Pack: &skills.LockedPack{Name: "demo"}},
	}}); err != nil {
		t.Fatal(err)
	}
	want := packops.OverridePath(home, "demo")
	enableOverride = filepath.Join(home, "other.yaml")
	enableNoOverride = false
	t.Cleanup(func() {
		enableOverride = ""
		enableNoOverride = false
	})
	if _, _, err := selectEnableTarget(stack, nil); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err = %v", err)
	}
	enableOverride = want
	path, kind, err := selectEnableTarget(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "override" || path != want {
		t.Fatalf("path=%s kind=%s", path, kind)
	}
	enableOverride = ""
	enableNoOverride = true
	path, kind, err = selectEnableTarget(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "override" || path != want {
		t.Fatalf("--no-override escaped the pack redirect: path=%s kind=%s", path, kind)
	}
}

func TestWriteReloadLists_IncludesEnableTransitions(t *testing.T) {
	var buf bytes.Buffer
	writeReloadLists(&buf, &reload.ReloadResult{
		Disabled: []string{"mcp-server:parked"},
		Enabled:  []string{"mcp-server:live"},
	})
	got := buf.String()
	if !strings.Contains(got, "Disabled: [mcp-server:parked]") || !strings.Contains(got, "Enabled: [mcp-server:live]") {
		t.Fatalf("lists = %q", got)
	}
	if reloadListCount(reload.ReloadResult{Disabled: []string{"mcp-server:parked"}}) == 0 {
		t.Fatal("disabled list was not counted")
	}
}
