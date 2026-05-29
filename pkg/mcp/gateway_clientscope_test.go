package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

// newScopeTestGateway builds a gateway with two servers (github, gitlab), each
// advertising two tools, ready for per-client scope assertions.
func newScopeTestGateway(t *testing.T) *Gateway {
	t.Helper()
	ctrl := gomock.NewController(t)
	g := NewGateway()
	g.Router().AddClient(setupMockAgentClient(ctrl, "github", []Tool{
		{Name: "search-repos"}, {Name: "create-issue"},
	}))
	g.Router().AddClient(setupMockAgentClient(ctrl, "gitlab", []Tool{
		{Name: "list-issues"}, {Name: "merge-request"},
	}))
	g.Router().RefreshTools()
	return g
}

func ctxWithAccess(id string) context.Context {
	return WithClientAccessID(context.Background(), id)
}

func TestGateway_ToolsList_ScopedPerClient(t *testing.T) {
	g := newScopeTestGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Servers: []string{"github"}},
		},
	}))

	// Scoped client sees only github tools.
	res, err := g.HandleToolsList(ctxWithAccess("cursor"))
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	got := toolNames(res.Tools)
	want := []string{"github__create-issue", "github__search-repos"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("scoped tools/list = %v, want %v", got, want)
	}

	// Unlisted client under default-deny sees nothing.
	res, err = g.HandleToolsList(ctxWithAccess("windsurf"))
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	if len(res.Tools) != 0 {
		t.Errorf("unlisted client should see no tools, got %v", toolNames(res.Tools))
	}

	// Unscoped (no policy) sees all four.
	g.SetClientAccessPolicy(nil)
	res, _ = g.HandleToolsList(ctxWithAccess("cursor"))
	if len(res.Tools) != 4 {
		t.Errorf("no policy should expose all 4 tools, got %d", len(res.Tools))
	}
}

// TestGateway_ToolsListUnscoped_BypassesPolicy locks the operator-facing
// guarantee: the web console and optimize paths see the full tool surface even
// under a deny-all policy that would hide everything from a scoped MCP client.
func TestGateway_ToolsListUnscoped_BypassesPolicy(t *testing.T) {
	g := newScopeTestGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "deny", // no profiles: every MCP client is denied
	}))

	// A scoped MCP client sees nothing.
	scoped, err := g.HandleToolsList(ctxWithAccess("windsurf"))
	if err != nil {
		t.Fatalf("HandleToolsList: %v", err)
	}
	if len(scoped.Tools) != 0 {
		t.Errorf("deny-all policy should hide all tools from MCP client, got %d", len(scoped.Tools))
	}

	// The operator-facing unscoped view still sees the full surface.
	full, err := g.HandleToolsListUnscoped()
	if err != nil {
		t.Fatalf("HandleToolsListUnscoped: %v", err)
	}
	if len(full.Tools) != 4 {
		t.Errorf("unscoped view should expose all 4 tools regardless of policy, got %d", len(full.Tools))
	}
}

func TestGateway_ToolsCall_DeniedByScope(t *testing.T) {
	g := newScopeTestGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Servers: []string{"github"}},
		},
	}))

	// A gitlab tool is out of scope for cursor: rejected before routing.
	res, err := g.HandleToolsCall(ctxWithAccess("cursor"), ToolCallParams{Name: "gitlab__list-issues"})
	if err != nil {
		t.Fatalf("HandleToolsCall: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected denied call to return an error result")
	}
	if !strings.Contains(res.Content[0].Text, "access scope") {
		t.Errorf("expected access-scope denial message, got %q", res.Content[0].Text)
	}
}

// TestGateway_CodeMode_UniverseScoped asserts the code-mode search surface only
// exposes the connecting client's allowed tools — the bypass the design guards
// against. With code mode on, search must not reveal a denied tool.
func TestGateway_CodeMode_UniverseScoped(t *testing.T) {
	g := newScopeTestGateway(t)
	g.SetCodeMode(5 * time.Second)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Servers: []string{"github"}},
		},
	}))

	res, err := g.HandleToolsCall(ctxWithAccess("cursor"), ToolCallParams{
		Name:      MetaToolSearch,
		Arguments: map[string]any{"query": ""},
	})
	if err != nil {
		t.Fatalf("code-mode search: %v", err)
	}
	text := res.Content[0].Text
	if strings.Contains(text, "gitlab__") {
		t.Errorf("code-mode search leaked out-of-scope tools: %q", text)
	}
	if !strings.Contains(text, "github__search-repos") {
		t.Errorf("code-mode search should include in-scope tools: %q", text)
	}
}

func TestGateway_ClientScope_API(t *testing.T) {
	g := newScopeTestGateway(t)

	// No policy: not configured, unscoped, full surface.
	if g.ClientAccessConfigured() {
		t.Error("ClientAccessConfigured should be false without a policy")
	}
	scope := g.ClientScope("cursor")
	if scope.Configured || !scope.Unscoped || len(scope.Tools) != 4 {
		t.Errorf("unscoped result unexpected: %+v", scope)
	}

	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"gitlab"}}},
	}))
	if !g.ClientAccessConfigured() {
		t.Error("ClientAccessConfigured should be true with a policy")
	}
	scope = g.ClientScope("cursor")
	if !scope.Configured || scope.Unscoped {
		t.Errorf("expected configured+scoped, got %+v", scope)
	}
	if len(scope.Servers) != 1 || scope.Servers[0] != "gitlab" {
		t.Errorf("servers = %v, want [gitlab]", scope.Servers)
	}
}
