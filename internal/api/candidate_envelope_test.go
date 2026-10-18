package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestHandleStackInitialize_HiddenToolErrorsEnvelope(t *testing.T) {
	dir := t.TempDir()
	const name = "hidden"
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(`version: "1"
name: hidden
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    tools: ["list_paths"]
groups:
  ops:
    tools: ["vault__read_secret"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := reload.NewHandler("", nil, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	s := &Server{stacksDir: dir, reloadHandler: h}
	w := httptest.NewRecorder()
	s.handleStackInitialize(w, loopbackRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(`{"name":"hidden"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "invalid_candidate" {
		t.Fatalf("code = %#v", body["code"])
	}
	want := "groups[ops].tools[0]: tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called"
	assertErrorList(t, body["errors"], want)
	nested, _ := body["error"].(map[string]any)
	if nested["code"] != "invalid_candidate" {
		t.Fatalf("nested = %#v", body["error"])
	}
	assertErrorList(t, nested["errors"], want)
	if s.stackFile != "" {
		t.Fatal("initialize accepted a rejected candidate")
	}
}

func TestHandleReload_HiddenToolErrorsEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte(`version: "1"
name: hidden
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    tools: ["list_paths"]
groups:
  ops:
    tools: ["vault__read_secret"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := reload.NewHandler(path, &config.Stack{Name: "active"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	s := &Server{}
	s.SetStackFile(path)
	s.SetReloadHandler(h)
	w := httptest.NewRecorder()
	s.handleReload(w, loopbackRequest(http.MethodPost, "/api/reload", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := "groups[ops].tools[0]: tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called"
	assertErrorList(t, body["errors"], want)
	nested, _ := body["error"].(map[string]any)
	assertErrorList(t, nested["errors"], want)
}

func TestWritePreflightFailure_OmitsEmptyErrors(t *testing.T) {
	w := httptest.NewRecorder()
	if !writePreflightFailure(w, &reload.ReloadResult{Code: "invalid_candidate", Message: "unchanged"}) {
		t.Fatal("expected preflight write")
	}
	if strings.Contains(w.Body.String(), `"errors"`) {
		t.Fatalf("empty errors serialized: %s", w.Body.String())
	}
}

func assertErrorList(t *testing.T, raw any, want string) {
	t.Helper()
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("errors = %#v", raw)
	}
	found := false
	for _, item := range items {
		text, _ := item.(string)
		if strings.Contains(text, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("errors = %#v, want %s", raw, want)
	}
}
