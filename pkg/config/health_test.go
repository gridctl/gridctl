package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gridctl/gridctl/pkg/pricing"
)

func TestValidateWithIssues_ValidStack(t *testing.T) {
	stack := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{Auth: &AuthConfig{Type: "bearer", Token: "secret"}},
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}

	result := ValidateWithIssues(stack)
	assert.True(t, result.Valid)
	assert.Equal(t, 0, result.ErrorCount)
	assert.Equal(t, 0, result.WarningCount)
}

func TestValidateWithIssues_ErrorsFromValidate(t *testing.T) {
	stack := &Stack{
		// Missing name and network — should produce errors
		MCPServers: []MCPServer{
			{Name: "s1"}, // Missing image/url/etc
		},
	}

	result := ValidateWithIssues(stack)
	assert.False(t, result.Valid)
	assert.Greater(t, result.ErrorCount, 0)

	// Check that errors have correct severity
	for _, issue := range result.Issues {
		if issue.Severity == SeverityError {
			assert.NotEmpty(t, issue.Field)
			assert.NotEmpty(t, issue.Message)
		}
	}
}

func TestValidateWithIssues_WarningNoAuth(t *testing.T) {
	stack := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{}, // No auth
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}

	result := ValidateWithIssues(stack)
	assert.True(t, result.Valid)
	assert.Greater(t, result.WarningCount, 0)

	found := false
	for _, issue := range result.Issues {
		if issue.Field == "gateway.auth" && issue.Severity == SeverityWarning {
			found = true
		}
	}
	assert.True(t, found, "expected warning about missing auth")
}

func TestValidateWithIssues_MixedErrorsAndWarnings(t *testing.T) {
	stack := &Stack{
		// Missing name — error
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{}, // No auth — warning
		MCPServers: []MCPServer{
			{Name: "s1", Image: "alpine", Port: 3000},
		},
	}

	result := ValidateWithIssues(stack)
	assert.False(t, result.Valid)
	assert.Greater(t, result.ErrorCount, 0)
	assert.Greater(t, result.WarningCount, 0)
}

func TestValidateStackFile_ValidFile(t *testing.T) {
	content := `
name: test-stack
network:
  name: test-net
mcp-servers:
  - name: s1
    image: alpine
    port: 3000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	stack, result, err := ValidateStackFile(path)
	require.NoError(t, err)
	assert.NotNil(t, stack)
	assert.True(t, result.Valid)
	assert.Equal(t, "test-stack", stack.Name)
	// Defaults should be applied
	assert.Equal(t, "1", stack.Version)
}

func TestValidateStackFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(":::invalid"), 0644))

	_, _, err := ValidateStackFile(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing stack YAML")
}

func TestValidateStackFile_MissingFile(t *testing.T) {
	_, _, err := ValidateStackFile("/nonexistent/stack.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading stack file")
}

func TestValidateStackFile_InvalidStack(t *testing.T) {
	content := `
mcp-servers:
  - name: s1
`
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	stack, result, err := ValidateStackFile(path)
	require.NoError(t, err) // No parse error
	assert.NotNil(t, stack)
	assert.False(t, result.Valid)
	assert.Greater(t, result.ErrorCount, 0)
}

func TestValidateStackFile_DefaultsApplied(t *testing.T) {
	content := `
name: test
mcp-servers:
  - name: s1
    image: alpine
    port: 3000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	stack, _, err := ValidateStackFile(path)
	require.NoError(t, err)
	assert.Equal(t, "1", stack.Version)
	assert.Equal(t, "bridge", stack.Network.Driver)
	assert.Equal(t, "test-net", stack.Network.Name)
}

