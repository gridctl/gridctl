package controller

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestServerRegistrar_RedactsHeaderValuesOnApplyAndReload(t *testing.T) {
	var buf bytes.Buffer
	redactor := logging.NewRedactingHandler(slog.NewTextHandler(&buf, nil))
	registrar := NewServerRegistrar(nil, false)
	registrar.SetRedactor(redactor.RegisterRedactValues)
	logger := slog.New(redactor)

	applied := config.MCPServer{
		Name:        "api",
		URL:         "https://example.test/mcp",
		Description: "notes",
		Headers:     map[string]string{"X-Tenant": "literal-one"},
	}
	cfg := registrar.buildServerConfig(runtime.MCPServerResult{
		Name:     "api",
		External: true,
		URL:      applied.URL,
	}, applied, t.TempDir()+"/stack.yaml")
	if cfg.Headers["X-Tenant"] != "literal-one" || cfg.Description != "notes" {
		t.Fatalf("apply config headers=%v description=%q", cfg.Headers, cfg.Description)
	}
	logger.Info("startup", "note", "literal-one")
	if strings.Contains(buf.String(), "literal-one") {
		t.Fatalf("startup log leaked header value: %s", buf.String())
	}

	reloaded := applied
	reloaded.Headers = map[string]string{"X-Tenant": "literal-two"}
	reloaded.Description = "reloaded notes"
	cfg = registrar.buildConfigFromMCPServer(reloaded, 0, "", t.TempDir()+"/stack.yaml")
	if cfg.Headers["X-Tenant"] != "literal-two" || cfg.Description != "reloaded notes" {
		t.Fatalf("reload config headers=%v description=%q", cfg.Headers, cfg.Description)
	}
	logger.Info("reload", "note", "literal-two and literal-one")
	out := buf.String()
	if strings.Contains(out, "literal-one") || strings.Contains(out, "literal-two") {
		t.Fatalf("reload log leaked header values: %s", out)
	}

	openAPI := config.MCPServer{
		Name:    "pet",
		OpenAPI: &config.OpenAPIConfig{Spec: "https://example.test/openapi.json"},
		Headers: map[string]string{"X-Tenant": "literal-openapi"},
	}
	cfg = registrar.buildOpenAPIConfig(openAPI.Name, openAPI.OpenAPI, nil, openAPI.Headers)
	if cfg.OpenAPIConfig == nil || cfg.OpenAPIConfig.Headers["X-Tenant"] != "literal-openapi" {
		t.Fatalf("openapi headers = %#v", cfg.OpenAPIConfig)
	}
}
