package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/stretchr/testify/require"
)

func TestWizardDrafts_YamlRoundTrip(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	srv := NewServer(mcp.NewGateway(), nil)

	post := func(body string) map[string]json.RawMessage {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/wizard/drafts", strings.NewReader(body)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		return raw
	}

	yamlText := "name: petstore\nopenapi:\n  spec: ./spec.json\n  baseUrl: https://api.example.com\n"
	withYaml := post(`{"name":"yaml-draft","resourceType":"mcp-server","formData":{"name":"petstore"},"yaml":` + mustJSON(t, yamlText) + `}`)
	var gotYaml string
	if err := json.Unmarshal(withYaml["yaml"], &gotYaml); err != nil || gotYaml != yamlText {
		t.Fatalf("create response yaml = %q (err %v), want %q", gotYaml, err, yamlText)
	}

	withoutYaml := post(`{"name":"form-draft","resourceType":"resource","formData":{"name":"db","image":"postgres:16"}}`)
	if _, ok := withoutYaml["yaml"]; ok {
		t.Fatalf("create response for a form draft has a yaml key: %v", withoutYaml)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/wizard/drafts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 drafts, got %d", len(list))
	}
	found := map[string]bool{}
	for _, d := range list {
		var name string
		_ = json.Unmarshal(d["name"], &name)
		found[name] = true
		switch name {
		case "yaml-draft":
			var y string
			if err := json.Unmarshal(d["yaml"], &y); err != nil || y != yamlText {
				t.Errorf("listed yaml-draft yaml = %q (err %v), want %q", y, err, yamlText)
			}
		case "form-draft":
			if _, ok := d["yaml"]; ok {
				t.Errorf("listed form-draft has a yaml key: %s", d["yaml"])
			}
		}
	}
	if !found["yaml-draft"] || !found["form-draft"] {
		t.Fatalf("missing drafts in list: %v", found)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestWizardDrafts_FileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	dir := wizardDraftsDir()
	require.NotEmpty(t, dir)
	// A directory left by an earlier release is tightened on the next save.
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Chmod(dir, 0o755))

	srv := NewServer(mcp.NewGateway(), nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/wizard/drafts",
		strings.NewReader(`{"name":"modes","resourceType":"mcp-server","formData":{"name":"x"}}`)))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created WizardDraft
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	info, err = os.Stat(filepath.Join(dir, created.ID+".json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWizardDrafts_NewDirectoryMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not enforced on Windows")
	}
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	srv := NewServer(mcp.NewGateway(), nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/wizard/drafts",
		strings.NewReader(`{"name":"fresh","resourceType":"resource","formData":{}}`)))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	info, err := os.Stat(wizardDraftsDir())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestWizardDrafts_ListDeleteRoundTrip(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	srv := NewServer(mcp.NewGateway(), nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/wizard/drafts",
		strings.NewReader(`{"name":"gone","resourceType":"resource","formData":{}}`)))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created WizardDraft
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	list := func() []WizardDraft {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/wizard/drafts", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var drafts []WizardDraft
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &drafts))
		return drafts
	}
	require.Len(t, list(), 1)

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodDelete, "/api/wizard/drafts/"+created.ID, nil))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, list())

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodDelete, "/api/wizard/drafts/"+created.ID, nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}
