package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// telemetryHandlerFixture is a minimal valid stack with one server and a
// pre-populated telemetry block. Comments and non-canonical key order
// (transport before url) are present so round-trip preservation regressions
// fail loudly.
const telemetryHandlerFixture = `# top-of-file comment — must survive round-trip
version: "1"
name: example
network:
  name: example-net
  driver: bridge
telemetry:
  persist:
    logs: false
    metrics: false
    traces: false
  retention:
    max_size_mb: 100
    max_backups: 5
    max_age_days: 7
mcp-servers:
  - name: github
    transport: http
    url: https://api.github.com/mcp
`

func patchStackTelemetryRequest(t *testing.T, server *Server, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/api/stack/telemetry", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	server.handlePatchStackTelemetry(w, req)
	return w
}

func patchServerTelemetryRequest(t *testing.T, server *Server, name string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/api/mcp-servers/"+name+"/telemetry", strings.NewReader(string(raw)))
	req.SetPathValue("name", name)
	w := httptest.NewRecorder()
	server.handlePatchServerTelemetry(w, req)
	return w
}

func TestHandlePatchStackTelemetry_PreservesCommentsAndOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	s := &Server{stackFile: path}
	w := patchStackTelemetryRequest(t, s, map[string]any{
		"persist": map[string]any{"logs": true},
	})
	require.Equal(t, http.StatusOK, w.Code)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "# top-of-file comment")

	// transport must remain before url (struct round-trip would reverse).
	transportIdx := strings.Index(got, "transport: http")
	urlIdx := strings.Index(got, "url: https://api.github.com/mcp")
	require.Greater(t, transportIdx, 0)
	require.Greater(t, urlIdx, 0)
	assert.Less(t, transportIdx, urlIdx)

	// logs flipped, others retained.
	assert.Regexp(t, `(?m)^\s*logs:\s*true`, got)
	assert.Regexp(t, `(?m)^\s*metrics:\s*false`, got)
	assert.Regexp(t, `(?m)^\s*traces:\s*false`, got)

	// Response carries an inventory array (empty in this no-files test).
	var resp telemetryPatchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.NotNil(t, resp.Inventory)
}

func TestHandlePatchStackTelemetry_ConflictWhenDiskChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	external := telemetryHandlerFixture + "\n# touched externally\n"
	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte(external), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	s := &Server{stackFile: path}
	w := patchStackTelemetryRequest(t, s, map[string]any{
		"persist": map[string]any{"logs": true},
	})

	assert.Equal(t, http.StatusConflict, w.Code)

	var resp structuredError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, errCodeStackModified, resp.Error.Code)

	// On-disk file retains the external edit; intended PATCH did not land.
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, external, string(got))
	assert.NotRegexp(t, `(?m)^\s*logs:\s*true`, string(got))
}

func TestHandlePatchStackTelemetry_AtomicOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(realPath, []byte(telemetryHandlerFixture), 0o600))

	missing := filepath.Join(dir, "nonexistent-subdir", "stack.yaml")
	s := &Server{stackFile: missing}
	w := patchStackTelemetryRequest(t, s, map[string]any{
		"persist": map[string]any{"logs": true},
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Original file is bit-for-bit unchanged.
	got, err := os.ReadFile(realPath)
	require.NoError(t, err)
	assert.Equal(t, telemetryHandlerFixture, string(got))

	// No leftover temp files.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".tmp."), "leftover temp file: %s", e.Name())
	}
}

func TestHandlePatchStackTelemetry_SerializesConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	s := &Server{stackFile: path}

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		w := patchStackTelemetryRequest(t, s, map[string]any{
			"persist": map[string]any{"logs": true},
		})
		codes[0] = w.Code
	}()
	go func() {
		defer wg.Done()
		w := patchStackTelemetryRequest(t, s, map[string]any{
			"persist": map[string]any{"metrics": true},
		})
		codes[1] = w.Code
	}()
	wg.Wait()

	assert.Equal(t, http.StatusOK, codes[0])
	assert.Equal(t, http.StatusOK, codes[1])

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	// Both writes landed.
	assert.Regexp(t, `(?m)^\s*logs:\s*true`, got)
	assert.Regexp(t, `(?m)^\s*metrics:\s*true`, got)

	// Comments survived and the file is well-formed.
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal(out, &node))
	assert.Contains(t, got, "# top-of-file comment")
}

