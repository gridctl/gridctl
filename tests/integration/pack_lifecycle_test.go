//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/skillsync"
	"github.com/gridctl/gridctl/pkg/wiring"
)

// TestPackLifecycle_AddStatusApplyRemove drives the full pack verb set
// against a real local git repository and real client directories under
// a sandboxed HOME: clone and manifest resolution, registry import with
// rule-fragment installation, projection to a detected client, status
// depth for rules (per-client rows, drift detection), and the cascade
// removal, exactly as the CLI and the REST handlers run it.
func TestPackLifecycle_AddStatusApplyRemove(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// A real pack repo: manifest, one skill, one agent, one rule.
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"gridctl-pack.yaml": `apiVersion: gridctl.dev/v1
kind: Pack
name: integ-pack
version: 1.0.0
description: Integration fixture
skills: [alpha]
agents: [reviewer]
rules: [team-style]
wiring: false
`,
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: Test skill\n---\n\nDo alpha.\n",
		"agents/reviewer.md":    "---\nname: reviewer\ndescription: Reviews\n---\n\nReview.\n",
		"rules/team-style.md":   "Use the Oxford comma.\n",
	}
	for path, content := range files {
		full := filepath.Join(repoDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
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

	engine := func() (*packops.Managers, *skills.Importer) {
		t.Helper()
		registryDir := filepath.Join(home, ".gridctl", "skills")
		store := registry.NewStore(registryDir)
		if err := store.Load(); err != nil {
			t.Fatal(err)
		}
		imp := skills.NewImporter(store, registryDir, skills.LockFilePath(), slog.Default())
		sm, err := skillsync.NewManager(store)
		if err != nil {
			t.Fatal(err)
		}
		am, err := agentsync.NewManager(registryDir)
		if err != nil {
			t.Fatal(err)
		}
		wm, err := wiring.NewManager()
		if err != nil {
			t.Fatal(err)
		}
		cm, err := contexts.NewManager()
		if err != nil {
			t.Fatal(err)
		}
		return &packops.Managers{Skills: sm, Agents: am, Wiring: wm, Contexts: cm, Home: home}, imp
	}

	// Add: real clone, manifest resolution, rule install.
	mgrs, imp := engine()
	added, err := mgrs.Add(ctx, imp, packops.AddOptions{Repo: repoDir})
	if err != nil {
		t.Fatal(err)
	}
	if added.Doc.Pack != "integ-pack" || len(added.Doc.Rules) != 1 {
		t.Fatalf("add doc = %+v", added.Doc)
	}

	// Apply: files land in the real client tree.
	mgrs, _ = engine()
	applied, err := mgrs.Apply(ctx, "integ-pack", packops.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if applied.Applied == 0 {
		t.Fatalf("apply = %+v", applied)
	}
	skillDir := filepath.Join(home, ".claude", "skills", "alpha")
	if _, err := os.Stat(skillDir); err != nil {
		t.Fatalf("skill not projected: %v", err)
	}
	ruleFile := filepath.Join(home, ".claude", "rules", "gridctl-team-style.md")
	if _, err := os.Stat(ruleFile); err != nil {
		t.Fatalf("rule not projected: %v", err)
	}

	// Status: per-client rule rows, then drift after a hand edit.
	mgrs, _ = engine()
	statuses, err := mgrs.Statuses(ctx, packops.StatusOptions{Pack: "integ-pack"})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].NeedsAttention {
		t.Fatalf("clean status = %+v", statuses)
	}
	if err := os.WriteFile(ruleFile, []byte("Edited by hand.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgrs, _ = engine()
	statuses, err = mgrs.Statuses(ctx, packops.StatusOptions{Pack: "integ-pack"})
	if err != nil {
		t.Fatal(err)
	}
	sawDrift := false
	for _, r := range statuses[0].Rows {
		if r.Kind == "rule" && r.Client == "claude-code" && r.State == "drifted" {
			sawDrift = true
		}
	}
	if !sawDrift {
		t.Fatalf("rule drift not detected: %+v", statuses[0].Rows)
	}

	// Remove without force keeps nothing here (only the rule drifted,
	// and rule drift does not gate removal); the cascade clears the
	// client tree and the pack record.
	mgrs, imp = engine()
	removed, err := mgrs.Remove(ctx, imp, "integ-pack", packops.RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Kept) != 0 {
		t.Fatalf("kept = %v", removed.Kept)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("skill projection survived removal")
	}
	if _, err := os.Stat(ruleFile); !os.IsNotExist(err) {
		t.Error("rule projection survived removal")
	}
	if _, err := packops.LoadLockedPack("integ-pack"); err == nil {
		t.Error("pack record survived removal")
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range lf.Sources {
		if src.Pack != nil {
			t.Errorf("source %s still carries a pack record", name)
		}
	}
}
