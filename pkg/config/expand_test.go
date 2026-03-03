package config

import (
	"os"
	"testing"
)

// mockVault implements VaultLookup for testing.
type mockVault struct {
	secrets map[string]string
}

func (m *mockVault) Get(key string) (string, bool) {
	v, ok := m.secrets[key]
	return v, ok
}

func TestExpandString(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		env            map[string]string
		vault          map[string]string
		want           string
		wantUnresolved []string
		wantEmpty      []string
	}{
		// Simple ${VAR} expansion
		{
			name:  "simple env var",
			input: "${TEST_VAR}",
			env:   map[string]string{"TEST_VAR": "hello"},
			want:  "hello",
		},
		{
			name:      "undefined env var expands to empty",
			input:     "${UNDEFINED_VAR}",
			want:      "",
			wantEmpty: []string{"UNDEFINED_VAR"},
		},
		{
			name:      "empty env var expands to empty",
			input:     "${EMPTY_VAR}",
			env:       map[string]string{"EMPTY_VAR": ""},
			want:      "",
			wantEmpty: []string{"EMPTY_VAR"},
		},

		// ${VAR:-default} expansion
		{
			name:  "default when undefined",
			input: "${API_URL:-https://default.example.com}",
			want:  "https://default.example.com",
		},
		{
			name:  "default when empty",
			input: "${API_URL:-https://default.example.com}",
			env:   map[string]string{"API_URL": ""},
			want:  "https://default.example.com",
		},
		{
			name:  "no default when defined",
			input: "${API_URL:-https://default.example.com}",
			env:   map[string]string{"API_URL": "https://real.example.com"},
			want:  "https://real.example.com",
		},

		// ${VAR:+replacement} expansion
		{
			name:  "replacement when defined",
			input: "${TOKEN:+present}",
			env:   map[string]string{"TOKEN": "secret"},
			want:  "present",
		},
		{
			name:  "no replacement when undefined",
			input: "${TOKEN:+present}",
			want:  "",
		},
		{
			name:  "no replacement when empty",
			input: "${TOKEN:+present}",
			env:   map[string]string{"TOKEN": ""},
			want:  "",
		},

		// ${vault:KEY} expansion
		{
			name:  "vault hit",
			input: "${vault:SECRET_KEY}",
			vault: map[string]string{"SECRET_KEY": "vault-value"},
			want:  "vault-value",
		},
		{
			name:  "vault miss env hit",
			input: "${vault:SECRET_KEY}",
			env:   map[string]string{"SECRET_KEY": "env-value"},
			want:  "env-value",
		},
		{
			name:           "vault and env miss",
			input:          "${vault:MISSING_KEY}",
			want:           "${vault:MISSING_KEY}", // left as-is for error reporting
			wantUnresolved: []string{"MISSING_KEY"},
		},
		{
			name:  "vault takes priority over env",
			input: "${vault:KEY}",
			vault: map[string]string{"KEY": "vault"},
			env:   map[string]string{"KEY": "env"},
			want:  "vault",
		},

		// Mixed text
		{
			name:  "mixed text with vault ref",
			input: "prefix ${vault:KEY} suffix",
			vault: map[string]string{"KEY": "val"},
			want:  "prefix val suffix",
		},
		{
			name:  "multiple refs in one string",
			input: "${HOST}:${PORT:-8080}",
			env:   map[string]string{"HOST": "localhost"},
			want:  "localhost:8080",
		},

		// No references
		{
			name:  "no references",
			input: "plain string",
			want:  "plain string",
		},
		{
			name:  "dollar without brace",
			input: "$VAR",
			env:   map[string]string{"VAR": "value"},
			want:  "value", // $VAR supported for backward compatibility
		},

		// Edge cases
		{
			name:  "empty default",
			input: "${VAR:-}",
			want:  "",
		},
		{
			name:  "default with special chars",
			input: "${URL:-http://localhost:8080/api?key=value}",
			want:  "http://localhost:8080/api?key=value",
		},
		{
			name:  "underscore prefix variable",
			input: "${_PRIVATE}",
			env:   map[string]string{"_PRIVATE": "secret"},
			want:  "secret",
		},
		{
			name:  "empty string input",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear env
			for _, key := range []string{"TEST_VAR", "UNDEFINED_VAR", "EMPTY_VAR", "API_URL", "TOKEN", "SECRET_KEY", "MISSING_KEY", "KEY", "HOST", "PORT", "VAR", "URL", "_PRIVATE"} {
				os.Unsetenv(key)
			}

			// Set env
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			// Build resolver
			var resolve Resolver
			if tc.vault != nil {
				resolve = VaultResolver(&mockVault{secrets: tc.vault})
			} else {
				resolve = EnvResolver()
			}

			got, unresolvedVault, emptyVars := ExpandString(tc.input, resolve)

			if got != tc.want {
				t.Errorf("ExpandString() = %q, want %q", got, tc.want)
			}

			if len(unresolvedVault) != len(tc.wantUnresolved) {
				t.Errorf("unresolved vault refs = %v, want %v", unresolvedVault, tc.wantUnresolved)
			}

			if tc.wantEmpty != nil && len(emptyVars) < len(tc.wantEmpty) {
				t.Errorf("empty env vars = %v, want at least %v", emptyVars, tc.wantEmpty)
			}
		})
	}
}

