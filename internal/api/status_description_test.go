package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestHandleMCPServers_DescriptionOmitsHeaders(t *testing.T) {
	const secret = "header-secret-value"
	srv := newTestServer(t)
	srv.gateway.SetServerMeta(mcp.MCPServerConfig{
		Name:        "api",
		Transport:   mcp.TransportHTTP,
		Endpoint:    "https://example.test/mcp",
		External:    true,
		Description: "operator note",
		Headers:     map[string]string{"X-Tenant": secret},
		Auth:        &mcp.ServerAuthConfig{Type: "bearer", Token: secret},
	})
	rec := httptest.NewRecorder()
	srv.handleMCPServers(rec, loopbackRequest(http.MethodGet, "/api/mcp-servers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body []MCPServerStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body[0].Description != "operator note" {
		t.Fatalf("servers = %+v", body)
	}
	if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), "X-Tenant") {
		t.Fatalf("header material leaked: %s", rec.Body.String())
	}
}
