package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/vault"
)

const unlockSourcePass = "unlock-source-pass" // #nosec G101 -- test fixture

func lockedVaultServer(t *testing.T) (*Server, *vault.Store) {
	t.Helper()
	server, store := setupVaultServer(t)
	if err := store.Set("KEY", "val"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(unlockSourcePass); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return server, store
}

func varStatus(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loopbackRequest(http.MethodGet, "/api/var/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postJSON(h http.Handler, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, loopbackRequest(http.MethodPost, path, bytes.NewBufferString(body)))
	return w
}

func TestHandleVaultStatus_UnlockSourceOmittedWhenEmpty(t *testing.T) {
	server, store := setupVaultServer(t)
	_ = store.Set("K", "v")
	if _, ok := varStatus(t, server.Handler())["unlock_source"]; ok {
		t.Fatal("plaintext status carries unlock_source")
	}

	locked, _ := lockedVaultServer(t)
	if _, ok := varStatus(t, locked.Handler())["unlock_source"]; ok {
		t.Fatal("locked status carries unlock_source")
	}
}

func TestHandleVaultStatus_ReportsUnlockSource(t *testing.T) {
	server, store := lockedVaultServer(t)
	if err := store.Unlock(unlockSourcePass); err != nil {
		t.Fatal(err)
	}
	store.SetUnlockSource(vault.UnlockSourceStdin)
	got := varStatus(t, server.Handler())
	if got["unlock_source"] != "stdin" || got["locked"] != false || got["encrypted"] != true {
		t.Fatalf("status = %v", got)
	}
}

func TestHandleVaultUnlock_Source(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "no source", body: `{"passphrase":"` + unlockSourcePass + `"}`, want: "api"},
		{name: "api", body: `{"passphrase":"` + unlockSourcePass + `","source":"api"}`, want: "api"},
		{name: "cli", body: `{"passphrase":"` + unlockSourcePass + `","source":"cli"}`, want: "cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := lockedVaultServer(t)
			h := server.Handler()
			w := postJSON(h, "/api/var/unlock", tc.body)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"unlocked"`) {
				t.Fatalf("unlock = %d %s", w.Code, w.Body.String())
			}
			if got := varStatus(t, h)["unlock_source"]; got != tc.want {
				t.Fatalf("unlock_source = %v, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleVaultUnlock_InvalidSource(t *testing.T) {
	for _, source := range []string{"stdin", "environment", "CLI", "other"} {
		server, store := lockedVaultServer(t)
		w := postJSON(server.Handler(), "/api/var/unlock", `{"passphrase":"`+unlockSourcePass+`","source":"`+source+`"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("source %q: status = %d, want 400", source, w.Code)
		}
		if strings.Contains(w.Body.String(), unlockSourcePass) {
			t.Fatal("response echoes the passphrase")
		}
		if !store.IsLocked() {
			t.Fatalf("source %q unlocked the store", source)
		}
	}
}

func TestHandleVaultUnlock_AlreadyUnlockedKeepsSource(t *testing.T) {
	server, store := lockedVaultServer(t)
	if err := store.Unlock(unlockSourcePass); err != nil {
		t.Fatal(err)
	}
	store.SetUnlockSource(vault.UnlockSourceStdin)
	h := server.Handler()
	w := postJSON(h, "/api/var/unlock", `{"passphrase":"`+unlockSourcePass+`","source":"cli"}`)
	if !strings.Contains(w.Body.String(), "already_unlocked") {
		t.Fatalf("body = %s", w.Body.String())
	}
	if got := varStatus(t, h)["unlock_source"]; got != "stdin" {
		t.Fatalf("unlock_source = %v, want stdin", got)
	}
}

func TestHandleVaultUnlock_RegistersPassphraseForRedaction(t *testing.T) {
	const pass = "registered-unlock-pass" // #nosec G101 -- test fixture
	server, store := setupVaultServer(t)
	_ = store.Set("K", "v")
	if err := store.Lock(pass); err != nil {
		t.Fatal(err)
	}
	_ = store.Load()
	w := postJSON(server.Handler(), "/api/var/unlock", `{"passphrase":"`+pass+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("unlock = %d", w.Code)
	}
	if strings.Contains(logging.RedactString("log "+pass), pass) {
		t.Fatal("passphrase not registered for redaction")
	}
}

func TestHandleVaultLock_SetsSourceAPI(t *testing.T) {
	server, store := setupVaultServer(t)
	_ = store.Set("K", "v")
	h := server.Handler()
	w := postJSON(h, "/api/var/lock", `{"passphrase":"lock-source-pass"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("lock = %d %s", w.Code, w.Body.String())
	}
	got := varStatus(t, h)
	if got["encrypted"] != true || got["locked"] != false || got["unlock_source"] != "api" {
		t.Fatalf("status after lock = %v", got)
	}
}
