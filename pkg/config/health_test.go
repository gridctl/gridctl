package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gridctl/gridctl/pkg/flags"
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



func TestValidateWithIssues_ExperimentalIssues(t *testing.T) {
	base := func() *Stack {
		return &Stack{
			Name:    "test",
			Network: Network{Name: "test-net"},
			Gateway: &GatewayConfig{Auth: &AuthConfig{Type: "bearer", Token: "secret"}},
			MCPServers: []MCPServer{
				{Name: "s1", Image: "alpine", Port: 3000},
			},
		}
	}

	t.Run("omitted block adds nothing (back-compat)", func(t *testing.T) {
		result := ValidateWithIssues(base())
		for _, issue := range result.Issues {
			if strings.HasPrefix(issue.Field, "experimental.") {
				t.Fatalf("unexpected experimental issue: %+v", issue)
			}
		}
	})

	t.Run("unknown flag name warns without invalidating", func(t *testing.T) {
		stack := base()
		stack.Experimental = map[string]bool{"tpyo_flag": true}
		result := ValidateWithIssues(stack)
		require.True(t, result.Valid)
		var found bool
		for _, issue := range result.Issues {
			if issue.Field == "experimental.tpyo_flag" {
				found = true
				assert.Equal(t, SeverityWarning, issue.Severity)
				assert.Contains(t, issue.Message, "unknown experimental flag")
			}
		}
		require.True(t, found, "expected a warning for the unknown flag name")
		assert.Greater(t, result.WarningCount, 0)
	})

	t.Run("graduated flag warns with migration message", func(t *testing.T) {
		stack := base()
		stack.Experimental = map[string]bool{"transport_dual_stack": true}
		result := ValidateWithIssues(stack)
		require.True(t, result.Valid, "graduated names warn, never block (Article IX)")
		var found bool
		for _, issue := range result.Issues {
			if issue.Field == "experimental.transport_dual_stack" {
				found = true
				assert.Equal(t, SeverityWarning, issue.Severity)
				assert.Contains(t, issue.Message, "graduated in")
			}
		}
		require.True(t, found, "expected a migration warning for the graduated flag")
	})

	t.Run("enabled experimental flag reports as info", func(t *testing.T) {
		reg, err := flags.NewRegistry(flags.Flag{
			Name:        "test_flag",
			Description: "synthetic flag for validation tests",
			Stage:       flags.StageExperimental,
			Since:       "0.1.0",
			GraduatesBy: "99.0.0",
		})
		require.NoError(t, err)
		stack := base()
		stack.Experimental = map[string]bool{"test_flag": true, "off_flag": false}
		result := &ValidationResult{Valid: true}
		result.addExperimentalIssues(reg, stack)
		require.Len(t, result.Issues, 2, "expected the unknown-name warning and the info line")
		var found bool
		for _, issue := range result.Issues {
			if issue.Field == "experimental.test_flag" {
				found = true
				assert.Equal(t, SeverityInfo, issue.Severity)
			}
		}
		require.True(t, found, "expected an info line for the enabled flag")
	})

	t.Run("disabled experimental flag stays silent", func(t *testing.T) {
		reg, err := flags.NewRegistry(flags.Flag{
			Name:        "test_flag",
			Description: "synthetic flag for validation tests",
			Stage:       flags.StageExperimental,
			Since:       "0.1.0",
			GraduatesBy: "99.0.0",
		})
		require.NoError(t, err)
		stack := base()
		stack.Experimental = map[string]bool{"test_flag": false}
		result := &ValidationResult{Valid: true}
		result.addExperimentalIssues(reg, stack)
		assert.Empty(t, result.Issues, "a known experimental flag set to false must add nothing")
	})
}
