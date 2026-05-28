package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// --- Skills Sources Endpoints ---

// seedSkillSource writes a minimal lock-file entry so handlers can iterate a
// source without going through the importer (which would clone a real repo).
func seedSkillSource(t *testing.T, srv *Server, sourceName, repo string, skillNames ...string) {
	t.Helper()
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{}}
	locked := skills.LockedSource{
		Repo:      repo,
		Ref:       "main",
		CommitSHA: "abc1234567890abc1234567890abc1234567890a",
		FetchedAt: time.Now().UTC(),
		Skills:    map[string]skills.LockedSkill{},
	}
	for _, name := range skillNames {
		locked.Skills[name] = skills.LockedSkill{Path: name, ContentHash: "h"}
	}
	lf.Sources[sourceName] = locked
	if err := skills.WriteLockFile(srv.lockFilePath(), lf); err != nil {
		t.Fatalf("write lock: %v", err)
	}
}

func TestHandleSkills_SourcesList_PopulatesUpdateAvailFromCache(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "skill-a", registry.StateActive)
	seedSkillSource(t, srv, "my-source", "https://github.com/org/repo", "skill-a")

	// Seed the update cache: skill-a has a pending update.
	status := &skills.UpdateStatus{
		CheckedAt: time.Now().UTC(),
		Updates: map[string]skills.SkillUpdate{
			"skill-a": {CurrentSHA: "old", LatestSHA: "new", Repo: "https://github.com/org/repo", Ref: "main"},
		},
	}
	if err := skills.WriteUpdateCacheAt(srv.updateCachePath(), status); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

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
	if !result[0].UpdateAvail {
		t.Errorf("expected updateAvailable=true for source with pending skill update")
	}
}

func TestHandleSkills_SourcesList_MissingCacheFailsOpen(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "skill-a", registry.StateActive)
	seedSkillSource(t, srv, "my-source", "https://github.com/org/repo", "skill-a")
	// No cache file written.

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result []SkillSourceStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result))
	}
	if result[0].UpdateAvail {
		t.Errorf("expected updateAvailable=false with missing cache")
	}
}

func TestHandleSkills_SourcesList_Empty(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result []SkillSourceStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty sources, got %d", len(result))
	}
}

func TestHandleSkills_SourceAdd_MissingRepo(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var errResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if !strings.Contains(errResp["error"], "repo") {
		t.Errorf("expected error about repo, got %q", errResp["error"])
	}
}

func TestHandleSkills_SourceAdd_InvalidJSON(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	body := strings.NewReader(`{invalid}`)
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkills_UpdatesEmpty(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/updates", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var summary UpdateSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if summary.Available != 0 {
		t.Errorf("expected 0 updates, got %d", summary.Available)
	}
}

func TestHandleSkills_SourceRemove_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodDelete, "/api/skills/sources/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkills_SourceCheck_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/nonexistent/check", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkills_SyncAll_HonorsPinsAndCountsFailures(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "pinned-skill", registry.StateActive)
	seedSkill(t, regServer, "floating-skill", registry.StateActive)

	// Seed a pinned source (Ref looks like a semver tag) and an unpinned
	// source pointing at a non-existent repo so its Update will error.
	// Exercises error accounting without needing a live git fixture.
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"pinned-source": {
			Repo:      "https://github.com/example/pinned",
			Ref:       "v1.0.0",
			CommitSHA: "abc1234567890abc1234567890abc1234567890a",
			Skills:    map[string]skills.LockedSkill{"pinned-skill": {Path: "pinned-skill"}},
		},
		"floating-source": {
			Repo:      "/nonexistent/path/to/repo",
			Ref:       "main",
			CommitSHA: "def1234567890def1234567890def1234567890d",
			Skills:    map[string]skills.LockedSkill{"floating-skill": {Path: "floating-skill"}},
		},
	}}
	if err := skills.WriteLockFile(srv.lockFilePath(), lf); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/update", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var summary SourceSyncSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if summary.PinnedSources != 1 {
		t.Errorf("expected 1 pinned source, got %d", summary.PinnedSources)
	}
	if summary.FailedSources != 1 {
		t.Errorf("expected 1 failed source, got %d", summary.FailedSources)
	}
	if summary.SyncedSources != 0 {
		t.Errorf("expected 0 cleanly-synced sources, got %d", summary.SyncedSources)
	}
	if summary.UpdatedSkills != 0 {
		t.Errorf("expected 0 updated skills, got %d", summary.UpdatedSkills)
	}
	if len(summary.Sources) != 2 {
		t.Fatalf("expected 2 source results, got %d", len(summary.Sources))
	}

	// Sources are returned in deterministic alphabetical order.
	if summary.Sources[0].Name != "floating-source" {
		t.Errorf("expected floating-source first, got %s", summary.Sources[0].Name)
	}
	if !summary.Sources[1].Pinned {
		t.Errorf("expected pinned-source to be marked pinned")
	}
	if summary.Sources[1].Skills != nil {
		t.Errorf("expected pinned-source to have no skills attempted")
	}
}

func TestHandleSkills_SyncAll_EmptyLockFile(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/update", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var summary SourceSyncSummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(summary.Sources) != 0 {
		t.Errorf("expected empty sources, got %d", len(summary.Sources))
	}
	if summary.SyncedSources != 0 || summary.UpdatedSkills != 0 ||
		summary.FailedSources != 0 || summary.PinnedSources != 0 {
		t.Errorf("expected zero counters on empty lock file, got %+v", summary)
	}
}

func TestHandleSkills_SourceUpdate_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/skills/sources/nonexistent/update", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkills_SourcePreview_MissingRepo(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources/newrepo/preview", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkills_NoRegistry(t *testing.T) {
	gateway := mcp.NewGateway()
	srv := NewServer(gateway, nil)
	// No registry set

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSkills_RoutingMethodNotAllowed(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()

	// GET on sources should work
	req := httptest.NewRequest(http.MethodGet, "/api/skills/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET sources, got %d", rec.Code)
	}
}

func TestStoreDir(t *testing.T) {
	dir := t.TempDir()
	store := registry.NewStore(dir)

	if store.Dir() != dir {
		t.Errorf("expected Dir() = %q, got %q", dir, store.Dir())
	}
}