func TestHandlePatchStackTelemetry_RejectsEmptyBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	s := &Server{stackFile: path}
	w := patchStackTelemetryRequest(t, s, map[string]any{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandlePatchStackTelemetry_RejectsInvalidRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	s := &Server{stackFile: path}
	// 99999 backups is above telemetryMaxBackupsHardCap (1024). SetDefaults
	// only fills zeros, so a non-zero out-of-range value survives to the
	// validator and surfaces the 422 we expect.
	w := patchStackTelemetryRequest(t, s, map[string]any{
		"retention": map[string]any{"max_backups": 99999},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// File on disk unchanged.
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, telemetryHandlerFixture, string(got))
}

func TestHandlePatchStackTelemetry_NoStackFile(t *testing.T) {
	s := &Server{}
	w := patchStackTelemetryRequest(t, s, map[string]any{
		"persist": map[string]any{"logs": true},
	})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandlePatchServerTelemetry_AddsBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	s := &Server{stackFile: path}
	w := patchServerTelemetryRequest(t, s, "github", map[string]any{
		"persist": map[string]any{"logs": true, "traces": false},
	})
	require.Equal(t, http.StatusOK, w.Code)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	// Comments and order preserved.
	assert.Contains(t, got, "# top-of-file comment")
	transportIdx := strings.Index(got, "transport: http")
	urlIdx := strings.Index(got, "url: https://api.github.com/mcp")
	assert.Less(t, transportIdx, urlIdx)

	// Github now has the override; metrics absent because not specified.
	gIdx := strings.Index(got, "name: github")
	require.Greater(t, gIdx, 0)
	tail := got[gIdx:]
	assert.Contains(t, tail, "telemetry:")
	assert.Regexp(t, `(?m)^\s*logs:\s*true`, tail)
	assert.Regexp(t, `(?m)^\s*traces:\s*false`, tail)
}

func TestHandlePatchServerTelemetry_ClearAllRemovesBlock(t *testing.T) {
	// First seed a server-level override, then clear it.
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	seed := strings.ReplaceAll(telemetryHandlerFixture,
		"    url: https://api.github.com/mcp\n",
		"    url: https://api.github.com/mcp\n    telemetry:\n      persist:\n        logs: true\n",
	)
	require.NoError(t, os.WriteFile(path, []byte(seed), 0o600))

	s := &Server{stackFile: path}
	// persist:null clears the entire server telemetry block.
	w := patchServerTelemetryRequest(t, s, "github", map[string]json.RawMessage{
		"persist": json.RawMessage("null"),
	})
	require.Equal(t, http.StatusOK, w.Code)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)
	gIdx := strings.Index(got, "name: github")
	require.Greater(t, gIdx, 0)
	tail := got[gIdx:]
	assert.NotContains(t, tail, "telemetry:")
	assert.NotContains(t, tail, "persist:")
}

func TestHandlePatchServerTelemetry_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	s := &Server{stackFile: path}
	w := patchServerTelemetryRequest(t, s, "missing", map[string]any{
		"persist": map[string]any{"logs": true},
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandlePatchServerTelemetry_ConflictWhenDiskChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(telemetryHandlerFixture), 0o600))

	external := telemetryHandlerFixture + "\n# touched externally\n"
	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte(external), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	s := &Server{stackFile: path}
	w := patchServerTelemetryRequest(t, s, "github", map[string]any{
		"persist": map[string]any{"logs": true},
	})
	assert.Equal(t, http.StatusConflict, w.Code)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, external, string(got))
}

func TestHandlePatchServerTelemetry_SerializesConcurrentCallers(t *testing.T) {
	// Same fixture extended with a second server so the two PATCHes target
	// different entries (proving the per-path lock serializes both writes
	// without lost updates).
	source := strings.Replace(telemetryHandlerFixture,
		"    url: https://api.github.com/mcp\n",
		"    url: https://api.github.com/mcp\n  - name: filesystem\n    transport: http\n    url: https://example.com/fs\n",
		1,
	)
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))

	s := &Server{stackFile: path}

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		w := patchServerTelemetryRequest(t, s, "github", map[string]any{
			"persist": map[string]any{"logs": true},
		})
		codes[0] = w.Code
	}()
	go func() {
		defer wg.Done()
		w := patchServerTelemetryRequest(t, s, "filesystem", map[string]any{
			"persist": map[string]any{"metrics": true},
		})
		codes[1] = w.Code
	}()
	wg.Wait()

	assert.Equal(t, http.StatusOK, codes[0])
	assert.Equal(t, http.StatusOK, codes[1])

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	gIdx := strings.Index(got, "name: github")
	fIdx := strings.Index(got, "name: filesystem")
	require.Greater(t, fIdx, gIdx)
	githubBlock := got[gIdx:fIdx]
	fsBlock := got[fIdx:]

	assert.Contains(t, githubBlock, "logs: true")
	assert.Contains(t, fsBlock, "metrics: true")
}

