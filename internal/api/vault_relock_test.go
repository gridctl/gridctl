package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/vault"
)

func postRelock(h http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loopbackRequest(http.MethodPost, "/api/var/relock", nil))
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func TestHandleVaultRelock_LocksUnlockedStore(t *testing.T) {
	dir := t.TempDir()
	store := vault.NewStore(dir)
	if err := store.Set("KEY", "val"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(unlockSourcePass); err != nil {
		t.Fatal(err)
	}
	store.SetUnlockSource(vault.UnlockSourceCLI)
	server := &Server{vaultStore: store}
	h := server.Handler()

	encPath := filepath.Join(dir, "secrets.enc")
	before, err := os.ReadFile(encPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}

	w := postRelock(h)
	if w.Code != http.StatusOK {
		t.Fatalf("relock = %d: %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w)["status"]; got != "locked" {
		t.Fatalf("status = %v, want locked", got)
	}
	if !store.IsLocked() {
		t.Fatal("store still unlocked after relock")
	}

	list := httptest.NewRecorder()
	h.ServeHTTP(list, loopbackRequest(http.MethodGet, "/api/var", nil))
	if list.Code != statusLocked {
		t.Fatalf("GET /api/var after relock = %d, want %d", list.Code, statusLocked)
	}
	st := varStatus(t, h)
	if st["locked"] != true || st["encrypted"] != true {
		t.Fatalf("status after relock = %v", st)
	}
	if _, ok := st["unlock_source"]; ok {
		t.Fatalf("unlock_source survived relock: %v", st)
	}

	after, err := os.ReadFile(encPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("relock rewrote secrets.enc")
	}
}

func TestHandleVaultRelock_AlreadyLocked(t *testing.T) {
	server, _ := lockedVaultServer(t)
	w := postRelock(server.Handler())
	if w.Code != http.StatusOK {
		t.Fatalf("relock = %d: %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w)["status"]; got != "already_locked" {
		t.Fatalf("status = %v, want already_locked", got)
	}
}

func TestHandleVaultRelock_PlaintextConflict(t *testing.T) {
	server, store := setupVaultServer(t)
	if err := store.Set("K", "v"); err != nil {
		t.Fatal(err)
	}
	w := postRelock(server.Handler())
	if w.Code != http.StatusConflict {
		t.Fatalf("relock = %d, want 409: %s", w.Code, w.Body.String())
	}
	if got := decodeBody(t, w)["error"]; got != "store is not encrypted" {
		t.Fatalf("error = %v", got)
	}
	if store.IsLocked() || store.IsEncrypted() {
		t.Fatal("plaintext store changed state")
	}
}

func TestHandleVaultRelock_NilStore(t *testing.T) {
	server := &Server{}
	w := postRelock(server.Handler())
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("relock = %d, want 503", w.Code)
	}
}

func TestHandleVaultRelock_NotMirroredOnDeprecatedSurface(t *testing.T) {
	server, _ := lockedVaultServer(t)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, loopbackRequest(http.MethodPost, "/api/vault/relock", nil))
	if w.Code == http.StatusOK {
		t.Fatalf("/api/vault/relock answered 200: %s", w.Body.String())
	}
}
