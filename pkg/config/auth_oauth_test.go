package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestExportStack_OAuthFields(t *testing.T) {
	auth := &AuthConfig{Type: "oauth", Issuer: "https://auth.example/tenant", Audience: "gridctl", JWKSURL: "https://auth.example/keys", RequiredScopes: []string{"tools", "openid"}, IdentityClaim: "email"}
	stack := &Stack{Name: "oauth", Gateway: &GatewayConfig{Auth: auth}}
	data, err := yaml.Marshal(stack)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "stack.yaml")
	require.NoError(t, os.WriteFile(file, data, 0600))
	exported, _, err := ExportStack(context.Background(), file)
	require.NoError(t, err)
	require.Equal(t, auth, exported.Gateway.Auth)
}

func TestValidate_GatewayAuthOAuth(t *testing.T) {
	base := func(auth *AuthConfig) *Stack {
		return &Stack{
			Name:       "test",
			Network:    Network{Name: "test-net"},
			MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Port: 3000}},
			Gateway:    &GatewayConfig{Auth: auth},
		}
	}
	valid := &AuthConfig{
		Type:     "oauth",
		Issuer:   "https://auth.example/dex",
		Audience: "gridctl-gateway",
	}
	tests := []struct {
		name   string
		auth   *AuthConfig
		field  string
		substr string
	}{
		{name: "valid", auth: valid},
		{name: "valid with optional fields", auth: &AuthConfig{
			Type: "oauth", Issuer: "http://127.0.0.1:5556/dex", Audience: "api://gridctl",
			JWKSURL: "https://auth.example/jwks", RequiredScopes: []string{"mcp:tools"}, IdentityClaim: "email",
		}},
		{name: "missing audience", auth: &AuthConfig{Type: "oauth", Issuer: "https://auth.example"}, field: "gateway.auth.audience", substr: "is required when type is 'oauth'"},
		{name: "missing issuer", auth: &AuthConfig{Type: "oauth", Audience: "gridctl"}, field: "gateway.auth.issuer"},
		{name: "issuer query", auth: &AuthConfig{Type: "oauth", Issuer: "https://auth.example?secret=x", Audience: "gridctl"}, field: "gateway.auth.issuer"},
		{name: "issuer fragment", auth: &AuthConfig{Type: "oauth", Issuer: "https://auth.example#x", Audience: "gridctl"}, field: "gateway.auth.issuer"},
		{name: "token rejected", auth: &AuthConfig{Type: "oauth", Issuer: "https://auth.example", Audience: "gridctl", Token: "secret"}, field: "gateway.auth.token"},
		{name: "header rejected", auth: &AuthConfig{Type: "oauth", Issuer: "https://auth.example", Audience: "gridctl", Header: "X-Api-Key"}, field: "gateway.auth.header"},
		{name: "issuer on bearer", auth: &AuthConfig{Type: "bearer", Token: "secret", Issuer: "https://auth.example"}, field: "gateway.auth.issuer"},
		{name: "audience on api key", auth: &AuthConfig{Type: "api_key", Token: "secret", Audience: "gridctl"}, field: "gateway.auth.audience"},
		{name: "scopes on bearer", auth: &AuthConfig{Type: "bearer", Token: "secret", RequiredScopes: []string{"mcp"}}, field: "gateway.auth.required_scopes"},
		{name: "blank scope", auth: &AuthConfig{Type: "oauth", Issuer: "https://auth.example", Audience: "gridctl", RequiredScopes: []string{"mcp", ""}}, field: "gateway.auth.required_scopes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(base(tc.auth))
			if tc.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error %q does not name %s", err, tc.field)
			}
			if tc.substr != "" && !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("error %q does not contain %q", err, tc.substr)
			}
		})
	}
}

func TestAuthConfig_ConfiguredAndIssuerHost(t *testing.T) {
	if (*AuthConfig)(nil).Configured() {
		t.Fatal("nil auth is configured")
	}
	if (&AuthConfig{Type: "bearer"}).Configured() {
		t.Fatal("bearer without token is configured")
	}
	if !(&AuthConfig{Type: "bearer", Token: "s"}).Configured() {
		t.Fatal("bearer with token is not configured")
	}
	if !(&AuthConfig{Type: "oauth", Issuer: "https://auth.example/dex?secret=x", Audience: "g"}).Configured() {
		t.Fatal("oauth is not configured")
	}
	host := (&AuthConfig{Issuer: "https://user:pass@auth.example:8443/dex?secret=x#f"}).IssuerHost()
	if host != "auth.example:8443" {
		t.Fatalf("issuer host = %q", host)
	}
	if (&AuthConfig{Type: "bearer", Token: "s"}).IssuerHost() != "" {
		t.Fatal("bearer issuer host is not empty")
	}
	if (&AuthConfig{}).IdentityClaimOrDefault() != "sub" {
		t.Fatal("default identity claim")
	}
}