func TestHandleGetTelemetryInventory_EmptyStack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // isolate from real ~/.gridctl

	s := &Server{stackName: "no-such-stack"}
	req := httptest.NewRequest(http.MethodGet, "/api/telemetry/inventory", nil)
	w := httptest.NewRecorder()
	s.handleGetTelemetryInventory(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Response is a JSON array, never null.
	body := strings.TrimSpace(w.Body.String())
	assert.Equal(t, "[]", body)
}

func TestHandleGetTelemetryInventory_PopulatedStack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stackName := "demo"
	srvDir := filepath.Join(home, ".gridctl", "telemetry", stackName, "github")
	require.NoError(t, os.MkdirAll(srvDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(srvDir, "logs.jsonl"), []byte("ab\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(srvDir, "metrics.jsonl"), []byte("c\n"), 0o600))

	s := &Server{stackName: stackName}
	req := httptest.NewRequest(http.MethodGet, "/api/telemetry/inventory", nil)
	w := httptest.NewRecorder()
	s.handleGetTelemetryInventory(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var records []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &records))
	require.Len(t, records, 2)
	assert.Equal(t, "github", records[0]["server"])
	assert.Equal(t, "logs", records[0]["signal"])
	assert.Equal(t, "github", records[1]["server"])
	assert.Equal(t, "metrics", records[1]["signal"])
}

func TestHandleDeleteTelemetry_WildcardWipesEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stackName := "demo"
	for _, server := range []string{"github", "filesystem"} {
		srvDir := filepath.Join(home, ".gridctl", "telemetry", stackName, server)
		require.NoError(t, os.MkdirAll(srvDir, 0o700))
		for _, sig := range []string{"logs", "metrics", "traces"} {
			require.NoError(t, os.WriteFile(filepath.Join(srvDir, sig+".jsonl"), []byte("{}\n"), 0o600))
		}
	}

	s := &Server{stackName: stackName}
	req := httptest.NewRequest(http.MethodDelete, "/api/telemetry", nil)
	w := httptest.NewRecorder()
	s.handleDeleteTelemetry(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	for _, server := range []string{"github", "filesystem"} {
		for _, sig := range []string{"logs", "metrics", "traces"} {
			_, err := os.Stat(filepath.Join(home, ".gridctl", "telemetry", stackName, server, sig+".jsonl"))
			assert.True(t, os.IsNotExist(err), "expected %s/%s gone", server, sig)
		}
	}

	// Inventory in response is now empty.
	var resp telemetryPatchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Inventory)
}

func TestHandlePatchStackTelemetry_PreservesCommentOnNullReplacement(t *testing.T) {
	// Edge case from pitfall #1: replacing a non-mapping `telemetry: null`
	// with a fresh mapping must not silently drop the head comment attached
	// to the old null node.
	source := `# header
version: "1"
name: example
network:
  name: example-net
  driver: bridge
# legacy placeholder; switch to opt-in once we audit traffic
telemetry: null
mcp-servers:
  - name: github
    transport: http
    url: https://api.github.com/mcp
`
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))

	s := &Server{stackFile: path}
	w := patchStackTelemetryRequest(t, s, map[string]any{
		"persist": map[string]any{"logs": true},
	})
	require.Equal(t, http.StatusOK, w.Code)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "legacy placeholder")
}

func TestHandleDeleteTelemetry_NoStack(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodDelete, "/api/telemetry", nil)
	w := httptest.NewRecorder()
	s.handleDeleteTelemetry(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleDeleteTelemetry_InvalidSignal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	s := &Server{stackName: "demo"}
	req := httptest.NewRequest(http.MethodDelete, "/api/telemetry?signal=garbage", nil)
	w := httptest.NewRecorder()
	s.handleDeleteTelemetry(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleDeleteTelemetry_DeletesScopedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stackName := "demo"
	srvDir := filepath.Join(home, ".gridctl", "telemetry", stackName, "github")
	require.NoError(t, os.MkdirAll(srvDir, 0o700))
	for _, sig := range []string{"logs", "metrics", "traces"} {
		require.NoError(t, os.WriteFile(filepath.Join(srvDir, sig+".jsonl"), []byte("{}\n"), 0o600))
	}

	s := &Server{stackName: stackName}
	req := httptest.NewRequest(http.MethodDelete, "/api/telemetry?server=github&signal=logs", nil)
	w := httptest.NewRecorder()
	s.handleDeleteTelemetry(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Logs gone, metrics + traces still present.
	_, err := os.Stat(filepath.Join(srvDir, "logs.jsonl"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(srvDir, "metrics.jsonl"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(srvDir, "traces.jsonl"))
	assert.NoError(t, err)
}
