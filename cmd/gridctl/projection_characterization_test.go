package main

// Characterization tests freezing the CLI contract of `gridctl ctx` and
// `gridctl skill project` across the pkg/project engine extraction. The
// golden files under testdata/characterization capture the pre-refactor
// output byte-for-byte (after normalizing temp-dir paths, timestamps,
// and table padding, which vary per run) and must pass unchanged after
// any refactor of pkg/contexts or pkg/skillsync.
//
// Regenerate with: GRIDCTL_UPDATE_GOLDEN=1 go test ./cmd/gridctl -run TestCharacterization

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillsync"
)

var (
	charTimestampRe = regexp.MustCompile(`"synced_at": "[^"]*"`)
	charPaddingRe   = regexp.MustCompile(` {2,}`)
	charTrailingRe  = regexp.MustCompile(`[ \t]+\n`)
)

// normalizeCharOutput makes captured CLI output run-independent: the
// temp home collapses to $HOME, timestamps to a placeholder, and runs
// of alignment padding to a single two-space gap (temp-dir names vary
// in length per run, so column widths would otherwise never be stable).
func normalizeCharOutput(out, home string) string {
	out = strings.ReplaceAll(out, home, "$HOME")
	out = charTimestampRe.ReplaceAllString(out, `"synced_at": "<TS>"`)
	out = charPaddingRe.ReplaceAllString(out, "  ")
	out = charTrailingRe.ReplaceAllString(out, "\n")
	return out
}

