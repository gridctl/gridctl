package mcp

import "testing"

// TestClientScopePreview_ServerDraft verifies the read-only preview computes a
// hypothetical server allow-list against the live surface without installing any
// policy: a github-only draft reaches exactly github's two tools.
func TestClientScopePreview_ServerDraft(t *testing.T) {
	g := newScopeTestGateway(t)

	res := g.ClientScopePreview("cursor", []string{"github"}, nil)
	if res.Unscoped {
		t.Fatalf("a narrowed draft should not be unscoped")
	}
	if len(res.Servers) != 1 || res.Servers[0] != "github" {
		t.Errorf("servers = %v, want [github]", res.Servers)
	}
	want := []string{"github__create-issue", "github__search-repos"}
	if len(res.Tools) != len(want) || res.Tools[0] != want[0] || res.Tools[1] != want[1] {
		t.Errorf("tools = %v, want %v", res.Tools, want)
	}

	// The preview must not mutate gateway state: with no policy installed, the
	// real scope stays unscoped.
	live := g.ClientScope("cursor")
	if !live.Unscoped {
		t.Errorf("preview leaked into installed policy: live scope = %+v", live)
	}
}

// TestClientScopePreview_PreservesToolAllowList confirms a server-only draft
// (tools nil) keeps an operator-authored tool allow-list on the live profile,
// rather than silently widening the client to every tool of the drafted servers.
func TestClientScopePreview_PreservesToolAllowList(t *testing.T) {
	g := newScopeTestGateway(t)
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Profiles: map[string]ClientProfileSpec{
			"cursor": {Servers: []string{"github"}, Tools: []string{"github__search-repos"}},
		},
	}))

	// Draft adds gitlab at the server level but leaves the tools axis untouched
	// (nil). The tool allow-list is GLOBAL, not per-server, so preserving it keeps
	// the client pinned to github__search-repos even though gitlab is now granted
	// — the faithful consequence the commit gate must show, and exactly why a
	// client-side guess would mislead.
	res := g.ClientScopePreview("cursor", []string{"github", "gitlab"}, nil)
	want := []string{"github__search-repos"}
	if len(res.Tools) != len(want) || res.Tools[0] != want[0] {
		t.Fatalf("tools = %v, want %v", res.Tools, want)
	}
}

// TestClientScopePreview_EmptyServersMeansAll documents the backend footgun the
// editor guards against: an empty server allow-list is "no restriction" (all
// servers), NOT "deny everything". The UI must forbid saving an empty selection
// precisely because committing it would silently grant the full surface.
func TestClientScopePreview_EmptyServersMeansAll(t *testing.T) {
	g := newScopeTestGateway(t)
	// Both axes explicitly empty: per config semantics this is unrestricted.
	res := g.ClientScopePreview("bot", []string{}, []string{})
	if !res.Unscoped {
		t.Errorf("empty allow-lists should be unscoped (all), got %+v", res)
	}
	if len(res.Tools) != 4 {
		t.Errorf("empty draft should reach all 4 tools, got %v", res.Tools)
	}
}
