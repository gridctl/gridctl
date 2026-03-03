package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/vault"
)

func setupVaultServer(t *testing.T) (*Server, *vault.Store) {
	t.Helper()
	store := vault.NewStore(t.TempDir())
	server := &Server{vaultStore: store}
	return server, store
}

func TestHandleVault_List_Empty(t *testing.T) {
	server, _ := setupVaultServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/vault", nil)
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var keys []map[string]string
	_ = json.NewDecoder(w.Body).Decode(&keys)
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %d entries", len(keys))
	}
}

func TestHandleVault_CreateAndGet(t *testing.T) {
	server, _ := setupVaultServer(t)

	// Create
	body := `{"key":"API_KEY","value":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/vault", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("create status = %d, want %d", w.Code, http.StatusCreated)
	}

	// Get
	req = httptest.NewRequest(http.MethodGet, "/api/vault/API_KEY", nil)
	w = httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("get status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result["value"] != "secret123" {
		t.Errorf("value = %q, want %q", result["value"], "secret123")
	}
}

func TestHandleVault_List_NoValues(t *testing.T) {
	server, store := setupVaultServer(t)
	_ = store.Set("SECRET", "hidden-value")

	req := httptest.NewRequest(http.MethodGet, "/api/vault", nil)
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	// Verify response contains key but not value
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("SECRET")) {
		t.Error("list should contain key name")
	}
	if bytes.Contains([]byte(body), []byte("hidden-value")) {
		t.Error("list should NOT contain secret value")
	}
}

func TestHandleVault_Delete(t *testing.T) {
	server, store := setupVaultServer(t)
	_ = store.Set("TO_DELETE", "value")

	req := httptest.NewRequest(http.MethodDelete, "/api/vault/TO_DELETE", nil)
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want %d", w.Code, http.StatusNoContent)
	}

	if store.Has("TO_DELETE") {
		t.Error("key should be deleted")
	}
}

func TestHandleVault_NotFound(t *testing.T) {
	server, _ := setupVaultServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/vault/MISSING", nil)
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleVault_Import(t *testing.T) {
	server, store := setupVaultServer(t)

	body := `{"secrets":{"KEY1":"val1","KEY2":"val2"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/vault/import", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("import status = %d, want %d", w.Code, http.StatusOK)
	}

	if !store.Has("KEY1") || !store.Has("KEY2") {
		t.Error("imported keys should exist")
	}
}

func TestHandleVault_NotAvailable(t *testing.T) {
	server := &Server{} // no vault store

	req := httptest.NewRequest(http.MethodGet, "/api/vault", nil)
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleVault_Update(t *testing.T) {
	server, store := setupVaultServer(t)
	_ = store.Set("KEY", "old")

	body := `{"value":"new"}`
	req := httptest.NewRequest(http.MethodPut, "/api/vault/KEY", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("update status = %d, want %d", w.Code, http.StatusOK)
	}

	got, _ := store.Get("KEY")
	if got != "new" {
		t.Errorf("value = %q, want %q", got, "new")
	}
}

func TestHandleVault_CreateMissingKey(t *testing.T) {
	server, _ := setupVaultServer(t)

	body := `{"value":"val"}`
	req := httptest.NewRequest(http.MethodPost, "/api/vault", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	server.handleVault(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
