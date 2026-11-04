package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

const hookPackManifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: hook-pack
version: 1.0.0
description: Hook projection pack
author:
  name: Acme
skills: [alpha]
hooks: [block-force-push]
wiring: false
`

const hookPackDefinition = `{
  "name": "block-force-push",
  "description": "Deny git push --force",
  "dialect": "claude-code",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/block.sh"}]}
    ]
  }
}
`

func TestHandlePackApplyAndRemove_ProjectsHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv, _ := setupRegistryTestServer(t)
	srv.SetSkillSourcePaths(skills.LockFilePath(), filepath.Join(home, ".gridctl", "skills.yaml"))

	repo := hookPackRepo(t)
	rec := doJSON(t, srv, http.MethodPost, "/api/packs/preview", `{"repo":`+jsonQuote(repo)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "block-force-push") {
		t.Fatalf("preview omitted the hook: %s", rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/packs", `{"repo":`+jsonQuote(repo)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/packs/hook-pack/apply", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind":"hook"`) {
		t.Fatalf("apply omitted hook rows: %s", rec.Body.String())
	}
	script := filepath.Join(home, ".gridctl", "hooks", "block-force-push", "scripts", "block.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("script copy missing after apply: %v", err)
	}
	settings, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), script) {
		t.Fatalf("settings group missing script path:\n%s", settings)
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/packs/hook-pack", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatalf("script copy still present after remove: %v", err)
	}
	settings, err = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settings), script) || strings.Contains(string(settings), "block-force-push") {
		t.Fatalf("settings group survived remove:\n%s", settings)
	}
}

func TestHandlePackRemove_KeepsBlockedHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// removePackHooks deletes <home>/.gridctl/registry. The default test
	// registry is a separate temp dir, so root this server where the
	// cascade actually deletes or the assertion cannot fail on the bug.
	registryDir := filepath.Join(home, ".gridctl", "registry")
	store := registry.NewStore(registryDir)
	regServer := registry.New(store)
	if err := regServer.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv, _ := setupRegistryTestServer(t)
	srv.SetRegistryServer(regServer)
	srv.SetSkillSourcePaths(skills.LockFilePath(), filepath.Join(home, ".gridctl", "skills.yaml"))

	repo := hookPackRepo(t)
	rec := doJSON(t, srv, http.MethodPost, "/api/packs", `{"repo":`+jsonQuote(repo)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/packs/hook-pack/apply", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", rec.Code, rec.Body.String())
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/packs/hook-pack?force=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove = %d: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		Kept []string `json:"kept"`
		Rows []struct {
			Kind        string `json:"kind"`
			Name        string `json:"name"`
			Action      string `json:"action"`
			Remediation string `json:"remediation"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "hook/block-force-push" {
		t.Fatalf("kept = %v\nbody = %s", doc.Kept, rec.Body.String())
	}
	var saw bool
	for _, row := range doc.Rows {
		if row.Kind == "hook" && row.Action == "skipped-unmanaged" && row.Remediation != "" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	hookJSON := filepath.Join(registryDir, "hooks", "block-force-push", "hook.json")
	if _, err := os.Stat(hookJSON); err != nil {
		t.Fatalf("registry hook removed: %v", err)
	}
	pins := hookpins.New()
	if err := pins.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := pins.Get("block-force-push"); !ok {
		t.Fatal("pin reset despite a blocked unsync")
	}
}

func TestHandlePackPreview_ListsHookFindings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, _ := setupRegistryTestServer(t)
	srv.SetSkillSourcePaths(skills.LockFilePath(), filepath.Join(home, ".gridctl", "skills.yaml"))
	repo := hookPackRepoWithScript(t, "#!/bin/sh\nchmod 777 \"$0\"\n")
	rec := doJSON(t, srv, http.MethodPost, "/api/packs/preview", `{"repo":`+jsonQuote(repo)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Hooks []struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			Findings []struct {
				Description string `json:"description"`
			} `json:"findings"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Hooks) != 1 || body.Hooks[0].Name != "block-force-push" || body.Hooks[0].Kind != "hook" {
		t.Fatalf("hooks = %+v", body.Hooks)
	}
	if len(body.Hooks[0].Findings) == 0 {
		t.Fatalf("hook findings missing: %s", rec.Body.String())
	}
}

func hookPackRepo(t *testing.T) string {
	t.Helper()
	return hookPackRepoWithScript(t, "#!/bin/sh\necho deny\n")
}

func hookPackRepoWithScript(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"gridctl-pack.yaml":                       hookPackManifest,
		"skills/alpha/SKILL.md":                   "---\nname: alpha\ndescription: Test skill\n---\n\nDo alpha.\n",
		"hooks/block-force-push/hook.json":        hookPackDefinition,
		"hooks/block-force-push/scripts/block.sh": script,
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(full, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}
