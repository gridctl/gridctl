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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSetServerTools_HiddenToolReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	original := []byte(`version: "1"
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    tools: ["list_paths", "read_secret"]
groups:
  ops:
    tools: ["vault__read_secret"]
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))
	s := &Server{}
	s.SetStackFile(path)
	s.SetReloadHandler(reload.NewHandler(path, &config.Stack{Name: "test-stack"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil))

	body, _ := json.Marshal(map[string]any{"tools": []string{"list_paths"}})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/vault/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "vault")
	w := httptest.NewRecorder()
	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"hidden_tool_reference"`)
	assert.Contains(t, w.Body.String(), "groups[ops].tools[0]")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestHandleSetServerToolsBatch_HiddenToolReference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	original := []byte(`version: "1"
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    tools: ["list_paths", "read_secret"]
groups:
  ops:
    tools: ["vault__read_secret"]
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))
	s := &Server{}
	s.SetStackFile(path)
	s.SetReloadHandler(reload.NewHandler(path, &config.Stack{Name: "test-stack"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil))

	body, _ := json.Marshal(map[string]any{
		"servers": []map[string]any{{"name": "vault", "tools": []string{"list_paths"}}},
	})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/tools", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	s.handleSetServerToolsBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"hidden_tool_reference"`)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestHandleSetServerTools_ExtendsParentWhitelist(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.yaml")
	childPath := filepath.Join(dir, "child.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(`version: "1"
name: parent
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    tools: ["list_paths"]
`), 0o600))
	original := []byte(`version: "1"
name: child
extends: parent.yaml
network:
  name: test-net
mcp-servers:
  - name: notes
    url: https://example.test/notes
groups:
  ops:
    tools: ["vault__read_secret"]
`)
	require.NoError(t, os.WriteFile(childPath, original, 0o600))
	s := &Server{}
	s.SetStackFile(childPath)
	s.SetReloadHandler(reload.NewHandler(childPath, &config.Stack{Name: "child"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil))

	body, _ := json.Marshal(map[string]any{"tools": []string{"list"}})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/notes/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "notes")
	w := httptest.NewRecorder()
	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"hidden_tool_reference"`)
	assert.NotContains(t, w.Body.String(), "unknown MCP server")
	got, err := os.ReadFile(childPath)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestHandleSetServerTools_OtherValidationIsNotHiddenTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	original := []byte(`version: "1"
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
groups:
  BAD:
    tools: ["vault__list"]
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))
	s := &Server{}
	s.SetStackFile(path)
	s.SetReloadHandler(reload.NewHandler(path, &config.Stack{Name: "test-stack"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil))

	body, _ := json.Marshal(map[string]any{"tools": []string{"list"}})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/vault/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "vault")
	w := httptest.NewRecorder()
	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "hidden_tool_reference")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

type emptyVault struct{}

func (emptyVault) Get(string) (string, bool) { return "", false }

func TestHandleSetServerTools_MissingVariableIs400(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	original := []byte(`version: "1"
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
    headers:
      X-Tenant: "${vault:MISSING}"
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))
	s := &Server{}
	s.SetStackFile(path)
	s.SetReloadHandler(reload.NewHandler(path, &config.Stack{Name: "test-stack"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, emptyVault{}, nil))

	body, _ := json.Marshal(map[string]any{"tools": []string{"list"}})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/vault/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "vault")
	w := httptest.NewRecorder()
	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "hidden_tool_reference")
	assert.Contains(t, w.Body.String(), "missing variable(s): MISSING")
	assert.NotContains(t, w.Body.String(), "Failed to update stack")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestHandleSetServerToolsBatch_UnreadableExtendsIs400(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	original := []byte(`version: "1"
name: test-stack
extends: missing-parent.yaml
network:
  name: test-net
mcp-servers:
  - name: vault
    url: https://example.test/mcp
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))
	s := &Server{}
	s.SetStackFile(path)
	s.SetReloadHandler(reload.NewHandler(path, &config.Stack{Name: "test-stack"}, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil))

	body, _ := json.Marshal(map[string]any{
		"servers": []map[string]any{{"name": "vault", "tools": []string{"list"}}},
	})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/tools", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	s.handleSetServerToolsBatch(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "hidden_tool_reference")
	assert.Contains(t, w.Body.String(), "extends: reading parent stack")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestHandleSetServerTools_ReadErrorStays500(t *testing.T) {
	s := &Server{}
	s.SetStackFile(filepath.Join(t.TempDir(), "missing.yaml"))
	body, _ := json.Marshal(map[string]any{"tools": []string{}})
	req := loopbackRequest(http.MethodPut, "/api/mcp-servers/vault/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "vault")
	w := httptest.NewRecorder()
	s.handleSetServerTools(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
