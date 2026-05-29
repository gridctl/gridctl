package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidate_Clients(t *testing.T) {
	base := func(clients *ClientsConfig) *Stack {
		return &Stack{
			Name:    "test",
			Network: Network{Name: "net"},
			MCPServers: []MCPServer{
				{Name: "github", Image: "alpine", Port: 3000},
				{Name: "gitlab", Image: "alpine", Port: 3001},
			},
			Clients: clients,
		}
	}

	tests := []struct {
		name    string
		clients *ClientsConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "no clients block is valid (back-compat)",
			clients: nil,
			wantErr: false,
		},
		{
			name: "valid server allow-list",
			clients: &ClientsConfig{
				Default:  "deny",
				Profiles: map[string]ClientProfile{"cursor": {Servers: []string{"github"}}},
			},
			wantErr: false,
		},
		{
			name: "valid tool allow-list",
			clients: &ClientsConfig{
				Profiles: map[string]ClientProfile{"cursor": {Tools: []string{"github__search-repos"}}},
			},
			wantErr: false,
		},
		{
			name: "valid default allow",
			clients: &ClientsConfig{
				Default:  "allow",
				Profiles: map[string]ClientProfile{"cursor": {}},
			},
			wantErr: false,
		},
		{
			name: "invalid default value",
			clients: &ClientsConfig{
				Default:  "permit",
				Profiles: map[string]ClientProfile{"cursor": {}},
			},
			wantErr: true,
			errMsg:  "clients.default",
		},
		{
			name: "unknown server reference",
			clients: &ClientsConfig{
				Profiles: map[string]ClientProfile{"cursor": {Servers: []string{"nonexistent"}}},
			},
			wantErr: true,
			errMsg:  "unknown MCP server 'nonexistent'",
		},
		{
			name: "tool not prefixed",
			clients: &ClientsConfig{
				Profiles: map[string]ClientProfile{"cursor": {Tools: []string{"search-repos"}}},
			},
			wantErr: true,
			errMsg:  "must be a prefixed name",
		},
		{
			name: "tool references unknown server",
			clients: &ClientsConfig{
				Profiles: map[string]ClientProfile{"cursor": {Tools: []string{"slack__post"}}},
			},
			wantErr: true,
			errMsg:  "references unknown MCP server 'slack'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(base(tc.clients))
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

// TestClientsConfig_RoundTrip asserts the clients block survives a load/save
// cycle without dropping or reordering its fields (Article IX back-compat).
func TestClientsConfig_RoundTrip(t *testing.T) {
	src := `version: "1"
name: test
network:
  name: net
mcp-servers:
  - name: github
    image: alpine
    port: 3000
clients:
  default: deny
  profiles:
    cursor:
      servers:
        - github
      tools:
        - github__search-repos
    team-bot:
      aliases:
        - Claude Code
      servers:
        - github
`
	var stack Stack
	if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stack.Clients == nil {
		t.Fatal("clients block dropped on unmarshal")
	}
	if stack.Clients.Default != "deny" {
		t.Errorf("default = %q, want deny", stack.Clients.Default)
	}
	if got := stack.Clients.Profiles["cursor"].Servers; len(got) != 1 || got[0] != "github" {
		t.Errorf("cursor.servers = %v", got)
	}
	if got := stack.Clients.Profiles["team-bot"].Aliases; len(got) != 1 || got[0] != "Claude Code" {
		t.Errorf("team-bot.aliases = %v", got)
	}

	// Marshal back and re-parse: the block must survive intact.
	out, err := yaml.Marshal(&stack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var reparsed Stack
	if err := yaml.Unmarshal(out, &reparsed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if reparsed.Clients == nil || len(reparsed.Clients.Profiles) != 2 {
		t.Fatalf("round-trip lost profiles: %+v", reparsed.Clients)
	}
	if reparsed.Clients.Profiles["cursor"].Tools[0] != "github__search-repos" {
		t.Errorf("round-trip lost tool: %+v", reparsed.Clients.Profiles["cursor"])
	}
}
