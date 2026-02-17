package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
)

// setupRegistryTestServer creates a Server with a temp registry store for testing.
func setupRegistryTestServer(t *testing.T) (*Server, *registry.Server) {
	t.Helper()
	dir := t.TempDir()
	store := registry.NewStore(dir)
	regServer := registry.New(store)
	_ = regServer.Initialize(context.Background())

	gateway := mcp.NewGateway()
	apiServer := NewServer(gateway, nil)
	apiServer.SetRegistryServer(regServer)
	return apiServer, regServer
}

// seedSkill creates a skill in the store for testing.
func seedSkill(t *testing.T, regServer *registry.Server, name string, state registry.ItemState) {
	t.Helper()
	sk := &registry.AgentSkill{
		Name:        name,
		Description: "Test skill: " + name,
		State:       state,
		Body:        "# " + name + "\n\nSkill instructions.",
	}
	if err := regServer.Store().SaveSkill(sk); err != nil {
		t.Fatalf("failed to seed skill: %v", err)
	}
}

// --- Status endpoint ---

func TestHandleRegistry_Status(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "s1", registry.StateActive)
	seedSkill(t, regServer, "s2", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result registry.RegistryStatus
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.TotalSkills != 2 {
		t.Errorf("expected 2 total skills, got %d", result.TotalSkills)
	}
	if result.ActiveSkills != 1 {
		t.Errorf("expected 1 active skill, got %d", result.ActiveSkills)
	}
}

func TestHandleRegistry_Status_MethodNotAllowed(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/registry/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Skills: list ---

func TestHandleRegistry_ListSkills_Empty(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result []registry.AgentSkill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d", len(result))
	}
}

// --- Skills: create ---

func TestHandleRegistry_CreateSkill(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"name":"my-skill","description":"A new skill","state":"active"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.AgentSkill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "my-skill" {
		t.Errorf("expected name %q, got %q", "my-skill", result.Name)
	}
	if result.Description != "A new skill" {
		t.Errorf("expected description %q, got %q", "A new skill", result.Description)
	}
}

