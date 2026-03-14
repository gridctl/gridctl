package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/assert"
)

// writeTestStack creates a temporary stack.yaml and returns its path.
func writeTestStack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "stack.yaml")
	content := `name: test-stack
network:
  name: test-net
mcp-servers:
  - name: server-a
    image: alpine
    port: 3000
    env:
      API_KEY: "${vault:MY_KEY}"
      DB_PASSWORD: secret123
      HOST: localhost
  - name: server-b
    image: nginx
    port: 3001
    env:
      AUTH_TOKEN: "${vault:AUTH_TOK}"
agents:
  - name: agent-1
    runtime: claude-code
    prompt: test
    uses:
      - server: server-a
`
	err := os.WriteFile(p, []byte(content), 0644)
	assert.NoError(t, err)
	return p
}

func TestHandleStackValidate_ValidYAML(t *testing.T) {
	s := &Server{}
	body := `
name: test
network:
  name: test-net
mcp-servers:
  - name: s1
    image: alpine
    port: 3000
`
	req := httptest.NewRequest(http.MethodPost, "/api/stack/validate", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleStackValidate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":true`)
}

func TestHandleStackValidate_InvalidYAML(t *testing.T) {
	s := &Server{}
	body := `:::not yaml`
	req := httptest.NewRequest(http.MethodPost, "/api/stack/validate", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleStackValidate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":false`)
	assert.Contains(t, w.Body.String(), `"severity":"error"`)
}

func TestHandleStackValidate_InvalidStack(t *testing.T) {
	s := &Server{}
	body := `
mcp-servers:
  - name: s1
`
	req := httptest.NewRequest(http.MethodPost, "/api/stack/validate", strings.NewReader(body))
	w := httptest.NewRecorder()

	s.handleStackValidate(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"valid":false`)
	assert.Contains(t, w.Body.String(), `"errorCount"`)
}

func TestHandleStackSpec_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/spec", nil)
	w := httptest.NewRecorder()

	s.handleStackSpec(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackPlan_NoStackDeployed(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/plan", nil)
	w := httptest.NewRecorder()

	s.handleStackPlan(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackHealth_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/health", nil)
	w := httptest.NewRecorder()

	s.handleStackHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"unknown"`)
}

func TestHandleStackExport_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/export", nil)
	w := httptest.NewRecorder()

	s.handleStackExport(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackSecretsMap_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/secrets-map", nil)
	w := httptest.NewRecorder()

	s.handleStackSecretsMap(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"secrets"`)
	assert.Contains(t, w.Body.String(), `"nodes"`)
}

func TestHandleStackRecipes(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/recipes", nil)
	w := httptest.NewRecorder()

	s.handleStackRecipes(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"rag-pipeline"`)
	assert.Contains(t, w.Body.String(), `"dev-toolbox"`)
}

func TestSanitizeStackSecrets(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{"already vault ref", "API_KEY", "${vault:MY_KEY}", "${vault:MY_KEY}"},
		{"sensitive password", "DB_PASSWORD", "secret123", "${vault:test_DB_PASSWORD}"},
		{"sensitive token", "AUTH_TOKEN", "tok_abc", "${vault:test_AUTH_TOKEN}"},
		{"non-sensitive", "HOST", "localhost", "localhost"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{tc.key: tc.value}
			sanitizeStackSecrets(&config.Stack{
				MCPServers: []config.MCPServer{{Name: "test", Env: env}},
			})
			assert.Equal(t, tc.expected, env[tc.key])
		})
	}
}

func TestAppendUnique(t *testing.T) {
	result := appendUnique([]string{"a", "b"}, "c")
	assert.Equal(t, []string{"a", "b", "c"}, result)

	result = appendUnique([]string{"a", "b"}, "a")
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestHandleStackSpec_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/spec", nil)
	w := httptest.NewRecorder()

	s.handleStackSpec(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test-stack")
	assert.Contains(t, w.Body.String(), "server-a")
}

func TestHandleStackExport_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/export", nil)
	w := httptest.NewRecorder()

	s.handleStackExport(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "content")
	// Secrets should be sanitized — DB_PASSWORD should be vault ref
	assert.Contains(t, body, "${vault:")
	assert.NotContains(t, body, "secret123")
}

func TestHandleStackSecretsMap_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/secrets-map", nil)
	w := httptest.NewRecorder()

	s.handleStackSecretsMap(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "secrets")
	assert.Contains(t, body, "nodes")
	// vault refs should appear as secret keys
	assert.Contains(t, body, "MY_KEY")
}

func TestHandleStackHealth_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf, stackName: "test-stack"}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/health", nil)
	w := httptest.NewRecorder()

	s.handleStackHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// Should have validation status
	assert.Contains(t, body, `"status"`)
}

func TestHandleStackPlan_WithStackFile(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf, stackName: "test-stack"}
	req := httptest.NewRequest(http.MethodGet, "/api/stack/plan", nil)
	w := httptest.NewRecorder()

	s.handleStackPlan(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "hasChanges")
}

func TestSanitizeStackSecrets_NilEnv(t *testing.T) {
	// Should not panic with nil env maps
	sanitizeStackSecrets(&config.Stack{
		MCPServers: []config.MCPServer{{Name: "test"}},
		Agents:     []config.Agent{{Name: "agent"}},
		Resources:  []config.Resource{{Name: "res"}},
	})
}

func TestSanitizeStackSecrets_AllTypes(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{{Name: "srv", Env: map[string]string{"DB_PASSWORD": "pass"}}},
		Agents:     []config.Agent{{Name: "agt", Env: map[string]string{"API_SECRET": "s3cr3t"}}},
		Resources:  []config.Resource{{Name: "res", Env: map[string]string{"AUTH_TOKEN": "tok"}}},
	}
	sanitizeStackSecrets(stack)
	assert.Equal(t, "${vault:srv_DB_PASSWORD}", stack.MCPServers[0].Env["DB_PASSWORD"])
	assert.Equal(t, "${vault:agt_API_SECRET}", stack.Agents[0].Env["API_SECRET"])
	assert.Equal(t, "${vault:res_AUTH_TOKEN}", stack.Resources[0].Env["AUTH_TOKEN"])
}

func TestHandleStack_Routing(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"validate POST", http.MethodPost, "/api/stack/validate", http.StatusOK},
		{"plan GET no stack", http.MethodGet, "/api/stack/plan", http.StatusServiceUnavailable},
		{"health GET", http.MethodGet, "/api/stack/health", http.StatusOK},
		{"spec GET no stack", http.MethodGet, "/api/stack/spec", http.StatusServiceUnavailable},
		{"export GET no stack", http.MethodGet, "/api/stack/export", http.StatusServiceUnavailable},
		{"secrets-map GET", http.MethodGet, "/api/stack/secrets-map", http.StatusOK},
		{"recipes GET", http.MethodGet, "/api/stack/recipes", http.StatusOK},
		{"unknown path", http.MethodGet, "/api/stack/unknown", http.StatusMethodNotAllowed},
		{"validate wrong method", http.MethodGet, "/api/stack/validate", http.StatusMethodNotAllowed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.method == http.MethodPost {
				body = strings.NewReader(`name: test`)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			w := httptest.NewRecorder()

			s.handleStack(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
		})
	}
}
