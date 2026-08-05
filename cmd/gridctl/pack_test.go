package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

const packTestManifest = `apiVersion: gridctl.dev/v1alpha1
kind: Pack
name: team-pack
version: 1.0.0
skills: [alpha]
agents: [reviewer]
wiring: true
`

// packFixture builds a local git repo shaped like a pack.
func packFixture(t *testing.T, manifest string, extra map[string]string) string {
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
		"gridctl-pack.yaml":     manifest,
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: Test skill\n---\n\nDo alpha.\n",
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: Test skill\n---\n\nDo beta.\n",
		"agents/reviewer.md":    "---\nname: reviewer\ndescription: Reviews\n---\n\nReview.\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
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
	return dir
}

// packTestEnv sandboxes HOME (with a detected ~/.claude so agent
// projection has one available target) and returns the cmd-layer
// helpers rooted in it.
func packTestEnv(t *testing.T) (*skills.Importer, func() *packops.Managers) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	imp := newImporter(store)
	// Managers snapshot the registry at construction (exactly as each
	// CLI invocation does), so tests build them fresh per step.
	freshManagers := func() *packops.Managers {
		t.Helper()
		mgrs, err := newPackManagers()
		if err != nil {
			t.Fatal(err)
		}
		return mgrs
	}
	return imp, freshManagers
}

func TestPackAddApplyStatusRemove_EndToEnd(t *testing.T) {
	imp, freshManagers := packTestEnv(t)
	ctx := context.Background()
	repo := packFixture(t, packTestManifest, nil)
	var stdout, stderr bytes.Buffer

	// Add: imports exactly the selection.
	if exit := runPackAdd(ctx, &stdout, &stderr, freshManagers(), imp, repo, "", false, false, "text"); exit != ctxExitOK {
		t.Fatalf("add exit = %d\n%s%s", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `Imported pack "team-pack" (1 skills, 1 agents, wiring: yes)`) {
		t.Errorf("add summary:\n%s", stdout.String())
	}
	locked, err := loadLockedPack("team-pack")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(locked.Skills, ",") != "alpha" || strings.Join(locked.Agents, ",") != "reviewer" || !locked.Wiring {
		t.Errorf("locked pack = %+v", locked)
	}

	// beta was discovered but not selected: must not be imported.
	if _, err := imp.Info("beta"); err == nil {
		t.Error("unselected skill beta was imported")
	}

	// Apply: skills + agents project (wiring skips, no gateway) with
	// pack tags; exit 1 because the wiring row needs attention.
	stdout.Reset()
	exit := runPackApply(ctx, &stdout, &stderr, freshManagers(), "team-pack", false, false, nil, "json", false)
	if exit != ctxExitAttention {
		t.Fatalf("apply exit = %d\n%s%s", exit, stdout.String(), stderr.String())
	}
	var applyDoc packops.ApplyDoc
	if err := json.Unmarshal(stdout.Bytes(), &applyDoc); err != nil {
		t.Fatal(err)
	}
	sawWiringSkip := false
	for _, r := range applyDoc.Rows {
		if r.Kind == "wiring" && strings.Contains(r.Detail, "no running gateway") {
			sawWiringSkip = true
		}
	}
	if !sawWiringSkip {
		t.Errorf("apply rows lack the no-gateway wiring skip: %+v", applyDoc.Rows)
	}

	// Pack tags stamped on the projections.
	home, _ := os.UserHomeDir()
	l, err := project.NewStore(home).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []project.Kind{project.KindSkill, project.KindAgent} {
		entries := l.Entries(kind)
		if len(entries) == 0 {
			t.Fatalf("no %s entries recorded", kind)
		}
		for _, e := range entries {
			if e.Pack != "team-pack" {
				t.Errorf("%s %s pack tag = %q", kind, e.Source, e.Pack)
			}
		}
	}

	// Status: rows for each kind; wiring not yet recorded so only
	// skills/agents rows appear; all in-sync → exit 0.
	stdout.Reset()
	if exit := runPackStatus(ctx, &stdout, &stderr, freshManagers(), "team-pack", "json", false); exit != ctxExitOK {
		t.Fatalf("status exit = %d\n%s%s", exit, stdout.String(), stderr.String())
	}
	var statusDoc packStatusDoc
	if err := json.Unmarshal(stdout.Bytes(), &statusDoc); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, r := range statusDoc.Rows {
		kinds[r.Kind] = true
		if r.State != "in-sync" {
			t.Errorf("row %+v not in-sync", r)
		}
	}
	if !kinds["skill"] || !kinds["agent"] {
		t.Errorf("status rows missing kinds: %+v", statusDoc.Rows)
	}

	// Remove: cascade — projections unsynced, registry entries gone,
	// pack record dropped.
	stdout.Reset()
	if exit := runPackRemove(ctx, &stdout, &stderr, freshManagers(), imp, "team-pack", false, false, "text"); exit != ctxExitOK {
		t.Fatalf("remove exit = %d\n%s%s", exit, stdout.String(), stderr.String())
	}
	if _, err := loadLockedPack("team-pack"); err == nil {
		t.Error("pack record survived removal")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Error("skill projection survived removal")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "agents", "reviewer.md")); !os.IsNotExist(err) {
		t.Error("agent projection survived removal")
	}
	l, _ = project.NewStore(home).Load(ctx)
	if n := len(l.Entries(project.KindSkill)) + len(l.Entries(project.KindAgent)); n != 0 {
		t.Errorf("%d projection records survived removal", n)
	}
}

