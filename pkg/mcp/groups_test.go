package mcp

import (
	"strings"
	"testing"
)

func releaseSpec() GroupsSpec {
	return GroupsSpec{
		"release": {
			Description: "Release bundle",
			Servers:     []string{"github"},
			Tools:       []string{"gitlab__create_merge_request"},
			Exclude:     []string{"github__delete_repo"},
			Overrides: map[string]GroupOverrideSpec{
				"github__create_issue": {
					Name:            "create_issue",
					Description:     "File a release-blocking issue.",
					DestructiveHint: boolPtr(true),
				},
				"github__search_code": {
					ReadOnlyHint: boolPtr(true),
				},
			},
		},
	}
}

func groupSurface() []Tool {
	return []Tool{
		{Name: "github__create_issue", Title: "github__create_issue", Description: `MCP server: github. Call using the exact tool name "github__create_issue". Creates an issue.`},
		{Name: "github__search_code", Description: "Searches code.", Annotations: &ToolAnnotations{OpenWorldHint: boolPtr(true)}},
		{Name: "github__delete_repo", Description: "Deletes a repository."},
		{Name: "gitlab__create_merge_request", Description: "Opens an MR."},
		{Name: "gitlab__list_projects", Description: "Lists projects."},
	}
}

func TestNewGroupPolicy_NilAndEmpty(t *testing.T) {
	if p := NewGroupPolicy(nil); p != nil {
		t.Error("nil spec should compile to nil policy")
	}
	if p := NewGroupPolicy(GroupsSpec{}); p != nil {
		t.Error("empty spec should compile to nil policy")
	}
	var p *GroupPolicy
	if p.Has("release") {
		t.Error("nil policy has no groups")
	}
	if _, ok := p.ResolveAlias("release", "anything", nil); ok {
		t.Error("nil policy resolves nothing")
	}
	if got := p.FilterAndRewrite("release", groupSurface()); len(got) != 0 {
		t.Error("nil policy filters to empty")
	}
	if st := p.Status(nil); len(st) != 0 {
		t.Error("nil policy has empty status")
	}
}

func TestGroupPolicy_FilterAndRewrite(t *testing.T) {
	p := NewGroupPolicy(releaseSpec())
	out := p.FilterAndRewrite("release", groupSurface())

	byName := map[string]Tool{}
	for _, tool := range out {
		byName[tool.Name] = tool
	}

	if len(out) != 3 {
		t.Fatalf("exposed %d tools, want 3: %+v", len(out), byName)
	}
	if _, leaked := byName["github__delete_repo"]; leaked {
		t.Error("excluded tool leaked into the group surface")
	}
	if _, leaked := byName["gitlab__list_projects"]; leaked {
		t.Error("non-member tool leaked into the group surface")
	}

	// Rename: exposed under the alias, description wrapper updated, and
	// the override description wins.
	renamed, ok := byName["create_issue"]
	if !ok {
		t.Fatal("renamed tool not exposed under its alias")
	}
	if renamed.Description != "File a release-blocking issue." {
		t.Errorf("description rewrite lost: %q", renamed.Description)
	}
	if renamed.Annotations == nil || renamed.Annotations.DestructiveHint == nil || !*renamed.Annotations.DestructiveHint {
		t.Errorf("injected destructive_hint lost: %+v", renamed.Annotations)
	}

	// Annotation merge: injected hint layered over the downstream one.
	search := byName["github__search_code"]
	if search.Annotations == nil || search.Annotations.ReadOnlyHint == nil || !*search.Annotations.ReadOnlyHint {
		t.Errorf("injected read_only_hint lost: %+v", search.Annotations)
	}
	if search.Annotations.OpenWorldHint == nil || !*search.Annotations.OpenWorldHint {
		t.Errorf("downstream annotation clobbered by merge: %+v", search.Annotations)
	}

	// The router's cached tool must not be mutated by the rewrite.
	original := groupSurface()[1]
	if original.Annotations.ReadOnlyHint != nil {
		t.Error("test fixture mutated")
	}
}

func TestGroupPolicy_RenameUpdatesWrapperText(t *testing.T) {
	p := NewGroupPolicy(GroupsSpec{
		"g": {
			Servers: []string{"github"},
			Overrides: map[string]GroupOverrideSpec{
				"github__create_issue": {Name: "create_issue"},
			},
		},
	})
	out := p.FilterAndRewrite("g", groupSurface()[:1])
	if len(out) != 1 {
		t.Fatal("expected one tool")
	}
	if strings.Contains(out[0].Description, "github__create_issue") {
		t.Errorf("stale canonical name left in call-routing wrapper: %q", out[0].Description)
	}
	if !strings.Contains(out[0].Description, `"create_issue"`) {
		t.Errorf("wrapper does not instruct the exposed name: %q", out[0].Description)
	}
}

