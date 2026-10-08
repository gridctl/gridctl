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
