package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPatchServerModel_InsertIntoServer(t *testing.T) {
	source := []byte(`# my stack
name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
    port: 3000
  - name: gitlab
    image: mcp/gitlab:latest
    port: 3001
`)
	out, err := patchServerModel(source, "github", "claude-opus-4-7")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "model: claude-opus-4-7") {
		t.Errorf("expected model entry; got:\n%s", got)
	}
	if !strings.Contains(got, "# my stack") {
		t.Errorf("comment lost:\n%s", got)
	}
	if !strings.Contains(got, "name: gitlab") {
		t.Errorf("sibling server lost:\n%s", got)
	}
	// The sibling must not gain a model.
	if strings.Count(got, "model:") != 1 {
		t.Errorf("expected exactly one model: key; got:\n%s", got)
	}
}

func TestPatchServerModel_ReplaceExisting(t *testing.T) {
	source := []byte(`name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
    model: claude-opus-4-7   # priced as opus
`)
	out, err := patchServerModel(source, "github", "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "model: claude-haiku-4-5") {
		t.Errorf("expected replaced model; got:\n%s", got)
	}
	if strings.Contains(got, "claude-opus-4-7") {
		t.Errorf("old model survived:\n%s", got)
	}
}

func TestPatchServerModel_ClearDeletesKey(t *testing.T) {
	source := []byte(`name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
    model: claude-opus-4-7
    port: 3000
`)
	out, err := patchServerModel(source, "github", "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "model") {
		t.Errorf("cleared key must be deleted, never written as empty:\n%s", got)
	}
	if !strings.Contains(got, "port: 3000") {
		t.Errorf("sibling field lost:\n%s", got)
	}
	// The result must remain a loadable stack.
	var roundTrip map[string]any
	if err := yaml.Unmarshal(out, &roundTrip); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
}

func TestPatchServerModel_ClearAbsentKeyIsNoOp(t *testing.T) {
	source := []byte(`name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	out, err := patchServerModel(source, "github", "")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if strings.Contains(string(out), "model") {
		t.Errorf("clearing an absent key must not create it:\n%s", out)
	}
}

func TestPatchServerModel_ServerNotFound(t *testing.T) {
	source := []byte(`name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	_, err := patchServerModel(source, "missing", "claude-opus-4-7")
	if !errors.Is(err, errServerNotFound) {
		t.Errorf("err = %v, want errServerNotFound", err)
	}
}

func TestHandleSetServerModel_RoundTrip(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/github/model",
		strings.NewReader(`{"model":"claude-opus-4-7"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp setServerModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Server != "github" || resp.Model != "claude-opus-4-7" {
		t.Errorf("response = %+v", resp)
	}
	assertStackContains(t, stackFile, "model: claude-opus-4-7")

	// Clear.
	req = httptest.NewRequest(http.MethodPut, "/api/mcp-servers/github/model",
		strings.NewReader(`{"model":""}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertStackNotContains(t, stackFile, "model:")
}

func TestHandleSetServerModel_UnknownServer(t *testing.T) {
	srv := newTestServer(t)
	stackFile := writeTempStack(t, `name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	srv.SetStackFile(stackFile)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/missing/model",
		strings.NewReader(`{"model":"claude-opus-4-7"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleSetServerModel_NoStackFile(t *testing.T) {
	srv := newTestServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/github/model",
		strings.NewReader(`{"model":"claude-opus-4-7"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestSetServerModel_ConflictOnExternalEdit(t *testing.T) {
	stackFile := writeTempStack(t, `name: test
mcp-servers:
  - name: github
    image: mcp/github:latest
`)
	swapBetweenReadsHook(func() {
		// Simulate an external editor landing a change between our initial
		// read and the pre-write re-read.
		_ = os.WriteFile(stackFile, []byte("name: test\n# externally edited\n"), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	err := setServerModel(stackFile, "github", "claude-opus-4-7")
	if !errors.Is(err, errStackModified) {
		t.Errorf("err = %v, want errStackModified", err)
	}
}

func TestHandleStatus_ServerModelsAndDefaultModel(t *testing.T) {
	srv := newTestServerWithMetrics(t)
	srv.SetModelAttribution(func() map[string]string {
		return map[string]string{"github": "claude-opus-4-7", "gitlab": "claude-haiku-4-5"}
	})
	srv.SetDeclaredServerModels(func() map[string]string {
		return map[string]string{"github": "claude-opus-4-7"}
	})
	srv.SetDefaultModel(func() string { return "claude-haiku-4-5" })
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		CostAttribution bool              `json:"cost_attribution"`
		ServerModels    map[string]string `json:"server_models"`
		DefaultModel    string            `json:"default_model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.CostAttribution {
		t.Error("cost_attribution must be true when server models are configured")
	}
	if resp.ServerModels["github"] != "claude-opus-4-7" || resp.ServerModels["gitlab"] != "claude-haiku-4-5" {
		t.Errorf("server_models = %v", resp.ServerModels)
	}
	if resp.DefaultModel != "claude-haiku-4-5" {
		t.Errorf("default_model = %q", resp.DefaultModel)
	}
}
