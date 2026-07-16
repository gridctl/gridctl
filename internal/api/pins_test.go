package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	// An unconfigured pin store is a normal state, not an error: the list
	// endpoint returns 200 with an empty object so the polling UI does not log
	// a console error on every refresh cycle.
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

func TestHandlePinsDiff_NoStore(t *testing.T) {
	server := &Server{gateway: mcp.NewGateway()}

	req := httptest.NewRequest(http.MethodGet, "/api/pins/myserver/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePinsDiff_PinsNotFound(t *testing.T) {
	server, _ := setupPinsServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/pins/unknown-server/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePinsDiff_ServerNotInGateway(t *testing.T) {
	server, ps := setupPinsServer(t)

	tools := []mcp.Tool{{Name: "tool1", Description: "does something"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/pins/myserver/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePinsDiff_Clean(t *testing.T) {
	server, ps := setupPinsServer(t)

	tools := []mcp.Tool{{Name: "tool1", Description: "does something"}}
	if _, err := ps.VerifyOrPin("myserver", tools); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}
	server.gateway.Router().AddClient(newMockAgentClient("myserver", tools))

	req := httptest.NewRequest(http.MethodGet, "/api/pins/myserver/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result pinsDiffResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Status != pins.VerifyStatusVerified {
		t.Errorf("status = %q, want %q", result.Status, pins.VerifyStatusVerified)
	}
	// Empty deltas must serialize as [] rather than null.
	if result.ModifiedTools == nil || result.NewTools == nil || result.RemovedTools == nil {
		t.Error("expected empty slices, got nil")
	}
	if len(result.ModifiedTools)+len(result.NewTools)+len(result.RemovedTools) != 0 {
		t.Errorf("expected empty diff, got %+v", result)
	}
}

func TestHandlePinsDiff_Drift(t *testing.T) {
	server, ps := setupPinsServer(t)

	pinned := []mcp.Tool{
		{Name: "stable", Description: "unchanged"},
		{Name: "poisoned", Description: "original description"},
		{Name: "retired", Description: "will disappear"},
	}
	if _, err := ps.VerifyOrPin("myserver", pinned); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}

	live := []mcp.Tool{
		{Name: "stable", Description: "unchanged"},
		{Name: "poisoned", Description: "new sneaky description"},
		{Name: "added", Description: "brand new"},
	}
	server.gateway.Router().AddClient(newMockAgentClient("myserver", live))

	req := httptest.NewRequest(http.MethodGet, "/api/pins/myserver/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result pinsDiffResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Server != "myserver" {
		t.Errorf("server = %q, want %q", result.Server, "myserver")
	}
	if result.Status != pins.VerifyStatusDrift {
		t.Errorf("status = %q, want %q", result.Status, pins.VerifyStatusDrift)
	}
	if len(result.ModifiedTools) != 1 {
		t.Fatalf("modified_tools = %d, want 1", len(result.ModifiedTools))
	}
	mod := result.ModifiedTools[0]
	if mod.Name != "poisoned" {
		t.Errorf("modified tool = %q, want %q", mod.Name, "poisoned")
	}
	if mod.OldDescription != "original description" || mod.NewDescription != "new sneaky description" {
		t.Errorf("descriptions = %q -> %q, want original -> new", mod.OldDescription, mod.NewDescription)
	}
	if mod.OldHash == "" || mod.NewHash == "" || mod.OldHash == mod.NewHash {
		t.Errorf("hashes = %q -> %q, want distinct non-empty", mod.OldHash, mod.NewHash)
	}
	if len(result.NewTools) != 1 || result.NewTools[0] != "added" {
		t.Errorf("new_tools = %v, want [added]", result.NewTools)
	}
	if len(result.RemovedTools) != 1 || result.RemovedTools[0] != "retired" {
		t.Errorf("removed_tools = %v, want [retired]", result.RemovedTools)
	}
	if result.LiveServerHash == "" {
		t.Error("live_server_hash missing; approve cannot be bound to the reviewed diff without it")
	}

	// Viewing a diff must not mutate pin state: still drift-free on disk
	// until approved, and a subsequent approve clears it.
	if sp, ok := ps.GetServer("myserver"); !ok || len(sp.Tools) != 3 {
		t.Error("diff mutated stored pins")
	}
	if err := ps.Approve("myserver", live); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if sp, _ := ps.GetServer("myserver"); sp.Status != pins.StatusPinned {
		t.Errorf("status after approve = %q, want %q", sp.Status, pins.StatusPinned)
	}
}

func TestHandlePins_Approve_ExpectedHash(t *testing.T) {
	server, ps := setupPinsServer(t)

	pinned := []mcp.Tool{{Name: "echo", Description: "original"}}
	if _, err := ps.VerifyOrPin("myserver", pinned); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}
	live := []mcp.Tool{{Name: "echo", Description: "changed"}}
	server.gateway.Router().AddClient(newMockAgentClient("myserver", live))

	// Fetch the diff to get the reviewed fingerprint.
	req := httptest.NewRequest(http.MethodGet, "/api/pins/myserver/diff", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	var diff pinsDiffResponse
	if err := json.NewDecoder(w.Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}

	// A stale fingerprint (definitions changed after review) must be rejected.
	stale := strings.NewReader(`{"expected_server_hash":"h2:not-what-was-reviewed"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/pins/myserver/approve", stale)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("stale hash: status = %d, want %d", w.Code, http.StatusConflict)
	}
	if sp, _ := ps.GetServer("myserver"); sp.Tools["echo"].Description != "original" {
		t.Error("stale-hash approve must not re-pin")
	}

	// The reviewed fingerprint still matches the live tools: approve succeeds.
	fresh := strings.NewReader(`{"expected_server_hash":"` + diff.LiveServerHash + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/pins/myserver/approve", fresh)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("matching hash: status = %d, want %d (body=%s)", w.Code, http.StatusOK, w.Body.String())
	}
	if sp, _ := ps.GetServer("myserver"); sp.Tools["echo"].Description != "changed" {
		t.Error("approve with matching hash should re-pin the live definitions")
	}
}

func TestHandlePins_Approve_NoBodyStaysUnconditional(t *testing.T) {
	server, ps := setupPinsServer(t)

	pinned := []mcp.Tool{{Name: "echo", Description: "original"}}
	if _, err := ps.VerifyOrPin("myserver", pinned); err != nil {
		t.Fatalf("VerifyOrPin: %v", err)
	}
	server.gateway.Router().AddClient(newMockAgentClient("myserver", []mcp.Tool{{Name: "echo", Description: "changed"}}))

	req := httptest.NewRequest(http.MethodPost, "/api/pins/myserver/approve", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (empty body preserves the legacy contract)", w.Code, http.StatusOK)
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
