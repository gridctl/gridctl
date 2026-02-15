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
	gateway := mcp.NewGateway()
	regServer := registry.New(store, gateway)
	_ = regServer.Initialize(context.Background())

	apiServer := NewServer(gateway, nil)
	apiServer.SetRegistryServer(regServer)
	return apiServer, regServer
}

// seedPrompt creates a prompt in the store for testing.
func seedPrompt(t *testing.T, regServer *registry.Server, name, content string, state registry.ItemState) {
	t.Helper()
	p := &registry.Prompt{
		Name:    name,
		Content: content,
		State:   state,
	}
	if err := regServer.Store().SavePrompt(p); err != nil {
		t.Fatalf("failed to seed prompt: %v", err)
	}
}

// seedSkill creates a skill in the store for testing.
func seedSkill(t *testing.T, regServer *registry.Server, name string, state registry.ItemState) {
	t.Helper()
	sk := &registry.Skill{
		Name:  name,
		Steps: []registry.Step{{Tool: "some-tool"}},
		State: state,
	}
	if err := regServer.Store().SaveSkill(sk); err != nil {
		t.Fatalf("failed to seed skill: %v", err)
	}
}

// --- Status endpoint ---

func TestHandleRegistry_Status(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "p1", "content", registry.StateActive)
	seedPrompt(t, regServer, "p2", "content", registry.StateDraft)
	seedSkill(t, regServer, "s1", registry.StateActive)

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
	if result.TotalPrompts != 2 {
		t.Errorf("expected 2 total prompts, got %d", result.TotalPrompts)
	}
	if result.ActivePrompts != 1 {
		t.Errorf("expected 1 active prompt, got %d", result.ActivePrompts)
	}
	if result.TotalSkills != 1 {
		t.Errorf("expected 1 total skill, got %d", result.TotalSkills)
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

// --- Prompts: list ---

func TestHandleRegistry_ListPrompts_Empty(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/prompts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result []registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d", len(result))
	}
}

func TestHandleRegistry_ListPrompts_WithData(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "greeting", "Hello {{name}}", registry.StateActive)
	seedPrompt(t, regServer, "farewell", "Goodbye", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/prompts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result []registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 prompts, got %d", len(result))
	}
}

// --- Prompts: create ---

func TestHandleRegistry_CreatePrompt(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"name":"test-prompt","content":"Hello world","state":"active"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "test-prompt" {
		t.Errorf("expected name %q, got %q", "test-prompt", result.Name)
	}
	if result.Content != "Hello world" {
		t.Errorf("expected content %q, got %q", "Hello world", result.Content)
	}
	if result.State != registry.StateActive {
		t.Errorf("expected state %q, got %q", registry.StateActive, result.State)
	}
}

func TestHandleRegistry_CreatePrompt_DefaultState(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"name":"no-state","content":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.State != registry.StateDraft {
		t.Errorf("expected default state %q, got %q", registry.StateDraft, result.State)
	}
}

