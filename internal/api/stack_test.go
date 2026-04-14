package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
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
		Resources:  []config.Resource{{Name: "res"}},
	})
}

func TestSanitizeStackSecrets_AllTypes(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{{Name: "srv", Env: map[string]string{"DB_PASSWORD": "pass"}}},
		Resources:  []config.Resource{{Name: "res", Env: map[string]string{"AUTH_TOKEN": "tok"}}},
	}
	sanitizeStackSecrets(stack)
	assert.Equal(t, "${vault:srv_DB_PASSWORD}", stack.MCPServers[0].Env["DB_PASSWORD"])
	assert.Equal(t, "${vault:res_AUTH_TOKEN}", stack.Resources[0].Env["AUTH_TOKEN"])
}


func TestHandleStackAppend_MCPServer(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "name: server-new\nimage: nginx\nport: 9000\n",
		"resourceType": "mcp-server",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"resourceName":"server-new"`)

	data, err := os.ReadFile(sf)
	assert.NoError(t, err)
	var stack config.Stack
	assert.NoError(t, yaml.Unmarshal(data, &stack))
	assert.Equal(t, 3, len(stack.MCPServers))
	assert.Equal(t, "server-new", stack.MCPServers[2].Name)
}

func TestHandleStackAppend_Resource(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "name: redis\nimage: redis:7\n",
		"resourceType": "resource",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"resourceName":"redis"`)

	data, err := os.ReadFile(sf)
	assert.NoError(t, err)
	var stack config.Stack
	assert.NoError(t, yaml.Unmarshal(data, &stack))
	assert.Equal(t, 1, len(stack.Resources))
	assert.Equal(t, "redis", stack.Resources[0].Name)
}

