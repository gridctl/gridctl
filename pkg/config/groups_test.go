package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidate_Groups(t *testing.T) {
	base := func(groups map[string]GroupConfig) *Stack {
		return &Stack{
			Name:    "test",
			Network: Network{Name: "net"},
			MCPServers: []MCPServer{
				{Name: "github", Image: "alpine", Port: 3000},
				{Name: "gitlab", Image: "alpine", Port: 3001},
			},
			Groups: groups,
		}
	}

	tests := []struct {
		name    string
		groups  map[string]GroupConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no groups block is valid (back-compat)",
			groups:  nil,
			wantErr: false,
		},
		{
			name: "valid group with servers, tools, exclude, and overrides",
			groups: map[string]GroupConfig{
				"release": {
					Description: "Release bundle",
					Servers:     []string{"github"},
					Tools:       []string{"gitlab__create_merge_request"},
					Exclude:     []string{"github__delete_repo"},
					Overrides: map[string]GroupOverride{
						"github__create_issue": {
							Name:         "create_issue",
							Description:  "File a release-blocking issue.",
							ReadOnlyHint: boolRef(false),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "bad group name",
			groups:  map[string]GroupConfig{"Bad Name": {Servers: []string{"github"}}},
			wantErr: true,
			errMsg:  "group name must match",
		},
		{
			name:    "empty membership",
			groups:  map[string]GroupConfig{"release": {Description: "empty"}},
			wantErr: true,
			errMsg:  "at least one server or tool",
		},
		{
			name:    "unknown server",
			groups:  map[string]GroupConfig{"release": {Servers: []string{"nonexistent"}}},
			wantErr: true,
			errMsg:  "unknown MCP server 'nonexistent'",
		},
		{
			name:    "tool not prefixed",
			groups:  map[string]GroupConfig{"release": {Tools: []string{"create_issue"}}},
			wantErr: true,
			errMsg:  "must be a prefixed name",
		},
		{
			name:    "exclude references unknown server",
			groups:  map[string]GroupConfig{"release": {Servers: []string{"github"}, Exclude: []string{"slack__post"}}},
			wantErr: true,
			errMsg:  "unknown MCP server 'slack'",
		},
		{
			name: "exclude empties the group",
			groups: map[string]GroupConfig{"release": {
				Tools:   []string{"github__create_issue"},
				Exclude: []string{"github__create_issue"},
			}},
			wantErr: true,
			errMsg:  "the group would be empty",
		},
		{
			name: "override key not a member",
			groups: map[string]GroupConfig{"release": {
				Tools:     []string{"github__create_issue"},
				Overrides: map[string]GroupOverride{"gitlab__list_projects": {Name: "list"}},
			}},
			wantErr: true,
			errMsg:  "not a member of the group",
		},
		{
			name: "override key excluded from the group",
			groups: map[string]GroupConfig{"release": {
				Servers:   []string{"github"},
				Exclude:   []string{"github__delete_repo"},
				Overrides: map[string]GroupOverride{"github__delete_repo": {Name: "nuke"}},
			}},
			wantErr: true,
			errMsg:  "not a member of the group",
		},
		{
			name: "rename with double underscore",
			groups: map[string]GroupConfig{"release": {
				Servers:   []string{"github"},
				Overrides: map[string]GroupOverride{"github__create_issue": {Name: "my__issue"}},
			}},
			wantErr: true,
			errMsg:  "contain no '__'",
		},
		{
			name: "rename collides with another rename",
			groups: map[string]GroupConfig{"release": {
				Servers: []string{"github"},
				Overrides: map[string]GroupOverride{
					"github__create_issue": {Name: "issue"},
					"github__close_issue":  {Name: "issue"},
				},
			}},
			wantErr: true,
			errMsg:  "collides with override",
		},
		{
			name: "rename reserved for meta-tools",
			groups: map[string]GroupConfig{"release": {
				Servers:   []string{"github"},
				Overrides: map[string]GroupOverride{"github__create_issue": {Name: "search"}},
			}},
			wantErr: true,
			errMsg:  "reserved for the code-mode meta-tools",
		},
		{
			name: "rename shadows explicit member tool tail on same server",
			groups: map[string]GroupConfig{"release": {
				Tools: []string{"github__create_issue", "github__find_code"},
				Overrides: map[string]GroupOverride{
					"github__create_issue": {Name: "find_code"},
				},
			}},
			wantErr: true,
			errMsg:  "collides with member tool 'github__find_code'",
		},
		{
			name: "explicit member tool overflowing the client budget",
			groups: map[string]GroupConfig{"release": {
				Tools: []string{"github__" + strings.Repeat("b", 60)},
			}},
			wantErr: true,
			errMsg:  "rename it via overrides",
		},
		{
			name: "rename overflowing the client budget",
			groups: map[string]GroupConfig{"release": {
				Servers: []string{"github"},
				Overrides: map[string]GroupOverride{
					"github__create_issue": {Name: strings.Repeat("a", 60)},
				},
			}},
			wantErr: true,
			errMsg:  "cap tool names at 64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(base(tc.groups))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("expected error containing %q, got %q", tc.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func boolRef(b bool) *bool { return &b }

// TestGroupsConfig_RoundTrip asserts the groups block survives a load/save
// cycle without dropping fields (Article IX back-compat).
func TestGroupsConfig_RoundTrip(t *testing.T) {
	src := `version: "1"
name: test
network:
  name: net
mcp-servers:
  - name: github
    image: alpine
    port: 3000
groups:
  release:
    description: Release bundle
    servers:
      - github
    exclude:
      - github__delete_repo
    overrides:
      github__create_issue:
        name: create_issue
        description: File a release issue.
        read_only_hint: false
        destructive_hint: true
`
	var stack Stack
	if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	g, ok := stack.Groups["release"]
	if !ok {
		t.Fatal("groups block dropped on unmarshal")
	}
	ov := g.Overrides["github__create_issue"]
	if ov.Name != "create_issue" || ov.Description != "File a release issue." {
		t.Errorf("override = %+v", ov)
	}
	if ov.ReadOnlyHint == nil || *ov.ReadOnlyHint || ov.DestructiveHint == nil || !*ov.DestructiveHint {
		t.Errorf("hint pointers lost: %+v", ov)
	}
	if ov.IdempotentHint != nil {
		t.Error("unset hint must stay nil")
	}

	out, err := yaml.Marshal(&stack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var reparsed Stack
	if err := yaml.Unmarshal(out, &reparsed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if reparsed.Groups["release"].Overrides["github__create_issue"].Name != "create_issue" {
		t.Fatalf("round-trip lost override: %+v", reparsed.Groups)
	}
}
