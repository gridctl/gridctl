package reload

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestSecurityPreflight_BearerToOAuth(t *testing.T) {
	before := &config.Stack{Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{Type: "bearer", Token: "${GATEWAY_TOKEN}"}}}
	after := &config.Stack{Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{
		Type: "oauth", Issuer: "https://auth.example", Audience: "gridctl",
		JWKSURL: "https://auth.example/keys", RequiredScopes: []string{"tools"}, IdentityClaim: "email",
	}}}
	require.Equal(t, []string{
		"gateway.auth.type", "gateway.auth.token", "gateway.auth.header",
		"gateway.auth.issuer", "gateway.auth.audience", "gateway.auth.jwks_url",
		"gateway.auth.required_scopes", "gateway.auth.identity_claim",
	}, NewSecurityPreflight(before, "", false)(after))
	require.Equal(t, []string{"true"}, effectiveSecurity(after, "", false)["gateway.auth.enabled"])
}
