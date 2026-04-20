package api

import (
	"context"
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

// newStackHarness writes a stack.yaml with one external MCP server ("ext")
// that the tests can freely rewrite tools on. Returns the stack path and a
// Server wired with the stack path set.
func newStackHarness(t *testing.T) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `version: "1"
name: test-stack
network:
  name: test-net
  driver: bridge
mcp-servers:
  - name: ext
    url: https://example.com/mcp
    transport: http
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	s := &Server{}
	s.SetStackFile(path)
	return path, s
}

func TestHandleSetServerTools_HappyPath_NoReload(t *testing.T) {
	path, s := newStackHarness(t)

	body, _ := json.Marshal(map[string]any{"tools": []string{"a", "b"}})
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp setServerToolsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ext", resp.Server)
	assert.Equal(t, []string{"a", "b"}, resp.Tools)
	assert.False(t, resp.Reloaded, "no reloadHandler means reloaded: false")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "- a")
	assert.Contains(t, string(data), "- b")
}

func TestHandleSetServerTools_HappyPath_WithReload(t *testing.T) {
	path, s := newStackHarness(t)

	// currentCfg must mirror the stack on disk so Reload's diff sees only the
	// tools: change and not a spurious network change (which would otherwise
	// short-circuit into a "full restart required" error).
	currentCfg := &config.Stack{
		Version: "1",
		Name:    "test-stack",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "ext", URL: "https://example.com/mcp", Transport: "http"},
		},
	}
	gw := mcp.NewGateway()
	orch := runtime.NewOrchestrator(nil, nil)
	rh := reload.NewHandler(path, currentCfg, gw, orch, 8180, 9000, nil, nil)
	rh.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		return nil
	})
	s.SetReloadHandler(rh)

	body, _ := json.Marshal(map[string]any{"tools": []string{"a"}})
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(string(body)))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp setServerToolsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Reloaded)
	assert.NotEmpty(t, resp.ReloadedAt)
}

func TestHandleSetServerTools_EmptyListClearsField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `version: "1"
name: test-stack
network:
  name: test-net
  driver: bridge
mcp-servers:
  - name: ext
    url: https://example.com/mcp
    transport: http
    tools:
      - a
      - b
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	s := &Server{}
	s.SetStackFile(path)

	body := `{"tools":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(body))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "tools:")
}

func TestHandleSetServerTools_InvalidJSON(t *testing.T) {
	_, s := newStackHarness(t)

	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(":::not json"))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetServerTools_MissingToolsField(t *testing.T) {
	_, s := newStackHarness(t)

	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(`{}`))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tools array")
}

func TestHandleSetServerTools_EmptyToolName(t *testing.T) {
	_, s := newStackHarness(t)

	body := `{"tools":["a",""]}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(body))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "non-empty")
}

func TestHandleSetServerTools_UnknownServer(t *testing.T) {
	_, s := newStackHarness(t)

	body := `{"tools":["a"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/missing/tools", strings.NewReader(body))
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleSetServerTools_Conflict(t *testing.T) {
	path, s := newStackHarness(t)

	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte("version: \"2\"\nname: mutated\nnetwork:\n  name: n\nmcp-servers:\n  - name: ext\n    url: https://example.com/mcp\n"), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	body := `{"tools":["a"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(body))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp structuredError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, errCodeStackModified, resp.Error.Code)
	assert.NotEmpty(t, resp.Error.Hint)
}

func TestHandleSetServerTools_NoStackFile(t *testing.T) {
	s := &Server{}

	body := `{"tools":["a"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(body))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleSetServerTools_ReloadFailure(t *testing.T) {
	path, s := newStackHarness(t)

	currentCfg := &config.Stack{
		Version: "1",
		Name:    "test-stack",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "ext", URL: "https://example.com/mcp", Transport: "http"},
		},
	}
	gw := mcp.NewGateway()
	orch := runtime.NewOrchestrator(nil, nil)
	rh := reload.NewHandler(path, currentCfg, gw, orch, 8180, 9000, nil, nil)
	// The registrar errors on every call; since this server is external the
	// reload still invokes the registrar and accumulates the error.
	rh.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		return assertErrForTest("register failed")
	})
	s.SetReloadHandler(rh)

	body := `{"tools":["a"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/ext/tools", strings.NewReader(body))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()

	s.handleSetServerTools(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	var resp structuredError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, errCodeReloadFailed, resp.Error.Code)

	// Crucially, the YAML write must have succeeded despite the reload failure.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "- a")
}

// assertErrForTest returns a plain error value. A tiny helper purely to make
// the test literal's intent obvious.
func assertErrForTest(msg string) error { return &testErr{msg} }

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
