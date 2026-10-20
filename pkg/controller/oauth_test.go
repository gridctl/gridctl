package controller

import (
	"log/slog"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestOAuth_StateAndExposure(t *testing.T) {
	stack := &config.Stack{Gateway: &config.GatewayConfig{
		Bind: "0.0.0.0", AllowedOrigins: []string{"https://ui.example.com"},
		Auth: &config.AuthConfig{Type: "oauth", Issuer: "https://auth.example.com:8443/tenant", Audience: "gateway"},
	}}
	b := NewGatewayBuilder(Config{}, stack, "", nil, nil)
	require.NoError(t, b.checkExposure(slog.Default()))
	token, header, kind, host := authStateFrom(stack)
	require.Empty(t, token)
	require.Empty(t, header)
	require.Equal(t, "oauth", kind)
	require.Equal(t, "auth.example.com:8443", host)
	// Defense in depth: even an unvalidated URL cannot disclose its query.
	stack.Gateway.Auth.Issuer += "?private=value"
	_, _, _, host = authStateFrom(stack)
	require.Equal(t, "auth.example.com:8443", host)
	for _, mode := range []string{"bearer", "api_key"} {
		stack.Gateway.Auth.Type = mode
		_, _, _, host = authStateFrom(stack)
		require.Empty(t, host)
	}
}
