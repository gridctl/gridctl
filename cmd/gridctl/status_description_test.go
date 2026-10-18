package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusJSON_DescriptionOmitsHeaders(t *testing.T) {
	const secret = "header-secret-value"
	payload := []byte(`[{"name":"api","transport":"http","external":true,"description":"operator note","headers":{"X-Tenant":"` + secret + `"}}]`)
	var servers []mcpServerAPI
	if err := json.Unmarshal(payload, &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Description != "operator note" {
		t.Fatalf("servers = %+v", servers)
	}
	report := statusReport{
		MCPServers: []statusMCPServerJSON{{
			Stack:        "demo",
			mcpServerAPI: servers[0],
		}},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"description":"operator note"`) {
		t.Fatalf("status JSON = %s", text)
	}
	if strings.Contains(text, secret) || strings.Contains(text, "X-Tenant") {
		t.Fatalf("header material leaked into status JSON: %s", text)
	}
}