// assertGolden compares got against testdata/characterization/<name>.golden,
// rewriting the golden when GRIDCTL_UPDATE_GOLDEN is set.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "characterization", name+".golden")
	if os.Getenv("GRIDCTL_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (regenerate with GRIDCTL_UPDATE_GOLDEN=1): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("output diverged from golden %s\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// newCtxCharFixture stages a contexts manager with every state the
// status table can show: in-sync (gemini shim), stale (claude-code,
// canonical changed after sync), drifted (opencode block hand-edited),
// never-synced and unavailable (the rest), plus the unsupported rows.
func newCtxCharFixture(t *testing.T) (*contexts.Manager, string) {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{".claude", ".gemini", filepath.Join(".config", "opencode")} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mgr := contexts.NewManagerWithHome(home)
	if err := mgr.SaveCanonical("# Rules\n\n- One.\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, slug := range []string{"claude-code", "gemini", "opencode"} {
		if _, err := mgr.SyncClient(ctx, slug, contexts.SyncOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	// Drift opencode: hand-edit inside the managed block.
	opencodeTarget := filepath.Join(home, ".config", "opencode", "AGENTS.md")
	data, err := os.ReadFile(opencodeTarget)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "- One.", "- One, edited by hand.", 1)
	if err := os.WriteFile(opencodeTarget, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stale claude-code (and gemini, whose shim strategy is stale-immune):
	// the canonical file moves on after the sync.
	if err := mgr.SaveCanonical("# Rules\n\n- One.\n- Two.\n"); err != nil {
		t.Fatal(err)
	}
	return mgr, home
}

func TestCharacterizationCtxStatus(t *testing.T) {
	mgr, home := newCtxCharFixture(t)
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	if exit := runCtxStatus(ctx, &stdout, &stderr, mgr, "", true); exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", exit, stderr.String())
	}
	assertGolden(t, "ctx-status-plain", normalizeCharOutput(stdout.String(), home))

	stdout.Reset()
	if exit := runCtxStatus(ctx, &stdout, &stderr, mgr, "json", false); exit != ctxExitAttention {
		t.Fatalf("json exit = %d, want 1", exit)
	}
	assertGolden(t, "ctx-status-json", normalizeCharOutput(stdout.String(), home))
}

func TestCharacterizationCtxSyncDryRun(t *testing.T) {
	mgr, home := newCtxCharFixture(t)
	ctx := context.Background()
	opts := contexts.SyncOptions{DryRun: true}

	var stdout, stderr bytes.Buffer
	if exit := runCtxSync(ctx, &stdout, &stderr, mgr, nil, opts, "", true); exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", exit, stderr.String())
	}
	assertGolden(t, "ctx-sync-dry-run-plain", normalizeCharOutput(stdout.String(), home))

	stdout.Reset()
	if exit := runCtxSync(ctx, &stdout, &stderr, mgr, nil, opts, "json", false); exit != ctxExitAttention {
		t.Fatalf("json exit = %d, want 1", exit)
	}
	assertGolden(t, "ctx-sync-dry-run-json", normalizeCharOutput(stdout.String(), home))
}

// skillCharFixture stages a skillsync manager with an in-sync symlink
// (alpha → claude-code), a drifted copy (alpha → antigravity, copy
// hand-edited), and a stale copy (beta → antigravity, registry edited
// after the copy was made).
func newSkillCharFixture(t *testing.T) (*skillsync.Manager, string) {
	t.Helper()
	home := t.TempDir()
	regDir := filepath.Join(home, ".gridctl", "registry")
	writeCharSkill := func(name, body string) {
		dir := filepath.Join(regDir, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: Characterization skill " + name + "\nstate: active\n---\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeCharSkill("alpha", "Alpha body.")
	writeCharSkill("beta", "Beta body.")
	for _, d := range []string{".claude", filepath.Join(".gemini", "config")} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := registry.NewStore(regDir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	mgr := skillsync.NewManagerWithHome(home, store)
	ctx := context.Background()
	if _, err := mgr.Sync(ctx, []string{"alpha"}, skillsync.SyncOptions{Clients: []string{"claude-code", "antigravity"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Sync(ctx, []string{"beta"}, skillsync.SyncOptions{Clients: []string{"antigravity"}}); err != nil {
		t.Fatal(err)
	}
	// Drift alpha's antigravity copy.
	alphaCopy := filepath.Join(home, ".gemini", "config", "skills", "alpha", "SKILL.md")
	if err := os.WriteFile(alphaCopy, []byte("hand edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stale beta: the registry moves on after the copy.
	writeCharSkill("beta", "Beta body, revised.")
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return mgr, home
}

func TestCharacterizationSkillProjectStatus(t *testing.T) {
	mgr, home := newSkillCharFixture(t)
	ctx := context.Background()

	var stdout, stderr bytes.Buffer
	if exit := runSkillProjectStatus(ctx, &stdout, &stderr, mgr, "", true); exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", exit, stderr.String())
	}
	assertGolden(t, "skill-project-status-plain", normalizeCharOutput(stdout.String(), home))

	stdout.Reset()
	if exit := runSkillProjectStatus(ctx, &stdout, &stderr, mgr, "json", false); exit != ctxExitAttention {
		t.Fatalf("json exit = %d, want 1", exit)
	}
	assertGolden(t, "skill-project-status-json", normalizeCharOutput(stdout.String(), home))
}

func TestCharacterizationSkillProjectSyncDryRun(t *testing.T) {
	mgr, home := newSkillCharFixture(t)
	ctx := context.Background()
	opts := skillsync.SyncOptions{DryRun: true}

	var stdout, stderr bytes.Buffer
	if exit := runSkillProjectSync(ctx, &stdout, &stderr, mgr, nil, opts, "", true); exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1 (stderr: %s)", exit, stderr.String())
	}
	assertGolden(t, "skill-project-sync-dry-run-plain", normalizeCharOutput(stdout.String(), home))

	stdout.Reset()
	if exit := runSkillProjectSync(ctx, &stdout, &stderr, mgr, nil, opts, "json", false); exit != ctxExitAttention {
		t.Fatalf("json exit = %d, want 1", exit)
	}
	assertGolden(t, "skill-project-sync-dry-run-json", normalizeCharOutput(stdout.String(), home))
}
