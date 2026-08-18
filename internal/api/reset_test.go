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

	// 5. Preview, then purge-execute with the token but the WRONG phrase: 422.
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

	// 6. The wrong-phrase attempt consumed the token (single use): a
	// retry with the CORRECT phrase but the same token is also 422.
	body = `{"purge":true,"confirm_token":"` + pv.ConfirmToken + `","confirm_phrase":"` + gridctlDir + `"}`
	if rec := serveReset(t, srv, resetRequest(t, "/api/reset", body)); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("reused token status = %d, want 422 (single use)", rec.Code)
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
