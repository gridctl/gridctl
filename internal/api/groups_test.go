package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupsGateway() *mcp.Gateway {
	g := mcp.NewGateway()
	g.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"release": {
			Description: "Release bundle",
			Servers:     []string{"github"},
			Overrides: map[string]mcp.GroupOverrideSpec{
				"github__create_issue": {Name: "create_issue"},
			},
		},
	}))
	return g
}

func TestHandleGroups_Unconfigured(t *testing.T) {
	s := NewServer(mcp.NewGateway(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	w := httptest.NewRecorder()

	s.handleGroups(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp mcp.GroupsReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Configured)
	assert.NotNil(t, resp.Groups)
	assert.Empty(t, resp.Groups)
}

func TestHandleGroups_ReportsResolvedGroups(t *testing.T) {
	s := NewServer(groupsGateway(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	w := httptest.NewRecorder()

	s.handleGroups(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp mcp.GroupsReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Configured)
	require.Len(t, resp.Groups, 1)
	g := resp.Groups[0]
	assert.Equal(t, "release", g.Name)
	assert.Equal(t, "/groups/release/mcp", g.Endpoint)
	assert.Equal(t, "create_issue", g.Overrides["github__create_issue"])
	// No servers connected: membership resolves to zero against the live
	// surface, but the group itself is still reported.
	assert.Equal(t, 0, g.MemberCount)
}

func TestHandleGroupMCP_UnknownGroup404s(t *testing.T) {
	s := NewServer(groupsGateway(), nil)

	// The unknown group 404s without any MCP handling.
	req := httptest.NewRequest(http.MethodPost, "/groups/nope/mcp", nil)
	req.SetPathValue("name", "nope")
	w := httptest.NewRecorder()
	s.handleGroupMCP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// A configured group reaches the streamable transport (which then
	// rejects the empty body as a JSON parse error, proving pass-through).
	req = httptest.NewRequest(http.MethodPost, "/groups/release/mcp", nil)
	req.SetPathValue("name", "release")
	w = httptest.NewRecorder()
	s.handleGroupMCP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid JSON")
}

func TestHandleGroupSSE_PointsAtGroupPath(t *testing.T) {
	s := NewServer(groupsGateway(), nil)

	req := httptest.NewRequest(http.MethodGet, "/groups/release/sse", nil)
	req.SetPathValue("name", "release")
	w := httptest.NewRecorder()
	s.handleGroupSSE(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: POST /groups/release/mcp")

	req = httptest.NewRequest(http.MethodGet, "/groups/nope/sse", nil)
	req.SetPathValue("name", "nope")
	w = httptest.NewRecorder()
	s.handleGroupSSE(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
