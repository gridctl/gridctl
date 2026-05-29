package mcp

import (
	"reflect"
	"sort"
	"testing"
)

// toolNames is a small helper to extract sorted names from a tool slice.
func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}

// sampleTools is the global (unscoped) surface used across the policy tests:
// two servers, two tools each, with the router's prefixed-name convention.
func sampleTools() []Tool {
	return []Tool{
		{Name: "github__search-repos"},
		{Name: "github__create-issue"},
		{Name: "gitlab__list-issues"},
		{Name: "gitlab__merge-request"},
	}
}

func TestClientAccessPolicy_Filter(t *testing.T) {
	tests := []struct {
		name     string
		spec     *ClientAccessSpec
		accessID string
		want     []string // expected visible prefixed tool names (sorted)
	}{
		{
			name:     "no block: every client sees everything",
			spec:     nil,
			accessID: "anyone",
			want:     []string{"github__create-issue", "github__search-repos", "gitlab__list-issues", "gitlab__merge-request"},
		},
		{
			name:     "default deny: unlisted client sees nothing",
			spec:     &ClientAccessSpec{Default: "deny", Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}}},
			accessID: "windsurf",
			want:     nil,
		},
		{
			name:     "default empty is deny: unlisted client sees nothing",
			spec:     &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}}},
			accessID: "windsurf",
			want:     nil,
		},
		{
			name:     "default allow: unlisted client sees everything",
			spec:     &ClientAccessSpec{Default: "allow", Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}}},
			accessID: "windsurf",
			want:     []string{"github__create-issue", "github__search-repos", "gitlab__list-issues", "gitlab__merge-request"},
		},
		{
			name:     "server-level allow-list",
			spec:     &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}}},
			accessID: "cursor",
			want:     []string{"github__create-issue", "github__search-repos"},
		},
		{
			name:     "tool-level allow-list",
			spec:     &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Tools: []string{"github__search-repos", "gitlab__list-issues"}}}},
			accessID: "cursor",
			want:     []string{"github__search-repos", "gitlab__list-issues"},
		},
		{
			name: "server and tool allow-list intersect",
			spec: &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {
				Servers: []string{"github"},
				Tools:   []string{"github__search-repos", "gitlab__list-issues"},
			}}},
			accessID: "cursor",
			want:     []string{"github__search-repos"},
		},
		{
			name:     "listed client with empty profile sees everything",
			spec:     &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {}}},
			accessID: "cursor",
			want:     []string{"github__create-issue", "github__search-repos", "gitlab__list-issues", "gitlab__merge-request"},
		},
		{
			name:     "unknown server reference filters to nothing for that profile",
			spec:     &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"nonexistent"}}}},
			accessID: "cursor",
			want:     nil,
		},
		{
			name: "alias resolves a divergent wire name to a profile",
			spec: &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"team-bot": {
				Aliases: []string{"Claude Code"},
				Servers: []string{"gitlab"},
			}}},
			accessID: "claude-code", // normalized form of "Claude Code"
			want:     []string{"gitlab__list-issues", "gitlab__merge-request"},
		},
		{
			name: "profile key matched case-insensitively via normalization",
			spec: &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"Claude-Code": {
				Servers: []string{"github"},
			}}},
			accessID: "claude-code",
			want:     []string{"github__create-issue", "github__search-repos"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := NewClientAccessPolicy(tt.spec)
			got := toolNames(policy.Filter(tt.accessID, sampleTools()))
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Filter(%q) = %v, want %v", tt.accessID, got, tt.want)
			}
		})
	}
}

func TestClientAccessPolicy_Allows(t *testing.T) {
	tests := []struct {
		name     string
		spec     *ClientAccessSpec
		accessID string
		tool     string
		want     bool
	}{
		{"no block allows any tool", nil, "anyone", "github__search-repos", true},
		{"default deny rejects unlisted client", &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {}}}, "windsurf", "github__search-repos", false},
		{"default allow accepts unlisted client", &ClientAccessSpec{Default: "allow", Profiles: map[string]ClientProfileSpec{"cursor": {}}}, "windsurf", "github__search-repos", true},
		{"server allow-list accepts in-scope tool", &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}}}, "cursor", "github__search-repos", true},
		{"server allow-list rejects out-of-scope tool", &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}}}, "cursor", "gitlab__list-issues", false},
		{"tool allow-list accepts listed tool", &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Tools: []string{"github__search-repos"}}}}, "cursor", "github__search-repos", true},
		{"tool allow-list rejects unlisted tool", &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {Tools: []string{"github__search-repos"}}}}, "cursor", "github__create-issue", false},
		{"unparseable tool name denied under a profile", &ClientAccessSpec{Profiles: map[string]ClientProfileSpec{"cursor": {}}}, "cursor", "no-prefix", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := NewClientAccessPolicy(tt.spec)
			if got := policy.Allows(tt.accessID, tt.tool); got != tt.want {
				t.Errorf("Allows(%q, %q) = %v, want %v", tt.accessID, tt.tool, got, tt.want)
			}
		})
	}
}

func TestClientAccessPolicy_ScopeResult(t *testing.T) {
	t.Run("nil policy is unscoped and lists everything", func(t *testing.T) {
		var policy *ClientAccessPolicy
		res := policy.scopeResult("anyone", sampleTools())
		if res.Configured {
			t.Error("Configured should be false for nil policy")
		}
		if !res.Unscoped {
			t.Error("Unscoped should be true for nil policy")
		}
		if len(res.Tools) != 4 || len(res.Servers) != 2 {
			t.Errorf("expected 4 tools/2 servers, got %d/%d", len(res.Tools), len(res.Servers))
		}
	})

	t.Run("scoped client reports effective servers and tools", func(t *testing.T) {
		policy := NewClientAccessPolicy(&ClientAccessSpec{
			Profiles: map[string]ClientProfileSpec{"cursor": {Servers: []string{"github"}}},
		})
		res := policy.scopeResult("cursor", sampleTools())
		if !res.Configured {
			t.Error("Configured should be true")
		}
		if res.Unscoped {
			t.Error("Unscoped should be false for a narrowed client")
		}
		if !reflect.DeepEqual(res.Servers, []string{"github"}) {
			t.Errorf("servers = %v, want [github]", res.Servers)
		}
		if !reflect.DeepEqual(res.Tools, []string{"github__create-issue", "github__search-repos"}) {
			t.Errorf("tools = %v", res.Tools)
		}
	})

	t.Run("default-deny unlisted client reports empty scope", func(t *testing.T) {
		policy := NewClientAccessPolicy(&ClientAccessSpec{
			Profiles: map[string]ClientProfileSpec{"cursor": {}},
		})
		res := policy.scopeResult("windsurf", sampleTools())
		if res.Unscoped {
			t.Error("Unscoped should be false for a denied client")
		}
		if len(res.Tools) != 0 || len(res.Servers) != 0 {
			t.Errorf("expected empty scope, got tools=%v servers=%v", res.Tools, res.Servers)
		}
	})
}
