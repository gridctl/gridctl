package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	"github.com/gridctl/gridctl/pkg/skills"
)

func TestHandleSkillSourceAdd_LocalPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, _ := setupRegistryTestServer(t)

	root := filepath.Join(home, "src", "alpha")
	requireMkdirSkill(t, root)

	req := loopbackRequest(http.MethodPost, "/api/skills/sources", strings.NewReader(`{"repo":"`+root+`","trust":true}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	listReq := loopbackRequest(http.MethodGet, "/api/skills/sources", nil)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", listRec.Code, listRec.Body.String())
	}
	var sources []SkillSourceStatus
	if err := json.Unmarshal(listRec.Body.Bytes(), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Kind != "local" {
		t.Fatalf("sources = %+v", sources)
	}
}

func TestHandleSkillSourceAdd_RejectsRelativeAndHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	if _, _, code, _ := classifySkillSourceRepo("."); code != http.StatusBadRequest {
		t.Fatalf("relative directory status = %d", code)
	}
	inside := filepath.Join(home, ".gridctl", "copied")
	requireMkdirSkill(t, inside)
	if _, _, code, msg := classifySkillSourceRepo(inside); code != http.StatusBadRequest || !strings.Contains(msg, "gridctl home") {
		t.Fatalf("home path status=%d msg=%s", code, msg)
	}
}

func TestHandleSkillSourceUpdate_DriftedLocalDoesNotAdvance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, reg := setupRegistryTestServer(t)
	root := filepath.Join(home, "src", "alpha")
	requireMkdirSkill(t, root)
	imp := skills.NewImporter(reg.Store(), reg.Store().Dir(), srv.lockFilePath(), slog.Default())
	if _, err := imp.Import(context.Background(), skills.ImportOptions{Repo: root, Kind: skills.SourceKindLocal, Trust: true}); err != nil {
		t.Fatal(err)
	}
	before, err := skills.ReadLockFile(srv.lockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	prev := before.Sources["alpha"].ContentHash
	skillFile := filepath.Join(reg.Store().Dir(), "skills", "alpha", "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: alpha\ndescription: edited\n---\n\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := loopbackRequest(http.MethodPost, "/api/skills/sources/alpha/update", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"skipped":"local edits"`) && !strings.Contains(rec.Body.String(), `"skipped": "local edits"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	after, err := skills.ReadLockFile(srv.lockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if after.Sources["alpha"].ContentHash != prev || after.Sources["alpha"].CommitSHA != "" {
		t.Fatalf("lock changed: %+v", after.Sources["alpha"])
	}
}

func TestHandleSkillSourceAdd_KnownLocationName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, _ := setupRegistryTestServer(t)
	root := filepath.Join(home, ".claude", "skills", "pcap")
	requireMkdirSkill(t, root)

	req := loopbackRequest(http.MethodPost, "/api/skills/sources", strings.NewReader(`{"repo":"`+filepath.Join(home, ".claude", "skills")+`","trust":true}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	listReq := loopbackRequest(http.MethodGet, "/api/skills/sources", nil)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	var sources []SkillSourceStatus
	if err := json.Unmarshal(listRec.Body.Bytes(), &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "claude-code" || sources[0].Kind != "local" {
		t.Fatalf("sources = %+v", sources)
	}
}

func TestHandleSkillSourceAdd_ConflictAndEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, reg := setupRegistryTestServer(t)
	first := filepath.Join(home, "a", "skills")
	requireMkdirSkill(t, first)
	imp := skills.NewImporter(reg.Store(), reg.Store().Dir(), srv.lockFilePath(), slog.Default())
	if _, err := imp.Import(context.Background(), skills.ImportOptions{Repo: first, Kind: skills.SourceKindLocal, Trust: true}); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(home, "b", "skills", "other")
	requireMkdirSkillNamed(t, second, "other")
	req := loopbackRequest(http.MethodPost, "/api/skills/sources", strings.NewReader(`{"repo":"`+filepath.Dir(second)+`","trust":true}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body %s", rec.Code, rec.Body.String())
	}

	empty := filepath.Join(home, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyReq := loopbackRequest(http.MethodPost, "/api/skills/sources", strings.NewReader(`{"repo":"`+empty+`","trust":true}`))
	emptyRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(emptyRec, emptyReq)
	if emptyRec.Code != http.StatusBadRequest {
		t.Fatalf("empty status = %d body %s", emptyRec.Code, emptyRec.Body.String())
	}
	if strings.Contains(emptyRec.Body.String(), "Import failed") {
		t.Fatalf("local caller error used the git mapper: %s", emptyRec.Body.String())
	}
}

func TestHandleSkillSource_LocalCheckPreviewDiffUpdates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, reg := setupRegistryTestServer(t)
	root := filepath.Join(home, "src", "alpha")
	requireMkdirSkill(t, root)
	imp := skills.NewImporter(reg.Store(), reg.Store().Dir(), srv.lockFilePath(), slog.Default())
	if _, err := imp.Import(context.Background(), skills.ImportOptions{Repo: root, Kind: skills.SourceKindLocal, Trust: true}); err != nil {
		t.Fatal(err)
	}

	check := loopbackRequest(http.MethodPost, "/api/skills/sources/alpha/check", strings.NewReader(`{}`))
	checkRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(checkRec, check)
	if checkRec.Code != http.StatusOK || !strings.Contains(checkRec.Body.String(), `"hasUpdate":false`) {
		t.Fatalf("check: %d %s", checkRec.Code, checkRec.Body.String())
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: alpha\ndescription: Alpha skill\n---\n\nChanged.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check = loopbackRequest(http.MethodPost, "/api/skills/sources/alpha/check", strings.NewReader(`{}`))
	checkRec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(checkRec, check)
	if checkRec.Code != http.StatusOK || !strings.Contains(checkRec.Body.String(), `"hasUpdate":true`) {
		t.Fatalf("changed check: %d %s", checkRec.Code, checkRec.Body.String())
	}

	updates := loopbackRequest(http.MethodGet, "/api/skills/updates", nil)
	updatesRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(updatesRec, updates)
	if updatesRec.Code != http.StatusOK || !strings.Contains(updatesRec.Body.String(), `"hasUpdate":true`) {
		t.Fatalf("updates: %d %s", updatesRec.Code, updatesRec.Body.String())
	}

	preview := loopbackRequest(http.MethodGet, "/api/skills/sources/alpha/preview", nil)
	previewRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(previewRec, preview)
	if previewRec.Code != http.StatusOK || !strings.Contains(previewRec.Body.String(), "alpha") {
		t.Fatalf("preview: %d %s", previewRec.Code, previewRec.Body.String())
	}

	diff := loopbackRequest(http.MethodGet, "/api/skills/sources/alpha/skills/alpha/diff", nil)
	diffRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(diffRec, diff)
	if diffRec.Code != http.StatusOK || !strings.Contains(diffRec.Body.String(), `"upstream"`) {
		t.Fatalf("diff: %d %s", diffRec.Code, diffRec.Body.String())
	}
}

func TestHandleSkillSourcesSyncAll_RefreshesLocalAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	srv, reg := setupRegistryTestServer(t)
	root := filepath.Join(home, "src")
	requireMkdirSkill(t, filepath.Join(root, "alpha"))
	agentDir := filepath.Join(root, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := "---\nname: reviewer\ndescription: Reviews things\n---\n\nReview.\n"
	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"), []byte(agent), 0o644); err != nil {
		t.Fatal(err)
	}
	imp := skills.NewImporter(reg.Store(), reg.Store().Dir(), srv.lockFilePath(), slog.Default())
	if _, err := imp.Import(context.Background(), skills.ImportOptions{Repo: root, Kind: skills.SourceKindLocal, Trust: true}); err != nil {
		t.Fatal(err)
	}
	changed := "---\nname: reviewer\ndescription: Reviews things\n---\n\nReview again.\n"
	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	req := loopbackRequest(http.MethodPost, "/api/skills/sources/update", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync-all: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "no longer in registry") {
		t.Fatalf("agent treated as ghost: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"importedAgents":1`) && !strings.Contains(rec.Body.String(), `"importedAgents": 1`) {
		t.Fatalf("agent was not refreshed: %s", rec.Body.String())
	}
}

func requireMkdirSkill(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: alpha\ndescription: Alpha skill\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireMkdirSkillNamed(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
