package api

import (
	"context"
	"encoding/json"
	"io"
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
		{"/api/registry/skills/validate", http.MethodGet},
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

// --- Progressive disclosure: deleting last item deregisters from router ---

func TestHandleRegistry_ProgressiveDisclosure_Deregister(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "only-skill", registry.StateActive)

	handler := srv.Handler()

	// Verify the skill is registered (create triggers refreshRegistryRouter)
	srv.refreshRegistryRouter()
	client := srv.gateway.Router().GetClient("registry")
	if client == nil {
		t.Fatal("expected registry to be registered after seeding skill")
	}

	// Delete the only skill
	req := httptest.NewRequest(http.MethodDelete, "/api/registry/skills/only-skill", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	// Registry should be deregistered from router
	client = srv.gateway.Router().GetClient("registry")
	if client != nil {
		t.Error("expected registry to be deregistered after deleting last skill")
	}
}

// --- Validation endpoint ---

func TestHandleRegistry_Validate_Valid(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	content := "---\nname: test-skill\ndescription: A test\n---\n\n# Body\n\nInstructions here."
	body := `{"content":"` + strings.ReplaceAll(content, "\n", "\\n") + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
}

func TestHandleRegistry_Validate_Invalid(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	// Missing description
	content := "---\nname: test-skill\n---\n\n# Body"
	body := `{"content":"` + strings.ReplaceAll(content, "\n", "\\n") + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/validate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != false {
		t.Errorf("expected valid=false, got %v", result["valid"])
	}
}

func TestHandleRegistry_Validate_InvalidJSON(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/validate", strings.NewReader("nope"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// --- File management ---

func TestHandleRegistry_ListFiles_Empty(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "file-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/file-skill/files", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result []registry.SkillFile
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty file list, got %d", len(result))
	}
}

func TestHandleRegistry_WriteAndReadFile(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "file-skill", registry.StateActive)

	handler := srv.Handler()

	// Write a file
	fileContent := "#!/bin/bash\necho hello"
	req := httptest.NewRequest(http.MethodPut, "/api/registry/skills/file-skill/files/scripts/test.sh", strings.NewReader(fileContent))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for write, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Read the file back
	req = httptest.NewRequest(http.MethodGet, "/api/registry/skills/file-skill/files/scripts/test.sh", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for read, got %d", rec.Code)
	}

	data, _ := io.ReadAll(rec.Body)
	if string(data) != fileContent {
		t.Errorf("expected content %q, got %q", fileContent, string(data))
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "text/x-shellscript" {
		t.Errorf("expected Content-Type text/x-shellscript, got %q", ct)
	}

	// List files should show the new file
	req = httptest.NewRequest(http.MethodGet, "/api/registry/skills/file-skill/files", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var files []registry.SkillFile
	_ = json.NewDecoder(rec.Body).Decode(&files)
	found := false
	for _, f := range files {
		if strings.Contains(f.Path, "test.sh") {
			found = true
		}
	}
	if !found {
		t.Error("expected test.sh in file listing")
	}
}

func TestHandleRegistry_DeleteFile(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "file-skill", registry.StateActive)

	handler := srv.Handler()

	// Write a file first
	req := httptest.NewRequest(http.MethodPut, "/api/registry/skills/file-skill/files/scripts/temp.sh", strings.NewReader("temp"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for write, got %d", rec.Code)
	}

	// Delete it
	req = httptest.NewRequest(http.MethodDelete, "/api/registry/skills/file-skill/files/scripts/temp.sh", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for delete, got %d", rec.Code)
	}

	// Reading should return 404 or error
	req = httptest.NewRequest(http.MethodGet, "/api/registry/skills/file-skill/files/scripts/temp.sh", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestHandleRegistry_Files_SkillNotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/nonexistent/files", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent skill files, got %d", rec.Code)
	}
}

func TestHandleRegistry_Files_MethodNotAllowed(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "file-skill", registry.StateActive)
	handler := srv.Handler()

	// POST to files listing should be method not allowed
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/file-skill/files", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Workflow endpoints ---

// seedWorkflowSkill creates a skill with a workflow definition for testing.
func seedWorkflowSkill(t *testing.T, regServer *registry.Server, name string) {
	t.Helper()
	sk := &registry.AgentSkill{
		Name:        name,
		Description: "Workflow skill: " + name,
		State:       registry.StateActive,
		Body:        "# " + name,
		Inputs: map[string]registry.SkillInput{
			"target": {Type: "string", Required: true, Description: "Target host"},
		},
		Workflow: []registry.WorkflowStep{
			{ID: "step-a", Tool: "server__ping", Args: map[string]any{
				"host": "{{ inputs.target }}",
			}},
			{ID: "step-b", Tool: "server__scan", DependsOn: registry.StringOrSlice{"step-a"}, Args: map[string]any{
				"host": "{{ inputs.target }}",
			}},
		},
		Output: &registry.WorkflowOutput{Format: "merged"},
	}
	if err := regServer.Store().SaveSkill(sk); err != nil {
		t.Fatalf("failed to seed workflow skill: %v", err)
	}
}

func TestHandleRegistry_GetWorkflow(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedWorkflowSkill(t, regServer, "wf-skill")

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/wf-skill/workflow", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["name"] != "wf-skill" {
		t.Errorf("expected name 'wf-skill', got %v", result["name"])
	}
	if result["workflow"] == nil {
		t.Error("expected workflow field")
	}
	if result["dag"] == nil {
		t.Error("expected dag field")
	}
	dag := result["dag"].(map[string]any)
	if dag["levels"] == nil {
		t.Error("expected dag.levels field")
	}
}

func TestHandleRegistry_GetWorkflow_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/nonexistent/workflow", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleRegistry_GetWorkflow_NonExecutable(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "knowledge-skill", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/skills/knowledge-skill/workflow", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRegistry_ValidateWorkflow(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedWorkflowSkill(t, regServer, "validate-wf")

	handler := srv.Handler()
	body := `{"arguments":{"target":"10.1.1.1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/validate-wf/validate-workflow", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v; errors: %v", result["valid"], result["errors"])
	}
	if result["resolvedArgs"] == nil {
		t.Error("expected resolvedArgs field")
	}
}

func TestHandleRegistry_ExecuteWorkflow_NonExecutable(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedSkill(t, regServer, "knowledge-skill", registry.StateActive)

	handler := srv.Handler()
	body := `{"arguments":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/knowledge-skill/execute", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleRegistry_ExecuteWorkflow_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"arguments":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/nonexistent/execute", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleRegistry_ValidateWorkflow_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"arguments":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/nonexistent/validate-workflow", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Content type detection ---

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"readme.md", "text/markdown"},
		{"script.sh", "text/x-shellscript"},
		{"main.py", "text/x-python"},
		{"config.json", "application/json"},
		{"stack.yaml", "text/yaml"},
		{"stack.yml", "text/yaml"},
		{"data.csv", "text/csv"},
		{"binary.bin", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			ct := detectContentType(tt.path)
			if ct != tt.expected {
				t.Errorf("detectContentType(%q) = %q, want %q", tt.path, ct, tt.expected)
			}
		})
	}
}
