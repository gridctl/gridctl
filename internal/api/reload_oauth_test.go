package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/stretchr/testify/require"
)

func TestReloadSecurity_OAuthFields(t *testing.T) {
	const base = "name: oauth\ngateway:\n  auth:\n    type: oauth\n    issuer: https://auth.example.com\n    audience: gateway\n"
	for _, tc := range []struct {
		field string
		auth  *config.AuthConfig
	}{
		{"issuer", &config.AuthConfig{Type: "oauth", Issuer: "https://other.example.com", Audience: "gateway"}},
		{"audience", &config.AuthConfig{Type: "oauth", Issuer: "https://auth.example.com", Audience: "other"}},
		{"jwks_url", &config.AuthConfig{Type: "oauth", Issuer: "https://auth.example.com", Audience: "gateway", JWKSURL: "https://auth.example.com/keys"}},
		{"required_scopes", &config.AuthConfig{Type: "oauth", Issuer: "https://auth.example.com", Audience: "gateway", RequiredScopes: []string{"tools"}}},
		{"identity_claim", &config.AuthConfig{Type: "oauth", Issuer: "https://auth.example.com", Audience: "gateway", IdentityClaim: "email"}},
	} {
		t.Run(tc.field, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "stack.yaml")
			require.NoError(t, os.WriteFile(file, []byte(base), 0600))
			baseline := &config.Stack{Name: "oauth", Gateway: &config.GatewayConfig{Auth: tc.auth}}
			h := reload.NewHandler(file, baseline, nil, nil, 8180, 9000, nil, nil)
			h.SetSecurityPreflight(reload.NewSecurityPreflight(baseline, "", false))
			result, err := h.Reload(t.Context())
			require.NoError(t, err)
			require.Equal(t, "restart_required", result.Code)
			require.Equal(t, []string{"gateway.auth." + tc.field}, result.ChangedFields)
			require.Same(t, baseline, h.CurrentConfig())
			w := httptest.NewRecorder()
			require.True(t, writePreflightFailure(w, result))
			require.Equal(t, http.StatusConflict, w.Code)
			var response reload.ReloadResult
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, result.ChangedFields, response.ChangedFields)
		})
	}
}
