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
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlePatchServerEnabled_WritesAndReloads(t *testing.T) {
	path, s := newStackHarness(t)
	current := &config.Stack{
		Version:    "1",
		Name:       "test-stack",
		Network:    config.Network{Name: "test-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{Name: "ext", URL: "https://example.com/mcp", Transport: "http"}},
	}
	rh := reload.NewHandler(path, current, mcp.NewGateway(), runtime.NewOrchestrator(nil, nil), 8180, 9000, nil, nil)
	s.SetReloadHandler(rh)

	req := loopbackRequest(http.MethodPatch, "/api/mcp-servers/ext", strings.NewReader(`{"enabled":false}`))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()
	s.handlePatchServerEnabled(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp patchEnabledResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Enabled)
	assert.True(t, resp.Changed)
	assert.True(t, resp.Reloaded)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: false")
}

func TestHandlePatchServerEnabled_UnknownAndConflict(t *testing.T) {
	path, s := newStackHarness(t)
	req := loopbackRequest(http.MethodPatch, "/api/mcp-servers/missing", strings.NewReader(`{"enabled":false}`))
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	s.handlePatchServerEnabled(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	s.SetStackFile(path)
	req = loopbackRequest(http.MethodPatch, "/api/mcp-servers/ext", strings.NewReader(`{"enabled":false}`))
	req.SetPathValue("name", "ext")
	w = httptest.NewRecorder()
	swapBetweenReadsHook(func() {
		require.NoError(t, os.WriteFile(path, []byte("name: changed\n"), 0o600))
	})
	t.Cleanup(func() { swapBetweenReadsHook(nil) })
	s.handlePatchServerEnabled(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "stack_modified")
}

func TestHandlePatchServerEnabled_OverrideRequired(t *testing.T) {
	_, s := newStackHarness(t)
	s.SetOverrideSelection(filepath.Join(t.TempDir(), "stack.override.yaml"), false)
	req := loopbackRequest(http.MethodPatch, "/api/mcp-servers/ext", strings.NewReader(`{"enabled":false}`))
	req.SetPathValue("name", "ext")
	w := httptest.NewRecorder()
	s.handlePatchServerEnabled(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "override_required")
	assert.Contains(t, w.Body.String(), "gridctl enable")
	data, err := os.ReadFile(s.stackFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "enabled:")
}

func TestAppendDisabledServers_Order(t *testing.T) {
	off := false
	s := &Server{}
	stack := &config.Stack{MCPServers: []config.MCPServer{
		{Name: "on", Transport: "http", URL: "https://example.com"},
		{Name: "off", Transport: "stdio", Command: []string{"/bin/true"}, Enabled: &off},
	}}
	rh := reload.NewHandler("stack.yaml", stack, mcp.NewGateway(), nil, 0, 0, nil, nil)
	s.SetReloadHandler(rh)
	got := s.appendDisabledServers([]MCPServerStatus{{Name: "on", Initialized: true, Tools: []string{"a"}}})
	require.Len(t, got, 2)
	assert.Equal(t, "on", got[0].Name)
	assert.Equal(t, "off", got[1].Name)
	require.NotNil(t, got[1].Enabled)
	assert.False(t, *got[1].Enabled)
	assert.False(t, got[1].Initialized)
	assert.Empty(t, got[1].Tools)
	assert.Nil(t, got[1].Healthy)
	assert.Equal(t, "stdio", got[1].Transport)
}

func TestHandlePatchServerEnabled_PinnedPackRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	checkout := filepath.Join(home, ".gridctl", "packs", "demo", "abc")
	require.NoError(t, os.MkdirAll(checkout, 0o755))
	stackPath := filepath.Join(checkout, "stack.yaml")
	body := "version: \"1\"\nname: demo\nnetwork:\n  name: n\nmcp-servers:\n  - name: ext\n    url: https://example.com/mcp\n"
	require.NoError(t, os.WriteFile(stackPath, []byte(body), 0o600))
	require.NoError(t, skills.WriteLockFile(skills.LockFilePath(), &skills.LockFile{Sources: map[string]skills.LockedSource{
		"demo": {Pack: &skills.LockedPack{Name: "demo"}},
	}}))

	for _, noOverride := range []bool{false, true} {
		s := &Server{}
		s.SetStackFile(stackPath)
		s.SetOverrideSelection("", noOverride)
		req := loopbackRequest(http.MethodPatch, "/api/mcp-servers/ext", strings.NewReader(`{"enabled":false}`))
		req.SetPathValue("name", "ext")
		w := httptest.NewRecorder()
		s.handlePatchServerEnabled(w, req)
		assert.Equal(t, http.StatusConflict, w.Code, "noOverride=%v body=%s", noOverride, w.Body.String())
		assert.Contains(t, w.Body.String(), "override_required")
		data, err := os.ReadFile(stackPath)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "enabled:")
		override := filepath.Join(home, ".gridctl", "overrides", "demo.yaml")
		if _, err := os.Stat(override); !os.IsNotExist(err) {
			t.Fatalf("override was written: %v", err)
		}
	}
}

func TestDisabledTransport_UsesRegisteredVocabulary(t *testing.T) {
	cases := []struct {
		name string
		srv  config.MCPServer
		want string
	}{
		{name: "explicit sse", srv: config.MCPServer{Transport: "sse", URL: "https://example.com"}, want: "sse"},
		{name: "url default", srv: config.MCPServer{URL: "https://example.com"}, want: "http"},
		{name: "command", srv: config.MCPServer{Command: []string{"/bin/true"}}, want: ""},
		{name: "container", srv: config.MCPServer{Image: "alpine"}, want: "http"},
		{name: "container stdio", srv: config.MCPServer{Image: "alpine", Transport: "stdio"}, want: "stdio"},
		{name: "ssh", srv: config.MCPServer{Command: []string{"/bin/true"}, SSH: &config.SSHConfig{Host: "h", User: "u"}}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, disabledTransport(tc.srv))
		})
	}
}
