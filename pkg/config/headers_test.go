package config

import (
	"strings"
	"testing"
)

func externalServer(headers map[string]string, auth *ServerAuth) *Stack {
	return &Stack{
		Name:    "demo",
		Network: Network{Name: "demo"},
		MCPServers: []MCPServer{{
			Name:    "api",
			URL:     "https://example.test/mcp",
			Headers: headers,
			Auth:    auth,
		}},
	}
}

func TestValidate_Headers(t *testing.T) {
	tests := []struct {
		name    string
		stack   *Stack
		wantErr string
	}{
		{
			name:  "external accepted",
			stack: externalServer(map[string]string{"X-Tenant": "${var:TENANT}"}, &ServerAuth{Type: "bearer", Token: "${var:TOKEN}"}),
		},
		{
			name: "openapi accepted",
			stack: &Stack{
				Name:    "demo",
				Network: Network{Name: "demo"},
				MCPServers: []MCPServer{{
					Name:    "pet",
					OpenAPI: &OpenAPIConfig{Spec: "https://example.test/openapi.json"},
					Headers: map[string]string{"X-Tenant": "acme"},
				}},
			},
		},
		{
			name: "authorization without auth accepted",
			stack: externalServer(map[string]string{"Authorization": "Bearer synthetic"}, nil),
		},
		{
			name: "openapi auth does not conflict with Authorization",
			stack: &Stack{
				Name:    "demo",
				Network: Network{Name: "demo"},
				MCPServers: []MCPServer{{
					Name: "pet",
					OpenAPI: &OpenAPIConfig{
						Spec: "https://example.test/openapi.json",
						Auth: &OpenAPIAuth{Type: "bearer", TokenEnv: "TOKEN"},
					},
					Headers: map[string]string{"Authorization": "Bearer synthetic"},
				}},
			},
		},
		{
			name: "image rejected",
			stack: &Stack{
				Name: "demo",
				MCPServers: []MCPServer{{
					Name:    "box",
					Image:   "alpine",
					Port:    8080,
					Headers: map[string]string{"X-Tenant": "acme"},
				}},
			},
			wantErr: "mcp-servers[0].headers: only valid for external URL and OpenAPI servers",
		},
		{
			name: "local process rejected",
			stack: &Stack{
				Name: "demo",
				MCPServers: []MCPServer{{
					Name:    "local",
					Command: []string{"server"},
					Headers: map[string]string{"X-Tenant": "acme"},
				}},
			},
			wantErr: "only valid for external URL and OpenAPI servers",
		},
		{
			name: "ssh rejected",
			stack: &Stack{
				Name: "demo",
				MCPServers: []MCPServer{{
					Name:    "edge",
					Command: []string{"server"},
					SSH:     &SSHConfig{Host: "edge.example", User: "ops"},
					Headers: map[string]string{"X-Tenant": "acme"},
				}},
			},
			wantErr: "only valid for external URL and OpenAPI servers",
		},
		{
			name: "a2a rejected",
			stack: &Stack{
				Name:         "demo",
				Experimental: map[string]bool{"a2a": true},
				MCPServers: []MCPServer{{
					Name:    "agent",
					A2A:     &A2AConfig{Card: "https://example.test/card"},
					Headers: map[string]string{"X-Tenant": "acme"},
				}},
			},
			wantErr: "only valid for external URL and OpenAPI servers",
		},
		{
			name:    "reserved session",
			stack:   externalServer(map[string]string{"Mcp-Session-Id": "x"}, nil),
			wantErr: "mcp-servers[0].headers[Mcp-Session-Id]: header 'Mcp-Session-Id' is reserved and set by the gateway",
		},
		{
			name:    "reserved protocol any case",
			stack:   externalServer(map[string]string{"MCP-Protocol-Version": "2025-06-18"}, nil),
			wantErr: "header 'MCP-Protocol-Version' is reserved",
		},
		{
			name:    "reserved mcp prefix",
			stack:   externalServer(map[string]string{"Mcp-Param-Tenant": "acme"}, nil),
			wantErr: "header 'Mcp-Param-Tenant' is reserved",
		},
		{
			name:    "reserved content type",
			stack:   externalServer(map[string]string{"Content-Type": "application/json"}, nil),
			wantErr: "header 'Content-Type' is reserved",
		},
		{
			name:    "reserved accept",
			stack:   externalServer(map[string]string{"Accept": "application/json"}, nil),
			wantErr: "header 'Accept' is reserved",
		},
		{
			name:    "reserved host",
			stack:   externalServer(map[string]string{"Host": "example.test"}, nil),
			wantErr: "header 'Host' is reserved",
		},
		{
			name:    "authorization conflicts with auth",
			stack:   externalServer(map[string]string{"authorization": "Bearer x"}, &ServerAuth{Type: "oauth"}),
			wantErr: "mcp-servers[0].headers[Authorization]: conflicts with auth; set the credential in auth or in headers, not both",
		},
		{
			name:    "empty value",
			stack:   externalServer(map[string]string{"X-Tenant": ""}, nil),
			wantErr: "header value is empty",
		},
		{
			name:    "empty name",
			stack:   externalServer(map[string]string{"": "acme"}, nil),
			wantErr: "mcp-servers[0].headers[]: header name is empty",
		},
		{
			name:    "invalid name",
			stack:   externalServer(map[string]string{"Bad Name": "acme"}, nil),
			wantErr: "header name must be an HTTP token",
		},
		{
			name:    "control character",
			stack:   externalServer(map[string]string{"X-Tenant": "acme\n"}, nil),
			wantErr: "header value cannot be sent as an HTTP field value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.stack)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_HiddenToolReferences(t *testing.T) {
	base := func(tools []string, groups map[string]GroupConfig, clients *ClientsConfig) *Stack {
		return &Stack{
			Name:    "demo",
			Network: Network{Name: "demo"},
			MCPServers: []MCPServer{{
				Name:  "vault",
				URL:   "https://example.test/mcp",
				Tools: tools,
			}},
			Groups:  groups,
			Clients: clients,
		}
	}
	t.Run("group tools error", func(t *testing.T) {
		err := Validate(base([]string{"list_paths"}, map[string]GroupConfig{
			"ops": {Tools: []string{"vault__read_secret"}},
		}, nil))
		var ve ValidationErrors
		if !errorsAsValidation(err, &ve) {
			t.Fatalf("error = %v", err)
		}
		var hit ValidationError
		for _, item := range ve {
			if item.Field == "groups[ops].tools[0]" {
				hit = item
			}
		}
		if hit.Message != "tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called" {
			t.Fatalf("hit = %+v", hit)
		}
		if !IsHiddenToolReference(hit) {
			t.Fatal("IsHiddenToolReference false")
		}
	})
	t.Run("override key error", func(t *testing.T) {
		err := Validate(base([]string{"list_paths"}, map[string]GroupConfig{
			"ops": {
				Servers:   []string{"vault"},
				Overrides: map[string]GroupOverride{"vault__read_secret": {Name: "read"}},
			},
		}, nil))
		if err == nil || !strings.Contains(err.Error(), "groups[ops].overrides[vault__read_secret]") || !strings.Contains(err.Error(), "is hidden by") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("empty whitelist skips", func(t *testing.T) {
		err := Validate(base(nil, map[string]GroupConfig{
			"ops": {Tools: []string{"vault__read_secret"}},
		}, nil))
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("server without whitelist skips", func(t *testing.T) {
		stack := base(nil, map[string]GroupConfig{
			"ops": {Tools: []string{"other__read"}},
		}, nil)
		stack.MCPServers = append(stack.MCPServers, MCPServer{Name: "other", URL: "https://example.test/other"})
		if err := Validate(stack); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unknown server is not a hidden-tool error", func(t *testing.T) {
		err := Validate(base([]string{"list_paths"}, map[string]GroupConfig{
			"ops": {Tools: []string{"missing__read"}},
		}, nil))
		var ve ValidationErrors
		if !errorsAsValidation(err, &ve) {
			t.Fatalf("error = %v", err)
		}
		for _, item := range ve {
			if IsHiddenToolReference(item) {
				t.Fatalf("hidden-tool error on unknown server: %+v", item)
			}
		}
		if !strings.Contains(err.Error(), "unknown MCP server") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("profile and exclude warnings", func(t *testing.T) {
		stack := base([]string{"list_paths"}, map[string]GroupConfig{
			"ops": {Servers: []string{"vault"}, Exclude: []string{"vault__read_secret"}},
		}, &ClientsConfig{Profiles: map[string]ClientProfile{
			"x": {Tools: []string{"vault__read_secret"}},
		}})
		result := ValidateWithIssues(stack)
		if !result.Valid {
			t.Fatalf("valid = false, issues = %+v", result.Issues)
		}
		var profile, exclude bool
		for _, issue := range result.Issues {
			if issue.Field == "clients.profiles[x].tools[0]" && strings.Contains(issue.Message, "the profile may be wider than the whitelist on purpose") {
				profile = issue.Severity == SeverityWarning
			}
			if issue.Field == "groups[ops].exclude[0]" && strings.Contains(issue.Message, "excluding a hidden tool has no effect") {
				exclude = issue.Severity == SeverityWarning
			}
		}
		if !profile || !exclude {
			t.Fatalf("issues = %+v", result.Issues)
		}
	})
}