func TestPackAdd_UnresolvedSelection(t *testing.T) {
	imp, freshManagers := packTestEnv(t)
	manifest := strings.Replace(packTestManifest, "skills: [alpha]", "skills: [alpha, ghost]", 1)
	repo := packFixture(t, manifest, nil)
	var stdout, stderr bytes.Buffer

	exit := runPackAdd(context.Background(), &stdout, &stderr, freshManagers(), imp, repo, "", false, false, "text")
	if exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1\n%s%s", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `Unresolved: pack selects "ghost"`) {
		t.Errorf("output lacks unresolved report:\n%s", stdout.String())
	}
	locked, err := loadLockedPack("team-pack")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(locked.Unresolved, ",") != "ghost" {
		t.Errorf("unresolved = %v", locked.Unresolved)
	}
}

func TestPackAdd_NoManifestRefuses(t *testing.T) {
	imp, freshManagers := packTestEnv(t)
	repo := initRepoNoManifest(t)
	var stdout, stderr bytes.Buffer

	exit := runPackAdd(context.Background(), &stdout, &stderr, freshManagers(), imp, repo, "", false, false, "text")
	if exit != ctxExitInfrastructure {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if !strings.Contains(stderr.String(), "gridctl-pack.yaml") {
		t.Errorf("stderr lacks manifest hint: %s", stderr.String())
	}
	// Nothing half-imported.
	if _, err := imp.Info("alpha"); err == nil {
		t.Error("manifest-less repo still imported resources")
	}
}

// initRepoNoManifest builds a repo with skills but no pack manifest.
func initRepoNoManifest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	full := filepath.Join(dir, "skills/alpha/SKILL.md")
	_ = os.MkdirAll(filepath.Dir(full), 0o755)
	_ = os.WriteFile(full, []byte("---\nname: alpha\ndescription: d\n---\n\nBody.\n"), 0o644)
	_, _ = wt.Add("skills/alpha/SKILL.md")
	_, _ = wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t"}})
	return dir
}

func TestPackApply_ForeignPackRefusal(t *testing.T) {
	imp, freshManagers := packTestEnv(t)
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	// Import and apply pack A owning skill alpha.
	repoA := packFixture(t, packTestManifest, nil)
	if exit := runPackAdd(ctx, &stdout, &stderr, freshManagers(), imp, repoA, "", false, false, "text"); exit != ctxExitOK {
		t.Fatal(stderr.String())
	}
	if exit := runPackApply(ctx, &stdout, &stderr, freshManagers(), "team-pack", false, false, nil, "text", true); exit == ctxExitInfrastructure {
		t.Fatal(stderr.String())
	}

	// Pack B selects the same skill name from its own repo.
	manifestB := strings.Replace(packTestManifest, "name: team-pack", "name: other-pack", 1)
	manifestB = strings.Replace(manifestB, "wiring: true", "wiring: false", 1)
	repoB := packFixture(t, manifestB, nil)
	stdout.Reset()
	stderr.Reset()
	// Add B: alpha already exists; explicit selection overwrites in the
	// registry (selected-implies-force), which is the import contract.
	if exit := runPackAdd(ctx, &stdout, &stderr, freshManagers(), imp, repoB, "", false, false, "text"); exit == ctxExitInfrastructure {
		t.Fatal(stderr.String())
	}

	stdout.Reset()
	exit := runPackApply(ctx, &stdout, &stderr, freshManagers(), "other-pack", false, false, nil, "json", false)
	if exit != ctxExitAttention {
		t.Fatalf("apply exit = %d, want 1 (foreign refusal)\n%s", exit, stdout.String())
	}
	var doc packops.ApplyDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	foreign := 0
	for _, r := range doc.Rows {
		if r.Action == "skipped-foreign-pack" {
			foreign++
			if !strings.Contains(r.Detail, "team-pack") {
				t.Errorf("foreign detail lacks owner: %+v", r)
			}
		}
	}
	if foreign == 0 {
		t.Errorf("no foreign-pack refusals in rows: %+v", doc.Rows)
	}
}

