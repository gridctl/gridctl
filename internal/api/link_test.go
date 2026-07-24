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
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const linkStack = `version: "1"
name: linkdemo
network:
  name: linkdemo-net
groups:
  dev:
    servers: [github]
mcp-servers:
  - name: github
    command: [npx, github-mcp]
link:
  - claude
  - client: cursor
    group: dev
`

// linkFakeProv is a controllable ClientProvisioner for link endpoint tests.
type linkFakeProv struct {
	slug       string
	configPath string
	detected   bool
	linkErr    error
	unlinkErr  error
	linkedFor  map[string]bool

	linkCalls   []provisioner.LinkOptions
	unlinkNames []string
}

func (f *linkFakeProv) Name() string { return f.slug }
func (f *linkFakeProv) Slug() string { return f.slug }
func (f *linkFakeProv) Detect() (string, bool) {
	if !f.detected {
		return "", false
	}
	return f.configPath, true
}
func (f *linkFakeProv) IsLinked(_ string, serverName string) (bool, error) {
	return f.linkedFor[serverName], nil
}
func (f *linkFakeProv) Link(_ string, opts provisioner.LinkOptions) error {
	f.linkCalls = append(f.linkCalls, opts)
	return f.linkErr
}
func (f *linkFakeProv) Unlink(_ string, serverName string) error {
	f.unlinkNames = append(f.unlinkNames, serverName)
	return f.unlinkErr
}
func (f *linkFakeProv) NeedsBridge() bool { return false }
func (f *linkFakeProv) ListServers(string) ([]provisioner.ServerEntry, error) {
	return nil, nil
}

// newLinkHarness writes a stack with a link block and wires a Server with
// fake provisioners.
func newLinkHarness(t *testing.T, fakes ...*linkFakeProv) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(linkStack), 0o600))

	provs := make([]provisioner.ClientProvisioner, 0, len(fakes))
	for _, f := range fakes {
		if f.configPath == "" && f.detected {
			f.configPath = filepath.Join(dir, f.slug+".json")
			require.NoError(t, os.WriteFile(f.configPath, []byte("{}\n"), 0o600))
		}
		provs = append(provs, f)
	}

	s := &Server{}
	s.SetStackFile(path)
	s.SetProvisionerRegistry(provisioner.NewRegistryWith(provs...), "gridctl")
	s.SetGatewayAddr("http://localhost:8181")
	return path, s
}

func parseLinkStack(t *testing.T, data []byte) *config.Stack {
	t.Helper()
	var st config.Stack
	require.NoError(t, yaml.Unmarshal(data, &st))
	return &st
}

func doLinkRequest(s *Server, method, slug, sub string, body string) *httptest.ResponseRecorder {
	target := "/api/clients/" + slug + "/link" + sub
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	req.SetPathValue("slug", slug)
	w := httptest.NewRecorder()
	switch {
	case sub == "/preview":
		s.handleLinkPreview(w, req)
	case method == http.MethodDelete:
		s.handleUnlinkClient(w, req)
	default:
		s.handleLinkClient(w, req)
	}
	return w
}

func TestHandleLinkClient_HappyPath(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: true}
	path, s := newLinkHarness(t, grok)

	w := doLinkRequest(s, http.MethodPost, "grok", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp linkClientResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Linked)
	assert.True(t, resp.Declared)
	assert.Equal(t, "gridctl", resp.ServerName)

	require.Len(t, grok.linkCalls, 1)
	assert.Equal(t, 8181, grok.linkCalls[0].Port)

	out, _ := os.ReadFile(path)
	st := parseLinkStack(t, out)
	require.Len(t, st.Link, 3)
	assert.Equal(t, "grok", st.Link[2].Client)
	assert.NoError(t, config.Validate(st))
}

