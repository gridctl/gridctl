package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPatchClientModels_InsertIntoFreshMap(t *testing.T) {
	source := []byte(`# my stack
name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
    port: 3000
`)
	out, err := patchClientModels(source, "claude-code", "claude-opus-4-7")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "client_models:") || !strings.Contains(got, "claude-code: claude-opus-4-7") {
		t.Errorf("expected client_models entry; got:\n%s", got)
	}
	if !strings.Contains(got, "# my stack") {
		t.Errorf("comment lost:\n%s", got)
	}
	if strings.Contains(got, "clients:") {
		t.Errorf("model patch must never create a clients: access block:\n%s", got)
	}
}

func TestPatchClientModels_ReplaceExisting(t *testing.T) {
	source := []byte(`name: test
client_models:
  claude-code: claude-opus-4-7   # primary client
  gemini-cli: gemini-2.5-pro
`)
	out, err := patchClientModels(source, "claude-code", "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "claude-code: claude-haiku-4-5") {
		t.Errorf("expected replaced model; got:\n%s", got)
	}
	if !strings.Contains(got, "gemini-cli: gemini-2.5-pro") {
		t.Errorf("sibling entry lost:\n%s", got)
	}
}

func TestPatchClientModels_ClearDeletesKey(t *testing.T) {
	source := []byte(`name: test
client_models:
  claude-code: claude-opus-4-7
  gemini-cli: gemini-2.5-pro
`)
	out, err := patchClientModels(source, "claude-code", "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "claude-code") {
		t.Errorf("cleared key must be deleted, never written as empty:\n%s", got)
	}
	if !strings.Contains(got, "gemini-cli: gemini-2.5-pro") {
		t.Errorf("sibling entry lost:\n%s", got)
	}
}

func TestPatchClientModels_ClearLastEntryDropsMap(t *testing.T) {
	source := []byte(`name: test
client_models:
  claude-code: claude-opus-4-7
mcp-servers:
  - name: github
    image: mcp/github:latest
    port: 3000
`)
	out, err := patchClientModels(source, "claude-code", "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "client_models") {
		t.Errorf("emptied client_models map must be removed entirely:\n%s", got)
	}
	// The result must remain a loadable stack with siblings intact.
	var roundTrip map[string]any
	if err := yaml.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	if roundTrip["name"] != "test" {
		t.Errorf("sibling keys lost: %v", roundTrip)
	}
}

func TestPatchClientModels_ClearAbsentKeyIsNoOp(t *testing.T) {
	source := []byte("name: test\n")
	out, err := patchClientModels(source, "claude-code", "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if strings.Contains(string(out), "client_models") {
		t.Errorf("clearing an absent key must not create the map:\n%s", out)
	}
}

func TestHandleSetClientModel_RoundTrip(t *testing.T) {
	srv := newTestServer(t)
	stackFile := writeTempStack(t, `name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
    port: 3000
`)
	srv.SetStackFile(stackFile)
	handler := srv.Handler()

	// Set.
	req := httptest.NewRequest(http.MethodPut, "/api/clients/claude-code/model",
		strings.NewReader(`{"model":"claude-opus-4-7"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp setClientModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ProfileKey != "claude-code" || resp.Model != "claude-opus-4-7" {
		t.Errorf("response = %+v", resp)
	}
	assertStackContains(t, stackFile, "claude-code: claude-opus-4-7")

	// Clear.
	req = httptest.NewRequest(http.MethodPut, "/api/clients/claude-code/model",
		strings.NewReader(`{"model":""}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertStackNotContains(t, stackFile, "client_models")
}

func TestHandleSetClientModel_NormalizesSlug(t *testing.T) {
	srv := newTestServer(t)
	stackFile := writeTempStack(t, "name: test\n")
	srv.SetStackFile(stackFile)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPut, "/api/clients/Claude%20Code/model",
		strings.NewReader(`{"model":"claude-opus-4-7"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertStackContains(t, stackFile, "claude-code: claude-opus-4-7")
}

func TestHandleSetClientModel_NoStackFile(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPut, "/api/clients/claude-code/model",
		strings.NewReader(`{"model":"claude-opus-4-7"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandlePricingModels(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/pricing/models", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp pricingModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Source == "" {
		t.Error("expected a source name")
	}
	if resp.Models == nil {
		t.Error("models must be a JSON array, never null")
	}
	// The embedded LiteLLM snapshot backs the default source; a flagship
	// Anthropic ID should always be present.
	found := false
	for _, m := range resp.Models {
		if strings.HasPrefix(m, "claude-") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one claude-* model in %d models", len(resp.Models))
	}
}

func TestHandleStatus_ClientModelsAndCostAttribution(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.SetClientModelAttribution(func() map[string]string {
		return map[string]string{"claude-code": "claude-opus-4-7"}
	})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		CostAttribution bool              `json:"cost_attribution"`
		ClientModels    map[string]string `json:"client_models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.CostAttribution {
		t.Error("cost_attribution must be true when only client models are configured")
	}
	if resp.ClientModels["claude-code"] != "claude-opus-4-7" {
		t.Errorf("client_models = %v", resp.ClientModels)
	}
}

// writeTempStack writes a stack YAML into a temp dir and returns its path.
func writeTempStack(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/stack.yaml"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp stack: %v", err)
	}
	return path
}

func assertStackContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stack: %v", err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("stack file missing %q:\n%s", want, data)
	}
}

func assertStackNotContains(t *testing.T, path, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stack: %v", err)
	}
	if strings.Contains(string(data), unwanted) {
		t.Errorf("stack file unexpectedly contains %q:\n%s", unwanted, data)
	}
}
