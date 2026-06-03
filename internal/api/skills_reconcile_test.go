package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// seedDriftedSkill writes a skill, a lock-file source entry, and an origin
// sidecar whose InstalledHash deliberately mismatches the on-disk file so
// DetectDrift reports the skill as locally edited. No git repo involved.
func seedDriftedSkill(t *testing.T, srv *Server, regServer *registry.Server, sourceName, repo, skillName string) string {
	t.Helper()
	seedSkill(t, regServer, skillName, registry.StateActive)
	seedSkillSource(t, srv, sourceName, repo, skillName)

	store := regServer.Store()
	skillDir := filepath.Join(store.Dir(), "skills", skillName)
	origin := &skills.Origin{
		Repo:          repo,
		Ref:           "main",
		Path:          skillName,
		CommitSHA:     "abc1234567890abc1234567890abc1234567890a",
		ImportedAt:    time.Now().UTC(),
		InstalledHash: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	if err := skills.WriteOrigin(skillDir, origin); err != nil {
		t.Fatalf("write origin: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	return string(data)
}

func TestHandleSkillSourcesList_ReportsDrift(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedDriftedSkill(t, srv, regServer, "my-source", "https://github.com/org/repo", "drifted-skill")

	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result []SkillSourceStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result))
	}
	if len(result[0].DriftedSkills) != 1 || result[0].DriftedSkills[0] != "drifted-skill" {
		t.Errorf("expected driftedSkills=[drifted-skill], got %v", result[0].DriftedSkills)
	}
	if len(result[0].Skills) != 1 || !result[0].Skills[0].HasLocalEdits {
		t.Errorf("expected skill entry to report hasLocalEdits=true, got %+v", result[0].Skills)
	}
}

func TestHandleSkillSourceUpdate_SkipsDriftedByDefault(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	original := seedDriftedSkill(t, srv, regServer, "my-source", "https://github.com/org/repo", "drifted-skill")

	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/my-source/update", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []SkillSyncResult `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Skipped != "local edits" {
		t.Errorf("expected skipped=%q, got %+v", "local edits", resp.Results[0])
	}

	// The on-disk SKILL.md must not have been overwritten.
	skillDir := filepath.Join(regServer.Store().Dir(), "skills", "drifted-skill")
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if string(data) != original {
		t.Errorf("drifted skill was overwritten by a default sync")
	}
}

func TestHandleSkillDetach(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedDriftedSkill(t, srv, regServer, "my-source", "https://github.com/org/repo", "drifted-skill")

	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/my-source/skills/drifted-skill/detach", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	skillDir := filepath.Join(regServer.Store().Dir(), "skills", "drifted-skill")
	if skills.HasOrigin(skillDir) {
		t.Errorf("origin sidecar should be gone after detach")
	}
	lf, err := skills.ReadLockFile(srv.lockFilePath())
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, _, found := lf.FindSkillSource("drifted-skill"); found {
		t.Errorf("lock entry should be gone after detach")
	}
	if _, err := regServer.Store().GetSkill("drifted-skill"); err != nil {
		t.Errorf("skill should remain in the registry after detach: %v", err)
	}
}

// importLocalSkill initializes a local git repo with one SKILL.md and imports
// it into the server's registry + lock file, returning the repo for follow-up
// commits.
func importLocalSkill(t *testing.T, srv *Server, regServer *registry.Server, body string) (string, *git.Repository) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	content := "---\nname: repo-skill\ndescription: imported\nstate: active\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	commitWorktree(t, repo, "initial")

	store := regServer.Store()
	imp := skills.NewImporter(store, store.Dir(), srv.lockFilePath(), slog.Default())
	if _, err := imp.Import(skills.ImportOptions{Repo: dir, Ref: "master", Trust: true}); err != nil {
		t.Fatalf("import: %v", err)
	}
	return dir, repo
}

func commitWorktree(t *testing.T, repo *git.Repository, msg string) {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if _, err := wt.Add("SKILL.md"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com"},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestHandleSkillSourceUpdate_ForceBacksUpAndOverwrites(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, regServer := setupRegistryTestServer(t)
	dir, _ := importLocalSkill(t, srv, regServer, "# Repo skill\n\nupstream body.\n")

	// Local edit → drift.
	store := regServer.Store()
	skillDir := filepath.Join(store.Dir(), "skills", "repo-skill")
	skillPath := filepath.Join(skillDir, "SKILL.md")
	edited, _ := os.ReadFile(skillPath)
	editedStr := string(edited) + "\n\nLOCAL EDIT MARKER\n"
	if err := os.WriteFile(skillPath, []byte(editedStr), 0644); err != nil {
		t.Fatalf("edit skill: %v", err)
	}
	sourceName := skills.RepoToName(dir)

	body := strings.NewReader(`{"force":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/"+sourceName+"/update", body)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// A backup of the pre-overwrite content must exist next to the skill.
	entries, _ := os.ReadDir(skillDir)
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "SKILL.md.pre-") {
			backup = e.Name()
		}
	}
	if backup == "" {
		t.Fatalf("expected a SKILL.md.pre-* backup, dir has: %v", entries)
	}
	backupData, _ := os.ReadFile(filepath.Join(skillDir, backup))
	if !strings.Contains(string(backupData), "LOCAL EDIT MARKER") {
		t.Errorf("backup should contain the pre-overwrite local edit")
	}

	// The live SKILL.md must have been overwritten (local marker gone).
	after, _ := os.ReadFile(skillPath)
	if strings.Contains(string(after), "LOCAL EDIT MARKER") {
		t.Errorf("force sync should overwrite the local edit")
	}
}

func TestHandleSkillDiff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv, regServer := setupRegistryTestServer(t)
	dir, repo := importLocalSkill(t, srv, regServer, "# Repo skill\n\nv1 body.\n")

	// Local edit + an upstream commit so both sides differ.
	store := regServer.Store()
	skillPath := filepath.Join(store.Dir(), "skills", "repo-skill", "SKILL.md")
	edited, _ := os.ReadFile(skillPath)
	if err := os.WriteFile(skillPath, []byte(string(edited)+"\n\nLOCAL NOTE\n"), 0644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: repo-skill\ndescription: imported v2\nstate: active\n---\n\n# Repo skill\n\nv2 body.\n"), 0644); err != nil {
		t.Fatalf("upstream edit: %v", err)
	}
	commitWorktree(t, repo, "v2")

	sourceName := skills.RepoToName(dir)
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources/"+sourceName+"/skills/repo-skill/diff", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var diff SkillDiffResponse
	if err := json.NewDecoder(rec.Body).Decode(&diff); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !diff.Drifted {
		t.Errorf("expected drifted=true")
	}
	if !strings.Contains(diff.Local, "LOCAL NOTE") {
		t.Errorf("local side should contain the local edit")
	}
	if !strings.Contains(diff.Upstream, "v2 body.") {
		t.Errorf("upstream side should contain the latest commit, got: %q", diff.Upstream)
	}
	if diff.UnifiedDiff == "" {
		t.Errorf("expected a non-empty unified diff")
	}

	// Diff must not have mutated the on-disk file.
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("skill missing after diff: %v", err)
	}
}
