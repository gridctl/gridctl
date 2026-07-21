package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

// newGroupGateway builds a gateway with two mock servers and the release
// group from releaseSpec (github fully included minus delete_repo, plus
// gitlab's create_merge_request, with create_issue renamed).
func newGroupGateway(t *testing.T) (*Gateway, *MockAgentClient) {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()

	github := setupMockAgentClient(ctrl, "github", []Tool{
		{Name: "create_issue", Description: "Creates an issue", InputSchema: json.RawMessage(`{}`)},
		{Name: "search_code", Description: "Searches code", InputSchema: json.RawMessage(`{}`)},
		{Name: "delete_repo", Description: "Deletes a repo", InputSchema: json.RawMessage(`{}`)},
	})
	gitlab := setupMockAgentClient(ctrl, "gitlab", []Tool{
		{Name: "create_merge_request", Description: "Opens an MR", InputSchema: json.RawMessage(`{}`)},
		{Name: "list_projects", Description: "Lists projects", InputSchema: json.RawMessage(`{}`)},
	})
	g.Router().AddClient(github)
	g.Router().AddClient(gitlab)
	g.Router().RefreshTools()

	g.SetGroupPolicy(NewGroupPolicy(releaseSpec()))
	return g, github
}

func groupCtx(group string) context.Context {
	return WithGroup(context.Background(), group)
}

func TestGroupGateway_ToolsListRewritten(t *testing.T) {
	g, _ := newGroupGateway(t)

	res, err := g.HandleToolsList(groupCtx("release"))
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"create_issue", "github__search_code", "gitlab__create_merge_request"} {
		if !names[want] {
			t.Errorf("group surface missing %q: %v", want, names)
		}
	}
	for _, leak := range []string{"github__delete_repo", "gitlab__list_projects", "github__create_issue"} {
		if names[leak] {
			t.Errorf("group surface leaked %q", leak)
		}
	}

	// The default endpoint is unchanged: full canonical surface.
	res, err = g.HandleToolsList(context.Background())
	if err != nil {
		t.Fatalf("HandleToolsList(default): %v", err)
	}
	if len(res.Tools) != 5 {
		t.Errorf("default surface = %d tools, want 5", len(res.Tools))
	}
}

func TestGroupGateway_RenameDispatchesToCanonical(t *testing.T) {
	g, github := newGroupGateway(t)
	var calledTool string
	github.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, name string, _ map[string]any) (*ToolCallResult, error) {
			calledTool = name
			return &ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil
		},
	).Times(1)

	res, err := g.HandleToolsCall(groupCtx("release"), ToolCallParams{Name: "create_issue"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if res.IsError {
		t.Fatalf("renamed call failed: %+v", res.Content)
	}
	if calledTool != "create_issue" {
		t.Errorf("downstream received %q, want the raw tool name create_issue", calledTool)
	}
}

func TestGroupGateway_DenyOnCallShape(t *testing.T) {
	g, _ := newGroupGateway(t)

	res, err := g.HandleToolsCall(groupCtx("release"), ToolCallParams{Name: "gitlab__list_projects"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if !res.IsError {
		t.Fatal("non-member call must be denied")
	}
	msg := res.Content[0].Text
	for _, want := range []string{`"gitlab__list_projects"`, `"release"`, "exposes 3 tools", "tools/list"} {
		if !strings.Contains(msg, want) {
			t.Errorf("denial missing %q: %s", want, msg)
		}
	}

	// Excluded member denied too.
	res, _ = g.HandleToolsCall(groupCtx("release"), ToolCallParams{Name: "github__delete_repo"})
	if !res.IsError {
		t.Error("excluded member call must be denied")
	}
}

func TestGroupGateway_RemovedGroupDenies(t *testing.T) {
	g, _ := newGroupGateway(t)
	g.SetGroupPolicy(nil) // hot reload removed the block

	res, err := g.HandleToolsCall(groupCtx("release"), ToolCallParams{Name: "create_issue"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "no longer exists") {
		t.Errorf("removed-group denial wrong: %+v", res.Content)
	}

	// And its surface is empty, not full.
	list, _ := g.HandleToolsList(groupCtx("release"))
	if len(list.Tools) != 0 {
		t.Errorf("removed group lists %d tools, want 0", len(list.Tools))
	}
}

func TestGroupGateway_ClientScopeIntersects(t *testing.T) {
	g, _ := newGroupGateway(t)
	// cursor may only reach gitlab; github tools are out of scope even
	// under the group's rename.
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default:  "deny",
		Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"gitlab"}}},
	}))
	ctx := WithClientAccessID(groupCtx("release"), "cursor")

	list, err := g.HandleToolsList(ctx)
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "gitlab__create_merge_request" {
		t.Errorf("scoped group surface = %+v, want only the gitlab member", list.Tools)
	}

	// The renamed alias resolves to a canonical name the scope then denies.
	res, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "create_issue"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "access scope") {
		t.Errorf("scoped-out renamed call should hit the scope denial: %+v", res.Content)
	}
}

func TestGroupGateway_CodeModeUniverseIsGroupSurface(t *testing.T) {
	g, _ := newGroupGateway(t)
	g.SetCodeMode(30 * time.Second)

	res, err := g.HandleToolsCall(groupCtx("release"), ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{"query": ""},
	})
	if err != nil {
		t.Fatalf("code-mode search: %v", err)
	}
	text := res.Content[0].Text
	// Code mode keeps renames server-prefixed (github__create_issue is the
	// prefixed form of the create_issue alias here) so the sandbox's
	// callTool(server, tool) construction round-trips; the rewritten
	// description proves the override applied.
	if !strings.Contains(text, "github__create_issue") || !strings.Contains(text, "File a release-blocking issue.") {
		t.Errorf("search should show the rewritten group surface: %s", text)
	}
	if strings.Contains(text, "list_projects") || strings.Contains(text, "delete_repo") {
		t.Errorf("search leaked non-members: %s", text)
	}
}