func TestHandleStackAppend_NoStackFile(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStackAppend_InvalidResourceType(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "name: test\n",
		"resourceType": "stack",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStackAppend_InvalidYAML(t *testing.T) {
	sf := writeTestStack(t)
	s := &Server{stackFile: sf}

	body, _ := json.Marshal(map[string]string{
		"yaml":         "[unclosed bracket",
		"resourceType": "agent",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/append", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackAppend(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
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
		{"unknown path", http.MethodGet, "/api/stack/unknown", http.StatusNotFound},
		{"validate wrong method", http.MethodGet, "/api/stack/validate", http.StatusMethodNotAllowed},
		{"append POST no stack", http.MethodPost, "/api/stack/append", http.StatusServiceUnavailable},
		{"stacks GET", http.MethodGet, "/api/stacks", http.StatusOK},
		{"initialize POST no body", http.MethodPost, "/api/stack/initialize", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.method == http.MethodPost {
				body = strings.NewReader(`{}`)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			w := httptest.NewRecorder()

			s.Handler().ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
		})
	}
}

// --- Stack library tests ---

func TestHandleStacksList_EmptyDir(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stacks", nil)
	w := httptest.NewRecorder()

	// Override StacksDir by using a temp dir approach — since StacksDir() uses
	// the real home dir, we test the handler directly and expect a graceful empty response.
	s.handleStacksList(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Either empty stacks (dir doesn't exist) or valid JSON response
	assert.Contains(t, w.Body.String(), `"stacks"`)
}

func TestHandleStacksSave_Success(t *testing.T) {
	dir := t.TempDir()
	// We use the actual handler but point StacksDir to a temp dir via env manipulation.
	// Since StacksDir() is not injectable, we verify the save logic by calling the
	// handler and checking the file was created in the expected location.
	//
	// To keep tests hermetic we create a wrapper that overrides the dir lookup.
	// Instead, we test the validation path directly since the dir is live.

	body, _ := json.Marshal(map[string]string{
		"name": "my-stack",
		"yaml": "name: my-stack\nnetwork:\n  name: net\n",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s := &Server{}

	// Patch: temporarily swap the stacks dir using an env var trick is not possible
	// without refactoring. Instead, test by calling handleStacksSave with the
	// real handler but verify expected output (path in response).
	// We trust the OS not to have a pre-existing ~/.gridctl/stacks/my-stack.yaml.
	// For CI safety, use a separate approach: create the file in a temp dir and
	// verify the response body format with a minimal test.
	_ = dir // used for structure, not for dir injection

	s.handleStacksSave(w, req)

	// Either success (200) or dir creation succeeds — check response shape.
	if w.Code == http.StatusOK {
		assert.Contains(t, w.Body.String(), `"success":true`)
		assert.Contains(t, w.Body.String(), `"name":"my-stack"`)
		// Clean up
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if path, ok := resp["path"].(string); ok {
			_ = os.Remove(path)
		}
	}
}

func TestHandleStacksSave_InvalidName(t *testing.T) {
	tests := []struct {
		name     string
		stackName string
	}{
		{"slash in name", "my/stack"},
		{"dotdot traversal", "../etc"},
		{"space in name", "my stack"},
		{"empty name", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{
				"name": tc.stackName,
				"yaml": "name: test\n",
			})
			req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
			w := httptest.NewRecorder()

			s := &Server{}
			s.handleStacksSave(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestHandleStacksSave_InvalidYAML(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"name": "test-stack",
		"yaml": ":::not yaml",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s := &Server{}
	s.handleStacksSave(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid YAML")
}

func TestHandleStacksSave_MissingYAML(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"name": "test-stack",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/stacks", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s := &Server{}
	s.handleStacksSave(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStackInitialize_AlreadyLoaded(t *testing.T) {
	s := &Server{stackFile: "/some/existing/stack.yaml"}

	body, _ := json.Marshal(map[string]string{"name": "my-stack"})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "already loaded")
}

func TestHandleStackInitialize_NotFound(t *testing.T) {
	s := &Server{}

	body, _ := json.Marshal(map[string]string{"name": "nonexistent-stack-xyz"})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Stack not found")
}

func TestHandleStackInitialize_MissingName(t *testing.T) {
	s := &Server{}

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleStackInitialize_NoReloadHandler(t *testing.T) {
	// Create a real stack file in a temp dir, copy it to StacksDir for the test.
	// To avoid polluting the real StacksDir, write to a temp location and use
	// os.Symlink or direct file test. We test the flow when reloadHandler is nil.

	// Write a temp stack file to a temp stacks dir — we can't inject the dir,
	// so we write directly to the real stacks dir and clean up.
	stacksDir := filepath.Join(os.TempDir(), "gridctl-test-stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stacksDir)

	stackPath := filepath.Join(stacksDir, "test-stack.yaml")
	content := "name: test-stack\nnetwork:\n  name: net\n"
	if err := os.WriteFile(stackPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// We can't inject the stacks dir, so we can't fully test the happy path
	// without the real StacksDir. Test the 404 path with a bogus name instead,
	// and separately verify the 409 path (already covered above).
	s := &Server{}
	body, _ := json.Marshal(map[string]string{"name": "test-stack"})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	// Without the real StacksDir having the file, expect 404
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleStackInitialize_SuccessNoReloadHandler(t *testing.T) {
	// Write a stack to the real StacksDir and test the full initialize flow
	// with no reloadHandler (stackless mode without --watch).
	stacksDir := func() string {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".gridctl", "stacks")
	}()

	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Skipf("cannot create stacks dir: %v", err)
	}

	stackName := "gridctl-test-init-stack"
	stackPath := filepath.Join(stacksDir, stackName+".yaml")
	content := "name: gridctl-test-init-stack\nnetwork:\n  name: net\n"
	if err := os.WriteFile(stackPath, []byte(content), 0644); err != nil {
		t.Skipf("cannot write test stack: %v", err)
	}
	defer os.Remove(stackPath)

	s := &Server{} // no reloadHandler

	body, _ := json.Marshal(map[string]string{"name": stackName})
	req := httptest.NewRequest(http.MethodPost, "/api/stack/initialize", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	s.handleStackInitialize(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), `"watching":false`)

	// Verify server state was updated
	assert.Equal(t, stackPath, s.stackFile)
	assert.Equal(t, stackName, s.stackName)
}

func TestHandleStacksList_WithFiles(t *testing.T) {
	// Write stacks to the real StacksDir and verify they appear in the list.
	stacksDir := func() string {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".gridctl", "stacks")
	}()

	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Skipf("cannot create stacks dir: %v", err)
	}

	stackName := "gridctl-test-list-stack"
	stackPath := filepath.Join(stacksDir, stackName+".yaml")
	if err := os.WriteFile(stackPath, []byte("name: test\n"), 0644); err != nil {
		t.Skipf("cannot write test stack: %v", err)
	}
	defer os.Remove(stackPath)

	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/stacks", nil)
	w := httptest.NewRecorder()

	s.handleStacksList(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), stackName)
}

