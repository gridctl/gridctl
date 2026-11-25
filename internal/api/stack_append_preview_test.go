package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type appendPreviewBody struct {
	ResourceType string `json:"resourceType"`
	ResourceName string `json:"resourceName"`
	Path         string `json:"path"`
	Before       string `json:"before"`
	After        string `json:"after"`
	Watching     bool   `json:"watching"`
}

func previewRequest(t *testing.T, server *Server, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := loopbackRequest(http.MethodPost, "/api/stack/append/preview", strings.NewReader(string(raw)))
	w := httptest.NewRecorder()
	server.handleStackAppendPreview(w, req)
	return w
}

// dirSnapshot records every entry name and content in dir so a test can
// assert that a handler created, removed, or changed nothing.
func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		out[e.Name()] = string(data)
	}
	return out
}

func TestHandleStackAppendPreview_MCPServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))
	before := dirSnapshot(t, dir)

	s := &Server{stackFile: path}
	w := previewRequest(t, s, map[string]string{
		"yaml":         "name: fetch\nimage: gridctl/fetch:latest\nport: 3100\n",
		"resourceType": "mcp-server",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got appendPreviewBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "mcp-server", got.ResourceType)
	assert.Equal(t, "fetch", got.ResourceName)
	assert.Equal(t, path, got.Path)
	assert.Equal(t, commentedStack, got.Before)
	assert.Contains(t, got.After, "name: fetch")
	assert.Contains(t, got.After, "# top-of-file comment")
	assert.False(t, got.Watching)

	assert.Equal(t, before, dirSnapshot(t, dir), "preview must not write, back up, or leave temp files")
}

func TestHandleStackAppendPreview_Resource(t *testing.T) {
	sf := writeTestStack(t)
	original, err := os.ReadFile(sf)
	require.NoError(t, err)

	s := &Server{stackFile: sf}
	w := previewRequest(t, s, map[string]string{
		"yaml":         "name: redis\nimage: redis:7\n",
		"resourceType": "resource",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got appendPreviewBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "resource", got.ResourceType)
	assert.Equal(t, "redis", got.ResourceName)
	assert.Contains(t, got.After, "resources:")
	assert.Contains(t, got.After, "name: redis")

	after, err := os.ReadFile(sf)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after))
}

func TestHandleStackAppendPreview_AfterMatchesAppendWrite(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		yaml         string
	}{
		{"mcp-server", "mcp-server", "name: filesystem\nimage: gridctl/fs:latest\nport: 3100\n"},
		{"resource", "resource", "name: postgres\nimage: postgres:16\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stack.yaml")
			require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))
			s := &Server{stackFile: path}
			body := map[string]string{"yaml": tc.yaml, "resourceType": tc.resourceType}

			pw := previewRequest(t, s, body)
			require.Equal(t, http.StatusOK, pw.Code, pw.Body.String())
			var preview appendPreviewBody
			require.NoError(t, json.Unmarshal(pw.Body.Bytes(), &preview))

			aw := appendRequest(t, s, body)
			require.Equal(t, http.StatusOK, aw.Code, aw.Body.String())
			written, err := os.ReadFile(path)
			require.NoError(t, err)

			assert.Equal(t, string(written), preview.After)
		})
	}
}

func TestHandleStackAppendPreview_FailuresWriteNothing(t *testing.T) {
	tests := []struct {
		name       string
		noFile     bool
		body       map[string]string
		wantStatus int
		check      func(t *testing.T, body []byte)
	}{
		{
			name:       "duplicate server name",
			body:       map[string]string{"yaml": "name: github\nimage: alpine\nport: 3200\n", "resourceType": "mcp-server"},
			wantStatus: http.StatusUnprocessableEntity,
			check: func(t *testing.T, body []byte) {
				var resp map[string]any
				require.NoError(t, json.Unmarshal(body, &resp))
				assert.Equal(t, "stack validation failed after append", resp["error"])
				assert.NotNil(t, resp["validation"])
			},
		},
		{
			name:       "unsupported resource type",
			body:       map[string]string{"yaml": "name: test\n", "resourceType": "stack"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing name",
			body:       map[string]string{"yaml": "image: alpine\nport: 1\n", "resourceType": "mcp-server"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no stack file",
			noFile:     true,
			body:       map[string]string{"yaml": "name: x\nimage: alpine\nport: 1\n", "resourceType": "mcp-server"},
			wantStatus: http.StatusServiceUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stack.yaml")
			require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))
			snap := dirSnapshot(t, dir)

			s := &Server{stackFile: path}
			if tc.noFile {
				s = &Server{}
			}
			w := previewRequest(t, s, tc.body)
			assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			if tc.check != nil {
				tc.check(t, w.Body.Bytes())
			}
			assert.Equal(t, snap, dirSnapshot(t, dir))
		})
	}
}

func TestHandleStackAppend_ReportsWatchingAndPath(t *testing.T) {
	for _, watching := range []bool{false, true} {
		t.Run(map[bool]string{false: "not watching", true: "watching"}[watching], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stack.yaml")
			require.NoError(t, os.WriteFile(path, []byte(commentedStack), 0o600))
			s := &Server{stackFile: path}
			s.SetStackWatching(watching)
			assert.Equal(t, watching, s.StackWatching())

			pw := previewRequest(t, s, map[string]string{"yaml": "name: a\nimage: alpine\nport: 1\n", "resourceType": "mcp-server"})
			require.Equal(t, http.StatusOK, pw.Code, pw.Body.String())
			var preview appendPreviewBody
			require.NoError(t, json.Unmarshal(pw.Body.Bytes(), &preview))
			assert.Equal(t, watching, preview.Watching)

			aw := appendRequest(t, s, map[string]string{"yaml": "name: a\nimage: alpine\nport: 1\n", "resourceType": "mcp-server"})
			require.Equal(t, http.StatusOK, aw.Code, aw.Body.String())
			var resp map[string]any
			require.NoError(t, json.Unmarshal(aw.Body.Bytes(), &resp))
			assert.Equal(t, true, resp["success"])
			assert.Equal(t, "mcp-server", resp["resourceType"])
			assert.Equal(t, "a", resp["resourceName"])
			assert.Equal(t, path, resp["path"])
			assert.Equal(t, watching, resp["watching"])
		})
	}
}

func TestServer_StackWatchingDefaultsFalse(t *testing.T) {
	assert.False(t, (&Server{}).StackWatching())
	assert.False(t, NewServer(nil, nil).StackWatching())
}

// TestHandleStackAppend_BadInputSkipsLock holds the per-path lock and checks
// that an append with bad input still returns: phase one runs before the lock.
func TestHandleStackAppend_BadInputSkipsLock(t *testing.T) {
	tests := []struct {
		name string
		body map[string]string
	}{
		{"unsupported resource type", map[string]string{"yaml": "name: test\n", "resourceType": "stack"}},
		{"invalid snippet", map[string]string{"yaml": "[unclosed bracket", "resourceType": "mcp-server"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sf := writeTestStack(t)
			s := &Server{stackFile: sf}

			mu := stackFileLock(sf)
			mu.Lock()
			defer mu.Unlock()

			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- appendRequest(t, s, tc.body) }()
			select {
			case w := <-done:
				assert.Equal(t, http.StatusBadRequest, w.Code)
			case <-time.After(5 * time.Second):
				t.Fatal("append with bad input waited on the per-path lock")
			}
		})
	}
}