func TestHandleLinkClient_GroupOptions(t *testing.T) {
	zed := &linkFakeProv{slug: "zed", detected: true}
	path, s := newLinkHarness(t, zed)

	w := doLinkRequest(s, http.MethodPost, "zed", "", `{"group":"dev","clientId":"zed"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Len(t, zed.linkCalls, 1)
	opts := zed.linkCalls[0]
	assert.Equal(t, "gridctl-dev", opts.ServerName)
	assert.Contains(t, opts.GatewayURL, "/groups/dev/")
	assert.Contains(t, opts.GatewayURL, "client=zed")

	out, _ := os.ReadFile(path)
	st := parseLinkStack(t, out)
	require.Len(t, st.Link, 3)
	assert.Equal(t, "dev", st.Link[2].Group)
}

func TestHandleLinkClient_ConflictWritesNothing(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: true, linkErr: provisioner.ErrConflict}
	path, s := newLinkHarness(t, grok)
	before, _ := os.ReadFile(path)

	w := doLinkRequest(s, http.MethodPost, "grok", "", "")
	require.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), errCodeLinkConflict)

	after, _ := os.ReadFile(path)
	assert.Equal(t, string(before), string(after), "stack must be untouched on conflict")
}

func TestHandleLinkClient_NotDetected(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: false}
	path, s := newLinkHarness(t, grok)
	before, _ := os.ReadFile(path)

	w := doLinkRequest(s, http.MethodPost, "grok", "", "")
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Body.String(), errCodeClientNotDetected)

	after, _ := os.ReadFile(path)
	assert.Equal(t, string(before), string(after))
}

func TestHandleLinkClient_UnknownSlug(t *testing.T) {
	_, s := newLinkHarness(t)
	w := doLinkRequest(s, http.MethodPost, "nope", "", "")
	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), errCodeUnknownClient)
}

func TestHandleLinkClient_ExternalEditReportsBothFacts(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: true}
	path, s := newLinkHarness(t, grok)

	prev := swapBetweenReadsHook(func() {
		_ = os.WriteFile(path, []byte(linkStack+"# edited\n"), 0o600)
	})
	defer swapBetweenReadsHook(prev)

	w := doLinkRequest(s, http.MethodPost, "grok", "", "")
	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), errCodeStackNotUpdated)
	assert.Len(t, grok.linkCalls, 1, "the client link itself happened")
}

func TestHandleUnlinkClient_ResolvesDeclaredName(t *testing.T) {
	cursor := &linkFakeProv{slug: "cursor", detected: true}
	path, s := newLinkHarness(t, cursor)

	w := doLinkRequest(s, http.MethodDelete, "cursor", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The declared entry has group: dev, so unlink targets gridctl-dev.
	require.Equal(t, []string{"gridctl-dev"}, cursor.unlinkNames)

	out, _ := os.ReadFile(path)
	st := parseLinkStack(t, out)
	require.Len(t, st.Link, 1)
	assert.Equal(t, "claude", st.Link[0].Client)
}

func TestHandleLinkClient_UnknownGroup(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: true}
	path, s := newLinkHarness(t, grok)
	before, _ := os.ReadFile(path)

	w := doLinkRequest(s, http.MethodPost, "grok", "", `{"group":"nope"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), errCodeUnknownGroup)
	assert.Empty(t, grok.linkCalls, "unknown group must not touch the client config")

	after, _ := os.ReadFile(path)
	assert.Equal(t, string(before), string(after))
}

func TestHandleUnlinkClient_BrokenStackWritesNothing(t *testing.T) {
	claude := &linkFakeProv{slug: "claude", detected: true}
	_, s := newLinkHarness(t, claude)
	require.NoError(t, os.WriteFile(s.stackFile, []byte(":\n  broken: [\n"), 0o600))

	w := doLinkRequest(s, http.MethodDelete, "claude", "", "")
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.Empty(t, claude.unlinkNames, "a broken stack file must reject before the host write")
}

func TestHandleUnlinkClient_NeitherLinkedNorDeclared(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: true, unlinkErr: provisioner.ErrNotLinked}
	_, s := newLinkHarness(t, grok)

	w := doLinkRequest(s, http.MethodDelete, "grok", "", "")
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestHandleLinkPreview_WritesNothing(t *testing.T) {
	grok := &linkFakeProv{slug: "grok", detected: true}
	path, s := newLinkHarness(t, grok)
	beforeStack, _ := os.ReadFile(path)

	w := doLinkRequest(s, http.MethodPost, "grok", "/preview", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp linkPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, grok.configPath, resp.ConfigPath)
	assert.NotEmpty(t, resp.After)
	assert.Contains(t, resp.StackDiff, "+  - grok")
	assert.Empty(t, grok.linkCalls, "preview must not link")

	afterStack, _ := os.ReadFile(path)
	assert.Equal(t, string(beforeStack), string(afterStack))
}

func TestHandleClients_DeclaredState(t *testing.T) {
	claude := &linkFakeProv{slug: "claude", detected: true, linkedFor: map[string]bool{"gridctl": true}}
	cursor := &linkFakeProv{slug: "cursor", detected: true, linkedFor: map[string]bool{"gridctl-dev": true}}
	grok := &linkFakeProv{slug: "grok", detected: true}
	_, s := newLinkHarness(t, claude, cursor, grok)

	req := httptest.NewRequest(http.MethodGet, "/api/clients", nil)
	w := httptest.NewRecorder()
	s.handleClients(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var statuses []ClientStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &statuses))
	byslug := map[string]ClientStatus{}
	for _, st := range statuses {
		byslug[st.Slug] = st
	}

	assert.True(t, byslug["claude"].Declared)
	assert.True(t, byslug["claude"].Linked)

	// cursor is declared with group: dev; linked must reflect the resolved
	// gridctl-dev entry name, not the default.
	require.NotNil(t, byslug["cursor"].LinkEntry)
	assert.Equal(t, "dev", byslug["cursor"].LinkEntry.Group)
	assert.True(t, byslug["cursor"].Linked)

	assert.False(t, byslug["grok"].Declared)
	assert.Nil(t, byslug["grok"].LinkEntry)
}
