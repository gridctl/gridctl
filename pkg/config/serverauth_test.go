package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validStackWithAuth(auth *ServerAuth) *Stack {
	return &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		MCPServers: []MCPServer{
			{Name: "remote", URL: "https://mcp.example.com/mcp", Auth: auth},
		},
	}
}

func TestValidateServerAuth(t *testing.T) {
	tests := []struct {
		name    string
		auth    *ServerAuth
		wantErr string // substring of the expected validation error; "" = valid
	}{
		{"nil auth valid", nil, ""},
		{"bearer valid", &ServerAuth{Type: "bearer", Token: "tok"}, ""},
		{"bearer missing token", &ServerAuth{Type: "bearer"}, "auth.token"},
		{"header valid", &ServerAuth{Type: "header", Header: "X-API-Key", Value: "v"}, ""},
		{"header missing name", &ServerAuth{Type: "header", Value: "v"}, "auth.header"},
		{"header missing value", &ServerAuth{Type: "header", Header: "X-API-Key"}, "auth.value"},
		{"oauth valid empty", &ServerAuth{Type: "oauth"}, ""},
		{"oauth with client", &ServerAuth{Type: "oauth", ClientID: "id", ClientSecret: "sec", Scopes: []string{"read"}}, ""},
		{"oauth secret without id", &ServerAuth{Type: "oauth", ClientSecret: "sec"}, "auth.client_id"},
		{"missing type", &ServerAuth{Token: "tok"}, "auth.type"},
		{"unknown type", &ServerAuth{Type: "basic"}, "auth.type"},
		{"token on oauth", &ServerAuth{Type: "oauth", Token: "tok"}, "auth.token"},
		{"header fields on bearer", &ServerAuth{Type: "bearer", Token: "tok", Header: "X"}, "auth.header"},
		{"oauth fields on bearer", &ServerAuth{Type: "bearer", Token: "tok", ClientID: "id"}, "auth.scopes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(validStackWithAuth(tt.auth))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateServerAuthRejectedOnNonExternal(t *testing.T) {
	s := &Stack{
		Name: "test",
		MCPServers: []MCPServer{
			{Name: "boxed", Image: "example/image", Auth: &ServerAuth{Type: "bearer", Token: "tok"}},
		},
	}
	err := Validate(s)
	if err == nil || !strings.Contains(err.Error(), "only valid for external URL servers") {
		t.Fatalf("expected non-external auth rejection, got: %v", err)
	}
}

func TestLoadStackExpandsAuthFields(t *testing.T) {
	t.Setenv("TEST_AUTH_TOKEN", "secret-token")
	t.Setenv("TEST_AUTH_CLIENT_ID", "client-123")

	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	yaml := `
name: test
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
    auth:
      type: bearer
      token: ${TEST_AUTH_TOKEN}
  - name: remote-oauth
    url: https://mcp2.example.com/mcp
    auth:
      type: oauth
      client_id: ${TEST_AUTH_CLIENT_ID}
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	stack, err := LoadStack(path)
	if err != nil {
		t.Fatalf("LoadStack: %v", err)
	}
	if got := stack.MCPServers[0].Auth.Token; got != "secret-token" {
		t.Errorf("auth.token not expanded: got %q", got)
	}
	if got := stack.MCPServers[1].Auth.ClientID; got != "client-123" {
		t.Errorf("auth.client_id not expanded: got %q", got)
	}
}
