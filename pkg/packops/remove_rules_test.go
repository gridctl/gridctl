package packops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/skills"
)

const ruleBody = "Use the Oxford comma.\n"

func addRulePack(t *testing.T, clientDirs ...string) (*Managers, *skills.Importer, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", "")
	t.Setenv("HOME", home)
	for _, dir := range clientDirs {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mgrs, imp := freshEnv(t, home)
	repo := packFixture(t, testManifest+"rules: [team-style]\n", map[string]string{
		"rules/team-style.md": ruleBody,
	})
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	mgrs, imp = freshEnv(t, home)
	return mgrs, imp, home
}

func ruleStorePath(mgrs *Managers) string {
	return filepath.Join(mgrs.Contexts.FragmentsDir(), "team-style.md")
}

func assertOneRule(t *testing.T, rows []Row, action string) Row {
	t.Helper()
	got := rowsOfKind(rows, "rule")
	if len(got) != 1 {
		t.Fatalf("rule rows = %+v, want one %s", got, action)
	}
	if got[0].Action != action || got[0].Name != "team-style" {
		t.Errorf("rule row = %+v, want action %s", got[0], action)
	}
	return got[0]
}

func assertStore(t *testing.T, path, want string, present bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if present {
		if err != nil {
			t.Errorf("store file: %v", err)
			return
		}
		if want != "" && string(data) != want {
			t.Errorf("store = %q, want %q", data, want)
		}
		return
	}
	if err == nil {
		t.Errorf("store file still present: %q", data)
		return
	}
	if !os.IsNotExist(err) {
		t.Errorf("store file: %v", err)
	}
}

func assertRuleKept(t *testing.T, mgrs *Managers) {
	t.Helper()
	locked, err := mgrs.LoadLockedPack("team-pack")
	if err != nil {
		t.Errorf("pack record: %v", err)
		return
	}
	if !containsString(locked.Rules, "team-style") {
		t.Errorf("Rules = %v, want team-style", locked.Rules)
	}
	if _, ok := locked.RuleFiles["team-style"]; !ok {
		t.Errorf("RuleFiles = %+v, want team-style", locked.RuleFiles)
	}
}

func assertPackGone(t *testing.T, mgrs *Managers) {
	t.Helper()
	_, err := mgrs.LoadLockedPack("team-pack")
	if !errors.Is(err, ErrNotImported) {
		t.Errorf("pack record = %v, want ErrNotImported", err)
	}
}

func TestRemove_RemovesNeverProjectedRule(t *testing.T) {
	mgrs, imp, _ := addRulePack(t, ".claude")
	ctx := context.Background()
	store := ruleStorePath(mgrs)

	doc, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.DryRun {
		t.Error("dry_run flag missing")
	}
	if len(doc.Kept) != 0 {
		t.Errorf("dry-run kept = %v, want empty", doc.Kept)
	}
	row := assertOneRule(t, doc.Rows, "would-remove")
	if row.Action != "would-remove" {
		t.Errorf("dry-run action = %q", row.Action)
	}
	assertStore(t, store, ruleBody, true)
	assertRuleKept(t, mgrs)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	mgrs, imp = freshEnv(t, home)
	doc, err = mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Kept) != 0 {
		t.Errorf("kept = %v, want empty", doc.Kept)
	}
	row = assertOneRule(t, doc.Rows, "removed")
	if row.Detail != "fragment store entry removed" {
		t.Errorf("detail = %q", row.Detail)
	}
	if !strings.Contains(row.Remediation, "gridctl ctx sync") {
		t.Errorf("remediation = %q, want gridctl ctx sync", row.Remediation)
	}
	assertStore(t, store, "", false)
	assertPackGone(t, mgrs)
}

func TestRemove_RemovesRuleAfterUnsync(t *testing.T) {
	mgrs, _, home := addRulePack(t, ".claude")
	ctx := context.Background()
	if _, err := mgrs.Apply(ctx, "team-pack", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	mgrs, _ = freshEnv(t, home)
	if _, err := mgrs.Contexts.UnsyncAll(ctx); err != nil {
		t.Fatal(err)
	}
	mgrs, imp := freshEnv(t, home)
	store := ruleStorePath(mgrs)

	doc, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Kept) != 0 {
		t.Errorf("kept = %v, want empty", doc.Kept)
	}
	row := assertOneRule(t, doc.Rows, "removed")
	if row.Detail != "fragment store entry removed" {
		t.Errorf("detail = %q", row.Detail)
	}
	if !strings.Contains(row.Remediation, "gridctl ctx sync") {
		t.Errorf("remediation = %q", row.Remediation)
	}
	assertStore(t, store, "", false)
	assertPackGone(t, mgrs)
}

