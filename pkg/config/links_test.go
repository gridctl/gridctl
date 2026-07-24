package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLinkEntry_UnmarshalShorthand(t *testing.T) {
	var s Stack
	src := `
version: "1"
name: test
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
link:
  - claude
  - claude-code
`
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(s.Link) != 2 {
		t.Fatalf("want 2 link entries, got %d", len(s.Link))
	}
	if s.Link[0].Client != "claude" || s.Link[1].Client != "claude-code" {
		t.Errorf("unexpected clients: %+v", s.Link)
	}
	if !s.Link[0].IsShorthand() {
		t.Errorf("bare slug entry should be shorthand")
	}
}

func TestLinkEntry_UnmarshalMapping(t *testing.T) {
	var s Stack
	src := `
version: "1"
name: test
link:
  - client: cursor
    group: dev
    client_id: cursor
    name: custom
  - grok
`
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := s.Link[0]
	if e.Client != "cursor" || e.Group != "dev" || e.ClientID != "cursor" || e.Name != "custom" {
		t.Errorf("unexpected entry: %+v", e)
	}
	if e.IsShorthand() {
		t.Errorf("mapping entry with options must not be shorthand")
	}
	if s.Link[1].Client != "grok" {
		t.Errorf("mixed forms: want grok, got %+v", s.Link[1])
	}
}

func TestLinkEntry_UnmarshalRejectsSequence(t *testing.T) {
	var s Stack
	src := `
name: test
link:
  - [claude]
`
	err := yaml.Unmarshal([]byte(src), &s)
	if err == nil || !strings.Contains(err.Error(), "link entry must be a client slug or a mapping") {
		t.Fatalf("want kind error, got %v", err)
	}
}

func TestLinkEntry_EffectiveName(t *testing.T) {
	cases := []struct {
		name  string
		entry LinkEntry
		want  string
	}{
		{"default", LinkEntry{Client: "claude"}, "gridctl"},
		{"group", LinkEntry{Client: "cursor", Group: "dev"}, "gridctl-dev"},
		{"explicit name wins", LinkEntry{Client: "cursor", Group: "dev", Name: "custom"}, "custom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.entry.EffectiveName(); got != tc.want {
				t.Errorf("EffectiveName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateLinks(t *testing.T) {
	base := func() *Stack {
		return &Stack{
			Name:    "test",
			Network: Network{Name: "test-net"},
			MCPServers: []MCPServer{
				{Name: "github", Command: []string{"npx", "github-mcp"}},
			},
		}
	}

	cases := []struct {
		name    string
		mutate  func(*Stack)
		wantErr bool
		errMsg  string
	}{
		{
			name:   "absent block is valid",
			mutate: func(s *Stack) {},
		},
		{
			name: "known slugs valid",
			mutate: func(s *Stack) {
				s.Link = []LinkEntry{{Client: "claude"}, {Client: "cursor"}}
			},
		},
		{
			name: "unknown slug rejected",
			mutate: func(s *Stack) {
				s.Link = []LinkEntry{{Client: "clod"}}
			},
			wantErr: true,
			errMsg:  `unknown client "clod"`,
		},
		{
			name: "empty client rejected",
			mutate: func(s *Stack) {
				s.Link = []LinkEntry{{Group: "dev"}}
			},
			wantErr: true,
			errMsg:  "client is required",
		},
		{
			name: "duplicate slug rejected",
			mutate: func(s *Stack) {
				s.Link = []LinkEntry{{Client: "claude"}, {Client: "claude", Group: "dev"}}
			},
			wantErr: true,
			errMsg:  `client "claude" already declared`,
		},
		{
			name: "unknown group rejected",
			mutate: func(s *Stack) {
				s.Link = []LinkEntry{{Client: "cursor", Group: "nope"}}
			},
			wantErr: true,
			errMsg:  "references unknown group 'nope'",
		},
		{
			name: "declared group accepted",
			mutate: func(s *Stack) {
				s.Groups = map[string]GroupConfig{"dev": {Servers: []string{"github"}}}
				s.Link = []LinkEntry{{Client: "cursor", Group: "dev"}}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mutate(s)
			err := Validate(s)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tc.errMsg)
				}
				if !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
