package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/resourceauth"
	"github.com/gridctl/gridctl/pkg/secreport"
	"github.com/stretchr/testify/require"
)

func TestSecurityReport_OAuthDeclarationAndStartup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "stack.yaml")
	require.NoError(t, os.WriteFile(file, []byte("name: oauth\ngateway:\n  auth:\n    type: oauth\n    issuer: https://auth.example.com/tenant\n    audience: gateway\n"), 0600))
	in, err := secreport.InputsFromStackFile(t.Context(), file)
	require.NoError(t, err)
	require.True(t, in.Stack.Gateway.AuthDeclared)
	require.Equal(t, "oauth", in.Stack.Gateway.AuthType)
	data, err := json.Marshal(secreport.Assemble(time.Now(), in))
	require.NoError(t, err)
	require.Contains(t, string(data), `"auth_type":"oauth"`)
	require.NotContains(t, string(data), "auth.example.com")
	s := newTestServer(t)
	s.SetStackFile(file)
	s.SetOAuth(nil, resourceauth.Config{Issuer: "https://auth.example.com/tenant?private=value", Audience: "gateway"})
	w := httptest.NewRecorder()
	s.handleSecurityReport(w, loopbackRequest(http.MethodGet, "/api/security-report", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"auth_type":"oauth"`)
	require.Contains(t, w.Body.String(), "authentication enabled as oauth")
	require.NotContains(t, w.Body.String(), "authentication disabled")
	require.NotContains(t, w.Body.String(), "private=value")
}
