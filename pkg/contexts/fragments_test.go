package contexts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFragmentFrontmatterAndExtras(t *testing.T) {
	raw := "---\ndescription: style\npaths:\n  - \"**/*.go\"\ncustom: keep-me\n---\n\n# Body\n"
	f, err := ParseFragment("style", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Description != "style" || len(f.Paths) != 1 || f.Paths[0] != "**/*.go" {
		t.Fatalf("parsed = %+v", f)
	}
	if len(f.Extra) != 1 || f.Extra[0].Key != "custom" {
		t.Fatalf("extra = %+v", f.Extra)
	}
	if !strings.Contains(f.Body, "# Body") {
		t.Fatalf("body = %q", f.Body)
	}
	// Round-trip preserves unknown keys.
	out, err := RenderFragmentMD(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "custom:") {
		t.Fatalf("extra key lost: %s", out)
	}
}

func TestComposeFragmentsLexicographicWithSourceMarkers(t *testing.T) {
	frags := []*Fragment{
		{Name: "b-second", FileName: "b-second.md", Body: "second", Raw: []byte("second\n")},
		{Name: "a-first", FileName: "a-first.md", Body: "first", Paths: []string{"**/*.ts"}, Raw: []byte("---\npaths: ['**/*.ts']\n---\nfirst\n")},
	}
	// composeFragments expects pre-sorted input (ListFragments sorts).
	// Pass out of order to prove caller order is composition order.
	res := composeFragments([]*Fragment{frags[1], frags[0]})
	if !strings.Contains(res.document, "<!-- Source: fragments/a-first.md -->") {
		t.Fatalf("missing source marker: %s", res.document)
	}
	if idxA, idxB := strings.Index(res.document, "first"), strings.Index(res.document, "second"); idxA < 0 || idxB < 0 || idxA > idxB {
		t.Fatalf("order wrong: %s", res.document)
	}
	if len(res.droppedPaths) != 1 || res.droppedPaths[0] != "a-first" {
		t.Fatalf("droppedPaths = %v", res.droppedPaths)
	}
}

func TestFragmentsModeOptInNeverAutomatic(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Canon\n")
	ctx := context.Background()
	// Read-only surfaces must not create fragments/.
	if _, err := m.Statuses(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SyncClient(ctx, "claude-code", SyncOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if m.FragmentsActive() {
		t.Fatal("dry-run/status activated fragments mode")
	}
	if _, err := os.Stat(m.FragmentsDir()); !os.IsNotExist(err) {
		t.Fatalf("fragments dir should not exist, err=%v", err)
	}
}

func TestAddFragmentMigratesCanonical(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Canon body\n")
	res, err := m.AddFragment("style", "---\ndescription: style\n---\n\nBe concise.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated || res.MigratedBackup == "" {
		t.Fatalf("migration = %+v", res)
	}
	if m.HasCanonical() {
		t.Fatal("canonical AGENTS.md should be gone after migration")
	}
	frags, err := m.ListFragments()
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 2 {
		t.Fatalf("frags = %d, want 2 (00-default + style)", len(frags))
	}
	if frags[0].Name != migratedFragmentName || frags[1].Name != "style" {
		t.Fatalf("order = %s, %s", frags[0].Name, frags[1].Name)
	}
	def, err := m.ReadFragment(migratedFragmentName)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def.Body, "Canon body") {
		t.Fatalf("migrated body = %q", def.Body)
	}
}

func TestMultiFileSyncClaudeAndCopilot(t *testing.T) {
	m := newTestManager(t, ".claude", ".copilot")
	initCanonical(t, m, "# Base\n")
	if _, err := m.AddFragment("paths-demo", "---\ndescription: d\npaths:\n  - \"**/*.go\"\n---\n\nGo rules.\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	results, err := m.SyncAll(ctx, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var claude, vscode *SyncResult
	for i := range results {
		r := &results[i]
		if r.Slug == "claude-code" && r.Fragment == "paths-demo" {
			claude = r
		}
		if r.Slug == "vscode" && r.Fragment == "paths-demo" {
			vscode = r
		}
	}
	if claude == nil || vscode == nil {
		t.Fatalf("missing multi-file results: %+v", results)
	}
	if claude.Mode != ModeMultiFile || vscode.Mode != ModeMultiFile {
		t.Fatalf("modes: claude=%s vscode=%s", claude.Mode, vscode.Mode)
	}
	// Claude identity: paths preserved; Copilot: applyTo, no drop of paths.
	claudePath := filepath.Join(m.home, ".claude", "rules", "gridctl-paths-demo.md")
	body := readFile(t, claudePath)
	if !strings.Contains(body, "paths:") {
		t.Fatalf("claude lost paths frontmatter: %s", body)
	}
	vscodePath := filepath.Join(m.home, ".copilot", "instructions", "gridctl-paths-demo.instructions.md")
	vbody := readFile(t, vscodePath)
	if !strings.Contains(vbody, `applyTo: "**/*.go"`) && !strings.Contains(vbody, "applyTo:") {
		t.Fatalf("copilot missing applyTo: %s", vbody)
	}
	// Foreign file in shared dir is never touched.
	foreign := filepath.Join(m.home, ".claude", "rules", "user-rule.md")
	writeFile(t, foreign, "# mine\n")
	if _, err := m.Unsync(ctx, "claude-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign file was removed: %v", err)
	}
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatal("projected fragment should be gone after unsync")
	}
}

func TestCompiledMaxCharsHardError(t *testing.T) {
	m := newTestManager(t, ".codeium/windsurf")
	initCanonical(t, m, "# x\n")
	// One huge fragment exceeding Windsurf's 6000-char cap.
	big := strings.Repeat("a", 7000)
	if _, err := m.AddFragment("huge", big); err != nil {
		t.Fatal(err)
	}
	res, err := m.SyncClient(context.Background(), "windsurf", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionError || !strings.Contains(res.Error, "6000") {
		t.Fatalf("want over-cap error, got action=%s err=%s", res.Action, res.Error)
	}
	target := filepath.Join(m.home, ".codeium", "windsurf", "memories", "global_rules.md")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("over-cap sync must not write the target")
	}
}

func TestAdoptCompiledRefusesMultiFileRequiresFragment(t *testing.T) {
	m := newTestManager(t, ".claude", ".config/opencode")
	initCanonical(t, m, "# Base\n")
	if _, err := m.AddFragment("notes", "hello\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "opencode", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SyncClient(ctx, "claude-code", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	err := m.Adopt(ctx, "opencode")
	if err == nil || !strings.Contains(err.Error(), "--into") {
		t.Fatalf("compiled adopt should refuse with --into hint: %v", err)
	}
	if err := m.AdoptInto("opencode", "captured"); err != nil {
		t.Fatal(err)
	}
	cap, err := m.ReadFragment("captured")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.Body, "hello") && !strings.Contains(cap.Body, "Base") {
		t.Fatalf("captured body unexpected: %q", cap.Body)
	}
	// Multi-file identity adopt.
	target := filepath.Join(m.home, ".claude", "rules", "gridctl-notes.md")
	if err := os.WriteFile(target, []byte("# edited notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.AdoptFragment("claude-code", "notes"); err != nil {
		t.Fatal(err)
	}
	f, err := m.ReadFragment("notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Body, "edited notes") {
		t.Fatalf("adopt did not pull edit: %q", f.Body)
	}
}

func TestPlainRenderDropsPaths(t *testing.T) {
	f, err := ParseFragment("x", []byte("---\npaths: ['a']\ndescription: d\n---\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Roo is multi-file plain.
	tRoo, ok := FindTarget("roo")
	if !ok {
		t.Fatal("roo missing")
	}
	rendered := renderFragmentFor(tRoo, f)
	if strings.Contains(string(rendered.data), "paths") {
		t.Fatalf("plain render kept frontmatter: %s", rendered.data)
	}
	if len(rendered.dropped) == 0 || rendered.dropped[0] != "paths" && !containsStr(rendered.dropped, "paths") {
		t.Fatalf("dropped = %v", rendered.dropped)
	}
	detail := fragmentDropDetail(tRoo, rendered.dropped)
	if !strings.Contains(detail, "path scoping") {
		t.Fatalf("detail = %q", detail)
	}
}

func TestValidateFragmentName(t *testing.T) {
	if err := ValidateFragmentName("good-name"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFragmentName("Bad"); !errors.Is(err, ErrBadFragmentName) {
		t.Fatalf("err = %v", err)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestPackTagAppliesOnlyToPackRules(t *testing.T) {
	m := newTestManager(t, ".claude")
	initCanonical(t, m, "# Base\n")
	if _, err := m.AddFragment("mine", "user rules\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddFragment("team-style", "pack rules\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.SyncAll(ctx, SyncOptions{Pack: "netpack", PackRules: []string{"team-style"}}); err != nil {
		t.Fatal(err)
	}

	l, err := m.store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	flf := fragmentViewFromLock(l)
	if e := flf.entry("team-style", "claude-code"); e == nil || e.Pack != "netpack" {
		t.Fatalf("pack rule entry = %+v, want Pack=netpack", e)
	}
	if e := flf.entry("mine", "claude-code"); e == nil || e.Pack != "" {
		t.Fatalf("user fragment must not carry the pack tag: %+v", e)
	}

	// Cascade removal by tag retracts only the pack's projection.
	results, names, err := m.UnsyncPackFragments(ctx, "netpack")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Fragment != "team-style" {
		t.Fatalf("results = %+v, want only team-style", results)
	}
	if len(names) != 1 || names[0] != "team-style" {
		t.Fatalf("names = %v", names)
	}
	minePath := filepath.Join(m.home, ".claude", "rules", "gridctl-mine.md")
	if _, err := os.Stat(minePath); err != nil {
		t.Fatalf("user fragment projection was removed: %v", err)
	}
}

func TestStatusShimStaleAfterMigration(t *testing.T) {
	m := newTestManager(t, ".gemini")
	initCanonical(t, m, "# Canon\n")
	ctx := context.Background()
	if _, err := m.SyncClient(ctx, "gemini", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	// Migration deletes AGENTS.md; the shim's @import now dangles and the
	// row must demand a re-sync instead of claiming in-sync.
	if _, err := m.AddFragment("style", "Be concise.\n"); err != nil {
		t.Fatal(err)
	}
	statuses, err := m.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, cs := range statuses {
		if cs.Slug != "gemini" {
			continue
		}
		if cs.State != StateStale || !strings.Contains(cs.Detail, "migrated") {
			t.Fatalf("gemini row = %+v, want stale with migration detail", cs)
		}
		return
	}
	t.Fatal("no gemini row")
}

func TestClineFallsBackToCompiledWithoutRulesTree(t *testing.T) {
	m := newTestManager(t, ".agents") // Cline detected via ~/.agents only
	initCanonical(t, m, "# Canon\n")
	if _, err := m.AddFragment("style", "Be concise.\n"); err != nil {
		t.Fatal(err)
	}
	res, err := m.SyncClient(context.Background(), "cline", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModeCompiled {
		t.Fatalf("mode = %q, want compiled (no ~/Documents/Cline tree)", res.Mode)
	}
	if _, err := os.Stat(filepath.Join(m.home, "Documents")); !os.IsNotExist(err) {
		t.Fatalf("sync created ~/Documents wholesale, err=%v", err)
	}
	body := readFile(t, filepath.Join(m.home, ".agents", "AGENTS.md"))
	if !strings.Contains(body, "Be concise.") {
		t.Fatalf("compiled fallback did not write the block: %s", body)
	}
}
