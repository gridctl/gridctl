package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"

	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/resetops"
	"github.com/gridctl/gridctl/pkg/state"
)

// setupResetTestServer builds a Server whose reset engine is rooted at a
// temp GRIDCTL_HOME, with no kind managers (an empty machine).
func setupResetTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetResetManagers(&resetops.Managers{Home: home})
	return srv, home
}

// resetRequest builds a loopback JSON POST; the guard requires all three
// of loopback peer, JSON content type, and same-or-no origin.
func resetRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	req := loopbackRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}

func serveReset(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestResetPreview_IssuesTokenAndPhrase(t *testing.T) {
	srv, home := setupResetTestServer(t)

	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{"purge":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ConfirmToken  string        `json:"confirm_token"`
		ConfirmPhrase string        `json:"confirm_phrase"`
		Doc           *resetops.Doc `json:"doc"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ConfirmToken == "" {
		t.Error("preview must issue a confirm token")
	}
	want := filepath.Join(home, ".gridctl")
	if resp.ConfirmPhrase != want {
		t.Errorf("confirm_phrase = %q, want the RESOLVED path %q", resp.ConfirmPhrase, want)
	}
	if resp.Doc == nil || !resp.Doc.DryRun {
		t.Error("preview doc must be a dry run")
	}
}

func TestResetExecute_GuardsAndTokenFlow(t *testing.T) {
	srv, home := setupResetTestServer(t)
	gridctlDir := filepath.Join(home, ".gridctl")

	// 1. Non-loopback peer: rejected for BOTH endpoints.
	req := resetRequest(t, "/api/reset/preview", `{}`)
	req.RemoteAddr = "203.0.113.9:4444"
	if rec := serveReset(t, srv, req); rec.Code != http.StatusForbidden {
		t.Errorf("non-loopback preview status = %d, want 403", rec.Code)
	}
	req = resetRequest(t, "/api/reset", `{}`)
	req.RemoteAddr = "203.0.113.9:4444"
	if rec := serveReset(t, srv, req); rec.Code != http.StatusForbidden {
		t.Errorf("non-loopback execute status = %d, want 403", rec.Code)
	}

	// 2. Cross-origin: rejected.
	req = resetRequest(t, "/api/reset", `{}`)
	req.Header.Set("Origin", "https://evil.example")
	if rec := serveReset(t, srv, req); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin execute status = %d, want 403", rec.Code)
	}

	// 3. Wrong content type: 415 (never a CORS simple request).
	req = loopbackRequest(http.MethodPost, "/api/reset", strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Content-Type", "text/plain")
	if rec := serveReset(t, srv, req); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("wrong content-type status = %d, want 415", rec.Code)
	}

	// 4. Execute without a token: 422.
	if rec := serveReset(t, srv, resetRequest(t, "/api/reset", `{"purge":false}`)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("tokenless execute status = %d, want 422", rec.Code)
	}

	// 5. Preview, then purge-execute with the token but the WRONG phrase:
	// 422, and the typo must NOT burn the token (the phrase is printed by
	// the preview; it gates attention, not secrecy).
	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{"purge":true}`))
	var pv struct {
		ConfirmToken string `json:"confirm_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	body := `{"purge":true,"confirm_token":"` + pv.ConfirmToken + `","confirm_phrase":"~/.gridctl"}`
	if rec := serveReset(t, srv, resetRequest(t, "/api/reset", body)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("literal-tilde phrase status = %d, want 422 (phrase must be the resolved path)", rec.Code)
	}

	// 6. Retry with the CORRECT phrase and the SAME token succeeds: the
	// wrong-phrase attempt left it valid for exactly one correct use.
	srv.SetResetExit(func(int) {}) // purge succeeds; do not exit the test binary
	body = `{"purge":true,"confirm_token":"` + pv.ConfirmToken + `","confirm_phrase":"` + gridctlDir + `"}`
	if rec := serveReset(t, srv, resetRequest(t, "/api/reset", body)); rec.Code != http.StatusOK {
		t.Errorf("correct retry status = %d, want 200 (typo must not burn the token)", rec.Code)
	}

	// 7. After a successful purge the daemon is terminally busy: the
	// finalize path holds the in-flight lock until the process exits, so
	// any further reset attempt is 409 (never a second cascade racing a
	// pending exit).
	body = `{"purge":true,"confirm_token":"` + pv.ConfirmToken + `","confirm_phrase":"` + gridctlDir + `"}`
	if rec := serveReset(t, srv, resetRequest(t, "/api/reset", body)); rec.Code != http.StatusConflict {
		t.Errorf("post-purge attempt status = %d, want 409 (daemon is exiting)", rec.Code)
	}
}

func TestResetExecute_TokenPurgeTierMustMatch(t *testing.T) {
	srv, _ := setupResetTestServer(t)

	// Token issued for the DEFAULT tier must not authorize purge.
	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{"purge":false}`))
	var pv struct {
		ConfirmToken  string `json:"confirm_token"`
		ConfirmPhrase string `json:"confirm_phrase"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	body := `{"purge":true,"confirm_token":"` + pv.ConfirmToken + `","confirm_phrase":"` + pv.ConfirmPhrase + `"}`
	if rec := serveReset(t, srv, resetRequest(t, "/api/reset", body)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("tier-mismatched token status = %d, want 422", rec.Code)
	}
}

func TestResetExecute_DefaultTierSucceedsOnEmptyHome(t *testing.T) {
	srv, home := setupResetTestServer(t)
	// Seed something for the default tier to preserve.
	if err := os.MkdirAll(filepath.Join(home, ".gridctl", "vault"), 0o700); err != nil {
		t.Fatal(err)
	}

	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{"purge":false}`))
	var pv struct {
		ConfirmToken string `json:"confirm_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	body := `{"purge":false,"confirm_token":"` + pv.ConfirmToken + `"}`
	rec = serveReset(t, srv, resetRequest(t, "/api/reset", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body %s", rec.Code, rec.Body.String())
	}
	var doc resetops.Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode doc: %v", err)
	}
	if doc.BackupPath == "" {
		t.Error("execute must report the backup path")
	}
	// Default tier preserves the vault.
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "vault")); err != nil {
		t.Error("default tier must preserve ~/.gridctl/vault")
	}
	// A successful consume is single use: the default tier releases the
	// in-flight lock, so the reuse fails on the burned token (422), not
	// on the lock (409).
	rec = serveReset(t, srv, resetRequest(t, "/api/reset", body))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("reused token status = %d, want 422 (single use after success)", rec.Code)
	}
}

func TestResetExecute_PurgeSelfTermination(t *testing.T) {
	srv, home := setupResetTestServer(t)
	gridctlDir := filepath.Join(home, ".gridctl")
	if err := os.MkdirAll(filepath.Join(gridctlDir, "vault"), 0o700); err != nil {
		t.Fatal(err)
	}

	exited := make(chan int, 1)
	srv.SetResetExit(func(code int) { exited <- code })

	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{"purge":true}`))
	var pv struct {
		ConfirmToken  string `json:"confirm_token"`
		ConfirmPhrase string `json:"confirm_phrase"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	body := `{"purge":true,"confirm_token":"` + pv.ConfirmToken + `","confirm_phrase":"` + pv.ConfirmPhrase + `"}`
	rec = serveReset(t, srv, resetRequest(t, "/api/reset", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("purge execute status = %d, body %s", rec.Code, rec.Body.String())
	}
	// The result document must be complete BEFORE the process would exit.
	var doc resetops.Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response was not a complete document: %v", err)
	}
	if doc.BackupPath == "" {
		t.Error("purge response must carry the backup path")
	}
	// The finalize goroutine deletes .gridctl and then exits.
	select {
	case code := <-exited:
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("finalize goroutine never exited")
	}
	if _, err := os.Stat(gridctlDir); !os.IsNotExist(err) {
		t.Error("finalize must complete the purge RemoveAll")
	}
}

// apiFakeHooks is a resetops hook manager for handler tests. The field
// type is unexported; the method set is what the assignment checks.
type apiFakeHooks struct {
	statuses []hooksync.ProjectionStatus
	unsynced [][]string
}

func (f *apiFakeHooks) Statuses(context.Context) ([]hooksync.ProjectionStatus, error) {
	return f.statuses, nil
}

func (f *apiFakeHooks) Unsync(_ context.Context, names []string, _ hooksync.UnsyncOptions) ([]hooksync.UnsyncResult, error) {
	f.unsynced = append(f.unsynced, append([]string(nil), names...))
	var out []hooksync.UnsyncResult
	for _, n := range names {
		res := hooksync.UnsyncResult{Hook: n, Action: hooksync.ActionRemoved}
		for _, s := range f.statuses {
			if s.Hook == n {
				res.Client = s.Client
				res.Target = s.Target
				break
			}
		}
		out = append(out, res)
	}
	return out, nil
}

func TestResetPreview_IncludesHookRow(t *testing.T) {
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	settings := filepath.Join(home, ".claude", "settings.json")
	scriptDir := filepath.Join(home, ".gridctl", "hooks", "block-force-push")
	hooks := &apiFakeHooks{statuses: []hooksync.ProjectionStatus{{
		Hook: "block-force-push", Client: "claude-code", State: hooksync.StateInSync,
		Target: settings, ScriptDir: scriptDir,
	}}}
	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetResetManagers(&resetops.Managers{Home: home, Hooks: hooks})

	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Doc *resetops.Doc `json:"doc"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Doc == nil {
		t.Fatal("preview doc is nil")
	}
	var found bool
	for _, r := range resp.Doc.Rows {
		if r.Kind == "hook" && r.Name == "block-force-push" && r.Client == "claude-code" && r.Path == settings && r.ScriptDir == scriptDir && r.Action == resetops.ActionWouldRemove {
			found = true
		}
	}
	if !found {
		t.Fatalf("preview rows = %+v, want a hook would-remove row", resp.Doc.Rows)
	}

	var pv struct {
		ConfirmToken string `json:"confirm_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	body := `{"confirm_token":"` + pv.ConfirmToken + `"}`
	rec = serveReset(t, srv, resetRequest(t, "/api/reset", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body %s", rec.Code, rec.Body.String())
	}
	if len(hooks.unsynced) != 1 || len(hooks.unsynced[0]) != 1 || hooks.unsynced[0][0] != "block-force-push" {
		t.Fatalf("execute Unsync calls = %v, want [block-force-push]", hooks.unsynced)
	}
	var executed resetops.Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &executed); err != nil {
		t.Fatalf("decode execute: %v", err)
	}
	var removed bool
	for _, r := range executed.Rows {
		if r.Kind == "hook" && r.Action == resetops.ActionRemoved && r.Name == "block-force-push" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("execute rows = %+v, want a removed hook row", executed.Rows)
	}
}

func TestResetExecute_MissingHookManagerCounted(t *testing.T) {
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	srv := NewServer(mcp.NewGateway(), nil)
	srv.SetResetManagers(&resetops.Managers{Home: home, Missing: []string{"hook"}})

	rec := serveReset(t, srv, resetRequest(t, "/api/reset/preview", `{}`))
	var pv struct {
		ConfirmToken string `json:"confirm_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	body := `{"confirm_token":"` + pv.ConfirmToken + `"}`
	rec = serveReset(t, srv, resetRequest(t, "/api/reset", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body %s", rec.Code, rec.Body.String())
	}
	var doc resetops.Doc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode doc: %v", err)
	}
	var skipped bool
	for _, r := range doc.Rows {
		if r.Kind == "hook" && r.Action == resetops.ActionSkipped {
			skipped = true
		}
	}
	if !skipped || doc.Failed != 1 {
		t.Fatalf("missing hook manager: skipped=%v Failed=%d rows=%+v", skipped, doc.Failed, doc.Rows)
	}
}
