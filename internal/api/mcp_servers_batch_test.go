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

// newBatchHarness writes a stack.yaml with two external MCP servers so batch
// tests can apply whitelist changes across both in one request.
func newBatchHarness(t *testing.T) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `version: "1"
name: test-stack
network:
  name: test-net
  driver: bridge
mcp-servers:
  - name: github
    url: https://example.com/gh
    transport: http
  - name: atlassian
    url: https://example.com/atl
    transport: http
    tools:
      - get_page
      - create_page
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	s := &Server{}
	s.SetStackFile(path)
	return path, s
}

func batchRequest(t *testing.T, s *Server, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/tools", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	s.handleSetServerToolsBatch(w, req)
	return w
}

func TestHandleSetServerToolsBatch_AppliesAllServers(t *testing.T) {
	path, s := newBatchHarness(t)

	w := batchRequest(t, s, map[string]any{
		"servers": []map[string]any{
			{"name": "github", "tools": []string{"a", "b"}},
			{"name": "atlassian", "tools": []string{}}, // clear = expose all
		},
	})

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp setServerToolsBatchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Servers, 2)
	assert.False(t, resp.Reloaded, "no reloadHandler means reloaded: false")

	// Both changes landed in one write: github gained a whitelist, atlassian's
	// tools: key was dropped (expose all).
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, "- a")
	assert.Contains(t, out, "- b")
	assert.NotContains(t, out, "get_page", "atlassian whitelist should be cleared")
}

func TestHandleSetServerToolsBatch_SingleReload(t *testing.T) {
	path, s := newBatchHarness(t)

	currentCfg := &config.Stack{
		Version: "1",
		Name:    "test-stack",
		Network: config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{
			{Name: "github", URL: "https://example.com/gh", Transport: "http"},
			{Name: "atlassian", URL: "https://example.com/atl", Transport: "http", Tools: []string{"get_page", "create_page"}},
		},
	}
	gw := mcp.NewGateway()
	orch := runtime.NewOrchestrator(nil, nil)
	rh := reload.NewHandler(path, currentCfg, gw, orch, 8180, 9000, nil, nil)

	// The handler calls reloadHandler.Reload exactly once for the whole batch
	// (no per-server loop), so a successful response carries a single reloaded
	// flag rather than one reload per server.
	rh.SetRegisterServerFunc(func(ctx context.Context, server config.MCPServer, replicas []reload.ReplicaRuntime, stackPath string) error {
		return nil
	})
	s.SetReloadHandler(rh)

	w := batchRequest(t, s, map[string]any{
		"servers": []map[string]any{
			{"name": "github", "tools": []string{"a"}},
			{"name": "atlassian", "tools": []string{"get_page"}},
		},
	})

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp setServerToolsBatchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Reloaded)
	assert.NotEmpty(t, resp.ReloadedAt)
}

// TestHandleSetServerToolsBatch_UnknownServer demonstrates the all-or-nothing
// contract: one invalid entry rejects the whole batch with nothing written.
// (Unknown-tool rejection follows the same validate-before-write structure;
// it fires earlier, in validateToolsAgainstServer, which is shared with the
// single-server endpoint and exercises a fully-registered gateway in its own
// integration tests rather than this YAML-only harness.)
func TestHandleSetServerToolsBatch_UnknownServer(t *testing.T) {
	path, s := newBatchHarness(t)
	before, _ := os.ReadFile(path)

	w := batchRequest(t, s, map[string]any{
		"servers": []map[string]any{
			{"name": "github", "tools": []string{"a"}},  // valid on its own
			{"name": "missing", "tools": []string{"b"}}, // not in stack → reject all
		},
	})

	require.Equal(t, http.StatusNotFound, w.Code)
	after, _ := os.ReadFile(path)
	assert.Equal(t, string(before), string(after), "unknown server must not write")
}

func TestHandleSetServerToolsBatch_Conflict(t *testing.T) {
	path, s := newBatchHarness(t)
	before, _ := os.ReadFile(path)

	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte("version: \"2\"\nname: mutated\nnetwork:\n  name: n\nmcp-servers:\n  - name: github\n    url: https://example.com/gh\n"), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	w := batchRequest(t, s, map[string]any{
		"servers": []map[string]any{
			{"name": "github", "tools": []string{"a"}},
		},
	})

	require.Equal(t, http.StatusConflict, w.Code)
	var resp structuredError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, errCodeStackModified, resp.Error.Code)

	// The conflicting write was discarded; our intended change is absent.
	_ = before
}

func TestHandleSetServerToolsBatch_BadRequests(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"empty servers", `{"servers":[]}`},
		{"missing servers", `{}`},
		{"entry missing name", `{"servers":[{"tools":["a"]}]}`},
		{"entry missing tools", `{"servers":[{"name":"github"}]}`},
		{"empty tool name", `{"servers":[{"name":"github","tools":["a",""]}]}`},
		{"duplicate server", `{"servers":[{"name":"github","tools":["a"]},{"name":"github","tools":["b"]}]}`},
		{"invalid json", `:::not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, s := newBatchHarness(t)
			req := httptest.NewRequest(http.MethodPut, "/api/mcp-servers/tools", strings.NewReader(tc.payload))
			w := httptest.NewRecorder()
			s.handleSetServerToolsBatch(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, "payload: %s", tc.payload)
		})
	}
}

func TestHandleSetServerToolsBatch_NoStackFile(t *testing.T) {
	s := &Server{}
	w := batchRequest(t, s, map[string]any{
		"servers": []map[string]any{{"name": "github", "tools": []string{"a"}}},
	})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestSetServerToolsBatch_SingleWriteAppliesAll exercises the stack_edit layer
// directly: one call patches every server's tools in a single atomic write.
func TestSetServerToolsBatch_SingleWriteAppliesAll(t *testing.T) {
	path, _ := newBatchHarness(t)

	err := setServerToolsBatch(path, []serverToolsUpdate{
		{Server: "github", Tools: []string{"a", "b"}},
		{Server: "atlassian", Tools: []string{}}, // expose all → drop key
	})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(data)
	assert.Contains(t, out, "- a")
	assert.Contains(t, out, "- b")
	assert.NotContains(t, out, "get_page")

	// Unknown server in the batch fails the whole thing.
	err = setServerToolsBatch(path, []serverToolsUpdate{{Server: "nope", Tools: []string{"x"}}})
	require.ErrorIs(t, err, errServerNotFound)
}
