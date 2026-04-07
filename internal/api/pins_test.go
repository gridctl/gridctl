package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

// newTestPinStore creates an in-memory PinStore backed by a temp directory.
// It uses VerifyOrPin to pre-populate the store with tool data when tools are provided.
func setupPinsServer(t *testing.T) (*Server, *pins.PinStore) {
	t.Helper()
	gateway := mcp.NewGateway()
	server := NewServer(gateway, nil)
	ps := pins.NewWithPath(t.TempDir(), "test-stack")
	server.SetPinStore(ps)
	return server, ps
}

func TestHandlePins_NoStore(t *testing.T) {
	server := &Server{gateway: mcp.NewGateway()}

	req := httptest.NewRequest(http.MethodGet, "/api/pins", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePins_ListEmpty(t *testing.T) {
	server, _ := setupPinsServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/pins", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestHandlePins_GetServer_NotFound(t *testing.T) {
	server, _ := setupPinsServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/pins/unknown-server", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePins_Approve_PinsNotFound(t *testing.T) {
	server, _ := setupPinsServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/pins/unknown-server/approve", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePins_Reset_NotFound(t *testing.T) {
	server, _ := setupPinsServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/pins/unknown-server", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePins_MethodNotAllowed(t *testing.T) {
	server, _ := setupPinsServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/pins", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlePins_GetServer_Found(t *testing.T) {
	server, ps := setupPinsServer(t)

	// Pre-populate the store with a server.
	tools := []mcp.Tool{{Name: "tool1", Description: "does something"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pins/myserver", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != pins.StatusPinned {
		t.Errorf("status = %v, want %q", result["status"], pins.StatusPinned)
	}
}

func TestHandlePins_Approve_ServerNotInGateway(t *testing.T) {
	server, ps := setupPinsServer(t)

	// Pre-populate pins so the first check passes.
	tools := []mcp.Tool{{Name: "tool1", Description: "does something"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}

	// Gateway has no registered clients, so GetClient returns nil → 404.
	req := httptest.NewRequest(http.MethodPost, "/api/pins/myserver/approve", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePins_Reset_Success(t *testing.T) {
	server, ps := setupPinsServer(t)

	tools := []mcp.Tool{{Name: "tool1", Description: "does something"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/pins/myserver", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Server should be gone.
	if _, ok := ps.GetServer("myserver"); ok {
		t.Error("expected server to be removed after reset")
	}
}
