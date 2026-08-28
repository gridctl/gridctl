package config

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/vault"
)

// mockVault implements VaultLookup for testing.
type mockVault struct {
	secrets map[string]string
}

func TestResolveVariable_Verdicts(t *testing.T) {
	store := &mockVault{secrets: map[string]string{"STORE_ONLY": "stored", "BOTH": "stored"}}
	t.Setenv("BOTH", "ambient")
	t.Setenv("ENV_ONLY", "ambient")
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "must-not-resolve")

	tests := []struct {
		key         string
		wantValue   string
		wantVerdict ResolutionVerdict
		wantDenied  bool
	}{
		{key: "STORE_ONLY", wantValue: "stored", wantVerdict: ResolutionStore},
		{key: "BOTH", wantValue: "stored", wantVerdict: ResolutionStore},
		{key: "ENV_ONLY", wantValue: "ambient", wantVerdict: ResolutionEnvFallback},
		{key: "MISSING", wantVerdict: ResolutionUnset},
		{key: "GRIDCTL_VAULT_PASSPHRASE", wantVerdict: ResolutionDenied, wantDenied: true},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			got := ResolveVariable(store, tc.key)
			if got.Value != tc.wantValue || got.Verdict != tc.wantVerdict {
				t.Fatalf("ResolveVariable() = %+v, want value %q verdict %q", got, tc.wantValue, tc.wantVerdict)
			}
			var denied *vault.InternalCredentialError
			if errors.As(got.Error, &denied) != tc.wantDenied {
				t.Fatalf("denied error = %v, want %v", got.Error, tc.wantDenied)
			}
		})
	}
}

func TestExpandStringResolved_ReferenceAwarePolicy(t *testing.T) {
	t.Setenv("GRIDCTL_ORDINARY_SETTING", "allowed")
	t.Setenv("GRIDCTL_PRIVATE", "blocked")
	store := &mockVault{secrets: map[string]string{}}

	expanded, _, _, problems := ExpandStringResolved("${GRIDCTL_ORDINARY_SETTING}", store)
	if expanded != "allowed" || len(problems) != 0 {
		t.Fatalf("ordinary expansion = %q, problems=%v", expanded, problems)
	}
	expanded, _, _, problems = ExpandStringResolved("${var:GRIDCTL_PRIVATE}", store)
	if expanded != "${var:GRIDCTL_PRIVATE}" || len(problems) != 1 {
		t.Fatalf("store expansion = %q, problems=%v", expanded, problems)
	}
}

func TestVaultResolver_PreservesOrdinaryGridctlEnvironment(t *testing.T) {
	t.Setenv("GRIDCTL_ORDINARY_SETTING", "allowed")
	got, _, _ := ExpandString("${GRIDCTL_ORDINARY_SETTING}", VaultResolver(&mockVault{secrets: map[string]string{}}))
	if got != "allowed" {
		t.Fatalf("ExpandString() = %q, want allowed", got)
	}
}

func TestExpandStackVarsWithEnvChecked_Denied(t *testing.T) {
	t.Setenv("OP_CONNECT_TOKEN", "blocked")
	stack := &Stack{MCPServers: []MCPServer{{Env: map[string]string{"INNOCENT": "${var:OP_CONNECT_TOKEN}"}}}}
	err := ExpandStackVarsWithEnvChecked(stack)
	var denied *vault.InternalCredentialError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %v, want typed denial", err)
	}
	if got := stack.MCPServers[0].Env["INNOCENT"]; got != "${var:OP_CONNECT_TOKEN}" {
		t.Fatalf("denied reference = %q, want literal", got)
	}
}

func TestLoadStack_InternalCredentialsStayLiteralAndError(t *testing.T) {
	t.Setenv("GRIDCTL_VAULT_PASSPHRASE", "ambient-secret")
	content := `
name: denied
mcp-servers:
  - name: local
    command: ["helper"]
    env:
      CONTROL: "${var:GRIDCTL_VAULT_PASSPHRASE}"
`
	path := writeTempFile(t, content)
	_, err := LoadStack(path, WithVault(&mockVault{secrets: map[string]string{}}))
	var denied *vault.InternalCredentialError
	if !errors.As(err, &denied) || denied.Key != "GRIDCTL_VAULT_PASSPHRASE" {
		t.Fatalf("LoadStack() error = %v, want typed denied resolution", err)
	}
}

func TestLoadStack_InternalEnvironmentPolicy(t *testing.T) {
	t.Setenv("GRIDCTL_ORDINARY_SETTING", "allowed")
	t.Setenv("OP_CONNECT_TOKEN", "denied")

	allowed := `
name: allowed
mcp-servers:
  - name: local
    command: ["helper"]
    env:
      SETTING: "${GRIDCTL_ORDINARY_SETTING}"
`
	stack, err := LoadStack(writeTempFile(t, allowed))
	if err != nil {
		t.Fatalf("ordinary GRIDCTL_ interpolation failed: %v", err)
	}
	if got := stack.MCPServers[0].Env["SETTING"]; got != "allowed" {
		t.Fatalf("SETTING = %q, want allowed", got)
	}

	deniedContent := `
name: denied
mcp-servers:
  - name: local
    command: ["helper"]
    env:
      TOKEN: "$OP_CONNECT_TOKEN"
`
	_, err = LoadStack(writeTempFile(t, deniedContent))
	var denied *vault.InternalCredentialError
	if !errors.As(err, &denied) {
		t.Fatalf("exact credential interpolation error = %v, want typed denial", err)
	}
}

func TestValidateStackFile_DeniesInternalCredentialReferences(t *testing.T) {
	t.Setenv("OP_CONNECT_TOKEN", "must-not-export")
	content := `
name: denied
mcp-servers:
  - name: local
    command: ["helper"]
    env:
      INNOCENT_NAME: "${var:OP_CONNECT_TOKEN}"
`
	_, _, err := ValidateStackFile(writeTempFile(t, content))
	var denied *vault.InternalCredentialError
	if !errors.As(err, &denied) || denied.Key != "OP_CONNECT_TOKEN" {
		t.Fatalf("ValidateStackFile() error = %v, want typed denial", err)
	}
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

		// ${var:KEY} is the canonical syntax; resolves identically to vault.
		{
			name:  "var hit",
			input: "${var:REGION}",
			vault: map[string]string{"REGION": "us-east-1"},
			want:  "us-east-1",
		},
		{
			name:           "var miss",
			input:          "${var:MISSING}",
			want:           "${var:MISSING}",
			wantUnresolved: []string{"MISSING"},
		},
		{
			name:  "mixed var and vault refs resolve through same store",
			input: "${var:A}-${vault:B}",
			vault: map[string]string{"A": "1", "B": "2"},
			want:  "1-2",
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
	if !contains(err.Error(), "missing variable") {
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

// TestVaultSyntaxNoDeprecationWarning verifies that ${vault:KEY} still resolves
// correctly and no longer emits a deprecation warning. The alias is retained for
// back-compat; the prior once-per-process warning was removed because it was
// noisy on every stack stand-up.
func TestVaultSyntaxNoDeprecationWarning(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	vault := &mockVault{secrets: map[string]string{"A": "1", "B": "2"}}

	out, _, _ := ExpandString("${vault:A}-${vault:B}-${vault:A}", VaultResolver(vault))
	if out != "1-2-1" {
		t.Errorf("expansion = %q, want %q", out, "1-2-1")
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("${vault:KEY} emitted a deprecation warning; output: %q", buf.String())
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
