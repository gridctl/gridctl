package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// setupContextTestServer builds a Server whose context manager is rooted
// at a temp home with the given client detect dirs pre-created.
func setupContextTestServer(t *testing.T, detectDirs ...string) (*Server, *contexts.Manager, string) {
	t.Helper()
	home := t.TempDir()
	for _, d := range detectDirs {
		if err := os.MkdirAll(filepath.Join(home, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	mgr := contexts.NewManagerWithHome(home)
	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetContextsManager(mgr)
	return srv, mgr, home
}

func doJSON(t *testing.T, srv *Server, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHandleContextGetEmptyState(t *testing.T) {
	srv, _, _ := setupContextTestServer(t)

	rec := doJSON(t, srv, http.MethodGet, "/api/context", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var doc contextDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Canonical.Exists {
		t.Error("canonical should not exist yet")
	}
	if len(doc.Clients) == 0 {
		t.Error("clients list should include all known clients")
	}
}

func TestHandleContextPutCreatesAndReturnsDoc(t *testing.T) {
	srv, mgr, _ := setupContextTestServer(t)

	rec := doJSON(t, srv, http.MethodPut, "/api/context", `{"content":"# Rules\n"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var doc contextDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Canonical.Exists || !strings.Contains(doc.Canonical.Content, "# Rules") {
		t.Errorf("unexpected canonical: %+v", doc.Canonical)
	}
	if !mgr.HasCanonical() {
		t.Error("canonical file not written")
	}

	// Empty content rejected.
	rec = doJSON(t, srv, http.MethodPut, "/api/context", `{"content":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty content status = %d, want 400", rec.Code)
	}
}

func TestHandleContextInitSources(t *testing.T) {
	srv, mgr, home := setupContextTestServer(t, ".gemini")
	if err := os.WriteFile(filepath.Join(home, ".gemini", "GEMINI.md"), []byte("# imported\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/context/init", `{"source":"client","client":"gemini"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	content, err := mgr.CanonicalContent()
	if err != nil || !strings.Contains(content, "# imported") {
		t.Errorf("import failed: %q err=%v", content, err)
	}

	// Re-init without force conflicts.
	rec = doJSON(t, srv, http.MethodPost, "/api/context/init", `{"source":"template"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("re-init status = %d, want 409", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/context/init", `{"source":"template","force":true}`)
	if rec.Code != http.StatusOK {
		t.Errorf("forced re-init status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/context/init", `{"source":"bogus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bogus source status = %d, want 400", rec.Code)
	}
}

func TestHandleContextScan(t *testing.T) {
	srv, _, home := setupContextTestServer(t, ".gemini")
	if err := os.WriteFile(filepath.Join(home, ".gemini", "GEMINI.md"), []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, http.MethodGet, "/api/context/scan", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Entries []contexts.ScanEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range body.Entries {
		if e.Slug == "gemini" && e.Exists {
			found = true
		}
	}
	if !found {
		t.Errorf("gemini not reported as existing: %+v", body.Entries)
	}
}

func TestHandleContextSyncAllAndFailures(t *testing.T) {
	srv, mgr, _ := setupContextTestServer(t, ".claude", ".config/opencode")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/context/sync", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		HasFailures bool                  `json:"has_failures"`
		Results     []contexts.SyncResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.HasFailures {
		t.Errorf("unexpected failures: %+v", body.Results)
	}

	// Hand-edit one target: the next sync reports a failure but stays 200.
	var opencodeTarget string
	for _, r := range body.Results {
		if r.Slug == "opencode" {
			opencodeTarget = r.TargetPath
		}
	}
	data, err := os.ReadFile(opencodeTarget)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opencodeTarget, []byte(strings.Replace(string(data), "# Rules", "# EDITED", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	// Change the canon so a re-sync actually wants to write.
	if err := mgr.SaveCanonical("# Rules v2\n"); err != nil {
		t.Fatal(err)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/context/sync", `{"clients":["opencode"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.HasFailures || body.Results[0].Action != contexts.ActionSkippedDrift {
		t.Errorf("expected skipped-drift failure: %+v", body.Results)
	}

	// Unknown client is a 400.
	rec = doJSON(t, srv, http.MethodPost, "/api/context/sync", `{"clients":["bogus"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown client status = %d, want 400", rec.Code)
	}

	// Sync without a canonical file is a 404.
	srv2, _, _ := setupContextTestServer(t, ".claude")
	rec = doJSON(t, srv2, http.MethodPost, "/api/context/sync", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("no-canonical status = %d, want 404", rec.Code)
	}
}

func TestHandleContextAdoptAndDiff(t *testing.T) {
	srv, mgr, _ := setupContextTestServer(t, ".config/opencode")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, http.MethodPost, "/api/context/sync", `{"clients":["opencode"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync status = %d", rec.Code)
	}
	var syncBody struct {
		Results []contexts.SyncResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &syncBody); err != nil {
		t.Fatal(err)
	}
	target := syncBody.Results[0].TargetPath
	data, _ := os.ReadFile(target)
	if err := os.WriteFile(target, []byte(strings.Replace(string(data), "# Rules", "# ADOPTED", 1)), 0644); err != nil {
		t.Fatal(err)
	}

	// Diff shows the hand edit.
	rec = doJSON(t, srv, http.MethodGet, "/api/context/diff/opencode", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("diff status = %d", rec.Code)
	}
	var diffBody struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &diffBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diffBody.Diff, "+# ADOPTED") {
		t.Errorf("diff missing hunk: %q", diffBody.Diff)
	}

	// Adopt pulls it into the canon.
	rec = doJSON(t, srv, http.MethodPost, "/api/context/adopt/opencode", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt status = %d: %s", rec.Code, rec.Body.String())
	}
	content, _ := mgr.CanonicalContent()
	if !strings.Contains(content, "# ADOPTED") {
		t.Errorf("canonical missing adopted content: %q", content)
	}

	// Adopt for an unknown client is 404.
	rec = doJSON(t, srv, http.MethodPost, "/api/context/adopt/bogus", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown adopt status = %d, want 404", rec.Code)
	}
}

func TestHandleContextUnsync(t *testing.T) {
	srv, mgr, home := setupContextTestServer(t, ".claude")
	if err := mgr.SaveCanonical("# Rules\n"); err != nil {
		t.Fatal(err)
	}
	rec := doJSON(t, srv, http.MethodPost, "/api/context/sync", `{"clients":["claude-code"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync status = %d", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/context/unsync/claude-code", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unsync status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "rules", "gridctl.md")); !os.IsNotExist(err) {
		t.Error("dedicated file survived unsync")
	}

	// Unsync of a never-synced client is 409.
	rec = doJSON(t, srv, http.MethodPost, "/api/context/unsync/claude-code", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("re-unsync status = %d, want 409", rec.Code)
	}
}