func TestHandleRegistry_CreatePrompt_InvalidJSON(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRegistry_CreatePrompt_ValidationError(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	// Missing content field
	body := `{"name":"bad-prompt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(result["error"], "content") {
		t.Errorf("expected validation error about content, got %q", result["error"])
	}
}

func TestHandleRegistry_CreatePrompt_InvalidName(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"name":"bad name!","content":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRegistry_CreatePrompt_Duplicate(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "existing", "content", registry.StateActive)

	handler := srv.Handler()
	body := `{"name":"existing","content":"new content"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(result["error"], "already exists") {
		t.Errorf("expected 'already exists' in error, got %q", result["error"])
	}
}

// --- Prompts: get ---

func TestHandleRegistry_GetPrompt(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "my-prompt", "Hello {{name}}", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/prompts/my-prompt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "my-prompt" {
		t.Errorf("expected name %q, got %q", "my-prompt", result.Name)
	}
	if result.Content != "Hello {{name}}" {
		t.Errorf("expected content %q, got %q", "Hello {{name}}", result.Content)
	}
}

func TestHandleRegistry_GetPrompt_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/registry/prompts/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Prompts: update ---

func TestHandleRegistry_UpdatePrompt(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "updatable", "old content", registry.StateDraft)

	handler := srv.Handler()
	body := `{"content":"new content","state":"active"}`
	req := httptest.NewRequest(http.MethodPut, "/api/registry/prompts/updatable", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "updatable" {
		t.Errorf("expected name %q, got %q", "updatable", result.Name)
	}
	if result.Content != "new content" {
		t.Errorf("expected content %q, got %q", "new content", result.Content)
	}
	if result.State != registry.StateActive {
		t.Errorf("expected state %q, got %q", registry.StateActive, result.State)
	}
}

func TestHandleRegistry_UpdatePrompt_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	body := `{"content":"anything"}`
	req := httptest.NewRequest(http.MethodPut, "/api/registry/prompts/ghost", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleRegistry_UpdatePrompt_InvalidJSON(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "valid", "content", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPut, "/api/registry/prompts/valid", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleRegistry_UpdatePrompt_ValidationError(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "valid", "content", registry.StateActive)

	handler := srv.Handler()
	// Empty content should fail validation
	body := `{"content":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/registry/prompts/valid", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// --- Prompts: delete ---

func TestHandleRegistry_DeletePrompt(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "deletable", "bye", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodDelete, "/api/registry/prompts/deletable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}

	// Verify it's gone
	req = httptest.NewRequest(http.MethodGet, "/api/registry/prompts/deletable", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", rec.Code)
	}
}

// --- Prompts: state changes ---

func TestHandleRegistry_ActivatePrompt(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "dormant", "content", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts/dormant/activate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.State != registry.StateActive {
		t.Errorf("expected state %q, got %q", registry.StateActive, result.State)
	}
}

func TestHandleRegistry_DisablePrompt(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "active-prompt", "content", registry.StateActive)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts/active-prompt/disable", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result registry.Prompt
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.State != registry.StateDisabled {
		t.Errorf("expected state %q, got %q", registry.StateDisabled, result.State)
	}
}

func TestHandleRegistry_ActivatePrompt_NotFound(t *testing.T) {
	srv, _ := setupRegistryTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/registry/prompts/ghost/activate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleRegistry_ActivatePrompt_MethodNotAllowed(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)
	seedPrompt(t, regServer, "p1", "content", registry.StateDraft)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/registry/prompts/p1/activate", nil)
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

	var result []registry.Skill
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

	body := `{"name":"my-skill","steps":[{"tool":"do-thing"}],"state":"active"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.Skill
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Name != "my-skill" {
		t.Errorf("expected name %q, got %q", "my-skill", result.Name)
	}
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(result.Steps))
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

	// Missing steps
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
	body := `{"name":"existing-skill","steps":[{"tool":"t"}]}`
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

	var result registry.Skill
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
	body := `{"steps":[{"tool":"new-tool"}],"state":"active"}`
	req := httptest.NewRequest(http.MethodPut, "/api/registry/skills/updatable-skill", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result registry.Skill
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

	body := `{"steps":[{"tool":"t"}]}`
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

	var result registry.Skill
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

	var result registry.Skill
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
		"/api/registry/prompts",
		"/api/registry/prompts/test",
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
	seedPrompt(t, regServer, "p1", "content", registry.StateActive)
	seedSkill(t, regServer, "s1", registry.StateActive)
	handler := srv.Handler()

	tests := []struct {
		path   string
		method string
	}{
		{"/api/registry/status", http.MethodPost},
		{"/api/registry/status", http.MethodPut},
		{"/api/registry/prompts", http.MethodDelete},
		{"/api/registry/prompts", http.MethodPut},
		{"/api/registry/prompts/p1", http.MethodPost},
		{"/api/registry/prompts/p1/activate", http.MethodGet},
		{"/api/registry/prompts/p1/activate", http.MethodPut},
		{"/api/registry/prompts/p1/disable", http.MethodGet},
		{"/api/registry/skills", http.MethodDelete},
		{"/api/registry/skills", http.MethodPut},
		{"/api/registry/skills/s1", http.MethodPost},
		{"/api/registry/skills/s1/activate", http.MethodGet},
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
	seedPrompt(t, regServer, "p1", "content", registry.StateActive)
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
	if result.Registry.TotalPrompts != 1 {
		t.Errorf("expected 1 total prompt, got %d", result.Registry.TotalPrompts)
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

	// Initially, no tools from registry
	toolsReq := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	toolsRec := httptest.NewRecorder()
	handler.ServeHTTP(toolsRec, toolsReq)

	var toolsBefore mcp.ToolsListResult
	_ = json.NewDecoder(toolsRec.Body).Decode(&toolsBefore)
	initialToolCount := len(toolsBefore.Tools)

	// Create an active skill — should register with router
	body := `{"name":"new-skill","description":"A new skill","steps":[{"tool":"do-thing"}],"state":"active"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Now tools should include the new skill
	toolsReq = httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	toolsRec = httptest.NewRecorder()
	handler.ServeHTTP(toolsRec, toolsReq)

	var toolsAfter mcp.ToolsListResult
	_ = json.NewDecoder(toolsRec.Body).Decode(&toolsAfter)

	if len(toolsAfter.Tools) != initialToolCount+1 {
		t.Errorf("expected %d tools after creating skill, got %d", initialToolCount+1, len(toolsAfter.Tools))
	}
}

// --- Skills: test run ---

func TestHandleRegistry_SkillTest(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)

	// Create an active skill
	sk := &registry.Skill{
		Name:  "test-skill",
		Steps: []registry.Step{{Tool: "some-tool"}},
		State: registry.StateActive,
	}
	if err := regServer.Store().SaveSkill(sk); err != nil {
		t.Fatalf("failed to seed skill: %v", err)
	}

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
	// The gateway is the toolCaller here; since no real servers are registered,
	// it should return a result (either error or default behavior)
}

func TestHandleRegistry_SkillTest_WithArguments(t *testing.T) {
	srv, regServer := setupRegistryTestServer(t)

	sk := &registry.Skill{
		Name:  "arg-skill",
		Steps: []registry.Step{{Tool: "some-tool", Arguments: map[string]string{"key": "{{input.val}}"}}},
		Input: []registry.Argument{{Name: "val", Description: "A value", Required: true}},
		State: registry.StateActive,
	}
	if err := regServer.Store().SaveSkill(sk); err != nil {
		t.Fatalf("failed to seed skill: %v", err)
	}

	handler := srv.Handler()
	body := `{"val": "hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/registry/skills/arg-skill/test", strings.NewReader(body))
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
