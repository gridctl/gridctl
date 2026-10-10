package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestHandleStackExport_NoResolution(t *testing.T) {
	t.Setenv("EXPORT_PROBE", "synthetic-export-canary")
	path := filepath.Join(t.TempDir(), "stack.yaml")
	source := "name: test\ngateway:\n  auth:\n    token: ${EXPORT_PROBE}\nmcp-servers:\n  - name: test\n    env:\n      PAYLOAD: $EXPORT_PROBE\n      TOKEN: prefix-${vault:EXPORT_PROBE}\n"
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	s := &Server{stackFile: path}
	w := httptest.NewRecorder()
	s.handleStackExport(w, httptest.NewRequest(http.MethodGet, "/api/stack/export", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "synthetic-export-canary")
	var response map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Contains(t, response["content"], "${EXPORT_PROBE}")
	require.Contains(t, response["content"], "$EXPORT_PROBE")
	require.Contains(t, response["content"], "prefix-${vault:EXPORT_PROBE}")
	require.NotEmpty(t, response["notice"])
	w = httptest.NewRecorder()
	s.handleStackSpec(w, httptest.NewRequest(http.MethodGet, "/api/stack/spec", nil))
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, source, response["content"])
}

func TestHandleStackExport_ConfigFileNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	withFile := "name: export\nmcp-servers:\n  - name: prometheus\n    image: alpine\n    transport: stdio\n    configs:\n      - target: /etc/app.yaml\n        file: ./extra.yaml\n"
	require.NoError(t, os.WriteFile(path, []byte(withFile), 0600))
	s := &Server{stackFile: path}
	w := httptest.NewRecorder()
	s.handleStackExport(w, httptest.NewRequest(http.MethodGet, "/api/stack/export", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Contains(t, response["notice"], config.ExportConfigFilesNotice)

	withoutFile := "name: export\nmcp-servers:\n  - name: prometheus\n    image: alpine\n    transport: stdio\n    configs:\n      - target: /etc/app.yaml\n        content: alpha\n"
	require.NoError(t, os.WriteFile(path, []byte(withoutFile), 0600))
	w = httptest.NewRecorder()
	s.handleStackExport(w, httptest.NewRequest(http.MethodGet, "/api/stack/export", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotContains(t, response["notice"], config.ExportConfigFilesNotice)
}

func TestHandleStackExport_ValueFreeFailureAndRawSpec(t *testing.T) {
	for _, source := range []string{
		"gateway: {auth: {token: synthetic-inline-canary}}\n",
		"gateway: {auth: {token: '${PROBE:+synthetic-operand-canary}'}}\n",
		"name: [synthetic-syntax-canary\n",
	} {
		path := filepath.Join(t.TempDir(), "stack.yaml")
		require.NoError(t, os.WriteFile(path, []byte(source), 0600))
		s := &Server{stackFile: path}
		w := httptest.NewRecorder()
		s.handleStackExport(w, httptest.NewRequest(http.MethodGet, "/api/stack/export", nil))
		require.Equal(t, http.StatusInternalServerError, w.Code)
		require.NotContains(t, w.Body.String(), "canary")
		require.NotContains(t, w.Body.String(), "content")
		w = httptest.NewRecorder()
		s.handleStackSpec(w, httptest.NewRequest(http.MethodGet, "/api/stack/spec", nil))
		var response map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, source, response["content"])
	}
}
