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

// commentedStack is the fixture for round-trip preservation tests. It
// deliberately includes:
//   - a top-of-file comment line
//   - an inline comment on a key (`# do not expand`)
//   - a non-canonical key order (`transport:` before `url:`) that
//     yaml.Marshal-from-struct would reshuffle to the struct field order
const commentedStack = `# top-of-file comment — must survive round-trip
version: "1"
name: example
network:
  name: example-net
  driver: bridge
mcp-servers:
  - name: github
    transport: http
    url: https://api.github.com/mcp
    env:
      GITHUB_TOKEN: "${vault:GITHUB_TOKEN}" # do not expand
`

func appendRequest(t *testing.T, server *Server, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	server.handleStackAppend(w, req)
	return w
}

func TestHandleStackAppend_PreservesCommentsAndOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))

	s := &Server{stackFile: path}
	w := appendRequest(t, s, map[string]string{
		"yaml":         "name: filesystem\nimage: gridctl/fs:latest\nport: 3100\n",
		"resourceType": "mcp-server",
	})
	require.Equal(t, http.StatusOK, w.Code)

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)

	assert.Contains(t, got, "# top-of-file comment")
	assert.Contains(t, got, "do not expand")
	// Original `transport: http` then `url:` ordering must survive — a struct
	// round-trip would emit URL before Transport based on field order.
	transportIdx := strings.Index(got, "transport: http")
	urlIdx := strings.Index(got, "url: https://api.github.com/mcp")
	require.Greater(t, transportIdx, 0)
	require.Greater(t, urlIdx, 0)
	assert.Less(t, transportIdx, urlIdx, "transport must remain before url for the github server")

	// Newly appended entry is present.
	assert.Contains(t, got, "name: filesystem")
}

func TestHandleStackAppend_ConflictWhenDiskChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))

	external := commentedStack + "\n# touched externally\n"
	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte(external), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	s := &Server{stackFile: path}
	w := appendRequest(t, s, map[string]string{
		"yaml":         "name: lost\nimage: alpine\nport: 9999\n",
		"resourceType": "mcp-server",
	})

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "modified on disk")

	// On-disk file retains the external edit; our intended append did not land.
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, external, string(got))
	assert.NotContains(t, string(got), "name: lost")
}

func TestHandleStackAppend_AtomicOnWriteFailure(t *testing.T) {
	// Mirrors stack_edit_test.go:TestAtomicWrite_LeavesOriginalOnWriteFailure.
	// Point s.stackFile at a path under a directory that does not exist; the
	// handler's initial os.ReadFile fails so atomicWrite never runs and no
	// .tmp.* files are created. The original file at `realPath` is at a
	// distinct location and stays untouched.
	dir := t.TempDir()
	realPath := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(realPath, []byte(commentedStack), 0o600))

	missing := filepath.Join(dir, "nonexistent-subdir", "stack.yaml")
	s := &Server{stackFile: missing}
	w := appendRequest(t, s, map[string]string{
		"yaml":         "name: x\nimage: alpine\nport: 1\n",
		"resourceType": "mcp-server",
	})
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Original file is bit-for-bit unchanged.
	got, err := os.ReadFile(realPath)
	require.NoError(t, err)
	assert.Equal(t, commentedStack, string(got))

	// No leftover temp files anywhere in the tree.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".tmp."), "leftover temp file: %s", e.Name())
	}
}

func TestHandleStackAppend_SerializesConcurrentCallers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))

	s := &Server{stackFile: path}

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		w := appendRequest(t, s, map[string]string{
			"yaml":         "name: srv-a\nimage: alpine\nport: 5001\n",
			"resourceType": "mcp-server",
		})
		codes[0] = w.Code
	}()
	go func() {
		defer wg.Done()
		w := appendRequest(t, s, map[string]string{
			"yaml":         "name: res-a\nimage: redis:7\n",
			"resourceType": "resource",
		})
		codes[1] = w.Code
	}()
	wg.Wait()

	assert.Equal(t, http.StatusOK, codes[0])
	assert.Equal(t, http.StatusOK, codes[1])

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, "name: srv-a")
	assert.Contains(t, got, "name: res-a")

	// Post-concurrent file is well-formed and comments survived.
	var node yaml.Node
	require.NoError(t, yaml.Unmarshal(out, &node))
	assert.Contains(t, got, "# top-of-file comment")
	assert.Contains(t, got, "do not expand")
}
