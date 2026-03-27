package config

import (
	"strings"
	"testing"
)

func TestValidate_StackLevel(t *testing.T) {
	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing stack name",
			stack: &Stack{
				Network:    Network{Name: "test-net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			},
			wantErr: true,
			errMsg:  "stack.name",
		},
		{
			name: "valid stack name",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "test-net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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

func TestValidate_GatewayCodeMode(t *testing.T) {
	base := func() *Stack {
		return &Stack{
			Name:       "test",
			Network:    Network{Name: "test-net"},
			MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
		}
	}

	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid code_mode on",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{CodeMode: "on"}
				return s
			}(),
		},
		{
			name: "valid code_mode off",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{CodeMode: "off"}
				return s
			}(),
		},
		{
			name: "invalid code_mode value",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{CodeMode: "auto"}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.code_mode",
		},
		{
			name: "negative code_mode_timeout",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{CodeMode: "on", CodeModeTimeout: -1}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.code_mode_timeout",
		},
		{
			name: "valid code_mode_timeout",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{CodeMode: "on", CodeModeTimeout: 60}
				return s
			}(),
		},
		{
			name: "no gateway config is valid",
			stack: base(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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

func TestValidate_GatewayOutputFormat(t *testing.T) {
	base := func() *Stack {
		return &Stack{
			Name:       "test",
			Network:    Network{Name: "test-net"},
			MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
		}
	}

	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid output_format json",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{OutputFormat: "json"}
				return s
			}(),
		},
		{
			name: "valid output_format toon",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{OutputFormat: "toon"}
				return s
			}(),
		},
		{
			name: "valid output_format csv",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{OutputFormat: "csv"}
				return s
			}(),
		},
		{
			name: "valid output_format text",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{OutputFormat: "text"}
				return s
			}(),
		},
		{
			name: "empty output_format is valid",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{}
				return s
			}(),
		},
		{
			name: "invalid output_format value",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{OutputFormat: "xml"}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.output_format",
		},
		{
			name: "no gateway config is valid",
			stack: base(),
		},
		// Per-server output_format
		{
			name: "valid per-server output_format",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "test-net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000, OutputFormat: "toon"}},
			},
		},
		{
			name: "invalid per-server output_format",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "test-net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000, OutputFormat: "yaml"}},
			},
			wantErr: true,
			errMsg:  "mcp-servers[0].output_format",
		},
		{
			name: "per-server overrides gateway format",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{OutputFormat: "toon"}
				s.MCPServers[0].OutputFormat = "json"
				return s
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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

func TestValidate_GatewayAuth(t *testing.T) {
	base := func() *Stack {
		return &Stack{
			Name:       "test",
			Network:    Network{Name: "test-net"},
			MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
		}
	}

	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing auth type",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{Auth: &AuthConfig{Token: "secret"}}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.auth.type",
		},
		{
			name: "invalid auth type",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{Auth: &AuthConfig{Type: "oauth", Token: "secret"}}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.auth.type",
		},
		{
			name: "missing token",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{Auth: &AuthConfig{Type: "bearer"}}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.auth.token",
		},
		{
			name: "header with non-api_key type",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{Auth: &AuthConfig{Type: "bearer", Token: "secret", Header: "X-Custom"}}
				return s
			}(),
			wantErr: true,
			errMsg:  "gateway.auth.header",
		},
		{
			name: "valid bearer auth",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{Auth: &AuthConfig{Type: "bearer", Token: "secret"}}
				return s
			}(),
		},
		{
			name: "valid api_key auth",
			stack: func() *Stack {
				s := base()
				s.Gateway = &GatewayConfig{Auth: &AuthConfig{Type: "api_key", Token: "secret", Header: "X-API-Key"}}
				return s
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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

func TestValidate_Network(t *testing.T) {
	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name: "simple mode missing network name",
			stack: &Stack{
				Name:       "test",
				Network:    Network{},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			},
			wantErr: true,
			errMsg:  "stack.network.name",
		},
		{
			name: "simple mode invalid driver",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net", Driver: "overlay"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			},
			wantErr: true,
			errMsg:  "stack.network.driver",
		},
		{
			name: "simple mode valid config",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net", Driver: "bridge"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			},
		},
		{
			name: "advanced mode duplicate network names",
			stack: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "net1", Driver: "bridge"},
					{Name: "net1", Driver: "bridge"},
				},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000, Network: "net1"}},
			},
			wantErr: true,
			errMsg:  "duplicate network name",
		},
		{
			name: "advanced mode missing network name",
			stack: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "", Driver: "bridge"},
				},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			},
			wantErr: true,
			errMsg:  "networks[0].name",
		},
		{
			name: "advanced mode invalid driver",
			stack: &Stack{
				Name: "test",
				Networks: []Network{
					{Name: "net1", Driver: "overlay"},
				},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000, Network: "net1"}},
			},
			wantErr: true,
			errMsg:  "networks[0].driver",
		},
		{
			name: "both network and networks set",
			stack: &Stack{
				Name:     "test",
				Network:  Network{Name: "single"},
				Networks: []Network{{Name: "net1"}},
				MCPServers: []MCPServer{
					{Name: "s1", Image: "alpine", Port: 3000, Network: "net1"},
				},
			},
			wantErr: true,
			errMsg:  "cannot have both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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

func TestValidate_MCPServer(t *testing.T) {
	base := func(servers []MCPServer) *Stack {
		return &Stack{
			Name:       "test",
			Network:    Network{Name: "test-net"},
			MCPServers: servers,
		}
	}

	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing server name",
			stack:   base([]MCPServer{{Image: "alpine", Port: 3000}}),
			wantErr: true,
			errMsg:  "mcp-servers[0].name",
		},
		{
			name: "duplicate server names",
			stack: base([]MCPServer{
				{Name: "s1", Image: "alpine", Port: 3000},
				{Name: "s1", Image: "nginx", Port: 3001},
			}),
			wantErr: true,
			errMsg:  "duplicate MCP server name",
		},
		{
			name:    "no image/source/url/command/ssh/openapi",
			stack:   base([]MCPServer{{Name: "s1", Port: 3000}}),
			wantErr: true,
			errMsg:  "must have",
		},
		{
			name: "multiple of image and source",
			stack: base([]MCPServer{
				{Name: "s1", Image: "alpine", Source: &Source{Type: "git", URL: "https://example.com"}, Port: 3000},
			}),
			wantErr: true,
			errMsg:  "can only have one",
		},
		// External server validation
		{
			name: "external server stdio transport rejected",
			stack: base([]MCPServer{
				{Name: "s1", URL: "http://example.com", Transport: "stdio"},
			}),
			wantErr: true,
			errMsg:  "stdio not valid for external",
		},
		{
			name: "external server port set rejected",
			stack: base([]MCPServer{
				{Name: "s1", URL: "http://example.com", Port: 8080},
			}),
			wantErr: true,
			errMsg:  "should not be set for external URL",
		},
		{
			name: "external server network set rejected",
			stack: base([]MCPServer{
				{Name: "s1", URL: "http://example.com", Network: "some-net"},
			}),
			wantErr: true,
			errMsg:  "not applicable for external URL",
		},
		{
			name: "valid external server",
			stack: base([]MCPServer{
				{Name: "s1", URL: "http://example.com", Transport: "http"},
			}),
		},
		// Local process validation
		{
			name: "local process non-stdio transport rejected",
			stack: base([]MCPServer{
				{Name: "s1", Command: []string{"./server"}, Transport: "http"},
			}),
			wantErr: true,
			errMsg:  "must be 'stdio' for local process",
		},
		{
			name: "local process port set rejected",
			stack: base([]MCPServer{
				{Name: "s1", Command: []string{"./server"}, Port: 3000},
			}),
			wantErr: true,
			errMsg:  "should not be set for local process",
		},
		{
			name: "local process network set rejected",
			stack: base([]MCPServer{
				{Name: "s1", Command: []string{"./server"}, Network: "net"},
			}),
			wantErr: true,
			errMsg:  "not applicable for local process",
		},
		{
			name: "valid local process",
			stack: base([]MCPServer{
				{Name: "s1", Command: []string{"npx", "server"}},
			}),
		},
		// SSH server validation
		{
			name: "SSH server missing host",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{User: "user"}, Command: []string{"server"}},
			}),
			wantErr: true,
			errMsg:  "ssh.host",
		},
		{
			name: "SSH server missing user",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{Host: "10.0.0.1"}, Command: []string{"server"}},
			}),
			wantErr: true,
			errMsg:  "ssh.user",
		},
		{
			name: "SSH server invalid port",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{Host: "10.0.0.1", User: "user", Port: -1}, Command: []string{"server"}},
			}),
			wantErr: true,
			errMsg:  "ssh.port",
		},
		{
			name: "SSH server non-stdio transport rejected",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{Host: "10.0.0.1", User: "user"}, Command: []string{"server"}, Transport: "http"},
			}),
			wantErr: true,
			errMsg:  "must be 'stdio' for SSH",
		},
		{
			name: "SSH server port set rejected",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{Host: "10.0.0.1", User: "user"}, Command: []string{"server"}, Port: 3000},
			}),
			wantErr: true,
			errMsg:  "should not be set for SSH",
		},
		{
			name: "SSH server network set rejected",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{Host: "10.0.0.1", User: "user"}, Command: []string{"server"}, Network: "net"},
			}),
			wantErr: true,
			errMsg:  "not applicable for SSH",
		},
		{
			name: "valid SSH server",
			stack: base([]MCPServer{
				{Name: "s1", SSH: &SSHConfig{Host: "10.0.0.1", User: "user"}, Command: []string{"server"}},
			}),
		},
		// OpenAPI server validation
		{
			name: "OpenAPI missing spec",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{}},
			}),
			wantErr: true,
			errMsg:  "openapi.spec",
		},
		{
			name: "OpenAPI invalid auth type",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{
					Spec: "https://example.com/spec.json",
					Auth: &OpenAPIAuth{Type: "oauth"},
				}},
			}),
			wantErr: true,
			errMsg:  "openapi.auth.type",
		},
		{
			name: "OpenAPI bearer missing tokenEnv",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{
					Spec: "https://example.com/spec.json",
					Auth: &OpenAPIAuth{Type: "bearer"},
				}},
			}),
			wantErr: true,
			errMsg:  "openapi.auth.tokenEnv",
		},
		{
			name: "OpenAPI header auth missing header",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{
					Spec: "https://example.com/spec.json",
					Auth: &OpenAPIAuth{Type: "header", ValueEnv: "API_KEY"},
				}},
			}),
			wantErr: true,
			errMsg:  "openapi.auth.header",
		},
		{
			name: "OpenAPI header auth missing valueEnv",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{
					Spec: "https://example.com/spec.json",
					Auth: &OpenAPIAuth{Type: "header", Header: "X-API-Key"},
				}},
			}),
			wantErr: true,
			errMsg:  "openapi.auth.valueEnv",
		},
		{
			name: "OpenAPI include and exclude",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{
					Spec: "https://example.com/spec.json",
					Operations: &OperationsFilter{
						Include: []string{"op1"},
						Exclude: []string{"op2"},
					},
				}},
			}),
			wantErr: true,
			errMsg:  "cannot use both 'include' and 'exclude'",
		},
		{
			name: "OpenAPI transport set rejected",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{Spec: "spec.json"}, Transport: "http"},
			}),
			wantErr: true,
			errMsg:  "not applicable for OpenAPI",
		},
		{
			name: "OpenAPI port set rejected",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{Spec: "spec.json"}, Port: 3000},
			}),
			wantErr: true,
			errMsg:  "not applicable for OpenAPI",
		},
		{
			name: "OpenAPI network set rejected",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{Spec: "spec.json"}, Network: "net"},
			}),
			wantErr: true,
			errMsg:  "not applicable for OpenAPI",
		},
		{
			name: "valid OpenAPI server",
			stack: base([]MCPServer{
				{Name: "s1", OpenAPI: &OpenAPIConfig{Spec: "https://example.com/spec.json"}},
			}),
		},
		// Container server validation
		{
			name: "container invalid transport",
			stack: base([]MCPServer{
				{Name: "s1", Image: "alpine", Port: 3000, Transport: "grpc"},
			}),
			wantErr: true,
			errMsg:  "must be 'http', 'sse', or 'stdio'",
		},
		{
			name: "container missing port for HTTP",
			stack: base([]MCPServer{
				{Name: "s1", Image: "alpine", Port: 0},
			}),
			wantErr: true,
			errMsg:  "must be a positive integer",
		},
		{
			name: "container port too large",
			stack: base([]MCPServer{
				{Name: "s1", Image: "alpine", Port: 70000},
			}),
			wantErr: true,
			errMsg:  "must be <= 65535",
		},
		{
			name: "container stdio transport no port needed",
			stack: base([]MCPServer{
				{Name: "s1", Image: "alpine", Transport: "stdio"},
			}),
		},
		// Advanced network mode for container servers
		{
			name: "advanced mode missing server network",
			stack: &Stack{
				Name:     "test",
				Networks: []Network{{Name: "net1", Driver: "bridge"}},
				MCPServers: []MCPServer{
					{Name: "s1", Image: "alpine", Port: 3000},
				},
			},
			wantErr: true,
			errMsg:  "required when 'networks' is defined",
		},
		{
			name: "advanced mode unknown network name",
			stack: &Stack{
				Name:     "test",
				Networks: []Network{{Name: "net1", Driver: "bridge"}},
				MCPServers: []MCPServer{
					{Name: "s1", Image: "alpine", Port: 3000, Network: "nonexistent"},
				},
			},
			wantErr: true,
			errMsg:  "not found in networks list",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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

func TestValidate_Resource(t *testing.T) {
	tests := []struct {
		name    string
		stack   *Stack
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing resource name",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
				Resources:  []Resource{{Image: "postgres:16"}},
			},
			wantErr: true,
			errMsg:  "resources[0].name",
		},
		{
			name: "missing resource image",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
				Resources:  []Resource{{Name: "db"}},
			},
			wantErr: true,
			errMsg:  "resources[0].image",
		},
		{
			name: "duplicate resource names",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
				Resources: []Resource{
					{Name: "db", Image: "postgres:16"},
					{Name: "db", Image: "mysql:8"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate resource name",
		},
		{
			name: "resource name conflicts with server",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
				Resources:  []Resource{{Name: "s1", Image: "postgres:16"}},
			},
			wantErr: true,
			errMsg:  "conflicts with an MCP server",
		},
		{
			name: "advanced network mode missing resource network",
			stack: &Stack{
				Name:       "test",
				Networks:   []Network{{Name: "net1", Driver: "bridge"}},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000, Network: "net1"}},
				Resources:  []Resource{{Name: "db", Image: "postgres:16"}},
			},
			wantErr: true,
			errMsg:  "required when 'networks' is defined",
		},
		{
			name: "advanced network mode unknown network",
			stack: &Stack{
				Name:       "test",
				Networks:   []Network{{Name: "net1", Driver: "bridge"}},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000, Network: "net1"}},
				Resources:  []Resource{{Name: "db", Image: "postgres:16", Network: "nonexistent"}},
			},
			wantErr: true,
			errMsg:  "not found in networks list",
		},
		{
			name: "valid resource",
			stack: &Stack{
				Name:       "test",
				Network:    Network{Name: "net"},
				MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
				Resources:  []Resource{{Name: "db", Image: "postgres:16"}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.stack)
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


func TestValidateSource(t *testing.T) {
	tests := []struct {
		name    string
		source  *Source
		wantErr bool
		errMsg  string
	}{
		{
			name:    "git source missing URL",
			source:  &Source{Type: "git"},
			wantErr: true,
			errMsg:  "url",
		},
		{
			name:    "git source with path set",
			source:  &Source{Type: "git", URL: "https://example.com", Path: "/local/path"},
			wantErr: true,
			errMsg:  "should not be set for git source",
		},
		{
			name:    "local source missing path",
			source:  &Source{Type: "local"},
			wantErr: true,
			errMsg:  "path",
		},
		{
			name:    "local source with URL set",
			source:  &Source{Type: "local", Path: "/some/path", URL: "https://example.com"},
			wantErr: true,
			errMsg:  "should not be set for local source",
		},
		{
			name:    "missing type",
			source:  &Source{},
			wantErr: true,
			errMsg:  "is required",
		},
		{
			name:    "unknown type",
			source:  &Source{Type: "s3"},
			wantErr: true,
			errMsg:  "must be 'git' or 'local'",
		},
		{
			name:   "valid git source",
			source: &Source{Type: "git", URL: "https://github.com/example/repo"},
		},
		{
			name:   "valid local source",
			source: &Source{Type: "local", Path: "/app/src"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateSource(tc.source, "test.source")
			if tc.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected errors, got none")
				}
				if tc.errMsg != "" && !strings.Contains(errs.Error(), tc.errMsg) {
					t.Errorf("expected error containing %q, got %q", tc.errMsg, errs.Error())
				}
			} else if len(errs) > 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
		})
	}
}

func TestValidationErrors_String(t *testing.T) {
	// Empty errors
	var empty ValidationErrors
	if empty.Error() != "" {
		t.Errorf("expected empty string, got %q", empty.Error())
	}

	// Single error
	single := ValidationErrors{{Field: "test.field", Message: "is required"}}
	if !strings.Contains(single.Error(), "test.field: is required") {
		t.Errorf("expected formatted error, got %q", single.Error())
	}

	// Multiple errors
	multi := ValidationErrors{
		{Field: "a", Message: "msg1"},
		{Field: "b", Message: "msg2"},
	}
	result := multi.Error()
	if !strings.Contains(result, "validation errors:") {
		t.Errorf("expected 'validation errors:' prefix, got %q", result)
	}
	if !strings.Contains(result, "a: msg1") || !strings.Contains(result, "b: msg2") {
		t.Errorf("expected both errors in output, got %q", result)
	}
}