func TestHandleRegistry_CreateSkill_InvalidJSON(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader("nope"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRegistry_CreateSkill_ValidationError(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	// Missing description
	body := `{"name":"bad-skill"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRegistry_CreateSkill_Duplicate(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "existing-skill", registry.StateActive)

	handler := srv.Handler()
	body := `{"name":"existing-skill","description":"Duplicate"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

// --- Skills: get ---

func TestHandleRegistry_GetSkill(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "my-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/my-skill", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result registry.AgentSkill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "my-skill" {
		t.Errorf("expected name %q, got %q", "my-skill", result.Name)
	}
}

func TestHandleRegistry_GetSkill_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/nope", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Skills: update ---

func TestHandleRegistry_UpdateSkill(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "updatable-skill", registry.StateDraft)

	handler := srv.Handler()
	body := `{"description":"Updated description","state":"active"}`
	req := httptest.NewRequest(http.MethodPut, "/api/registry/skills/updatable-skill", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.AgentSkill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "updatable-skill" {
		t.Errorf("expected name %q, got %q", "updatable-skill", result.Name)
	}
	if result.State != registry.StateActive {
		t.Errorf("expected state %q, got %q", registry.StateActive, result.State)
	}
}

func TestHandleRegistry_UpdateSkill_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"description":"test"}`
	req := httptest.NewRequest(http.MethodPut, "/api/registry/skills/ghost", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Skills: delete ---

func TestHandleRegistry_DeleteSkill(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "deletable-skill", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodDelete, "/api/registry/skills/deletable-skill", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}

	// Verify it's gone
	req = httptest.NewRequest(http.MethodGet, "/api/registry/skills/deletable-skill", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", rec.Code)
	}
}

// --- Skills: state changes ---

func TestHandleRegistry_ActivateSkill(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "dormant-skill", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/dormant-skill/activate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result registry.AgentSkill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.State != registry.StateActive {
		t.Errorf("expected state %q, got %q", registry.StateActive, result.State)
	}
}

func TestHandleRegistry_DisableSkill(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "active-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/active-skill/disable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result registry.AgentSkill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.State != registry.StateDisabled {
		t.Errorf("expected state %q, got %q", registry.StateDisabled, result.State)
	}
}

func TestHandleRegistry_ActivateSkill_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/ghost/activate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- No registry server ---

func TestHandleRegistry_NoRegistryServer(t *testing.T) {
	srv := newTestServer(t) // no registry configured
	handler := srv.Handler()

	paths := []string{
		"/api/registry/status",
		"/api/registry/skills",
		"/api/registry/skills/test",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("expected 503 for %s, got %d", path, rec.Code)
			}

			var result map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if !strings.Contains(result["error"], "Registry not available") {
				t.Errorf("expected 'Registry not available' error, got %q", result["error"])
			}
		})
	}
}

// --- Unknown path ---

func TestHandleRegistry_UnknownPath(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Method not allowed table-driven ---

func TestHandleRegistry_MethodNotAllowed(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "s1", registry.StateActive)
	handler := srv.Handler()

	tests := []struct {
		path   string
		method string
	}{
		{"/api/registry/status", http.MethodPost},
		{"/api/registry/status", http.MethodPut},
		{"/api/registry/skills", http.MethodDelete},
		{"/api/registry/skills", http.MethodPut},
		{"/api/registry/skills/s1", http.MethodPost},
		{"/api/registry/skills/s1/activate", http.MethodGet},
		{"/api/registry/skills/s1/activate", http.MethodPut},
		{"/api/registry/skills/s1/disable", http.MethodGet},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s %s, got %d", tt.method, tt.path, rec.Code)
			}
		})
	}
}

// --- /api/status includes registry counts ---

func TestHandleStatus_WithRegistryCounts(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "s1", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result struct {
		Registry *registry.RegistryStatus `json:"registry"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Registry == nil {
		t.Fatal("expected registry field in status response")
	}
	if result.Registry.TotalSkills != 1 {
		t.Errorf("expected 1 total skill, got %d", result.Registry.TotalSkills)
	}
}

func TestHandleStatus_WithoutRegistryContent(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	// Registry is configured but empty

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := result["registry"]; ok {
		t.Error("expected no registry field when registry is empty")
	}
}

// --- Progressive disclosure: creating first item registers with router ---

func TestHandleRegistry_ProgressiveDisclosure(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	// Initially, registry status shows 0 skills
	req := httptest.NewRequest(http.MethodGet, "/api/registry/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var statusBefore registry.RegistryStatus
	_ = json.NewDecoder(rec.Body).Decode(&statusBefore)
	if statusBefore.TotalSkills != 0 {
		t.Fatalf("expected 0 skills initially, got %d", statusBefore.TotalSkills)
	}

	// Create an active skill — should register with router
	body := `{"name":"new-skill","description":"A new skill","state":"active"}`
	req = httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Registry status should now show the skill
	req = httptest.NewRequest(http.MethodGet, "/api/registry/status", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var statusAfter registry.RegistryStatus
	_ = json.NewDecoder(rec.Body).Decode(&statusAfter)
	if statusAfter.TotalSkills != 1 {
		t.Errorf("expected 1 skill after creating, got %d", statusAfter.TotalSkills)
	}
	if statusAfter.ActiveSkills != 1 {
		t.Errorf("expected 1 active skill after creating, got %d", statusAfter.ActiveSkills)
	}

	// Skills should NOT appear as tools (skills are knowledge, not tools)
	toolsReq := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	toolsRec := httptest.NewRecorder()
	handler.ServeHTTP(toolsRec, toolsReq)

	var toolsResult mcp.ToolsListResult
	_ = json.NewDecoder(toolsRec.Body).Decode(&toolsResult)

	for _, tool := range toolsResult.Tools {
		if strings.Contains(tool.Name, "new-skill") {
			t.Error("skills should not appear as tools in the aggregated list")
		}
	}
}

// --- Skills: test run ---

func TestHandleRegistry_SkillTest(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "test-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/test-skill/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result mcp.ToolCallResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestHandleRegistry_SkillTest_SkillNotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/nonexistent/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// CallTool returns an IsError ToolCallResult for not-found skills (not an HTTP error)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result mcp.ToolCallResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError true for nonexistent skill")
	}
}

func TestHandleRegistry_SkillTest_MethodNotAllowed(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "test-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/test-skill/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleRegistry_SkillTest_InvalidJSON(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "test-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/test-skill/test", strings.NewReader("{invalid"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}