func TestPackRemove_DriftedResourceKept(t *testing.T) {
	imp, freshManagers := packTestEnv(t)
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	repo := packFixture(t, strings.Replace(packTestManifest, "wiring: true", "wiring: false", 1), nil)

	if exit := runPackAdd(ctx, &stdout, &stderr, freshManagers(), imp, repo, "", false, false, "text"); exit != ctxExitOK {
		t.Fatal(stderr.String())
	}
	if exit := runPackApply(ctx, &stdout, &stderr, freshManagers(), "team-pack", false, false, nil, "text", true); exit != ctxExitOK {
		t.Fatalf("apply: %s%s", stdout.String(), stderr.String())
	}

	// Hand-edit the projected agent file.
	home, _ := os.UserHomeDir()
	dest := filepath.Join(home, ".claude", "agents", "reviewer.md")
	if err := os.WriteFile(dest, []byte("---\nname: reviewer\ndescription: mine\n---\n\nEdited.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	exit := runPackRemove(ctx, &stdout, &stderr, freshManagers(), imp, "team-pack", false, false, "text")
	if exit != ctxExitAttention {
		t.Fatalf("remove exit = %d, want 1\n%s%s", exit, stdout.String(), stderr.String())
	}
	// The drifted agent survives everywhere; the skill is fully removed.
	if _, err := os.Stat(dest); err != nil {
		t.Error("drifted agent projection was removed without --force")
	}
	if !strings.Contains(stdout.String(), "agent/reviewer") {
		t.Errorf("kept list missing drifted agent:\n%s", stdout.String())
	}
	locked, err := loadLockedPack("team-pack")
	if err != nil {
		t.Fatal("pack record must survive partial removal")
	}
	if len(locked.Skills) != 0 || strings.Join(locked.Agents, ",") != "reviewer" {
		t.Errorf("trimmed pack = %+v", locked)
	}

	// Force finishes the job.
	stdout.Reset()
	if exit := runPackRemove(ctx, &stdout, &stderr, freshManagers(), imp, "team-pack", true, false, "text"); exit != ctxExitOK {
		t.Fatalf("forced remove exit = %d\n%s%s", exit, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("forced remove left the agent projection")
	}
}

func TestPackAdd_FullyUnresolvedImportsNothing(t *testing.T) {
	// H3 regression: a manifest whose whole selection is unresolved must
	// not degrade to import-everything.
	imp, freshManagers := packTestEnv(t)
	manifest := strings.Replace(packTestManifest, "skills: [alpha]", "skills: [ghost]", 1)
	manifest = strings.Replace(manifest, "agents: [reviewer]", "agents: [phantom]", 1)
	repo := packFixture(t, manifest, nil)
	var stdout, stderr bytes.Buffer

	exit := runPackAdd(context.Background(), &stdout, &stderr, freshManagers(), imp, repo, "", false, false, "text")
	if exit != ctxExitAttention {
		t.Fatalf("exit = %d, want 1\n%s%s", exit, stdout.String(), stderr.String())
	}
	if _, err := imp.Info("alpha"); err == nil {
		t.Error("unselected skill imported despite fully unresolved selection")
	}
	locked, err := loadLockedPack("team-pack")
	if err != nil {
		t.Fatal("pack record must exist for unresolved packs so status reports them")
	}
	if len(locked.Skills) != 0 || len(locked.Unresolved) != 2 {
		t.Errorf("locked = %+v", locked)
	}
}

func TestPackAdd_SkillAddSourceKeepsItsIdentity(t *testing.T) {
	// H2 regression: a pack must tag its own source, never a different
	// source that happens to hold same-named resources.
	imp, freshManagers := packTestEnv(t)
	ctx := context.Background()
	var stdout, stderr bytes.Buffer

	// Plain skill repo imported first: source X holds alpha.
	plainRepo := initRepoNoManifest(t)
	if _, err := imp.Import(skills.ImportOptions{Repo: plainRepo}); err != nil {
		t.Fatal(err)
	}

	// Pack repo also ships a skill named alpha.
	packRepo := packFixture(t, strings.Replace(packTestManifest, "wiring: true", "wiring: false", 1), nil)
	if exit := runPackAdd(ctx, &stdout, &stderr, freshManagers(), imp, packRepo, "", false, false, "text"); exit == ctxExitInfrastructure {
		t.Fatal(stderr.String())
	}

	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	srcName, _, ok := lf.FindPackSource("team-pack")
	if !ok {
		t.Fatal("pack record missing")
	}
	if srcName != skills.RepoToName(packRepo) {
		t.Errorf("pack tagged source %q, want its own %q", srcName, skills.RepoToName(packRepo))
	}
	if lf.Sources[skills.RepoToName(plainRepo)].Pack != nil {
		t.Error("plain source acquired a pack record")
	}
}