func TestGroupPolicy_ResolveAlias(t *testing.T) {
	p := NewGroupPolicy(releaseSpec())

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"exposed rename", "create_issue", "github__create_issue", true},
		{"canonical member stays callable", "github__create_issue", "github__create_issue", true},
		{"unrenamed member", "github__search_code", "github__search_code", true},
		{"server-included member", "gitlab__create_merge_request", "gitlab__create_merge_request", true},
		{"sandbox-built server__alias form", "github__create_issue", "github__create_issue", true},
		{"excluded member denied", "github__delete_repo", "", false},
		{"non-member denied", "gitlab__list_projects", "", false},
		{"alias in wrong group form denied", "gitlab__create_issue", "", false},
		{"unknown name denied", "nope", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := p.ResolveAlias("release", tc.input, nil)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ResolveAlias(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.ok)
			}
		})
	}

	if _, ok := p.ResolveAlias("removed-group", "create_issue", nil); ok {
		t.Error("unknown group must resolve nothing")
	}
}

func TestGroupPolicy_SandboxAliasForm(t *testing.T) {
	// The code-mode sandbox constructs server__tool from its arguments; a
	// model that read the renamed surface calls callTool("github",
	// "create_issue"), producing "github__create_issue"... but when the
	// alias differs from the raw tool name, the constructed form is
	// server__alias and must still resolve.
	p := NewGroupPolicy(GroupsSpec{
		"g": {
			Servers: []string{"github"},
			Overrides: map[string]GroupOverrideSpec{
				"github__search_code": {Name: "find_code"},
			},
		},
	})
	noSuchTool := func(string) bool { return false }
	got, ok := p.ResolveAlias("g", "github__find_code", noSuchTool)
	if !ok || got != "github__search_code" {
		t.Errorf("server__alias form = (%q, %v), want (github__search_code, true)", got, ok)
	}
	// The alias bare form resolves too.
	got, ok = p.ResolveAlias("g", "find_code", noSuchTool)
	if !ok || got != "github__search_code" {
		t.Errorf("bare alias = (%q, %v)", got, ok)
	}
}

func TestGroupPolicy_AliasNeverShadowsLiveTool(t *testing.T) {
	// The server really has BOTH search_code (renamed to find_code) and a
	// separate tool literally named find_code. A call to the live literal
	// name must reach that tool, never the alias's canonical.
	p := NewGroupPolicy(GroupsSpec{
		"g": {
			Servers: []string{"github"},
			Overrides: map[string]GroupOverrideSpec{
				"github__search_code": {Name: "find_code"},
			},
		},
	})
	live := func(name string) bool { return name == "github__find_code" }

	got, ok := p.ResolveAlias("g", "github__find_code", live)
	if !ok || got != "github__find_code" {
		t.Errorf("live literal = (%q, %v), want (github__find_code, true)", got, ok)
	}
	// The bare alias still resolves to its canonical.
	got, ok = p.ResolveAlias("g", "find_code", live)
	if !ok || got != "github__search_code" {
		t.Errorf("bare alias = (%q, %v), want (github__search_code, true)", got, ok)
	}
}

func TestGroupPolicy_StatusAndCrossRefs(t *testing.T) {
	p := NewGroupPolicy(releaseSpec())

	st := p.Status(groupSurface())
	if len(st) != 1 {
		t.Fatalf("status groups = %d", len(st))
	}
	g := st[0]
	if g.Name != "release" || g.Endpoint != "/groups/release/mcp" || g.MemberCount != 3 {
		t.Errorf("status = %+v", g)
	}
	if g.Overrides["github__create_issue"] != "create_issue" {
		t.Errorf("override map = %+v", g.Overrides)
	}

	if got := p.GroupsRewritingTool("github__create_issue"); len(got) != 1 || got[0] != "release" {
		t.Errorf("GroupsRewritingTool(create_issue) = %v", got)
	}
	// Annotation-only override is not a description rewrite.
	if got := p.GroupsRewritingTool("github__search_code"); len(got) != 0 {
		t.Errorf("annotation-only override flagged as rewrite: %v", got)
	}

	renames := p.RenamedOriginals()
	if renames["release"]["github__create_issue"] != "create_issue" {
		t.Errorf("RenamedOriginals = %+v", renames)
	}
}