func TestValidateWithIssues_WarningTLSFilesNotFound(t *testing.T) {
	stack := &Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{Auth: &AuthConfig{Type: "bearer", Token: "secret"}},
		MCPServers: []MCPServer{
			{Name: "s1", OpenAPI: &OpenAPIConfig{
				Spec: "https://example.com/spec.json",
				TLS:  &OpenAPITLS{CertFile: "/nonexistent/cert.pem", KeyFile: "/nonexistent/key.pem", CaFile: "/nonexistent/ca.pem"},
			}},
		},
	}

	result := ValidateWithIssues(stack)
	assert.True(t, result.Valid) // warnings don't make it invalid
	assert.Equal(t, 0, result.ErrorCount)
	assert.Equal(t, 3, result.WarningCount)

	fields := make(map[string]bool)
	for _, issue := range result.Issues {
		if issue.Severity == SeverityWarning {
			fields[issue.Field] = true
		}
	}
	assert.True(t, fields["mcp-servers[0].openapi.tls.certFile"])
	assert.True(t, fields["mcp-servers[0].openapi.tls.keyFile"])
	assert.True(t, fields["mcp-servers[0].openapi.tls.caFile"])
}

func TestValidationResult_Counts(t *testing.T) {
	result := &ValidationResult{Valid: true}
	result.Issues = []ValidationIssue{
		{Field: "a", Message: "err1", Severity: SeverityError},
		{Field: "b", Message: "err2", Severity: SeverityError},
		{Field: "c", Message: "warn1", Severity: SeverityWarning},
	}
	result.ErrorCount = 2
	result.WarningCount = 1

	assert.Equal(t, 2, result.ErrorCount)
	assert.Equal(t, 1, result.WarningCount)
}

// modelWarningSource is a deterministic pricing.Source for warning tests.
type modelWarningSource struct{ known map[string]bool }

func (s modelWarningSource) Lookup(model string) (pricing.Rates, bool) {
	if s.known[model] {
		return pricing.Rates{InputPerToken: 1e-6}, true
	}
	return pricing.Rates{}, false
}

func (s modelWarningSource) Models() []string { return nil }

func (s modelWarningSource) Name() string { return "health-test-fixture" }

func TestValidateWithIssues_ModelWarnings(t *testing.T) {
	prev := pricing.CurrentSource()
	t.Cleanup(func() { pricing.SetSource(prev) })
	pricing.SetSource(modelWarningSource{known: map[string]bool{"known-model": true}})

	stack := &Stack{
		Version: "1",
		Name:    "test",
		Network: Network{Name: "test-net"},
		Gateway: &GatewayConfig{DefaultModel: "unknown-default"},
		MCPServers: []MCPServer{
			{Name: "a", Image: "img", Port: 3000, Model: "unknown-server-model"},
			{Name: "b", Image: "img", Port: 3001, Model: "known-model"},
		},
		ClientModels: map[string]string{
			"claude-code": "known-model",
			"gemini-cli":  "unknown-client-model",
			"Claude Code": "known-model", // non-normalized key
		},
	}

	result := ValidateWithIssues(stack)

	warnings := map[string]string{}
	for _, issue := range result.Issues {
		if issue.Severity == SeverityWarning {
			warnings[issue.Field] = issue.Message
		}
	}

	for _, field := range []string{
		"gateway.default_model",
		"mcp-servers[0].model",
		"client_models.gemini-cli",
		"client_models.Claude Code",
	} {
		if _, ok := warnings[field]; !ok {
			t.Errorf("expected warning for %s; got %v", field, warnings)
		}
	}
	if msg, ok := warnings["client_models.Claude Code"]; ok && !strings.Contains(msg, `"claude-code"`) {
		t.Errorf("non-normalized key warning should name the canonical form; got %q", msg)
	}
	if _, ok := warnings["mcp-servers[1].model"]; ok {
		t.Error("known model must not warn")
	}
	if _, ok := warnings["client_models.claude-code"]; ok {
		t.Error("known model on normalized key must not warn")
	}
	// Warnings never flip validity.
	if !result.Valid {
		t.Error("model warnings must not invalidate the stack")
	}
}