func TestRemove_KeepsLocallyModifiedRuleUnlessForced(t *testing.T) {
	mgrs, imp, home := addRulePack(t, ".claude")
	ctx := context.Background()
	store := ruleStorePath(mgrs)
	if err := os.WriteFile(store, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	row := assertOneRule(t, doc.Rows, "skipped-drift")
	if row.Remediation == "" || !strings.Contains(row.Remediation, "gridctl ctx rm team-style") || !strings.Contains(row.Remediation, "--force") || !strings.Contains(row.Remediation, "renam") {
		t.Errorf("dry-run remediation = %q", row.Remediation)
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "rule/team-style" {
		t.Errorf("dry-run kept = %v", doc.Kept)
	}
	assertStore(t, store, "mine\n", true)

	mgrs, imp = freshEnv(t, home)
	doc, err = mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row = assertOneRule(t, doc.Rows, "skipped-drift")
	if row.Detail != "the fragment store copy differs from what the pack installed" {
		t.Errorf("detail = %q", row.Detail)
	}
	if row.Remediation == "" {
		t.Error("remediation empty")
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "rule/team-style" {
		t.Errorf("kept = %v", doc.Kept)
	}
	assertStore(t, store, "mine\n", true)
	assertRuleKept(t, mgrs)

	mgrs, imp = freshEnv(t, home)
	doc, err = mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	row = assertOneRule(t, doc.Rows, "removed")
	if row.Detail != "fragment store entry removed" {
		t.Errorf("detail = %q", row.Detail)
	}
	if len(doc.Kept) != 0 {
		t.Errorf("forced kept = %v", doc.Kept)
	}
	assertStore(t, store, "", false)
	assertPackGone(t, mgrs)
	backupDir := filepath.Join(home, ".gridctl", "project-backups", "context", "canonical", "team-style")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("backup dir: %v", err)
	}
	var sawBackup bool
	for _, entry := range entries {
		data, rerr := os.ReadFile(filepath.Join(backupDir, entry.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if string(data) == "mine\n" {
			sawBackup = true
		}
	}
	if !sawBackup {
		t.Errorf("backup of the local edit not found in %s", backupDir)
	}
}

func TestRemove_KeepsRuleWithEmptyProvenanceUnlessForced(t *testing.T) {
	mgrs, _, home := addRulePack(t, ".claude")
	ctx := context.Background()
	store := ruleStorePath(mgrs)
	if err := skills.MutateLockFile(ctx, skills.LockFilePath(), func(lf *skills.LockFile) (bool, error) {
		srcName, src, ok := lf.FindPackSource("team-pack")
		if !ok || src.Pack == nil {
			return false, errors.New("pack record missing")
		}
		if src.Pack.RuleFiles == nil {
			src.Pack.RuleFiles = map[string]skills.LockedRule{}
		}
		src.Pack.RuleFiles["team-style"] = skills.LockedRule{Path: "rules/team-style.md"}
		lf.SetSource(srcName, *src)
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	mgrs, imp := freshEnv(t, home)
	doc, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := assertOneRule(t, doc.Rows, "skipped-drift")
	if !strings.Contains(row.Remediation, "gridctl pack add") || !strings.Contains(row.Remediation, "provenance") || !strings.Contains(row.Remediation, "--force") {
		t.Errorf("provenance remediation = %q", row.Remediation)
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "rule/team-style" {
		t.Errorf("kept = %v", doc.Kept)
	}
	assertStore(t, store, ruleBody, true)
	assertRuleKept(t, mgrs)
	locked, err := mgrs.LoadLockedPack("team-pack")
	if err != nil {
		t.Fatal(err)
	}
	if locked.RuleFiles["team-style"].ContentHash != "" {
		t.Errorf("hash = %q, want empty", locked.RuleFiles["team-style"].ContentHash)
	}

	mgrs, imp = freshEnv(t, home)
	doc, err = mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	assertOneRule(t, doc.Rows, "removed")
	assertStore(t, store, "", false)
	assertPackGone(t, mgrs)
}

func TestRemove_KeepsUnreadableRule(t *testing.T) {
	mgrs, imp, home := addRulePack(t, ".claude")
	ctx := context.Background()
	store := ruleStorePath(mgrs)
	broken := []byte("---\ndescription: [\n---\nbody\n")
	if err := os.WriteFile(store, broken, 0o644); err != nil {
		t.Fatal(err)
	}

	check := func(opts RemoveOptions) {
		t.Helper()
		doc, err := mgrs.Remove(ctx, imp, "team-pack", opts)
		if err != nil {
			t.Fatal(err)
		}
		row := assertOneRule(t, doc.Rows, "error")
		if row.Detail == "" {
			t.Error("error detail empty")
		}
		if row.Remediation == "" || !strings.Contains(row.Remediation, "gridctl ctx rm team-style") || !strings.Contains(row.Remediation, "gridctl pack remove team-pack") {
			t.Errorf("remediation = %q", row.Remediation)
		}
		if len(doc.Kept) != 1 || doc.Kept[0] != "rule/team-style" {
			t.Errorf("kept = %v", doc.Kept)
		}
		assertStore(t, store, string(broken), true)
		assertRuleKept(t, mgrs)
	}

	check(RemoveOptions{DryRun: true})
	mgrs, imp = freshEnv(t, home)
	check(RemoveOptions{})
	mgrs, imp = freshEnv(t, home)
	check(RemoveOptions{Force: true})
}

func TestRemove_NilContextsEmitsNoRuleRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", "")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgrs, imp := freshEnv(t, home)
	lockPath := skills.LockFilePath()
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"team-pack": {
			Repo: "https://example.com/team-pack.git",
			Pack: &skills.LockedPack{Name: "team-pack", Rules: []string{"team-style"}},
		},
	}}
	if err := skills.WriteLockFile(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	// Skills, agents, and wiring stay set: Remove always asks them for
	// drift and wiring status. Contexts is the nil the guard protects.
	mgrs.Contexts = nil

	doc, err := mgrs.Remove(context.Background(), imp, "team-pack", RemoveOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if rows := rowsOfKind(doc.Rows, "rule"); len(rows) != 0 {
		t.Errorf("dry-run rule rows = %+v", rows)
	}

	mgrs, imp = freshEnv(t, home)
	mgrs.Contexts = nil
	doc, err = mgrs.Remove(context.Background(), imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rows := rowsOfKind(doc.Rows, "rule"); len(rows) != 0 {
		t.Errorf("rule rows = %+v", rows)
	}
}

func TestRemove_CompiledClientKeepsDocumentUntilSync(t *testing.T) {
	// Only ~/.agents, not ~/Documents/Cline, so Cline is detected and
	// falls back to the compiled block at ~/.agents/AGENTS.md.
	mgrs, _, home := addRulePack(t, ".agents")
	ctx := context.Background()
	// A second fragment keeps the store non-empty after the pack rule
	// is removed. ctx sync refuses an empty fragments directory, and
	// that refusal is outside this fix.
	if _, err := mgrs.Contexts.AddFragment("local-note", "Local note.\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgrs.Apply(ctx, "team-pack", ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	compiled := filepath.Join(home, ".agents", "AGENTS.md")
	before, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), "Use the Oxford comma.") {
		t.Fatalf("compiled document missing rule body:\n%s", before)
	}

	mgrs, imp := freshEnv(t, home)
	store := ruleStorePath(mgrs)
	doc, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := assertOneRule(t, doc.Rows, "removed")
	if !strings.Contains(row.Remediation, "gridctl ctx sync") {
		t.Errorf("remediation = %q", row.Remediation)
	}
	assertStore(t, store, "", false)
	after, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "Use the Oxford comma.") {
		t.Fatalf("compiled document dropped the rule before ctx sync:\n%s", after)
	}

	if _, err := mgrs.Contexts.SyncAll(ctx, contexts.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	synced, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(synced), "Use the Oxford comma.") {
		t.Fatalf("ctx sync left the rule in the compiled document:\n%s", synced)
	}
	if !strings.Contains(string(synced), "Local note.") {
		t.Fatalf("ctx sync dropped the unrelated fragment:\n%s", synced)
	}
}

func TestRemove_TrimsDroppedRuleFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", "")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mgrs, imp := freshEnv(t, home)
	repo := packFixture(t, testManifest+"rules: [team-style, other-style]\n", map[string]string{
		"rules/team-style.md":  ruleBody,
		"rules/other-style.md": "Prefer table-driven tests.\n",
	})
	ctx := context.Background()
	if _, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	mgrs, imp = freshEnv(t, home)
	edited := filepath.Join(mgrs.Contexts.FragmentsDir(), "team-style.md")
	if err := os.WriteFile(edited, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(mgrs.Contexts.FragmentsDir(), "other-style.md")

	doc, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Row{}
	for _, row := range rowsOfKind(doc.Rows, "rule") {
		byName[row.Name] = row
	}
	if byName["team-style"].Action != "skipped-drift" || byName["other-style"].Action != "removed" {
		t.Errorf("rule rows = %+v", rowsOfKind(doc.Rows, "rule"))
	}
	if len(doc.Kept) != 1 || doc.Kept[0] != "rule/team-style" {
		t.Errorf("kept = %v", doc.Kept)
	}
	assertStore(t, edited, "mine\n", true)
	assertStore(t, other, "", false)
	locked, err := mgrs.LoadLockedPack("team-pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(locked.Rules) != 1 || locked.Rules[0] != "team-style" {
		t.Errorf("Rules = %v", locked.Rules)
	}
	if _, ok := locked.RuleFiles["team-style"]; !ok {
		t.Errorf("RuleFiles missing team-style: %+v", locked.RuleFiles)
	}
	if _, ok := locked.RuleFiles["other-style"]; ok {
		t.Errorf("RuleFiles still lists other-style: %+v", locked.RuleFiles)
	}
}
