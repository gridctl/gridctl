package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGatewayStatus_DescriptionOmitsHeaders(t *testing.T) {
	const secret = "header-secret-value"
	g := NewGateway()
	t.Cleanup(g.Close)
	g.SetServerMeta(MCPServerConfig{
		Name:        "api",
		Transport:   TransportHTTP,
		Endpoint:    "https://example.test/mcp",
		External:    true,
		Description: "operator note",
		Headers:     map[string]string{"X-Tenant": secret},
		Auth:        &ServerAuthConfig{Type: "bearer", Token: secret},
	})
	statuses := g.Status()
	if len(statuses) != 1 {
		t.Fatalf("statuses = %+v", statuses)
	}
	if statuses[0].Description != "operator note" {
		t.Fatalf("description = %q", statuses[0].Description)
	}
	raw, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"description":"operator note"`) {
		t.Fatalf("status JSON = %s", text)
	}
	if strings.Contains(text, secret) || strings.Contains(text, "X-Tenant") {
		t.Fatalf("header material leaked into status: %s", text)
	}
}