func TestExpandString_NilResolver(t *testing.T) {
	t.Setenv("TEST_NIL", "works")
	got, _, _ := ExpandString("${TEST_NIL}", nil)
	if got != "works" {
		t.Errorf("nil resolver should default to env: got %q", got)
	}
}

func TestLoadStack_VaultExpansion(t *testing.T) {
	vault := &mockVault{secrets: map[string]string{
		"SECRET_TOKEN": "vault-secret-123",
	}}

	content := `
name: test-vault
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      API_TOKEN: "${vault:SECRET_TOKEN}"
`
	path := writeTempFile(t, content)

	stack, err := LoadStack(path, WithVault(vault))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.MCPServers[0].Env["API_TOKEN"] != "vault-secret-123" {
		t.Errorf("vault expansion failed: got %q", stack.MCPServers[0].Env["API_TOKEN"])
	}
}

func TestLoadStack_VaultMissing(t *testing.T) {
	vault := &mockVault{secrets: map[string]string{}}

	content := `
name: test-vault
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      API_TOKEN: "${vault:MISSING_KEY}"
`
	path := writeTempFile(t, content)

	_, err := LoadStack(path, WithVault(vault))
	if err == nil {
		t.Fatal("expected error for missing vault key")
	}
	if !contains(err.Error(), "missing vault secret") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadStack_VaultFallbackToEnv(t *testing.T) {
	vault := &mockVault{secrets: map[string]string{}}
	t.Setenv("FALLBACK_KEY", "env-value")

	content := `
name: test-vault
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      TOKEN: "${vault:FALLBACK_KEY}"
`
	path := writeTempFile(t, content)

	stack, err := LoadStack(path, WithVault(vault))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.MCPServers[0].Env["TOKEN"] != "env-value" {
		t.Errorf("vault fallback to env failed: got %q", stack.MCPServers[0].Env["TOKEN"])
	}
}

func TestLoadStack_BackwardCompatible(t *testing.T) {
	t.Setenv("TEST_COMPAT_KEY", "compat-value")

	content := `
name: test-compat
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      KEY: "${TEST_COMPAT_KEY}"
`
	path := writeTempFile(t, content)

	// No vault option — backward compatible
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.MCPServers[0].Env["KEY"] != "compat-value" {
		t.Errorf("backward compatibility broken: got %q", stack.MCPServers[0].Env["KEY"])
	}
}

func TestLoadStack_POSIXOperators(t *testing.T) {
	content := `
name: test-posix
network:
  name: test-net
mcp-servers:
  - name: server1
    image: alpine:latest
    port: 3000
    env:
      URL: "${API_URL:-http://localhost:8080}"
`
	path := writeTempFile(t, content)

	stack, err := LoadStack(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stack.MCPServers[0].Env["URL"] != "http://localhost:8080" {
		t.Errorf("POSIX default operator failed: got %q", stack.MCPServers[0].Env["URL"])
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
