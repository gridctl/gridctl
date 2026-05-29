package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/config"
)

const clientsStack = `version: "1"
name: example
network:
  name: example-net
mcp-servers:
  - name: github
    url: https://api.github.com/mcp
    transport: http
    env:
      GITHUB_TOKEN: "${vault:GITHUB_TOKEN}" # do not expand on write
  - name: gitlab
    image: gridctl/gitlab:latest
    port: 3100
clients:
  default: deny
  profiles:
    cursor:
      servers:
        - github
    team-bot:
      aliases:
        - "Custom Agent" # keep this comment
      servers:
        - gitlab
      tools:
        - gitlab__list-issues
`

// sp returns a pointer to a string slice, for the tri-state patch axes.
func sp(v ...string) *[]string {
	s := append([]string{}, v...)
	return &s
}

// parseClients round-trips patched YAML back into a Stack for assertions.
func parseClients(t *testing.T, out []byte) *config.Stack {
	t.Helper()
	var stack config.Stack
	require.NoError(t, yaml.Unmarshal(out, &stack))
	return &stack
}

func TestPatchClientScope_CreatesBlockWhenAbsent(t *testing.T) {
	src := `version: "1"
name: example
network:
  name: example-net
mcp-servers:
  - name: github
    url: https://api.github.com/mcp
    transport: http
`
	out, err := patchClientScope([]byte(src), "cursor", sp("github"), sp("github__search-repos"))
	require.NoError(t, err)

	stack := parseClients(t, out)
	require.NotNil(t, stack.Clients)
	prof, ok := stack.Clients.Profiles["cursor"]
	require.True(t, ok, "cursor profile should be created")
	assert.Equal(t, []string{"github"}, prof.Servers)
	assert.Equal(t, []string{"github__search-repos"}, prof.Tools)
	assert.Contains(t, string(out), "name: github")
}

func TestPatchClientScope_UpdatesProfileWithoutClobberingSiblings(t *testing.T) {
	out, err := patchClientScope([]byte(clientsStack), "cursor", sp("github", "gitlab"), nil)
	require.NoError(t, err)

	stack := parseClients(t, out)
	assert.ElementsMatch(t, []string{"github", "gitlab"}, stack.Clients.Profiles["cursor"].Servers)
	// Sibling profile + its alias + tools + default untouched.
	assert.Equal(t, "deny", stack.Clients.Default)
	teamBot := stack.Clients.Profiles["team-bot"]
	assert.Equal(t, []string{"gitlab"}, teamBot.Servers)
	assert.Equal(t, []string{"Custom Agent"}, teamBot.Aliases)
	assert.Equal(t, []string{"gitlab__list-issues"}, teamBot.Tools)
	// Comments + vault ref preserved (Article IX round-trip).
	s := string(out)
	assert.Contains(t, s, "keep this comment")
	assert.Contains(t, s, `GITHUB_TOKEN: "${vault:GITHUB_TOKEN}"`)
	assert.Contains(t, s, "do not expand on write")
}

// The key regression for the server-level editor: editing a tool-scoped
// profile's servers (tools axis nil) must NOT wipe its tool allow-list.
func TestPatchClientScope_ServerOnlyEditPreservesTools(t *testing.T) {
	out, err := patchClientScope([]byte(clientsStack), "team-bot", sp("github", "gitlab"), nil)
	require.NoError(t, err)
	stack := parseClients(t, out)
	teamBot := stack.Clients.Profiles["team-bot"]
	assert.ElementsMatch(t, []string{"github", "gitlab"}, teamBot.Servers)
	assert.Equal(t, []string{"gitlab__list-issues"}, teamBot.Tools, "tool allow-list must survive a server-only edit")
}

func TestPatchClientScope_ToolsOnlyEditPreservesServers(t *testing.T) {
	out, err := patchClientScope([]byte(clientsStack), "cursor", nil, sp("github__search-repos"))
	require.NoError(t, err)
	stack := parseClients(t, out)
	assert.Equal(t, []string{"github"}, stack.Clients.Profiles["cursor"].Servers, "server list must survive a tools-only edit")
	assert.Equal(t, []string{"github__search-repos"}, stack.Clients.Profiles["cursor"].Tools)
}

func TestPatchClientScope_EmptyAxisDropsKey(t *testing.T) {
	// A present empty slice replaces the axis by dropping the key (no
	// restriction), distinct from nil which leaves it untouched.
	out, err := patchClientScope([]byte(clientsStack), "team-bot", sp("gitlab"), sp())
	require.NoError(t, err)
	assert.NotContains(t, string(out), "gitlab__list-issues", "empty tools slice should drop the tools key")
	stack := parseClients(t, out)
	assert.Empty(t, stack.Clients.Profiles["team-bot"].Tools)
	assert.Equal(t, []string{"gitlab"}, stack.Clients.Profiles["team-bot"].Servers)
}

func TestPatchClientScope_AddsNewProfileToExistingBlock(t *testing.T) {
	out, err := patchClientScope([]byte(clientsStack), "windsurf", sp("gitlab"), nil)
	require.NoError(t, err)
	stack := parseClients(t, out)
	assert.Len(t, stack.Clients.Profiles, 3)
	assert.Equal(t, []string{"gitlab"}, stack.Clients.Profiles["windsurf"].Servers)
	assert.Equal(t, []string{"github"}, stack.Clients.Profiles["cursor"].Servers)
}

func TestPatchClientScope_CreatesBlockFromBareNullNode(t *testing.T) {
	src := `version: "1"
name: example
network:
  name: example-net
mcp-servers:
  - name: github
    url: https://api.github.com/mcp
    transport: http
clients:
`
	out, err := patchClientScope([]byte(src), "cursor", sp("github"), nil)
	require.NoError(t, err)
	stack := parseClients(t, out)
	require.NotNil(t, stack.Clients)
	assert.Equal(t, []string{"github"}, stack.Clients.Profiles["cursor"].Servers)
}

func TestPatchClientScope_EmptyKeyErrors(t *testing.T) {
	_, err := patchClientScope([]byte(clientsStack), "", sp("github"), nil)
	assert.Error(t, err)
}

func TestPatchClientScope_BothAxesNilErrors(t *testing.T) {
	_, err := patchClientScope([]byte(clientsStack), "cursor", nil, nil)
	assert.Error(t, err)
}

func TestPatchClientScope_PatchedStackPassesValidation(t *testing.T) {
	out, err := patchClientScope([]byte(clientsStack), "windsurf", sp("github"), sp("github__search-repos"))
	require.NoError(t, err)
	stack := parseClients(t, out)
	assert.NoError(t, config.Validate(stack))
}

// newClientScopeHarness writes a stack with a clients block and returns a
// Server wired to it (no gateway, so scope validation is skipped).
func newClientScopeHarness(t *testing.T) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(clientsStack), 0o600))
	s := &Server{}
	s.SetStackFile(path)
	return path, s
}

func TestHandleSetClientScope_HappyPath_NoReload(t *testing.T) {
	path, s := newClientScopeHarness(t)

	body, _ := json.Marshal(map[string]any{"servers": []string{"github", "gitlab"}})
	req := httptest.NewRequest(http.MethodPut, "/api/clients/cursor/scope", strings.NewReader(string(body)))
	req.SetPathValue("slug", "cursor")
	w := httptest.NewRecorder()

	s.handleSetClientScope(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp setClientScopeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "cursor", resp.ProfileKey)
	assert.False(t, resp.Reloaded)

	out, _ := os.ReadFile(path)
	stack := parseClients(t, out)
	assert.ElementsMatch(t, []string{"github", "gitlab"}, stack.Clients.Profiles["cursor"].Servers)
}

// A server-only request (no tools key) must preserve an existing tool list.
func TestHandleSetClientScope_ServerOnlyPreservesTools(t *testing.T) {
	path, s := newClientScopeHarness(t)

	body, _ := json.Marshal(map[string]any{"servers": []string{"github", "gitlab"}})
	req := httptest.NewRequest(http.MethodPut, "/api/clients/team-bot/scope", strings.NewReader(string(body)))
	req.SetPathValue("slug", "team-bot")
	w := httptest.NewRecorder()

	s.handleSetClientScope(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	out, _ := os.ReadFile(path)
	stack := parseClients(t, out)
	assert.Equal(t, []string{"gitlab__list-issues"}, stack.Clients.Profiles["team-bot"].Tools)
}

func TestHandleSetClientScope_NormalizesSlugToProfileKey(t *testing.T) {
	path, s := newClientScopeHarness(t)

	body, _ := json.Marshal(map[string]any{"servers": []string{"github"}})
	req := httptest.NewRequest(http.MethodPut, "/api/clients/Claude%20Code/scope", strings.NewReader(string(body)))
	req.SetPathValue("slug", "Claude Code")
	w := httptest.NewRecorder()

	s.handleSetClientScope(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	out, _ := os.ReadFile(path)
	stack := parseClients(t, out)
	_, ok := stack.Clients.Profiles["claude-code"]
	assert.True(t, ok, "slug should normalize to claude-code profile key")
}

func TestHandleSetClientScope_ConflictOnExternalEdit(t *testing.T) {
	path, s := newClientScopeHarness(t)

	swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte(clientsStack+"\n# external edit\n"), 0o600)
	})
	defer swapBetweenReadsHook(nil)

	body, _ := json.Marshal(map[string]any{"servers": []string{"github"}})
	req := httptest.NewRequest(http.MethodPut, "/api/clients/cursor/scope", strings.NewReader(string(body)))
	req.SetPathValue("slug", "cursor")
	w := httptest.NewRecorder()

	s.handleSetClientScope(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), errCodeStackModified)
}

func TestHandleSetClientScope_RejectsEmptySlug(t *testing.T) {
	_, s := newClientScopeHarness(t)
	req := httptest.NewRequest(http.MethodPut, "/api/clients//scope", strings.NewReader(`{"servers":["github"]}`))
	req.SetPathValue("slug", "")
	w := httptest.NewRecorder()
	s.handleSetClientScope(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetClientScope_RejectsEmptyBody(t *testing.T) {
	_, s := newClientScopeHarness(t)
	req := httptest.NewRequest(http.MethodPut, "/api/clients/cursor/scope", strings.NewReader(`{}`))
	req.SetPathValue("slug", "cursor")
	w := httptest.NewRecorder()
	s.handleSetClientScope(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
